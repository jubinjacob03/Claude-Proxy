package bridge

import (
	"encoding/json"
	"net/http"
	"strings"
)

func writeAnthropicError(w http.ResponseWriter, status int, errType, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errType,
			"message": msg,
		},
	})
}

func writeOpenAIError(w http.ResponseWriter, status int, errType, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    errType,
			"code":    nil,
		},
	})
}

// writeErrorForPath emits an error in the shape the caller expects, inferred
// from the request path.
func writeErrorForPath(w http.ResponseWriter, path string, status int, errType, msg string) {
	if isAnthropicPath(path) {
		writeAnthropicError(w, status, errType, msg)
		return
	}
	writeOpenAIError(w, status, errType, msg)
}

func isAnthropicPath(path string) bool {
	return strings.HasPrefix(path, "/v1/messages")
}

// anthropicErrorType maps an HTTP status to an Anthropic error type string.
func anthropicErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusServiceUnavailable:
		return "overloaded_error"
	default:
		if status >= 500 {
			return "api_error"
		}
		return "invalid_request_error"
	}
}

func openAIErrorType(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusBadRequest:
		return "invalid_request_error"
	default:
		if status >= 500 {
			return "api_error"
		}
		return "invalid_request_error"
	}
}

func writeRawJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// isAnthropicErrorShape reports whether a body is already {"type":"error",
// "error":{...}} and can be forwarded untouched.
func isAnthropicErrorShape(body []byte) bool {
	var probe struct {
		Type  string          `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.Type == "error" && len(probe.Error) > 0
}

// isOpenAIErrorShape reports whether a body is already {"error":{"message":...}}.
func isOpenAIErrorShape(body []byte) bool {
	var probe struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.Error != nil && probe.Error.Message != ""
}

// extractErrorMessage pulls a human-readable message out of an upstream error
// body in either OpenAI or Anthropic shape, falling back to the raw text.
func extractErrorMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "upstream returned an error with an empty body"
	}

	var probe struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &probe); err == nil {
		if probe.Error.Message != "" {
			return probe.Error.Message
		}
		if probe.Message != "" {
			return probe.Message
		}
	}

	const max = 500
	if len(trimmed) > max {
		return trimmed[:max]
	}
	return trimmed
}
