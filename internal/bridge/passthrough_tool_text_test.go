package bridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

type sseFrame struct {
	data string
}

func parseSSE(t *testing.T, body string) []sseFrame {
	t.Helper()
	sc := bufio.NewScanner(strings.NewReader(body))
	frames := make([]sseFrame, 0)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if payload, ok := strings.CutPrefix(line, "data: "); ok {
			frames = append(frames, sseFrame{data: payload})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan sse: %v", err)
	}
	return frames
}

// This reproduces the observed failure: the model
// streams prose and then writes its tool calls as text. The editor executes
// structured calls only, so untranslated it renders the markup as prose and
// abandons the turn.
func TestPassthroughTranslatesTextToolCallsForTheEditor(t *testing.T) {
	prose := "I'll analyze the codebase. Let me start by exploring the project structure."
	markup := buildToolText("LS", map[string]string{"path": `d:\My-Projects\Claude-Proxy`})

	var upstream strings.Builder
	for _, part := range []string{prose, markup} {
		for _, piece := range chunkString(part, 12) {
			payload, err := json.Marshal(map[string]any{
				"id": "up-1", "object": "chat.completion.chunk", "created": 1, "model": "claude",
				"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": piece}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&upstream, "data: %s\n\n", payload)
		}
	}
	fmt.Fprint(&upstream, "data: [DONE]\n\n")

	rr := httptest.NewRecorder()
	relayOpenAIStream(rr, func() {}, strings.NewReader(upstream.String()), "claude-opus-5")
	body := rr.Body.String()

	var content string
	var toolCalls int
	var finish string
	for _, f := range parseSSE(t, body) {
		if f.data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string            `json:"content"`
					ToolCalls []json.RawMessage `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(f.data), &chunk); err != nil {
			t.Fatalf("the editor received an unparseable frame: %s", f.data)
		}
		for _, c := range chunk.Choices {
			content += c.Delta.Content
			toolCalls += len(c.Delta.ToolCalls)
			if c.FinishReason != nil {
				finish = *c.FinishReason
			}
		}
	}

	if toolCalls != 1 {
		t.Errorf("got %d structured tool calls, want 1; the editor cannot act on markup", toolCalls)
	}
	if finish != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls so the editor runs them", finish)
	}
	if !strings.Contains(content, "exploring the project structure") {
		t.Errorf("the prose answer was lost: %q", content)
	}
	if strings.Contains(content, "invoke name") {
		t.Errorf("raw tool markup reached the user: %q", content)
	}
}

// A normal stream must still pass through untouched.
func TestPassthroughLeavesOrdinaryStreamsAlone(t *testing.T) {
	upstream := "data: {\"id\":\"up-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"a < b and Map<K,V>\"}}]}\n\n" +
		"data: {\"id\":\"up-1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	rr := httptest.NewRecorder()
	relayOpenAIStream(rr, func() {}, strings.NewReader(upstream), "claude-opus-5")

	var content, finish string
	for _, f := range parseSSE(t, rr.Body.String()) {
		if f.data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(f.data), &chunk); err != nil {
			t.Fatalf("unparseable frame: %s", f.data)
		}
		for _, c := range chunk.Choices {
			content += c.Delta.Content
			if c.FinishReason != nil {
				finish = *c.FinishReason
			}
		}
	}

	if content != "a < b and Map<K,V>" {
		t.Errorf("ordinary content was altered: %q", content)
	}
	if finish != "stop" {
		t.Errorf("finish_reason = %q, want stop", finish)
	}
}

// Structured tool calls from a well-behaved model must survive untouched.
func TestPassthroughKeepsStructuredToolCalls(t *testing.T) {
	upstream := `data: {"id":"up-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"view_file","arguments":"{}"}}]}}]}` + "\n\n" +
		`data: {"id":"up-1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	rr := httptest.NewRecorder()
	relayOpenAIStream(rr, func() {}, strings.NewReader(upstream), "claude-opus-5")

	if !strings.Contains(rr.Body.String(), `"name":"view_file"`) {
		t.Errorf("a structured tool call was dropped:\n%s", rr.Body.String())
	}
}

// A client that hangs up mid-turn used to lose the calls entirely, because they
// were only emitted once the stream finished. Two rounds in, the editor's loop
// had nothing to run and stopped.
func TestToolCallsSurviveATruncatedStream(t *testing.T) {
	markup := buildToolText("LS", map[string]string{"path": "."})

	var upstream strings.Builder
	for _, piece := range chunkString("Looking now."+markup, 9) {
		payload, err := json.Marshal(map[string]any{
			"id": "up-1", "object": "chat.completion.chunk", "created": 1, "model": "claude",
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": piece}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&upstream, "data: %s\n\n", payload)
	}
	// No [DONE]: the connection dies right after the invocation completes.

	rr := httptest.NewRecorder()
	relayOpenAIStream(rr, func() {}, strings.NewReader(upstream.String()), "claude-opus-5")

	body := rr.Body.String()
	if !strings.Contains(body, `"name":"LS"`) {
		t.Errorf("the tool call was lost when the stream was cut:\n%s", body)
	}
	if !strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Errorf("the editor was never told to run the call:\n%s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("the stream was not terminated")
	}
}

// Delivering a call incrementally and again at the end would make the editor
// run the same tool twice.
func TestToolCallsAreNotDeliveredTwice(t *testing.T) {
	markup := buildToolText("Grep", map[string]string{"pattern": "deadline"})

	var upstream strings.Builder
	for _, piece := range chunkString(markup, 7) {
		payload, err := json.Marshal(map[string]any{
			"id": "up-1", "object": "chat.completion.chunk", "created": 1, "model": "claude",
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": piece}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&upstream, "data: %s\n\n", payload)
	}
	fmt.Fprint(&upstream, "data: [DONE]\n\n")

	rr := httptest.NewRecorder()
	relayOpenAIStream(rr, func() {}, strings.NewReader(upstream.String()), "claude-opus-5")

	if got := strings.Count(rr.Body.String(), `"name":"Grep"`); got != 1 {
		t.Errorf("the tool call was delivered %d times, want once:\n%s", got, rr.Body.String())
	}
}

// The upstream ends its own turn with "stop". Forwarding that alongside our
// tool_calls finish told the editor the turn was over with nothing to run, so
// it printed the prose and the agent loop died after a round or two.
func TestUpstreamStopIsNotSentAlongsideToolCalls(t *testing.T) {
	markup := buildToolText("LS", map[string]string{"path": "."})

	var upstream strings.Builder
	for _, piece := range chunkString("Exploring now."+markup, 10) {
		payload, err := json.Marshal(map[string]any{
			"id": "up-1", "object": "chat.completion.chunk", "created": 1, "model": "claude",
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": piece}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&upstream, "data: %s\n\n", payload)
	}
	// The upstream's own end-of-turn, which must not reach the client.
	fmt.Fprint(&upstream, `data: {"id":"up-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
	fmt.Fprint(&upstream, "data: [DONE]\n\n")

	rr := httptest.NewRecorder()
	relayOpenAIStream(rr, func() {}, strings.NewReader(upstream.String()), "claude-opus-5")

	var reasons []string
	for _, f := range parseSSE(t, rr.Body.String()) {
		if f.data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(f.data), &chunk) != nil {
			continue
		}
		for _, c := range chunk.Choices {
			if c.FinishReason != nil && *c.FinishReason != "" {
				reasons = append(reasons, *c.FinishReason)
			}
		}
	}

	if len(reasons) != 1 {
		t.Fatalf("turn ended with %d stop reasons %v, want exactly 1", len(reasons), reasons)
	}
	if reasons[0] != "tool_calls" {
		t.Errorf("stop reason = %q, want tool_calls so the editor runs the call", reasons[0])
	}
}

// A turn with no tool calls must still report the upstream's own stop reason.
func TestUpstreamStopSurvivesWhenThereAreNoToolCalls(t *testing.T) {
	upstream := `data: {"id":"up-1","choices":[{"index":0,"delta":{"content":"done"}}]}` + "\n\n" +
		`data: {"id":"up-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	rr := httptest.NewRecorder()
	relayOpenAIStream(rr, func() {}, strings.NewReader(upstream), "claude-opus-5")

	if !strings.Contains(rr.Body.String(), `"finish_reason":"stop"`) {
		t.Errorf("an ordinary turn lost its stop reason:\n%s", rr.Body.String())
	}
}

// The editor answers a tool call by id. Reusing call_1 every turn put a
// duplicate id into the same conversation, and the loop died on the next round.
func TestToolCallIdsAreUniqueAcrossTurns(t *testing.T) {
	markup := buildToolText("LS", map[string]string{"path": "."})
	stream := func() string {
		payload, err := json.Marshal(map[string]any{
			"id": "up-1", "object": "chat.completion.chunk", "created": 1, "model": "claude",
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": markup}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("data: %s\n\ndata: [DONE]\n\n", payload)
	}

	seen := map[string]bool{}
	for turn := 0; turn < 3; turn++ {
		rr := httptest.NewRecorder()
		relayOpenAIStream(rr, func() {}, strings.NewReader(stream()), "claude-opus-5")

		for _, f := range parseSSE(t, rr.Body.String()) {
			if f.data == "[DONE]" {
				continue
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						ToolCalls []struct {
							ID string `json:"id"`
						} `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(f.data), &chunk) != nil {
				continue
			}
			for _, c := range chunk.Choices {
				for _, call := range c.Delta.ToolCalls {
					if seen[call.ID] {
						t.Fatalf("tool call id %q was reused on turn %d", call.ID, turn+1)
					}
					seen[call.ID] = true
				}
			}
		}
	}

	if len(seen) != 3 {
		t.Errorf("saw %d distinct tool call ids across 3 turns, want 3", len(seen))
	}
}

// Held text has to reach the client before the turn ends. Flushing it after the
// stop frame put content on a message the client had already finalised, so the
// tail of the answer was silently dropped.
func TestHeldTextIsFlushedBeforeTheStopFrame(t *testing.T) {
	// Text that begins with '<' but is not a tool call, so it gets held.
	tail := "<note>this trailing text matters</note>"

	var upstream strings.Builder
	payload, err := json.Marshal(map[string]any{
		"id": "up-1", "object": "chat.completion.chunk", "created": 1, "model": "claude",
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": "answer " + tail}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&upstream, "data: %s\n\n", payload)
	fmt.Fprint(&upstream, `data: {"id":"up-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
	fmt.Fprint(&upstream, "data: [DONE]\n\n")

	rr := httptest.NewRecorder()
	relayOpenAIStream(rr, func() {}, strings.NewReader(upstream.String()), "claude-opus-5")

	frames := parseSSE(t, rr.Body.String())
	stopAt, contentAt := -1, -1
	for i, f := range frames {
		if f.data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(f.data), &chunk) != nil {
			continue
		}
		for _, c := range chunk.Choices {
			if strings.Contains(c.Delta.Content, "trailing text matters") {
				contentAt = i
			}
			if c.FinishReason != nil && *c.FinishReason != "" {
				if stopAt == -1 {
					stopAt = i
				}
			}
		}
	}

	if contentAt == -1 {
		t.Fatalf("the held tail never reached the client:\n%s", rr.Body.String())
	}
	if stopAt == -1 {
		t.Fatalf("the turn never ended:\n%s", rr.Body.String())
	}
	if contentAt > stopAt {
		t.Errorf("content was sent after the stop frame (content at %d, stop at %d)", contentAt, stopAt)
	}
}

func chunkString(s string, size int) []string {
	var out []string
	for len(s) > size {
		out = append(out, s[:size])
		s = s[size:]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}
