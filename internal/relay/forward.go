package relay

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"claude-proxy/internal/license"
	"claude-proxy/internal/logx"
)

const anthropicVersionDefault = "2023-06-01"

// ForwardResult holds the upstream response status and token usage extracted
// from the response body for accurate billing.
type ForwardResult struct {
	StatusCode   int
	InputTokens  int64
	OutputTokens int64
}

// forward relays the client's body to the upstream using a pooled credential
// and streams the answer straight back. It returns a ForwardResult so the
// caller can decide whether the request was billable and apply token-based pricing.
func (s *Server) forward(w http.ResponseWriter, r *http.Request, baseURL string, poolKey *license.PoolKey, body []byte, streamed bool) ForwardResult {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, baseURL+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		writeUpstreamError(w, r, http.StatusBadGateway, "upstream request could not be built")
		return ForwardResult{}
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
	req.Header.Set("Authorization", "Bearer "+poolKey.Secret)
	if isAnthropicPath(r.URL.Path) {
		req.Header.Set("x-api-key", poolKey.Secret)
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
		return ForwardResult{}
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

	result := ForwardResult{StatusCode: resp.StatusCode}

	if streamed {
		result.InputTokens, result.OutputTokens = flushCopySSE(w, resp.Body)
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		_, _ = w.Write(respBody)
		result.InputTokens, result.OutputTokens = extractTokensFromJSON(respBody)
	}
	return result
}

// flushCopySSE streams SSE events to the client and extracts token usage
// from the final usage event embedded in the stream.
func flushCopySSE(w http.ResponseWriter, src io.Reader) (inputTokens, outputTokens int64) {
	flush := func() {}
	if f, ok := w.(http.Flusher); ok {
		flush = f.Flush
	}

	var buf bytes.Buffer
	raw := make([]byte, 32*1024)
	for {
		n, err := src.Read(raw)
		if n > 0 {
			chunk := raw[:n]
			buf.Write(chunk)
			if _, werr := w.Write(chunk); werr != nil {
				return
			}
			flush()
		}
		if err != nil {
			break
		}
	}

	// Parse the accumulated SSE stream to find token usage in data events.
	inputTokens, outputTokens = extractTokensFromSSE(buf.Bytes())
	return
}

// extractTokensFromJSON parses a non-streamed JSON response and extracts
// the usage.prompt_tokens and usage.completion_tokens fields.
func extractTokensFromJSON(body []byte) (inputTokens, outputTokens int64) {
	var resp struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			InputTokens      int64 `json:"input_tokens"`
			OutputTokens     int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, 0
	}
	// New API returns prompt_tokens / completion_tokens (OpenAI style).
	// Anthropic native returns input_tokens / output_tokens. Handle both.
	in := resp.Usage.PromptTokens
	if in == 0 {
		in = resp.Usage.InputTokens
	}
	out := resp.Usage.CompletionTokens
	if out == 0 {
		out = resp.Usage.OutputTokens
	}
	return in, out
}

// extractTokensFromSSE scans SSE data lines for usage information.
// New API embeds a final "usage" object in the last data event before [DONE].
func extractTokensFromSSE(stream []byte) (inputTokens, outputTokens int64) {
	lines := bytes.Split(stream, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || string(data) == "[DONE]" {
			continue
		}
		in, out := extractTokensFromJSON(data)
		if in > 0 || out > 0 {
			inputTokens = in
			outputTokens = out
		}
	}
	return
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
