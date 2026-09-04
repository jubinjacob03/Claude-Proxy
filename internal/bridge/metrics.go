package bridge

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// handleMetrics exposes counters in Prometheus text format. /admin/stats serves
// the same numbers as JSON.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	c := s.cfg()
	clients := s.snapshotClients()

	var b strings.Builder
	metric := func(name, help, kind string, value any, labels string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s%s %v\n", name, help, name, kind, name, labels, value)
	}

	metric("claude_proxy_uptime_seconds", "Seconds since start.", "gauge",
		int(time.Since(s.startTime).Seconds()), "")
	metric("claude_proxy_requests_total", "API requests received.", "counter",
		s.totalRequests.Load(), "")
	metric("claude_proxy_clients", "Distinct clients seen.", "gauge",
		len(clients), "")
	metric("claude_proxy_upstream_errors_total", "Upstream responses with status >= 400.", "counter",
		s.upstreamErrors.Load(), "")
	metric("claude_proxy_streams_total", "Streaming responses served.", "counter",
		s.streamCount.Load(), "")

	metric("claude_proxy_info", "Build and routing info.", "gauge", 1,
		fmt.Sprintf("{version=%q,upstream=%q,format=%q,model=%q}",
			s.version, c.UpstreamBaseURL, c.UpstreamFormat, c.DefaultModel))

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

