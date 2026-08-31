package bridge

import (
	"encoding/json"
	"io"
	"net/http"

	"claude-proxy/internal/anthropic"
	"claude-proxy/internal/logx"
	"claude-proxy/internal/openai"
	"claude-proxy/internal/translate"
)

// handleMessages serves the Anthropic Messages API. When the upstream is
// Anthropic it forwards unchanged (model rewrite only); when the upstream is
// OpenAI it translates the request to chat completions and the response back.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}

	var req anthropic.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return
	}
	if req.Model == "" {
		req.Model = s.cfg().DefaultModel
		raw = rewriteModel(raw, req.Model)
	}

	upstreamModel := s.cfg().MapModel(req.Model)
	clientModel := req.Model
	annotate(r, upstreamModel, req.Stream)
	ctx, cancel := s.reqContext(r)
	defer cancel()

	s.logBody("anthropic request", raw)

	if s.cfg().UpstreamFormat == FormatAnthropic {
		body := applyFeatures(rewriteModel(raw, upstreamModel), upstreamModel, s.resolveFeatures(clientModel))
		s.logBody("-> anthropic passthrough", body)
		s.passthrough(ctx, w, r.Header, "/v1/messages", body, FormatAnthropic, req.Stream)
		return
	}

	oaReq := translate.AnthropicRequestToOpenAI(&req, upstreamModel)
	body, err := json.Marshal(oaReq)
	if err != nil {
		writeAnthropicError(w, http.StatusInternalServerError, "api_error", "failed to encode upstream request")
		return
	}
	s.logBody("-> openai request", body)

	upstreamReq, err := s.newUpstreamRequest(ctx, "/v1/chat/completions", body, FormatOpenAI, r.Header, req.Stream)
	if err != nil {
		s.upstreamError(w, r.URL.Path, err)
		return
	}
	resp, err := s.client.Do(upstreamReq)
	if err != nil {
		s.upstreamError(w, r.URL.Path, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		s.relayUpstreamError(w, resp, true)
		return
	}

	if req.Stream {
		flush := s.beginStream(w)
		if err := translate.OpenAIStreamToAnthropic(w, flush, resp.Body, clientModel, s.cfg().StreamIdlePing); err != nil {
			logx.Debug("stream openai->anthropic ended: %v", err)
		}
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "failed to read upstream response")
		return
	}
	var oaResp openai.Response
	if err := json.Unmarshal(data, &oaResp); err != nil {
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "unexpected upstream response: "+truncate(string(data), 200))
		return
	}
	writeJSON(w, http.StatusOK, translate.OpenAIResponseToAnthropic(&oaResp, clientModel))
}

// handleCountTokens serves /v1/messages/count_tokens. With an Anthropic upstream
// it forwards; with an OpenAI upstream (no such endpoint) it returns a local
// estimate.
func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}
	var req anthropic.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return
	}
	if req.Model == "" {
		req.Model = s.cfg().DefaultModel
		raw = rewriteModel(raw, req.Model)
	}

	ctx, cancel := s.reqContext(r)
	defer cancel()

	if s.cfg().UpstreamFormat == FormatAnthropic {
		s.passthrough(ctx, w, r.Header, "/v1/messages/count_tokens", rewriteModel(raw, s.cfg().MapModel(req.Model)), FormatAnthropic, false)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"input_tokens": estimateInputTokens(&req)})
}

// estimateInputTokens is a coarse heuristic (~4 chars/token) used only when the
// upstream cannot count tokens itself.
func estimateInputTokens(req *anthropic.Request) int {
	chars := len(anthropic.SystemText(req.System))
	for _, m := range req.Messages {
		for _, b := range m.Blocks() {
			chars += len(b.Text) + len(b.Thinking)
			chars += len(anthropic.ToolResultText(b.Content))
			chars += len(b.Input)
		}
		chars += 8
	}
	for _, t := range req.Tools {
		chars += len(t.Name) + len(t.Description) + len(t.InputSchema)
	}
	tokens := chars / 4
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}
