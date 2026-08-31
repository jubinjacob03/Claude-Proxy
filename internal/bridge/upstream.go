package bridge

import (
	"bytes"
	"claude-proxy/internal/logx"
	"context"
	"io"
	"net/http"
	"strings"
)

const anthropicVersionDefault = "2023-06-01"

// newUpstreamRequest builds a POST to the upstream at path, injecting auth and
// the headers the target format expects. clientHeader is the inbound request's
// header set, used to resolve a fallback key and forward Anthropic betas.
func (s *Server) newUpstreamRequest(ctx context.Context, path string, body []byte, format UpstreamFormat, clientHeader http.Header, stream bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg().UpstreamBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	key := s.resolveKey(clientHeader)
	switch format {
	case FormatAnthropic:
		if key != "" {
			req.Header.Set("x-api-key", key)
			req.Header.Set("Authorization", "Bearer "+key)
		}
		version := clientHeader.Get("anthropic-version")
		if version == "" {
			version = anthropicVersionDefault
		}
		req.Header.Set("anthropic-version", version)
		if beta := clientHeader.Get("anthropic-beta"); beta != "" {
			req.Header.Set("anthropic-beta", beta)
		}
	default:
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	return req, nil
}

// resolveKey prefers the configured upstream key, falling back to whatever key
// the client presented so users can put their real router key in the client
// instead of the proxy.
func (s *Server) resolveKey(clientHeader http.Header) string {
	if s.cfg().UpstreamAPIKey != "" {
		return s.cfg().UpstreamAPIKey
	}
	return extractClientKey(clientHeader)
}

// passthrough forwards a body to the upstream unchanged (aside from the model
// rewrite done by the caller) and relays the response, streaming when asked.
func (s *Server) passthrough(ctx context.Context, w http.ResponseWriter, clientHeader http.Header, path string, body []byte, format UpstreamFormat, stream bool) {
	req, err := s.newUpstreamRequest(ctx, path, body, format, clientHeader, stream)
	if err != nil {
		s.upstreamError(w, path, err)
		return
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.upstreamError(w, path, err)
		return
	}
	defer resp.Body.Close()

	// Errors are never streams; normalize them so the client always gets an
	// envelope it can parse, even if the upstream answered in the other dialect.
	if resp.StatusCode >= 400 {
		s.relayUpstreamError(w, resp, isAnthropicPath(path))
		return
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	if stream {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
	}
	w.WriteHeader(resp.StatusCode)

	if stream {
		s.streamCount.Add(1)
		flushCopy(w, resp.Body)
	} else {
		_, _ = io.Copy(w, resp.Body)
	}
}

// upstreamError reports a transport-level failure (connection refused, DNS,
// timeout) in the format the client expects.
func (s *Server) upstreamError(w http.ResponseWriter, path string, err error) {
	logx.Error("upstream request to %s failed: %v", path, err)
	writeErrorForPath(w, path, http.StatusBadGateway, "api_error", "upstream request failed: "+err.Error())
}

// relayUpstreamError forwards a >=400 upstream response to the client.
//
// When the body already matches the shape the client expects it is passed
// through byte-for-byte: Claude Code matches on the upstream's error wording to
// decide whether to retry with a capability disabled, and rewrapping breaks that
// recovery path. Anything else (for example an OpenAI-shaped error arriving on
// the Anthropic route) is normalized into the expected envelope with the
// original message preserved.
func (s *Server) relayUpstreamError(w http.ResponseWriter, resp *http.Response, wantAnthropic bool) {
	body, _ := io.ReadAll(resp.Body)
	logx.Warn("upstream error %d: %s", resp.StatusCode, truncate(string(body), 300))
	s.upstreamErrors.Add(1)

	if wantAnthropic {
		if isAnthropicErrorShape(body) {
			writeRawJSON(w, resp.StatusCode, body)
			return
		}
		writeAnthropicError(w, resp.StatusCode, anthropicErrorType(resp.StatusCode), extractErrorMessage(body))
		return
	}
	if isOpenAIErrorShape(body) {
		writeRawJSON(w, resp.StatusCode, body)
		return
	}
	writeOpenAIError(w, resp.StatusCode, openAIErrorType(resp.StatusCode), extractErrorMessage(body))
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}
