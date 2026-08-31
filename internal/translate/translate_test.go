package translate

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"claude-proxy/internal/anthropic"
	"claude-proxy/internal/openai"
)

func TestAnthropicRequestToOpenAIMapsSystemToolsAndChoice(t *testing.T) {
	req := &anthropic.Request{
		Model:     "claude-x",
		MaxTokens: 200,
		System:    json.RawMessage(`"be terse"`),
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"weather in Paris?"`)},
		},
		Tools: []anthropic.Tool{{
			Name:        "get_weather",
			Description: "Get weather",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
		ToolChoice: &anthropic.ToolChoice{Type: "any"},
	}

	out := AnthropicRequestToOpenAI(req, "claude-opus-4-8")

	if out.Model != "claude-opus-4-8" {
		t.Errorf("model = %q, want the mapped upstream id", out.Model)
	}
	if len(out.Messages) != 2 || out.Messages[0].Role != "system" {
		t.Fatalf("system prompt should become the first message: %+v", out.Messages)
	}
	if out.Messages[1].ContentText() != "weather in Paris?" {
		t.Errorf("user text = %q", out.Messages[1].ContentText())
	}
	if len(out.Tools) != 1 || out.Tools[0].Function.Name != "get_weather" {
		t.Errorf("tools not translated: %+v", out.Tools)
	}
	if string(out.ToolChoice) != `"required"` {
		t.Errorf("tool_choice = %s, want \"required\" for type any", out.ToolChoice)
	}
	if out.MaxTokens == nil || *out.MaxTokens != 200 {
		t.Errorf("max_tokens not carried over")
	}
}

func TestAnthropicToolResultBecomesToolMessage(t *testing.T) {
	req := &anthropic.Request{
		Model:     "m",
		MaxTokens: 10,
		Messages: []anthropic.Message{
			{Role: "assistant", Content: json.RawMessage(`[
				{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Paris"}}
			]`)},
			{Role: "user", Content: json.RawMessage(`[
				{"type":"tool_result","tool_use_id":"toolu_1","content":"18C"}
			]`)},
		},
	}

	out := AnthropicRequestToOpenAI(req, "m")

	if len(out.Messages) != 2 {
		t.Fatalf("want assistant + tool message, got %d: %+v", len(out.Messages), out.Messages)
	}
	if len(out.Messages[0].ToolCalls) != 1 || out.Messages[0].ToolCalls[0].ID != "toolu_1" {
		t.Errorf("tool_use should become tool_calls: %+v", out.Messages[0])
	}
	tool := out.Messages[1]
	if tool.Role != "tool" || tool.ToolCallID != "toolu_1" || tool.ContentText() != "18C" {
		t.Errorf("tool_result should become a tool message: %+v", tool)
	}
}

func TestOpenAIRequestToAnthropicMergesSystemAndDefaultsMaxTokens(t *testing.T) {
	req := &openai.Request{
		Model: "gpt",
		Messages: []openai.Message{
			{Role: "system", Content: json.RawMessage(`"a"`)},
			{Role: "developer", Content: json.RawMessage(`"b"`)},
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	}

	out := OpenAIRequestToAnthropic(req, "claude-opus-4-8", 4096)

	if out.MaxTokens != 4096 {
		t.Errorf("max_tokens = %d, want the injected default", out.MaxTokens)
	}
	if got := anthropic.SystemText(out.System); got != "a\n\nb" {
		t.Errorf("system = %q, want both system/developer messages merged", got)
	}
	if len(out.Messages) != 1 || out.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v", out.Messages)
	}
}

func TestOpenAIToAnthropicMergesConsecutiveSameRoleTurns(t *testing.T) {
	req := &openai.Request{
		Model: "gpt",
		Messages: []openai.Message{
			{Role: "assistant", ToolCalls: []openai.ToolCall{{
				ID: "call_1", Type: "function",
				Function: openai.FunctionCall{Name: "f", Arguments: `{"x":1}`},
			}}},
			{Role: "tool", ToolCallID: "call_1", Content: json.RawMessage(`"result"`)},
			{Role: "user", Content: json.RawMessage(`"and now?"`)},
		},
	}

	out := OpenAIRequestToAnthropic(req, "m", 100)

	// tool result + following user text must collapse into one user turn so the
	// Anthropic message list never has two adjacent user messages.
	if len(out.Messages) != 2 {
		t.Fatalf("want assistant + merged user turn, got %d", len(out.Messages))
	}
	blocks := out.Messages[1].Blocks()
	if len(blocks) != 2 || blocks[0].Type != "tool_result" || blocks[1].Type != "text" {
		t.Errorf("merged blocks = %+v", blocks)
	}
}

func TestImageRoundTripBase64(t *testing.T) {
	dataURL := "data:image/png;base64,iVBORw0KGgo="
	req := &openai.Request{
		Model: "gpt",
		Messages: []openai.Message{{
			Role: "user",
			Content: json.RawMessage(`[
				{"type":"text","text":"what is this?"},
				{"type":"image_url","image_url":{"url":"` + dataURL + `"}}
			]`),
		}},
	}

	an := OpenAIRequestToAnthropic(req, "m", 100)
	blocks := an.Messages[0].Blocks()
	if len(blocks) != 2 || blocks[1].Type != "image" || blocks[1].Source == nil {
		t.Fatalf("image block missing: %+v", blocks)
	}
	if blocks[1].Source.MediaType != "image/png" || blocks[1].Source.Data != "iVBORw0KGgo=" {
		t.Errorf("data URL not decoded: %+v", blocks[1].Source)
	}

	// and back again
	back := AnthropicRequestToOpenAI(an, "m")
	if !strings.Contains(back.Messages[0].ContentText()+string(back.Messages[0].Content), "data:image/png;base64,") {
		t.Errorf("image not restored as a data URL: %s", back.Messages[0].Content)
	}
}

func TestOpenAIResponseToAnthropicMapsToolCallsAndUsage(t *testing.T) {
	finish := "tool_calls"
	resp := &openai.Response{
		ID: "chatcmpl-1",
		Choices: []openai.Choice{{
			Message: &openai.Message{
				Role:             "assistant",
				Content:          json.RawMessage(`"thinking done"`),
				ReasoningContent: "because",
				ToolCalls: []openai.ToolCall{{
					ID: "call_1", Type: "function",
					Function: openai.FunctionCall{Name: "f", Arguments: `{"a":1}`},
				}},
			},
			FinishReason: &finish,
		}},
		Usage: &openai.Usage{PromptTokens: 11, CompletionTokens: 3},
	}

	out := OpenAIResponseToAnthropic(resp, "claude-x")

	if out.Model != "claude-x" || out.Role != "assistant" || out.Type != "message" {
		t.Errorf("envelope = %+v", out)
	}
	if out.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", out.StopReason)
	}
	kinds := []string{}
	for _, b := range out.Content {
		kinds = append(kinds, b.Type)
	}
	want := "thinking,text,tool_use"
	if strings.Join(kinds, ",") != want {
		t.Errorf("content blocks = %v, want %s", kinds, want)
	}
	if out.Usage.InputTokens != 11 || out.Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v", out.Usage)
	}
}

func TestAnthropicResponseToOpenAIMapsStopReason(t *testing.T) {
	resp := &anthropic.Response{
		ID:         "msg_1",
		StopReason: "max_tokens",
		Content:    []anthropic.ContentBlock{{Type: "text", Text: "hello"}},
		Usage:      anthropic.Usage{InputTokens: 5, OutputTokens: 2},
	}

	out := AnthropicResponseToOpenAI(resp, "gpt")

	if out.Object != "chat.completion" || len(out.Choices) != 1 {
		t.Fatalf("envelope = %+v", out)
	}
	if got := out.Choices[0].FinishReason; got == nil || *got != "length" {
		t.Errorf("finish_reason = %v, want length", got)
	}
	if out.Usage.TotalTokens != 7 {
		t.Errorf("total_tokens = %d, want 7", out.Usage.TotalTokens)
	}
}

// --- streaming ------------------------------------------------------------

func sse(lines ...string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("data: " + l + "\n\n")
	}
	return b.String()
}

func TestOpenAIStreamToAnthropicEmitsFullEventSequence(t *testing.T) {
	in := sse(
		`{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`,
		`[DONE]`,
	)

	var out bytes.Buffer
	if err := OpenAIStreamToAnthropic(&out, func() {}, strings.NewReader(in), "claude-x", 0); err != nil {
		t.Fatalf("stream: %v", err)
	}
	got := out.String()

	for _, want := range []string{
		"event: message_start", "event: content_block_start",
		`"text":"Hel"`, `"type":"text_delta"`, `"text":"lo"`,
		"event: content_block_stop", `"stop_reason":"end_turn"`, "event: message_stop",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestOpenAIStreamToAnthropicToolCallDeltas(t *testing.T) {
	in := sse(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"f","arguments":"{\"a\""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":1}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	)

	var out bytes.Buffer
	if err := OpenAIStreamToAnthropic(&out, func() {}, strings.NewReader(in), "m", 0); err != nil {
		t.Fatalf("stream: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, `"type":"tool_use"`) || !strings.Contains(got, `"name":"f"`) {
		t.Errorf("tool_use block missing:\n%s", got)
	}
	if !strings.Contains(got, "input_json_delta") || !strings.Contains(got, `:1}`) {
		t.Errorf("argument deltas missing:\n%s", got)
	}
	if !strings.Contains(got, `"stop_reason":"tool_use"`) {
		t.Errorf("stop_reason should be tool_use:\n%s", got)
	}
}

func TestAnthropicStreamToOpenAIEmitsChunksAndDone(t *testing.T) {
	in := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":6}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	var out bytes.Buffer
	if err := AnthropicStreamToOpenAI(&out, func() {}, strings.NewReader(in), "gpt", true, 0); err != nil {
		t.Fatalf("stream: %v", err)
	}
	got := out.String()

	for _, want := range []string{
		`"role":"assistant"`, `"content":"Hi"`, `"finish_reason":"stop"`,
		`"prompt_tokens":6`, `"completion_tokens":1`, "data: [DONE]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestAnthropicStreamThinkingBecomesReasoningContent(t *testing.T) {
	in := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1"}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	var out bytes.Buffer
	if err := AnthropicStreamToOpenAI(&out, func() {}, strings.NewReader(in), "gpt", false, 0); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if !strings.Contains(out.String(), `"reasoning_content":"hmm"`) {
		t.Errorf("thinking should map to reasoning_content:\n%s", out.String())
	}
}
