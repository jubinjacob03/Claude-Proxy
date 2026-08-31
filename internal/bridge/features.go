package bridge

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"claude-proxy/internal/logx"
)

// ModelFeatures holds per-model request defaults. Every field is optional: a nil
// field means "leave the request alone". Stored overrides only fill in fields the
// client omitted, so an explicit client value always wins.
type ModelFeatures struct {
	Thinking       *bool    `json:"thinking,omitempty"`
	ThinkingBudget *int     `json:"thinking_budget_tokens,omitempty"`
	MaxTokens      *int     `json:"max_tokens,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
	TopP           *float64 `json:"top_p,omitempty"`
}

// thinkingSupported reports whether a model id advertises Claude extended
// thinking. GoRouter exposes explicit "-thinking" variants, so a request that
// forces thinking on a non-thinking id is a likely 400 upstream.
func thinkingSupported(model string) bool {
	return strings.Contains(strings.ToLower(model), "thinking")
}

// resolveFeatures returns the effective features for a model: the configured
// defaults with any stored per-model overrides layered on top.
func (s *Server) resolveFeatures(model string) ModelFeatures {
	s.featuresMu.Lock()
	defer s.featuresMu.Unlock()

	var out ModelFeatures
	if f, ok := s.features[model]; ok {
		out = f
	}
	if out.MaxTokens == nil {
		mt := s.cfg().DefaultMaxTokens
		out.MaxTokens = &mt
	}
	return out
}

func (s *Server) storedFeatures() map[string]ModelFeatures {
	s.featuresMu.Lock()
	defer s.featuresMu.Unlock()
	out := make(map[string]ModelFeatures, len(s.features))
	for k, v := range s.features {
		out[k] = v
	}
	return out
}

func (s *Server) setFeatures(model string, f ModelFeatures) {
	s.featuresMu.Lock()
	defer s.featuresMu.Unlock()
	if s.features == nil {
		s.features = map[string]ModelFeatures{}
	}
	s.features[model] = f
}

// applyFeatures merges a model's resolved features into a raw Anthropic request
// body, filling only absent fields. The body is returned unchanged when nothing
// applies, so the common path stays byte-identical for passthrough.
func applyFeatures(raw []byte, model string, f ModelFeatures) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}

	changed := false
	setIfAbsent := func(key string, v any) {
		if _, exists := m[key]; exists {
			return
		}
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		m[key] = b
		changed = true
	}

	if f.MaxTokens != nil && *f.MaxTokens > 0 {
		setIfAbsent("max_tokens", *f.MaxTokens)
	}
	if f.Temperature != nil {
		setIfAbsent("temperature", *f.Temperature)
	}
	if f.TopP != nil {
		setIfAbsent("top_p", *f.TopP)
	}
	if f.Thinking != nil && thinkingSupported(model) {
		if *f.Thinking {
			think := map[string]any{"type": "enabled"}
			if f.ThinkingBudget != nil && *f.ThinkingBudget > 0 {
				think["budget_tokens"] = *f.ThinkingBudget
			}
			setIfAbsent("thinking", think)
		} else {
			setIfAbsent("thinking", map[string]any{"type": "disabled"})
		}
	}

	if !changed {
		return raw
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// handleFeatures inspects and updates per-model feature overrides.
//
//	GET  /features            -> every stored override
//	GET  /features?model=<id> -> resolved features for one model
//	POST /features            -> {"model":"<id>", ...overrides}
func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if model := strings.TrimSpace(r.URL.Query().Get("model")); model != "" {
			stored, hasStored := s.storedFeatures()[model]
			writeJSON(w, http.StatusOK, map[string]any{
				"model":              model,
				"resolved":           s.resolveFeatures(model),
				"stored":             stored,
				"has_overrides":      hasStored,
				"thinking_supported": thinkingSupported(model),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"features": s.storedFeatures()})

	case http.MethodPost:
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "failed to read body")
			return
		}
		var body struct {
			Model string `json:"model"`
			ModelFeatures
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
			return
		}
		model := strings.TrimSpace(body.Model)
		if model == "" {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "'model' is required")
			return
		}
		if body.Thinking != nil && *body.Thinking && !thinkingSupported(model) {
			logx.Warn("features: %q does not look like a thinking model; the upstream may reject it", model)
		}
		s.setFeatures(model, body.ModelFeatures)
		logx.Info("features updated for %s", model)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"model":    model,
			"resolved": s.resolveFeatures(model),
		})

	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
	}
}

// handleStop is an acknowledged no-op kept for client compatibility.
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stopped": false})
}
