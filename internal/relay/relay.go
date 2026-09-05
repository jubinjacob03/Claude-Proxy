// Package relay is the server side of the licensed proxy.
//
// It is the only component that holds upstream API keys. Clients authenticate
// with an activation token bound to their machine, and every forwarded request
// is charged against that licence before it leaves the building. A client that
// is tampered with cannot obtain a key, cannot change its own balance, and
// cannot reach the upstream directly.
package relay

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"claude-proxy/internal/license"
	"claude-proxy/internal/logx"
)

type Config struct {
	Addr           string
	DataDir        string
	TokenSecret    string
	AdminToken     string
	ClaudeBaseURL  string
	ClaudeFormat   string
	DefaultQuota   license.Money
	RequestTimeout time.Duration
}

type Server struct {
	cfg     Config
	store   *license.Store
	pricing *license.Pricing
	client  *http.Client
	secret  []byte
}

func New(cfg Config, store *license.Store) *Server {
	// Go's default transport keeps only 2 idle connections per host, so bursts
	// would pay a fresh TLS handshake to the upstream on most requests.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 200
	transport.MaxIdleConnsPerHost = 100
	transport.IdleConnTimeout = 90 * time.Second
	transport.ForceAttemptHTTP2 = true

	return &Server{
		cfg:     cfg,
		store:   store,
		pricing: license.DefaultPricing(),
		client:  &http.Client{Transport: transport},
		secret:  []byte(cfg.TokenSecret),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/activate", s.handleActivate)
	mux.HandleFunc("/v1/messages", s.handleProxy)
	mux.HandleFunc("/v1/messages/count_tokens", s.handleFree)
	mux.HandleFunc("/v1/chat/completions", s.handleProxy)
	mux.HandleFunc("/v1/models", s.handleFree)
	mux.HandleFunc("/health", s.handleHealth)

	mux.HandleFunc("/admin/licenses", s.handleAdminLicenses)
	mux.HandleFunc("/admin/licenses/", s.handleAdminLicense)
	mux.HandleFunc("/admin/pool", s.handleAdminPool)
	mux.HandleFunc("/admin/pool/", s.handleAdminPoolItem)
	mux.HandleFunc("/admin/endpoints", s.handleAdminEndpoints)
	mux.HandleFunc("/admin/endpoints/", s.handleAdminEndpoint)
	mux.HandleFunc("/admin/usage", s.handleAdminUsage)

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleActivate exchanges a licence key plus a machine id for a token. The
// binding is permanent: the first machine to activate keeps the licence.
func (s *Server) handleActivate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Key  string `json:"key"`
		HWID string `json:"hwid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	l, err := s.store.Activate(body.Key, strings.TrimSpace(body.HWID))
	if err != nil {
		status, msg := activationFailure(err)
		logx.Warn("activation refused (%s): %v", clientIP(r), err)
		writeError(w, status, msg)
		return
	}

	logx.Info("licence %s activated on %s", l.ID, short(l.HWID))
	// The balance is deliberately not returned; the client has no business
	// knowing it and must not be able to display or act on it.
	writeJSON(w, http.StatusOK, map[string]any{
		"status": license.StatusOK,
		"token":  license.Sign(s.secret, l.ID, l.HWID),
	})
}

func activationFailure(err error) (int, string) {
	switch {
	case errors.Is(err, license.ErrNotFound):
		return http.StatusUnauthorized, "That licence key was not recognised."
	case errors.Is(err, license.ErrInactive):
		return http.StatusForbidden, "That licence has been disabled."
	case errors.Is(err, license.ErrHWIDMismatch):
		return http.StatusForbidden, "That licence is already in use on another machine."
	case errors.Is(err, license.ErrHWIDMissing):
		return http.StatusBadRequest, "A machine id is required to activate."
	default:
		return http.StatusInternalServerError, "Activation failed."
	}
}

// authenticate resolves the caller's licence from its token. The machine id is
// taken from the signed token itself, so a copied token cannot be replayed with
// a different machine claim.
func (s *Server) authenticate(r *http.Request) (*license.License, error) {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(raw) > 7 && strings.EqualFold(raw[:7], "Bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	if raw == "" {
		return nil, license.ErrBadToken
	}
	tok, err := license.Verify(s.secret, raw)
	if err != nil {
		return nil, err
	}
	return s.store.Authorize(tok.LicenseID, tok.HWID)
}

// handleFree serves endpoints that cost nothing upstream but still require a
// valid licence, so an unlicensed client cannot even enumerate models.
func (s *Server) handleFree(w http.ResponseWriter, r *http.Request) {
	l, err := s.authenticate(r)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	profile, poolGroup := s.resolveEndpoint()
	key, err := s.store.TakePoolKeyForGroup(license.ProviderClaude, poolGroup)
	if err != nil {
		logx.Error("licence %s: %v", l.ID, err)
		writeUpstreamError(w, r, http.StatusServiceUnavailable, "No upstream capacity is available.")
		return
	}
	s.forward(w, r, profile.ClaudeBaseURL, key, body, false)
}

// handleProxy is the metered path: authenticate, price, debit, forward.
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeUpstreamError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	l, err := s.authenticate(r)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeUpstreamError(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}

	model, streamed := inspectRequest(body)
	provider := license.ProviderClaude
	profile, poolGroup := s.resolveEndpoint()

	// Determine billing mode and pre-charge cost.
	// For token-based endpoints we pre-charge a conservative estimate and
	// then reconcile after we see the actual token counts.
	var preChargeCost license.Money
	billingMode := profile.BillingMode
	if billingMode == "" {
		billingMode = license.BillingModePerRequest
	}
	switch billingMode {
	case license.BillingModeTokenBased:
		// Pre-charge a safe maximum (500 cents = $5) to guarantee the licence
		// has capacity; we will Release the difference after the real cost is known.
		preChargeCost = 500
	default:
		preChargeCost = profile.PerRequestCostCents
		if preChargeCost <= 0 {
			preChargeCost = s.pricing.Cost(model)
		}
	}

	poolKey, err := s.store.TakePoolKeyForGroup(provider, poolGroup)
	if err != nil {
		logx.Error("licence %s: %v", l.ID, err)
		writeUpstreamError(w, r, http.StatusServiceUnavailable, "No upstream capacity is available.")
		return
	}

	// Debit before forwarding. Charging first is what stops two parallel
	// requests from spending the same last cent.
	reservation, err := s.store.Reserve(l.ID, l.HWID, preChargeCost, provider, model, poolKey.ID)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}

	result := s.forward(w, r, profile.ClaudeBaseURL, poolKey, body, streamed)

	// Bill only what the upstream actually served.
	if !servedSuccessfully(result.StatusCode) {
		if rerr := s.store.Release(reservation); rerr != nil {
			logx.Error("refund failed for licence %s: %v", l.ID, rerr)
		}
		return
	}

	// For token-based billing, calculate the real cost and refund the difference.
	actualCost := preChargeCost
	if billingMode == license.BillingModeTokenBased && (result.InputTokens > 0 || result.OutputTokens > 0) {
		actualCost = license.TokenCost(result.InputTokens, result.OutputTokens, profile.InputCostPerM, profile.OutputCostPerM)
		if actualCost < preChargeCost {
			refund := preChargeCost - actualCost
			refundReservation := &license.Reservation{
				EventID:   "",
				LicenseID: reservation.LicenseID,
				PoolKeyID: reservation.PoolKeyID,
				Cost:      refund,
			}
			if rerr := s.store.Release(refundReservation); rerr != nil {
				logx.Error("token-based refund failed for licence %s: %v", l.ID, rerr)
			}
			reservation.Cost = actualCost
		}
	}

	if err := s.store.Commit(reservation, provider, model, poolKey.ID, result.StatusCode, streamed, result.InputTokens, result.OutputTokens); err != nil {
		logx.Error("usage commit failed for licence %s: %v", l.ID, err)
	}
}

func servedSuccessfully(status int) bool {
	return status >= 200 && status < 300
}

func (s *Server) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, license.ErrExhausted):
		writeUpstreamError(w, r, http.StatusPaymentRequired,
			"Usage quota exhausted. Contact your provider to top up this licence.")
	case errors.Is(err, license.ErrInactive):
		writeUpstreamError(w, r, http.StatusForbidden, "This licence has been disabled.")
	case errors.Is(err, license.ErrHWIDMismatch):
		writeUpstreamError(w, r, http.StatusForbidden, "This licence is bound to another machine.")
	default:
		writeUpstreamError(w, r, http.StatusUnauthorized, "This installation is not licensed.")
	}
}

func (s *Server) upstreamFor(provider string) string {
	return strings.TrimRight(s.cfg.ClaudeBaseURL, "/")
}

func (s *Server) resolveEndpoint() (*license.EndpointProfile, string) {
	if profile, err := s.store.ActiveEndpointProfile(); err == nil {
		base := strings.TrimRight(strings.TrimSpace(profile.ClaudeBaseURL), "/")
		poolGroup := strings.TrimSpace(profile.PoolGroup)
		if base != "" {
			if poolGroup == "" {
				poolGroup = "default"
			}
			profile.ClaudeBaseURL = base
			profile.PoolGroup = poolGroup
			return profile, poolGroup
		}
	}
	// Fallback: synthesize a profile with defaults from config.
	return &license.EndpointProfile{
		ClaudeBaseURL:       strings.TrimRight(s.cfg.ClaudeBaseURL, "/"),
		PoolGroup:           "default",
		BillingMode:         license.BillingModePerRequest,
		PerRequestCostCents: license.DefaultPerRequestCents,
	}, "default"
}

// inspectRequest pulls the billing-relevant fields out of a request body
// without disturbing it; the body is forwarded byte for byte.
func inspectRequest(body []byte) (model string, streamed bool) {
	var probe struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	_ = json.Unmarshal(body, &probe)
	return probe.Model, probe.Stream
}

func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); v != "" {
		if first, _, ok := strings.Cut(v, ","); ok {
			return strings.TrimSpace(first)
		}
		return v
	}
	return r.RemoteAddr
}

func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12] + "..."
}
