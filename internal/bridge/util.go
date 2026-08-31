package bridge

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"

	"claude-proxy/internal/logx"
)

// newHTTPClient builds the pooled HTTP/2-capable client used for all upstream
// calls. Per-request deadlines come from reqContext, not a client timeout, so
// long streams are never cut off.
func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}
	return &http.Client{Transport: transport}
}

func (s *Server) reqContext(r *http.Request) (context.Context, context.CancelFunc) {
	if s.cfg().RequestTimeout > 0 {
		return context.WithTimeout(r.Context(), s.cfg().RequestTimeout)
	}
	return r.Context(), func() {}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func (s *Server) logBody(label string, body []byte) {
	if !s.cfg().LogBodies || !logx.Enabled(logx.LevelDebug) {
		return
	}
	const max = 4000
	if len(body) > max {
		logx.Debug("%s (%d bytes): %s…", label, len(body), body[:max])
		return
	}
	logx.Debug("%s: %s", label, body)
}

func flusher(w http.ResponseWriter) func() {
	if f, ok := w.(http.Flusher); ok {
		return f.Flush
	}
	return func() {}
}

// beginStream sets SSE headers and counts the stream for /metrics.
func (s *Server) beginStream(w http.ResponseWriter) func() {
	s.streamCount.Add(1)
	return setSSEHeaders(w)
}

func setSSEHeaders(w http.ResponseWriter) func() {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flush := flusher(w)
	flush()
	return flush
}

// rewriteModel replaces the top-level "model" field in a raw JSON body while
// leaving every other field byte-identical, for passthrough forwarding.
func rewriteModel(raw []byte, model string) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	b, err := json.Marshal(model)
	if err != nil {
		return raw
	}
	m["model"] = b
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// flushCopy relays a streaming body to the client, flushing after every read so
// SSE chunks reach the client immediately instead of sitting in a buffer.
func flushCopy(w http.ResponseWriter, src io.Reader) {
	flush := flusher(w)
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			flush()
		}
		if err != nil {
			return
		}
	}
}
