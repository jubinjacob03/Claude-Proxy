package bridge

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed web
var webFS embed.FS

// serveDashboard serves the embedded single-page dashboard and its static
// assets. Unknown paths fall through to a 404 in the caller's format.
func (s *Server) serveDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorForPath(w, r.URL.Path, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if strings.Contains(name, "..") {
		writeErrorForPath(w, r.URL.Path, http.StatusNotFound, "not_found_error", "not found")
		return
	}
	data, err := webFS.ReadFile("web/" + name)
	if err != nil {
		writeErrorForPath(w, r.URL.Path, http.StatusNotFound, "not_found_error", "no such endpoint: "+r.URL.Path)
		return
	}
	w.Header().Set("Content-Type", dashboardContentType(name))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func dashboardContentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}
