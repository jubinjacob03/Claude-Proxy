// Package translate converts request and response payloads between the
// Anthropic Messages API and the OpenAI Chat Completions API, in both
// directions, for streaming and non-streaming traffic.
package translate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"
)

func genID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return prefix + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return prefix + hex.EncodeToString(b)
}

// ensureID keeps an upstream id when present, otherwise generates one with the
// given prefix.
func ensureID(id, prefix string) string {
	if id != "" {
		return id
	}
	return genID(prefix)
}

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return json.RawMessage(b)
}

// validObjectOrEmpty guarantees a JSON object for tool inputs/arguments.
func validObjectOrEmpty(raw json.RawMessage) json.RawMessage {
	s := string(raw)
	if s == "" || !json.Valid(raw) {
		return json.RawMessage("{}")
	}
	return raw
}

func validArgsString(s string) string {
	if s == "" || !json.Valid([]byte(s)) {
		return "{}"
	}
	return s
}

// openAIFinishToAnthropic maps an OpenAI finish_reason to an Anthropic
// stop_reason.
func openAIFinishToAnthropic(finish string) string {
	switch finish {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "end_turn"
	case "":
		return "end_turn"
	default:
		return "end_turn"
	}
}

// anthropicStopToOpenAI maps an Anthropic stop_reason to an OpenAI
// finish_reason.
func anthropicStopToOpenAI(stop string) string {
	switch stop {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "":
		return "stop"
	default:
		return "stop"
	}
}
