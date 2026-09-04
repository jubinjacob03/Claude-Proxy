package relay

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"claude-proxy/internal/logx"
)

const anthropicVersionDefault = "2023-06-01"

// forward relays the client's body to the upstream using a pooled credential
// and streams the answer straight back. It returns the upstream status so the
// caller can decide whether the request was billable; 0 means it never ran.
func (s *Server) forward(w http.ResponseWriter, r *http.Request, baseURL, secret string, body []byte, streamed bool) int {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, baseURL+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		writeUpstreamError(w, r, http.StatusBadGateway, "upstream request could not be built")
		return 0
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-relay")
	if streamed {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	// The pooled secret is attached here and nowhere else. It never travels
	// back towards the client.
	req.Header.Set("Authorization", "Bearer "+secret)
	if isAnthropicPath(r.URL.Path) {
		req.Header.Set("x-api-key", secret)
		version := r.Header.Get("anthropic-version")
		if version == "" {
			version = anthropicVersionDefault
		}
		req.Header.Set("anthropic-version", version)
		if beta := r.Header.Get("anthropic-beta"); beta != "" {
			req.Header.Set("anthropic-beta", beta)
		}
	}

	resp, err := s.client.Do(req)
	if err != nil {
		logx.Error("upstream %s failed: %v", r.URL.Path, err)
		writeUpstreamError(w, r, http.StatusBadGateway, "upstream request failed")
		return 0
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	if streamed {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
	}
	w.WriteHeader(resp.StatusCode)

	if streamed {
		flushCopy(w, resp.Body)
	} else {
		_, _ = io.Copy(w, resp.Body)
	}
	return resp.StatusCode
}

func flushCopy(w http.ResponseWriter, src io.Reader) {
	flush := func() {}
	if f, ok := w.(http.Flusher); ok {
		flush = f.Flush
	}
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

func isAnthropicPath(path string) bool {
	return strings.HasPrefix(path, "/v1/messages")
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

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"status": "error", "message": msg})
}

// writeUpstreamError answers in the dialect the caller is speaking so the
// message survives all the way to the user's editor instead of being swallowed
// as a malformed response.
func writeUpstreamError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if isAnthropicPath(r.URL.Path) {
		writeJSON(w, status, map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    anthropicErrorType(status),
				"message": msg,
			},
		})
		return
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    openAIErrorType(status),
			"code":    nil,
		},
	})
}

func anthropicErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden, http.StatusPaymentRequired:
		return "permission_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		return "api_error"
	}
}

func openAIErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusPaymentRequired:
		return "authentication_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		return "api_error"
	}
}
