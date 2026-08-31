package bridge

import (
	"net/http"
	"time"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	c := s.cfg()
	var problems []string
	if c.UpstreamBaseURL == "" {
		problems = append(problems, "UPSTREAM_BASE_URL is not set")
	}
	if c.UpstreamAPIKey == "" {
		problems = append(problems, "UPSTREAM_API_KEY is not set (clients must forward a key)")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"name":           "claude-proxy",
		"version":        s.version,
		"uptime_seconds": int(time.Since(s.startTime).Seconds()),
		"config": map[string]any{
			"upstream_base_url":    c.UpstreamBaseURL,
			"upstream_format":      string(c.UpstreamFormat),
			"default_model":        c.DefaultModel,
			"upstream_api_key_set": c.UpstreamAPIKey != "",
			"auth_required":        c.AuthToken != "",
			"model_map_count":      len(c.ModelMap),
		},
		"validation": map[string]any{
			"ok":       len(problems) == 0,
			"problems": problems,
		},
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	c := s.cfg()
	writeJSON(w, http.StatusOK, map[string]any{
		"name":                     "claude-proxy",
		"version":                  s.version,
		"mode":                     string(c.UpstreamFormat),
		"uptime_seconds":           int(time.Since(s.startTime).Seconds()),
		"upstream_base_url":        c.UpstreamBaseURL,
		"upstream_api_key_set":     c.UpstreamAPIKey != "",
		"auth_required":            c.AuthToken != "",
		"default_model":            c.DefaultModel,
		"model_map_count":          len(c.ModelMap),
		"stream_idle_ping_seconds": int(c.StreamIdlePing / time.Second),
		"request_timeout_seconds":  int(c.RequestTimeout / time.Second),
		"total_requests":           s.totalRequests.Load(),
		"total_clients":            len(s.snapshotClients()),
	})
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	c := s.cfg()
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":           string(c.UpstreamFormat),
		"version":        s.version,
		"uptime_seconds": int(time.Since(s.startTime).Seconds()),
		"total_clients":  len(s.snapshotClients()),
		"total_requests": s.totalRequests.Load(),
	})
}

func (s *Server) handleAdminClients(w http.ResponseWriter, r *http.Request) {
	clients := s.snapshotClients()
	writeJSON(w, http.StatusOK, map[string]any{
		"count":   len(clients),
		"clients": clients,
	})
}
