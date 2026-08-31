package bridge

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
)

// withMiddleware wraps the mux with CORS, client auth, request logging, and
// per-client request accounting.
func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, x-api-key, anthropic-version, anthropic-beta")
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Static dashboard assets would drown the console; serve them quietly.
		if isDashboardAsset(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		access := newAccessRecord(r.Method, r.URL.Path)
		sw := &statusWriter{ResponseWriter: w}
		defer func() { access.done(sw.statusOrOK(), sw.bytes) }()
		r = withAccess(r, access)

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

func isDashboardAsset(path string) bool {
	switch path {
	case "/app.js", "/styles.css", "/favicon.ico":
		return true
	}
	return false
}

// requiresClientAuth marks the API surface guarded by AUTH_TOKEN. The dashboard,
// health, and admin views stay open for localhost use.
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
