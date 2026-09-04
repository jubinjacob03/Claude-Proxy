package relay

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"claude-proxy/internal/license"
)

// The admin surface is what the licensing website drives. It is guarded by a
// single shared token rather than user accounts: the website is the only client
// and it holds the token server-side, never in a browser.
func (s *Server) adminAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.AdminToken == "" {
		writeError(w, http.StatusServiceUnavailable, "admin API is disabled: no admin token configured")
		return false
	}
	got := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.AdminToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid admin token")
		return false
	}
	return true
}

func (s *Server) handleAdminLicenses(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"licenses": viewLicenses(s.store.List())})
	case http.MethodPost:
		var body struct {
			QuotaCents int64  `json:"quota_cents"`
			Note       string `json:"note"`
			Count      int    `json:"count"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)

		quota := license.Money(body.QuotaCents)
		if quota <= 0 {
			quota = s.cfg.DefaultQuota
		}
		count := body.Count
		if count <= 0 {
			count = 1
		}
		if count > 500 {
			writeError(w, http.StatusBadRequest, "cannot mint more than 500 licences at once")
			return
		}

		created := make([]map[string]any, 0, count)
		for i := 0; i < count; i++ {
			l, err := s.store.CreateLicense(quota, body.Note)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			// The key itself is only ever shown here, at creation time.
			created = append(created, map[string]any{"id": l.ID, "key": l.Key, "quota_cents": int64(l.QuotaCents)})
		}
		writeJSON(w, http.StatusOK, map[string]any{"created": created})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleAdminLicense serves /admin/licenses/{id}/{action}.
func (s *Server) handleAdminLicense(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/licenses/"), "/")
	id, action, _ := strings.Cut(rest, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "licence id is required")
		return
	}

	switch action {
	case "":
		l, err := s.store.Get(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "licence not found")
			return
		}
		writeJSON(w, http.StatusOK, viewLicense(l))
	case "pause":
		s.mutate(w, s.store.SetActive(id, false))
	case "resume":
		s.mutate(w, s.store.SetActive(id, true))
	case "reset-hwid":
		s.mutate(w, s.store.ResetHWID(id))
	case "quota":
		var body struct {
			QuotaCents int64 `json:"quota_cents"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		s.mutate(w, s.store.SetQuota(id, license.Money(body.QuotaCents)))
	case "delete":
		s.mutate(w, s.store.DeleteLicense(id))
	default:
		writeError(w, http.StatusNotFound, "unknown action")
	}
}

func (s *Server) handleAdminPool(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		poolGroup := strings.TrimSpace(r.URL.Query().Get("pool_group"))
		writeJSON(w, http.StatusOK, map[string]any{"pool": viewPool(s.store.ListPoolKeysByGroup(poolGroup))})
	case http.MethodPost:
		var body struct {
			Label        string `json:"label"`
			Secret       string `json:"secret"`
			Provider     string `json:"provider"`
			PoolGroup    string `json:"pool_group"`
			BalanceCents int64  `json:"balance_cents"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.Provider == "" {
			body.Provider = license.ProviderClaude
		}
		k, err := s.store.AddPoolKeyInGroup(body.Label, body.Secret, body.Provider, body.PoolGroup, license.Money(body.BalanceCents))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": k.ID, "label": k.Label, "provider": k.Provider,
			"pool_group": k.PoolGroup, "balance_cents": int64(k.BalanceCents),
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleAdminPoolItem serves /admin/pool/{id}/{action}.
func (s *Server) handleAdminPoolItem(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/pool/"), "/")
	id, action, _ := strings.Cut(rest, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "pool key id is required")
		return
	}
	switch action {
	case "":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		k, err := s.store.GetPoolKey(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": k.ID, "label": k.Label, "provider": k.Provider,
			"pool_group": k.PoolGroup,
			"balance_cents": int64(k.BalanceCents),
			"spent_cents": int64(k.SpentCents),
			"remaining_cents": int64(k.Remaining()),
			"active": k.Active,
			"created_at": k.CreatedAt,
			"last_used": k.LastUsed,
			"exhausted_at": k.ExhaustedAt,
		})
	case "disable":
		s.mutate(w, s.store.SetPoolKeyActive(id, false))
	case "enable":
		s.mutate(w, s.store.SetPoolKeyActive(id, true))
	case "topup":
		var body struct {
			BalanceCents int64 `json:"balance_cents"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		s.mutate(w, s.store.TopUpPoolKey(id, license.Money(body.BalanceCents)))
	case "rotate":
		var body struct {
			Label        string `json:"label"`
			Secret       string `json:"secret"`
			BalanceCents int64  `json:"balance_cents"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		k, err := s.store.RotatePoolKey(id, body.Label, body.Secret, license.Money(body.BalanceCents))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": k.ID, "label": k.Label, "provider": k.Provider,
			"pool_group": k.PoolGroup, "balance_cents": int64(k.BalanceCents),
		})
	case "delete":
		s.mutate(w, s.store.RemovePoolKey(id))
	default:
		writeError(w, http.StatusNotFound, "unknown action")
	}
}

func (s *Server) handleAdminEndpoints(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"profiles": viewEndpointProfiles(s.store.ListEndpointProfiles())})
	case http.MethodPost:
		var body struct {
			Name          string `json:"name"`
			ClaudeBaseURL string `json:"claude_base_url"`
			PoolGroup     string `json:"pool_group"`
			Active        bool   `json:"active"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		profile, err := s.store.SaveEndpointProfile(body.Name, body.ClaudeBaseURL, body.PoolGroup, body.Active)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, viewEndpointProfile(profile))
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAdminEndpoint(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/endpoints/"), "/")
	name, action, _ := strings.Cut(rest, "/")
	if name == "" {
		writeError(w, http.StatusBadRequest, "endpoint profile name is required")
		return
	}
	switch action {
	case "":
		if r.Method == http.MethodGet {
			profile, err := s.store.GetEndpointProfile(name)
			if err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, viewEndpointProfile(profile))
			return
		}
		if r.Method == http.MethodDelete {
			s.mutate(w, s.store.DeleteEndpointProfile(name))
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	case "activate":
		s.mutate(w, s.store.SetActiveEndpointProfile(name))
	case "delete":
		s.mutate(w, s.store.DeleteEndpointProfile(name))
	default:
		writeError(w, http.StatusNotFound, "unknown action")
	}
}

// handleAdminUsage returns recorded requests so the site can show per-licence
// spend history.
func (s *Server) handleAdminUsage(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(w, r) {
		return
	}
	wantLicense := strings.TrimSpace(r.URL.Query().Get("license_id"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))

	events := []map[string]any{}
	for _, e := range s.store.UsageFiltered(wantLicense, query, status, 500) {
		events = append(events, map[string]any{
			"id":          e.ID,
			"license_id":  e.LicenseID,
			"pool_key_id": e.PoolKeyID,
			"provider":    e.Provider,
			"model":       e.Model,
			"cost_cents":  int64(e.CostCents),
			"streamed":    e.Streamed,
			"status_code": e.StatusCode,
			"created_at":  e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) mutate(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func viewLicenses(list []*license.License) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, l := range list {
		out = append(out, viewLicense(l))
	}
	return out
}

// viewLicense never returns the key itself. Once a licence is created the key
// is the customer's to keep; the panel shows only a prefix for identification.
func viewLicense(l *license.License) map[string]any {
	return map[string]any{
		"id":              l.ID,
		"key_hint":        l.KeyHint,
		"hwid":            short(l.HWID),
		"bound":           l.Bound(),
		"quota_cents":     int64(l.QuotaCents),
		"spent_cents":     int64(l.SpentCents),
		"remaining_cents": int64(l.Remaining()),
		"active":          l.Active,
		"note":            l.Note,
		"created_at":      l.CreatedAt,
		"bound_at":        l.BoundAt,
		"last_seen_at":    l.LastSeenAt,
	}
}

func viewPool(list []*license.PoolKey) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, k := range list {
		out = append(out, map[string]any{
			"id":              k.ID,
			"label":           k.Label,
			"provider":        k.Provider,
			"pool_group":      k.PoolGroup,
			"balance_cents":   int64(k.BalanceCents),
			"spent_cents":     int64(k.SpentCents),
			"remaining_cents": int64(k.Remaining()),
			"active":          k.Active,
			"created_at":      k.CreatedAt,
			"last_used":       k.LastUsed,
			"exhausted_at":    k.ExhaustedAt,
		})
	}
	return out
}

func viewEndpointProfiles(list []*license.EndpointProfile) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, viewEndpointProfile(p))
	}
	return out
}

func viewEndpointProfile(p *license.EndpointProfile) map[string]any {
	return map[string]any{
		"name":            p.Name,
		"claude_base_url": p.ClaudeBaseURL,
		"pool_group":      p.PoolGroup,
		"active":          p.Active,
		"created_at":      p.CreatedAt,
	}
}
