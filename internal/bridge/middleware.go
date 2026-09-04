package bridge

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// withMiddleware wraps the mux with CORS, client auth, request logging, and
// per-client request accounting.
func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originalPath := r.URL.Path
		if canonicalPath, changed := canonicalEndpointPath(originalPath); changed {
			r.URL.Path = canonicalPath
		}

		// Reflect CORS only for loopback origins. Echoing an arbitrary Origin
		// would let any website you visit drive this proxy from your browser --
		// including repointing the upstream and stealing the API key.
		if origin := r.Header.Get("Origin"); origin != "" && isLoopbackOrigin(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, x-api-key, anthropic-version, anthropic-beta")
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Max-Age", "86400")
			h.Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		access := newAccessRecord(r.Method, r.URL.Path, clientID(r))
		sw := &statusWriter{ResponseWriter: w}
		defer func() { access.done(sw.statusOrOK(), sw.bytes) }()
		r = withAccess(r, access)
		if originalPath != r.URL.Path {
			alias(r, originalPath)
		}

		if requiresClientAuth(r.URL.Path) {
			s.track(clientID(r))
			if s.cfg().AuthToken != "" && !s.clientAuthorized(r) {
				access.outcome = "bad client key"
				writeErrorForPath(sw, r.URL.Path, http.StatusUnauthorized, "authentication_error", "invalid or missing API key")
				return
			}
		}

		next.ServeHTTP(sw, r)
	})
}

func canonicalEndpointPath(path string) (string, bool) {
	lower := strings.ToLower(path)
	switch lower {
	case "/v1/chat/completions", "/chat/completions":
		return "/v1/chat/completions", path != "/v1/chat/completions"
	case "/v1/messages", "/messages":
		return "/v1/messages", path != "/v1/messages"
	case "/v1/messages/count_tokens", "/messages/count_tokens":
		return "/v1/messages/count_tokens", path != "/v1/messages/count_tokens"
	case "/v1/models":
		return "/v1/models", path != "/v1/models"
	case "/models":
		return "/models", path != "/models"
	case "/health":
		return "/health", path != "/health"
	case "/status":
		return "/status", path != "/status"
	case "/config":
		return "/config", path != "/config"
	case "/features":
		return "/features", path != "/features"
	case "/stop":
		return "/stop", path != "/stop"
	default:
		return path, false
	}
}

// mutationAuthorized guards state-changing management calls. It writes a 401 and
// reports false when an AUTH_TOKEN is configured but absent or wrong.
func (s *Server) mutationAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg().AuthToken == "" {
		return true
	}
	if s.clientAuthorized(r) {
		return true
	}
	note(r, "bad client key")
	writeOpenAIError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing API key")
	return false
}

// isLoopbackOrigin reports whether a browser Origin belongs to this machine, on
// any port. Anything else is treated as untrusted.
func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// requiresClientAuth marks the API surface guarded by AUTH_TOKEN. Health and
// admin views stay open for localhost use.
func requiresClientAuth(path string) bool {
	if strings.HasPrefix(path, "/v1/") {
		return true
	}
	switch path {
	case "/models", "/features", "/stop":
		return true
	}
	return false
}

func (s *Server) clientAuthorized(r *http.Request) bool {
	got := extractClientKey(r.Header)
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg().AuthToken)) == 1
}

// extractClientKey reads the client credential from x-api-key or an
// Authorization: Bearer header.
func extractClientKey(h http.Header) string {
	if v := strings.TrimSpace(h.Get("x-api-key")); v != "" {
		return v
	}
	if a := strings.TrimSpace(h.Get("Authorization")); a != "" {
		if len(a) > 7 && strings.EqualFold(a[:7], "Bearer ") {
			return strings.TrimSpace(a[7:])
		}
		return a
	}
	return ""
}

// clientID fingerprints a caller: a short hash of its key if present, otherwise
// its remote IP. Used only for admin telemetry, never for auth.
func clientID(r *http.Request) string {
	if key := extractClientKey(r.Header); key != "" {
		sum := sha256.Sum256([]byte(key))
		return "key:" + hex.EncodeToString(sum[:4])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return "ip:" + host
}
