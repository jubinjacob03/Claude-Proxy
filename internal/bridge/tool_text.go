package bridge

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
)

// Models sometimes emit tool invocations as text rather than as structured
// tool calls. The editor only executes structured calls, so text like
// <invoke name="LS">...</invoke> renders as prose and the agent loop stalls.
var (
	invokePattern    = regexp.MustCompile(`(?s)<invoke\s+name="([^"]+)"\s*>(.*?)</invoke>`)
	parameterPattern = regexp.MustCompile(`(?s)<parameter\s+name="([^"]+)"\s*>(.*?)</parameter>`)
	toolTextStart    = regexp.MustCompile(`<(?:antml:)?(?:function_calls|invoke)\b|<<<TOOL_CALL>>>`)
)

// The marker protocol the relay teaches models without a native tool API.
const (
	markerToolOpen  = "<<<TOOL_CALL>>>"
	markerToolClose = "<<<END_TOOL_CALL>>>"
)

type textToolCall struct {
	Name string
	Args map[string]string
	// RawArgs carries arguments that already arrived as JSON, so a schema with
	// non-string values survives instead of being flattened to strings.
	RawArgs string
}

// containsToolText reports whether a fragment has begun a text tool invocation.
func containsToolText(s string) bool {
	return toolTextStart.MatchString(s)
}

func parseTextToolCalls(s string) []textToolCall {
	var out []textToolCall
	for _, match := range invokePattern.FindAllStringSubmatch(s, -1) {
		call := textToolCall{Name: strings.TrimSpace(match[1]), Args: map[string]string{}}
		for _, param := range parameterPattern.FindAllStringSubmatch(match[2], -1) {
			call.Args[strings.TrimSpace(param[1])] = strings.TrimSpace(param[2])
		}
		if call.Name != "" {
			out = append(out, call)
		}
	}
	return append(out, parseMarkerToolCalls(s)...)
}

// parseMarkerToolCalls reads the marker protocol the relay teaches models that
// have no native tool API. A model may follow either that or the editor's own
// XML style, so both are accepted.
func parseMarkerToolCalls(s string) []textToolCall {
	var out []textToolCall
	rest := s
	for {
		start := strings.Index(rest, markerToolOpen)
		if start < 0 {
			return out
		}
		body := rest[start+len(markerToolOpen):]
		end := strings.Index(body, markerToolClose)
		if end < 0 {
			return out
		}
		payload := strings.TrimSpace(body[:end])
		rest = body[end+len(markerToolClose):]

		var parsed struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal([]byte(payload), &parsed) != nil || parsed.Name == "" {
			continue
		}
		call := textToolCall{Name: parsed.Name, Args: map[string]string{}, RawArgs: string(parsed.Arguments)}
		out = append(out, call)
	}
}

// toolCallSeq keeps synthesized ids unique for the life of the process. Reusing
// call_1 on every turn put a duplicate tool_call id into the same conversation,
// which the editor rejects once it has already answered that id.
var toolCallSeq atomic.Uint64

// toolCallsJSON renders parsed calls in the shape the editor expects back.
func toolCallsJSON(calls []textToolCall) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(calls))
	for i, call := range calls {
		arguments := call.RawArgs
		if arguments == "" {
			args, err := json.Marshal(call.Args)
			if err != nil {
				continue
			}
			arguments = string(args)
		}
		encoded, err := json.Marshal(map[string]any{
			"id":    fmt.Sprintf("call_%d", toolCallSeq.Add(1)),
			"type":  "function",
			"index": i,
			"function": map[string]any{
				"name":      call.Name,
				"arguments": arguments,
			},
		})
		if err != nil {
			continue
		}
		out = append(out, encoded)
	}
	return out
}

// toolTextFilter streams prose straight through but holds back anything from a
// '<' onwards until it knows whether it is a text tool invocation.
type toolTextFilter struct {
	held    strings.Builder
	holding bool
	emitted int
}

// push returns the text that is safe to stream now.
func (f *toolTextFilter) push(chunk string) string {
	if f.holding {
		f.held.WriteString(chunk)
		return ""
	}
	if idx := strings.IndexByte(chunk, '<'); idx >= 0 {
		f.holding = true
		f.held.WriteString(chunk[idx:])
		return chunk[:idx]
	}
	return chunk
}

// takeComplete returns invocations that have finished arriving. Waiting for the
// end of the stream lost them whenever the client hung up mid-turn, which
// stalled the editor's loop after a round or two.
func (f *toolTextFilter) takeComplete() []textToolCall {
	if !f.holding {
		return nil
	}
	all := parseTextToolCalls(f.held.String())
	if len(all) <= f.emitted {
		return nil
	}
	fresh := all[f.emitted:]
	f.emitted = len(all)
	return fresh
}

// finish returns any remaining prose and the tool calls found in the held text.
// It drains the filter, so calling it again yields nothing: the relay flushes
// once when the turn ends and once more at the terminator, and re-sending the
// same text duplicated the tail of the answer.
func (f *toolTextFilter) finish() (string, []textToolCall) {
	if !f.holding {
		return "", nil
	}
	held := f.held.String()
	f.held.Reset()
	f.holding = false

	calls := parseTextToolCalls(held)
	if len(calls) == 0 {
		// It was ordinary text that merely began with '<'.
		return held, nil
	}
	// Anything already delivered incrementally must not be sent twice.
	if f.emitted >= len(calls) {
		calls = nil
	} else {
		calls = calls[f.emitted:]
	}
	f.emitted = 0
	// Keep any prose that preceded the invocation.
	if loc := toolTextStart.FindStringIndex(held); loc != nil {
		return held[:loc[0]], calls
	}
	return "", calls
}
