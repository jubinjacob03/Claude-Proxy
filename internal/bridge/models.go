package bridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"claude-proxy/internal/logx"
)

// handleModels serves a model list whose entries carry both OpenAI fields (id,
// object) and Anthropic fields (id, display_name), so the same response
// satisfies OpenAI clients and Claude Code's gateway model discovery.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	ids := s.discoverModels(r)
	now := time.Now().Unix()
	created := time.Unix(now, 0).UTC().Format(time.RFC3339)

	data := make([]any, 0, len(ids))
	for _, id := range ids {
		data = append(data, map[string]any{
			"id":           id,
			"object":       "model",
			"type":         "model",
			"display_name": id,
			"created":      now,
			"created_at":   created,
			"owned_by":     "gorouter",
			"architecture": modelArchitecture(id),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object":   "list",
		"data":     data,
		"has_more": false,
	})
}

// handleModelsCompact serves the { models, currentModel } shape some clients
// expect from GET /models (plural).
func (s *Server) handleModelsCompact(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"models":       s.discoverModels(r),
		"currentModel": s.cfg().DefaultModel,
	})
}

func (s *Server) discoverModels(r *http.Request) []string {
	s.mu.Lock()
	if s.modelsCache != nil && time.Now().Before(s.modelsExp) {
		cached := s.modelsCache
		s.mu.Unlock()
		return cached
	}
	s.mu.Unlock()

	ids := s.fetchUpstreamModels(r)
	if len(ids) == 0 {
		ids = defaultModels()
	}
	for from := range s.cfg().ModelMap {
		if from != "*" {
			ids = appendUnique(ids, from)
		}
	}

	s.mu.Lock()
	s.modelsCache = ids
	s.modelsExp = time.Now().Add(5 * time.Minute)
	s.mu.Unlock()
	return ids
}

// fetchUpstreamModels asks the upstream for its catalog and returns the ids. Any
// failure yields nil so the caller can fall back to a static list.
func (s *Server) fetchUpstreamModels(r *http.Request) []string {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg().UpstreamBaseURL+"/v1/models", nil)
	if err != nil {
		return nil
	}
	key := s.resolveKey(r.Header)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("x-api-key", key)
	}
	req.Header.Set("anthropic-version", anthropicVersionDefault)
	req.Header.Set("User-Agent", s.userAgent())

	resp, err := s.client.Do(req)
	if err != nil {
		logx.Debug("model discovery failed: %v", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		logx.Debug("model discovery upstream status %d", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	var ids []string
	for _, m := range parsed.Data {
		if m.ID != "" {
			ids = appendUnique(ids, m.ID)
		}
	}
	return ids
}

// defaultModels is the fallback catalog: the GoRouter/AgentRouter Claude models
// plus common Claude ids, so discovery and the picker still work offline.
func defaultModels() []string {
	return []string{
		"claude-opus-4-8",
		"claude-opus-4-8-thinking",
		"claude-opus-5",
		"claude-opus-5-thinking",
		"claude-sonnet-4-5-20250929",
		"claude-opus-4-5-20250929",
		"claude-3-7-sonnet-20250219",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
	}
}

// modelArchitecture describes a model's modality, OpenAI-style. Claude models
// are multimodal (text+image in, text out); a "-haiku"-only text guess isn't
// worth the risk, so image input is advertised for the whole Claude family.
func modelArchitecture(id string) map[string]any {
	return map[string]any{
		"modality":          "text+image->text",
		"input_modalities":  []string{"text", "image"},
		"output_modalities": []string{"text"},
	}
}

func appendUnique(list []string, v string) []string {
	for _, existing := range list {
		if existing == v {
			return list
		}
	}
	return append(list, v)
}
