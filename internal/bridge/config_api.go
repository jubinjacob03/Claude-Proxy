package bridge

import (
	"claude-proxy/internal/logx"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// handleConfig exposes the live configuration. GET returns it with secrets
// redacted; POST applies changes atomically and optionally persists them to
// .env. Changes take effect immediately for all subsequent requests.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.configView(s.cfg()))
	case http.MethodPost:
		// Mutations can repoint the upstream, so they need the token when one is
		// configured. Reads stay open: they only expose a masked key.
		if !s.mutationAuthorized(w, r) {
			return
		}
		s.applyConfig(w, r)
	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
	}
}

func (s *Server) configView(c *Config) map[string]any {
	return map[string]any{
		"host":                     c.Host,
		"port":                     c.Port,
		"upstream_base_url":        c.UpstreamBaseURL,
		"upstream_format":          string(c.UpstreamFormat),
		"upstream_api_key_set":     c.UpstreamAPIKey != "",
		"upstream_api_key_masked":  logx.Redact(c.UpstreamAPIKey),
		"auth_token_set":           c.AuthToken != "",
		"default_model":            c.DefaultModel,
		"model_map":                ModelMapString(c.ModelMap),
		"default_max_tokens":       c.DefaultMaxTokens,
		"stream_idle_ping_seconds": int(c.StreamIdlePing / time.Second),
		"request_timeout_seconds":  int(c.RequestTimeout / time.Second),
		"log_level":                logx.LevelName(c.LogLevel),
		"log_format":               c.LogFormat,
		"log_bodies":               c.LogBodies,
		"env_path":                 c.EnvPath,
	}
}

type configUpdate struct {
	UpstreamBaseURL  *string `json:"upstream_base_url"`
	UpstreamFormat   *string `json:"upstream_format"`
	UpstreamAPIKey   *string `json:"upstream_api_key"`
	AuthToken        *string `json:"auth_token"`
	DefaultModel     *string `json:"default_model"`
	ModelMap         *string `json:"model_map"`
	DefaultMaxTokens *int    `json:"default_max_tokens"`
	StreamIdlePing   *int    `json:"stream_idle_ping_seconds"`
	RequestTimeout   *int    `json:"request_timeout_seconds"`
	LogLevel         *string `json:"log_level"`
	LogFormat        *string `json:"log_format"`
	LogBodies        *bool   `json:"log_bodies"`
	Persist          bool    `json:"persist"`
}

func (s *Server) applyConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "failed to read body")
		return
	}
	var upd configUpdate
	if err := json.Unmarshal(raw, &upd); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return
	}

	next := s.cfg().Clone()

	if upd.UpstreamBaseURL != nil {
		next.UpstreamBaseURL = strings.TrimRight(strings.TrimSpace(*upd.UpstreamBaseURL), "/")
	}
	if upd.UpstreamFormat != nil {
		f := UpstreamFormat(strings.ToLower(strings.TrimSpace(*upd.UpstreamFormat)))
		if f != FormatOpenAI && f != FormatAnthropic {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "upstream_format must be 'openai' or 'anthropic'")
			return
		}
		next.UpstreamFormat = f
	}
	// Only overwrite the upstream key when a non-empty value is supplied, so a
	// blank field never wipes a working key.
	if upd.UpstreamAPIKey != nil && strings.TrimSpace(*upd.UpstreamAPIKey) != "" {
		next.UpstreamAPIKey = strings.TrimSpace(*upd.UpstreamAPIKey)
	}
	if upd.AuthToken != nil {
		next.AuthToken = strings.TrimSpace(*upd.AuthToken)
	}
	if upd.DefaultModel != nil {
		next.DefaultModel = strings.TrimSpace(*upd.DefaultModel)
	}
	if upd.ModelMap != nil {
		next.ModelMap = ParseModelMap(*upd.ModelMap)
	}
	if upd.DefaultMaxTokens != nil && *upd.DefaultMaxTokens > 0 {
		next.DefaultMaxTokens = *upd.DefaultMaxTokens
	}
	if upd.StreamIdlePing != nil && *upd.StreamIdlePing >= 0 {
		next.StreamIdlePing = time.Duration(*upd.StreamIdlePing) * time.Second
	}
	if upd.RequestTimeout != nil && *upd.RequestTimeout >= 0 {
		next.RequestTimeout = time.Duration(*upd.RequestTimeout) * time.Second
	}
	if upd.LogLevel != nil {
		next.LogLevel = logx.ParseLevel(*upd.LogLevel)
	}
	if upd.LogFormat != nil {
		next.LogFormat = strings.ToLower(strings.TrimSpace(*upd.LogFormat))
	}
	if upd.LogBodies != nil {
		next.LogBodies = *upd.LogBodies
	}

	if next.UpstreamBaseURL == "" {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "upstream_base_url cannot be empty")
		return
	}

	s.setConfig(next)
	logx.SetLevel(next.LogLevel)
	logx.Info("config updated (upstream=%s format=%s persist=%t)", next.UpstreamBaseURL, next.UpstreamFormat, upd.Persist)

	saved := false
	var saveErr string
	if upd.Persist {
		if err := next.Save(); err != nil {
			saveErr = err.Error()
			logx.Warn("failed to persist config to %s: %v", next.EnvPath, err)
		} else {
			saved = true
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"persisted":     saved,
		"persist_error": saveErr,
		"config":        s.configView(next),
	})
}
