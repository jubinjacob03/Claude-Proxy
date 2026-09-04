package bridge

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// filterFrameContent lets a frame through unchanged unless it carries content
// that has begun a text tool invocation. Only the content field is touched; a
// frame with nothing left to say is skipped entirely.
func filterFrameContent(frame []byte, filter *toolTextFilter) (out []byte, skip bool) {
	var probe struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(frame, &probe); err != nil {
		return frame, false
	}
	if len(probe.Choices) == 0 || probe.Choices[0].Delta.Content == "" {
		return frame, false
	}

	safe := filter.push(probe.Choices[0].Delta.Content)
	if safe == probe.Choices[0].Delta.Content {
		return frame, false
	}
	if safe == "" {
		return nil, true
	}
	return replaceFrameContent(frame, safe), false
}

func replaceFrameContent(frame []byte, content string) []byte {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(frame, &envelope); err != nil {
		return frame
	}
	var choices []map[string]json.RawMessage
	if err := json.Unmarshal(envelope["choices"], &choices); err != nil || len(choices) == 0 {
		return frame
	}
	var delta map[string]json.RawMessage
	if err := json.Unmarshal(choices[0]["delta"], &delta); err != nil {
		return frame
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return frame
	}
	delta["content"] = encoded

	if choices[0]["delta"], err = json.Marshal(delta); err != nil {
		return frame
	}
	if envelope["choices"], err = json.Marshal(choices); err != nil {
		return frame
	}
	out, err := json.Marshal(envelope)
	if err != nil {
		return frame
	}
	return out
}

// frameHasFinishReason reports whether a frame ends the turn. Once tool calls
// have been sent, an upstream "stop" would tell the client the turn finished
// with nothing to run, so it must not be forwarded.
func frameHasFinishReason(frame []byte) bool {
	var probe struct {
		Choices []struct {
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(frame, &probe); err != nil {
		return false
	}
	for _, c := range probe.Choices {
		if c.FinishReason != nil && *c.FinishReason != "" {
			return true
		}
	}
	return false
}

// emitPendingToolText flushes anything the filter held back. Tool calls the
// model wrote as text are re-emitted as structured calls so the client can run
// them; held text that turned out to be ordinary prose is sent as content.
// It reports whether any tool calls were written.
func emitPendingToolText(w io.Writer, filter *toolTextFilter, template []byte, clientModel, forceID string, turn uint64) bool {
	trailing, calls := filter.finish()
	if trailing != "" {
		frame := frameWithDelta(template, map[string]any{"content": trailing}, nil, clientModel, forceID)
		traceOut(turn, "held text was ordinary prose", string(frame))
		fmt.Fprintf(w, "data: %s\n\n", frame)
	}
	if len(calls) == 0 {
		return false
	}
	frame := frameWithDelta(template, map[string]any{"tool_calls": toolCallsJSON(calls)}, nil, clientModel, forceID)
	traceOut(turn, "tool_calls(final)", string(frame))
	fmt.Fprintf(w, "data: %s\n\n", frame)
	return true
}

// emitToolCallStop tells the client the turn ended so it can run the calls.
func emitToolCallStop(w io.Writer, template []byte, clientModel, forceID string, turn uint64) {
	stop := "tool_calls"
	frame := frameWithDelta(template, map[string]any{}, &stop, clientModel, forceID)
	traceOut(turn, "finish_reason=tool_calls", string(frame))
	fmt.Fprintf(w, "data: %s\n\n", frame)
}

// frameWithDelta builds a chunk that matches the identity of the stream it
// joins, so the client does not see the message change part way through.
func frameWithDelta(template []byte, delta map[string]any, finish *string, clientModel, forceID string) []byte {
	id := forceID
	created := int64(0)
	model := clientModel

	if len(template) > 0 {
		var prev struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
			Model   string `json:"model"`
		}
		if json.Unmarshal(template, &prev) == nil {
			if id == "" {
				id = prev.ID
			}
			created = prev.Created
			if model == "" {
				model = prev.Model
			}
		}
	}

	choice := map[string]any{"index": 0, "delta": delta}
	if finish != nil {
		choice["finish_reason"] = *finish
	} else {
		choice["finish_reason"] = nil
	}

	out, err := json.Marshal(map[string]any{
		"id": id, "object": "chat.completion.chunk", "created": created,
		"model": model, "choices": []map[string]any{choice},
	})
	if err != nil {
		return []byte("{}")
	}
	return out
}

var _ = strings.TrimSpace
