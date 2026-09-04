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

// handleCompletions serves the OpenAI Chat Completions API. When the upstream is
// OpenAI it forwards unchanged (model rewrite only); when the upstream is
// Anthropic it translates the request to messages and the response back.
func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}

	var req openai.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return
	}
	if req.Model == "" {
		req.Model = s.cfg().DefaultModel
		raw = rewriteModel(raw, req.Model)
	}

	cfg := s.cfg()
	clientModel := req.Model
	upstreamModel := cfg.MapModel(req.Model)
	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	annotate(r, clientModel, req.Stream)
	ctx, cancel := s.reqContext(r)
	defer cancel()

	s.logBody("openai request", raw)
	if cfg.UpstreamFormat == FormatOpenAI {
		s.passthrough(ctx, w, r.Header, "/v1/chat/completions", rewriteModel(raw, upstreamModel), s.claudeTarget(), req.Stream)
		return
	}

	anReq := translate.OpenAIRequestToAnthropic(&req, upstreamModel, s.cfg().DefaultMaxTokens)
	body, err := json.Marshal(anReq)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "api_error", "failed to encode upstream request")
		return
	}
	body = applyFeatures(body, upstreamModel, s.resolveFeatures(clientModel))
	s.logBody("-> anthropic request", body)

	upstreamReq, err := s.newUpstreamRequest(ctx, "/v1/messages", body, upstreamTarget{
		BaseURL: cfg.UpstreamBaseURL,
		APIKey:  cfg.UpstreamAPIKey,
		Format:  FormatAnthropic,
		Name:    "claude",
	}, r.Header, req.Stream)
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
		s.relayUpstreamError(w, resp, false)
		return
	}

	if req.Stream {
		flush := s.beginStream(w)
		if err := translate.AnthropicStreamToOpenAI(w, flush, resp.Body, clientModel, includeUsage, s.cfg().StreamIdlePing); err != nil {
			logx.Debug("stream anthropic->openai ended: %v", err)
		}
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "api_error", "failed to read upstream response")
		return
	}
	var anResp anthropic.Response
	if err := json.Unmarshal(data, &anResp); err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "api_error", "unexpected upstream response: "+truncate(string(data), 200))
		return
	}
	writeJSON(w, http.StatusOK, translate.AnthropicResponseToOpenAI(&anResp, clientModel))
}
