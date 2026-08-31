package translate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"claude-proxy/internal/openai"
)

// streamLines reads an SSE body line by line on a goroutine, delivering each raw
// line (newline included) on the returned channel and the terminating error
// (io.EOF on clean end) on the error channel.
func streamLines(body io.Reader) (<-chan string, <-chan error) {
	lines := make(chan string)
	errc := make(chan error, 1)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				errc <- fmt.Errorf("stream reader panic: %v", rec)
			}
		}()
		r := bufio.NewReaderSize(body, 1<<20)
		for {
			line, err := r.ReadString('\n')
			if len(line) > 0 {
				lines <- line
			}
			if err != nil {
				errc <- err
				return
			}
		}
	}()
	return lines, errc
}

// sseData extracts the payload of a `data:` line, reporting false for comments,
// blank lines, and `event:` lines.
func sseData(line string) (string, bool) {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimSpace(line[len("data:"):]), true
}

// ---------------------------------------------------------------------------
// OpenAI stream  ->  Anthropic SSE
// ---------------------------------------------------------------------------

// OpenAIStreamToAnthropic reads an OpenAI chat completion stream and writes the
// equivalent Anthropic Messages SSE event sequence. It emits ping events every
// ping interval while the upstream is silent so the client's stream watchdog
// never trips.
func OpenAIStreamToAnthropic(w io.Writer, flush func(), body io.Reader, model string, ping time.Duration) error {
	s := &oaToAn{w: w, flush: flush, model: model, openIndex: -1, toolBlock: map[int]int{}}
	s.start()

	lines, errc := streamLines(body)
	var tickC <-chan time.Time
	if ping > 0 {
		t := time.NewTicker(ping)
		defer t.Stop()
		tickC = t.C
	}

	for {
		select {
		case line := <-lines:
			data, ok := sseData(line)
			if !ok {
				continue
			}
			if data == "[DONE]" {
				s.finalize()
				return nil
			}
			var chunk openai.StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			s.onChunk(&chunk)
		case err := <-errc:
			s.finalize()
			if err == io.EOF {
				return nil
			}
			return err
		case <-tickC:
			s.ping()
		}
	}
}

type oaToAn struct {
	w     io.Writer
	flush func()
	model string

	msgID        string
	nextIndex    int
	openIndex    int
	openType     string
	toolBlock    map[int]int
	inputTokens  int
	outputTokens int
	stopReason   string
	finalized    bool
}

func (s *oaToAn) event(name string, payload map[string]any) {
	b, _ := json.Marshal(payload)
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, b)
	s.flush()
}

func (s *oaToAn) ping() {
	fmt.Fprint(s.w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
	s.flush()
}

func (s *oaToAn) start() {
	s.msgID = genID("msg_")
	s.event("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            s.msgID,
			"type":          "message",
			"role":          "assistant",
			"model":         s.model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": s.inputTokens, "output_tokens": 0},
		},
	})
}

func (s *oaToAn) onChunk(c *openai.StreamChunk) {
	if c.Usage != nil {
		if c.Usage.PromptTokens > 0 {
			s.inputTokens = c.Usage.PromptTokens
		}
		if c.Usage.CompletionTokens > 0 {
			s.outputTokens = c.Usage.CompletionTokens
		}
	}
	if len(c.Choices) == 0 {
		return
	}
	ch := c.Choices[0]
	if ch.Delta.ReasoningContent != nil && *ch.Delta.ReasoningContent != "" {
		s.emitThinking(*ch.Delta.ReasoningContent)
	}
	if ch.Delta.Content != nil && *ch.Delta.Content != "" {
		s.emitText(*ch.Delta.Content)
	}
	for _, tc := range ch.Delta.ToolCalls {
		s.emitToolCall(tc)
	}
	if ch.FinishReason != nil && *ch.FinishReason != "" {
		s.stopReason = openAIFinishToAnthropic(*ch.FinishReason)
	}
}

func (s *oaToAn) openBlock(blockType string, block map[string]any) int {
	s.closeOpen()
	idx := s.nextIndex
	s.nextIndex++
	s.event("content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         idx,
		"content_block": block,
	})
	s.openIndex = idx
	s.openType = blockType
	return idx
}

func (s *oaToAn) delta(index int, delta map[string]any) {
	s.event("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": index,
		"delta": delta,
	})
}

func (s *oaToAn) emitText(text string) {
	if s.openType != "text" {
		s.openBlock("text", map[string]any{"type": "text", "text": ""})
	}
	s.delta(s.openIndex, map[string]any{"type": "text_delta", "text": text})
}

func (s *oaToAn) emitThinking(text string) {
	if s.openType != "thinking" {
		s.openBlock("thinking", map[string]any{"type": "thinking", "thinking": ""})
	}
	s.delta(s.openIndex, map[string]any{"type": "thinking_delta", "thinking": text})
}

func (s *oaToAn) emitToolCall(tc openai.ToolCall) {
	idx := 0
	if tc.Index != nil {
		idx = *tc.Index
	}
	blockIdx, ok := s.toolBlock[idx]
	if !ok {
		blockIdx = s.openBlock("tool", map[string]any{
			"type":  "tool_use",
			"id":    ensureID(tc.ID, "toolu_"),
			"name":  tc.Function.Name,
			"input": map[string]any{},
		})
		s.toolBlock[idx] = blockIdx
	}
	if tc.Function.Arguments != "" {
		s.delta(blockIdx, map[string]any{"type": "input_json_delta", "partial_json": tc.Function.Arguments})
	}
}

func (s *oaToAn) closeOpen() {
	if s.openIndex >= 0 {
		s.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": s.openIndex})
		s.openIndex = -1
		s.openType = ""
	}
}

func (s *oaToAn) finalize() {
	if s.finalized {
		return
	}
	s.finalized = true
	s.closeOpen()
	if s.stopReason == "" {
		s.stopReason = "end_turn"
	}
	s.event("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": s.stopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": s.outputTokens},
	})
	s.event("message_stop", map[string]any{"type": "message_stop"})
}

// ---------------------------------------------------------------------------
// Anthropic stream  ->  OpenAI SSE
// ---------------------------------------------------------------------------

// AnthropicStreamToOpenAI reads an Anthropic Messages SSE stream and writes the
// equivalent OpenAI chat completion chunk sequence, terminating with
// data: [DONE]. When includeUsage is set a final usage-only chunk is emitted.
func AnthropicStreamToOpenAI(w io.Writer, flush func(), body io.Reader, model string, includeUsage bool, ping time.Duration) error {
	s := &anToOa{w: w, flush: flush, model: model, id: genID("chatcmpl-"), created: time.Now().Unix(), includeUsage: includeUsage, blockToTool: map[int]int{}}

	lines, errc := streamLines(body)
	var tickC <-chan time.Time
	if ping > 0 {
		t := time.NewTicker(ping)
		defer t.Stop()
		tickC = t.C
	}

	for {
		select {
		case line := <-lines:
			data, ok := sseData(line)
			if !ok {
				continue
			}
			if data == "[DONE]" {
				s.finalize()
				return nil
			}
			s.onEvent(data)
		case err := <-errc:
			s.finalize()
			if err == io.EOF {
				return nil
			}
			return err
		case <-tickC:
			fmt.Fprint(s.w, ": ping\n\n")
			s.flush()
		}
	}
}

type anToOa struct {
	w     io.Writer
	flush func()
	model string

	id           string
	created      int64
	includeUsage bool
	roleSent     bool
	toolCount    int
	blockToTool  map[int]int
	finish       string
	inputTokens  int
	outputTokens int
	finalized    bool
}

type anStreamEvent struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Index        int `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (s *anToOa) writeChunk(delta map[string]any, finish any) {
	chunk := map[string]any{
		"id":      s.id,
		"object":  "chat.completion.chunk",
		"created": s.created,
		"model":   s.model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finish,
		}},
	}
	b, _ := json.Marshal(chunk)
	fmt.Fprintf(s.w, "data: %s\n\n", b)
	s.flush()
}

func (s *anToOa) ensureRole() {
	if s.roleSent {
		return
	}
	s.roleSent = true
	s.writeChunk(map[string]any{"role": "assistant"}, nil)
}

func (s *anToOa) onEvent(data string) {
	var e anStreamEvent
	if err := json.Unmarshal([]byte(data), &e); err != nil {
		return
	}
	switch e.Type {
	case "message_start":
		if e.Message.ID != "" {
			s.id = ensureID(e.Message.ID, "chatcmpl-")
		}
		s.inputTokens = e.Message.Usage.InputTokens
		s.ensureRole()
	case "content_block_start":
		if e.ContentBlock.Type == "tool_use" {
			toolIdx := s.toolCount
			s.toolCount++
			s.blockToTool[e.Index] = toolIdx
			s.ensureRole()
			s.writeChunk(map[string]any{
				"tool_calls": []any{map[string]any{
					"index":    toolIdx,
					"id":       ensureID(e.ContentBlock.ID, "call_"),
					"type":     "function",
					"function": map[string]any{"name": e.ContentBlock.Name, "arguments": ""},
				}},
			}, nil)
		}
	case "content_block_delta":
		switch e.Delta.Type {
		case "text_delta":
			if e.Delta.Text != "" {
				s.ensureRole()
				s.writeChunk(map[string]any{"content": e.Delta.Text}, nil)
			}
		case "thinking_delta":
			if e.Delta.Thinking != "" {
				s.ensureRole()
				s.writeChunk(map[string]any{"reasoning_content": e.Delta.Thinking}, nil)
			}
		case "input_json_delta":
			toolIdx := s.blockToTool[e.Index]
			s.writeChunk(map[string]any{
				"tool_calls": []any{map[string]any{
					"index":    toolIdx,
					"function": map[string]any{"arguments": e.Delta.PartialJSON},
				}},
			}, nil)
		}
	case "message_delta":
		if e.Delta.StopReason != "" {
			s.finish = anthropicStopToOpenAI(e.Delta.StopReason)
		}
		if e.Usage != nil {
			s.outputTokens = e.Usage.OutputTokens
		}
	case "message_stop":
		s.finalize()
	}
}

func (s *anToOa) finalize() {
	if s.finalized {
		return
	}
	s.finalized = true
	s.ensureRole()
	if s.finish == "" {
		s.finish = "stop"
	}
	s.writeChunk(map[string]any{}, s.finish)
	if s.includeUsage {
		chunk := map[string]any{
			"id":      s.id,
			"object":  "chat.completion.chunk",
			"created": s.created,
			"model":   s.model,
			"choices": []any{},
			"usage": map[string]any{
				"prompt_tokens":     s.inputTokens,
				"completion_tokens": s.outputTokens,
				"total_tokens":      s.inputTokens + s.outputTokens,
			},
		}
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(s.w, "data: %s\n\n", b)
		s.flush()
	}
	fmt.Fprint(s.w, "data: [DONE]\n\n")
	s.flush()
}
