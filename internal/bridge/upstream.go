package bridge

import (
	"bufio"
	"bytes"
	"claude-proxy/internal/logx"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const anthropicVersionDefault = "2023-06-01"

type upstreamTarget struct {
	BaseURL string
	APIKey  string
	Format  UpstreamFormat
	Name    string
}

func (s *Server) claudeTarget() upstreamTarget {
	c := s.cfg()
	return upstreamTarget{
		BaseURL: c.UpstreamBaseURL,
		APIKey:  c.UpstreamAPIKey,
		Format:  c.UpstreamFormat,
		Name:    "claude",
	}
}

// newUpstreamRequest builds a POST to the upstream at path, injecting auth and
// the headers the target format expects. clientHeader is the inbound request's
// header set, used to resolve a fallback key and forward Anthropic betas.
func (s *Server) newUpstreamRequest(ctx context.Context, path string, body []byte, target upstreamTarget, clientHeader http.Header, stream bool) (*http.Request, error) {
	baseURL := strings.TrimRight(target.BaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(s.cfg().UpstreamBaseURL, "/")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	// Identify ourselves rather than leaving Go's default UA. Routers like
	// GoRouter sit behind Cloudflare, which is friendlier to a named client.
	req.Header.Set("User-Agent", s.userAgent())

	key := s.resolveKeyFor(clientHeader, target.APIKey)
	format := target.Format
	if format == "" {
		format = FormatOpenAI
	}
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
	return s.resolveKeyFor(clientHeader, s.cfg().UpstreamAPIKey)
}

func (s *Server) resolveKeyFor(clientHeader http.Header, preferred string) string {
	if preferred != "" {
		return preferred
	}
	return extractClientKey(clientHeader)
}

// passthrough forwards a body to the upstream unchanged (aside from the model
// rewrite done by the caller) and relays the response, streaming when asked.
func (s *Server) passthrough(ctx context.Context, w http.ResponseWriter, clientHeader http.Header, path string, body []byte, target upstreamTarget, stream bool) {
	req, err := s.newUpstreamRequest(ctx, path, body, target, clientHeader, stream)
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
	s.writeRelayedError(w, resp.StatusCode, body, wantAnthropic)
}

func (s *Server) writeRelayedError(w http.ResponseWriter, status int, body []byte, wantAnthropic bool) {
	logx.Warn("upstream error %d: %s", status, truncate(string(body), 300))
	s.upstreamErrors.Add(1)

	if wantAnthropic {
		if isAnthropicErrorShape(body) {
			writeRawJSON(w, status, body)
			return
		}
		writeAnthropicError(w, status, anthropicErrorType(status), extractErrorMessage(body))
		return
	}
	if isOpenAIErrorShape(body) {
		writeRawJSON(w, status, body)
		return
	}
	writeOpenAIError(w, status, openAIErrorType(status), extractErrorMessage(body))
}

// relayOpenAI forwards an OpenAI-shaped response, rewriting the model id so the
// client only ever sees the model it asked for.
func (s *Server) relayOpenAI(w http.ResponseWriter, resp *http.Response, clientModel string, stream bool) {
	if stream {
		flush := s.beginStream(w)
		relayOpenAIStream(w, flush, resp.Body, clientModel)
		return
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "api_error", "failed to read upstream response")
		return
	}
	writeRawJSON(w, http.StatusOK, rewriteModel(data, clientModel))
}

func relayOpenAIStream(w http.ResponseWriter, flush func(), body io.Reader, clientModel string) {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	turn := nextStreamTurn()
	filter := &toolTextFilter{}
	var template []byte
	var forcedID string
	var deferredFinish []byte

	for sc.Scan() {
		line := sc.Text()
		traceIn(turn, line)
		payload, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			if _, err := io.WriteString(w, line+"\n"); err != nil {
				return
			}
			if line == "" {
				flush()
			}
			continue
		}

		trimmed := strings.TrimSpace(payload)
		if trimmed == "" || trimmed == "[DONE]" {
			continue
		}

		rewritten := rewriteModel([]byte(trimmed), clientModel)
		if len(template) == 0 {
			template = append([]byte(nil), rewritten...)
		}
		filtered, skip := filterFrameContent(rewritten, filter)
		if skip {
			continue
		}
		if frameHasFinishReason(filtered) {
			deferredFinish = append([]byte(nil), filtered...)
			continue
		}
		if forcedID == "" {
			var probe struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(filtered, &probe) == nil {
				forcedID = probe.ID
			}
		}
		out := "data: " + string(filtered)
		traceOut(turn, "pass-through", out)
		if _, err := io.WriteString(w, out+"\n\n"); err != nil {
			return
		}
		flush()
	}
	if err := sc.Err(); err != nil {
		return
	}

	if emitted := emitPendingToolText(w, filter, template, clientModel, forcedID, turn); emitted {
		emitToolCallStop(w, template, clientModel, forcedID, turn)
		flush()
	} else if len(deferredFinish) > 0 {
		out := "data: " + string(deferredFinish)
		traceOut(turn, "deferred-finish", out)
		if _, err := io.WriteString(w, out+"\n\n"); err != nil {
			return
		}
		flush()
	}
	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		return
	}
	flush()
}

// scrubModelName keeps an upstream's model id out of anything the client sees.
func scrubModelName(body []byte, from, to string) []byte {
	if from == "" || to == "" || len(body) == 0 {
		return body
	}
	out := strings.ReplaceAll(string(body), from, to)
	out = strings.ReplaceAll(out, strings.ToLower(from), to)
	return []byte(out)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}
