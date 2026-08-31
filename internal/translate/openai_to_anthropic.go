package translate

import (
	"encoding/json"
	"strings"
	"time"

	"claude-proxy/internal/anthropic"
	"claude-proxy/internal/openai"
)

// OpenAIRequestToAnthropic converts an OpenAI chat request into an Anthropic
// messages request. model is the already-mapped upstream model id;
// defaultMaxTokens fills Anthropic's required max_tokens when the client omits
// it.
func OpenAIRequestToAnthropic(r *openai.Request, model string, defaultMaxTokens int) *anthropic.Request {
	out := &anthropic.Request{
		Model:         model,
		Stream:        r.Stream,
		Temperature:   r.Temperature,
		TopP:          r.TopP,
		StopSequences: r.StopSequences(),
	}
	if mt := r.ResolveMaxTokens(); mt > 0 {
		out.MaxTokens = mt
	} else {
		out.MaxTokens = defaultMaxTokens
	}

	var systemText []string
	var turns []roleBlocks

	for _, m := range r.Messages {
		switch m.Role {
		case "system", "developer":
			if t := m.ContentText(); t != "" {
				systemText = append(systemText, t)
			}
		case "user":
			turns = append(turns, roleBlocks{role: "user", blocks: openAIUserToBlocks(m)})
		case "assistant":
			turns = append(turns, roleBlocks{role: "assistant", blocks: openAIAssistantToBlocks(m)})
		case "tool":
			block := anthropic.ContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   jsonString(m.ContentText()),
			}
			turns = append(turns, roleBlocks{role: "user", blocks: []anthropic.ContentBlock{block}})
		}
	}

	if len(systemText) > 0 {
		out.System = jsonString(strings.Join(systemText, "\n\n"))
	}
	out.Messages = mergeTurns(turns)

	for _, t := range r.Tools {
		if t.Type != "" && t.Type != "function" {
			continue
		}
		schema := t.Function.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out.Tools = append(out.Tools, anthropic.Tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: schema,
		})
	}
	if tc := openAIToolChoiceToAnthropic(r.ToolChoice); tc != nil {
		out.ToolChoice = tc
	}
	return out
}

type roleBlocks struct {
	role   string
	blocks []anthropic.ContentBlock
}

// mergeTurns collapses consecutive same-role turns into single messages, which
// keeps the message list valid for Anthropic (no two adjacent user/assistant
// turns) and groups tool results with any trailing user text.
func mergeTurns(turns []roleBlocks) []anthropic.Message {
	var msgs []anthropic.Message
	var cur *roleBlocks
	for _, t := range turns {
		if len(t.blocks) == 0 {
			continue
		}
		if cur != nil && cur.role == t.role {
			cur.blocks = append(cur.blocks, t.blocks...)
			continue
		}
		if cur != nil {
			msgs = append(msgs, materializeTurn(*cur))
		}
		c := t
		cur = &c
	}
	if cur != nil {
		msgs = append(msgs, materializeTurn(*cur))
	}
	return msgs
}

func materializeTurn(t roleBlocks) anthropic.Message {
	content, _ := json.Marshal(t.blocks)
	return anthropic.Message{Role: t.role, Content: content}
}

func openAIUserToBlocks(m openai.Message) []anthropic.ContentBlock {
	if parts := m.ContentParts(); parts != nil {
		var blocks []anthropic.ContentBlock
		for _, p := range parts {
			switch p.Type {
			case "text":
				blocks = append(blocks, anthropic.ContentBlock{Type: "text", Text: p.Text})
			case "image_url":
				if p.ImageURL != nil {
					blocks = append(blocks, imageURLToBlock(p.ImageURL.URL))
				}
			}
		}
		return blocks
	}
	if text := m.ContentText(); text != "" {
		return []anthropic.ContentBlock{{Type: "text", Text: text}}
	}
	return nil
}

func openAIAssistantToBlocks(m openai.Message) []anthropic.ContentBlock {
	var blocks []anthropic.ContentBlock
	if m.ReasoningContent != "" {
		blocks = append(blocks, anthropic.ContentBlock{Type: "thinking", Thinking: m.ReasoningContent})
	}
	if text := m.ContentText(); text != "" {
		blocks = append(blocks, anthropic.ContentBlock{Type: "text", Text: text})
	}
	for _, tc := range m.ToolCalls {
		blocks = append(blocks, anthropic.ContentBlock{
			Type:  "tool_use",
			ID:    ensureID(tc.ID, "toolu_"),
			Name:  tc.Function.Name,
			Input: validObjectOrEmpty(json.RawMessage(tc.Function.Arguments)),
		})
	}
	return blocks
}

func imageURLToBlock(u string) anthropic.ContentBlock {
	if media, data, ok := parseDataURL(u); ok {
		return anthropic.ContentBlock{
			Type: "image",
			Source: &anthropic.Source{
				Type:      "base64",
				MediaType: media,
				Data:      data,
			},
		}
	}
	return anthropic.ContentBlock{
		Type:   "image",
		Source: &anthropic.Source{Type: "url", URL: u},
	}
}

func parseDataURL(u string) (media, data string, ok bool) {
	if !strings.HasPrefix(u, "data:") {
		return "", "", false
	}
	rest := u[len("data:"):]
	marker := ";base64,"
	idx := strings.Index(rest, marker)
	if idx < 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+len(marker):], true
}

func openAIToolChoiceToAnthropic(raw json.RawMessage) *anthropic.ToolChoice {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch s {
		case "auto":
			return &anthropic.ToolChoice{Type: "auto"}
		case "required":
			return &anthropic.ToolChoice{Type: "any"}
		case "none":
			return &anthropic.ToolChoice{Type: "none"}
		}
		return nil
	}
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if obj.Function.Name != "" {
			return &anthropic.ToolChoice{Type: "tool", Name: obj.Function.Name}
		}
	}
	return nil
}

// AnthropicResponseToOpenAI converts a non-streaming Anthropic message response
// into an OpenAI chat completion.
func AnthropicResponseToOpenAI(resp *anthropic.Response, model string) *openai.Response {
	msg := &openai.Message{Role: "assistant"}

	text := ""
	reasoning := ""
	var toolCalls []openai.ToolCall
	for _, b := range resp.Content {
		switch b.Type {
		case "text":
			text += b.Text
		case "thinking":
			reasoning += b.Thinking
		case "tool_use":
			toolCalls = append(toolCalls, openai.ToolCall{
				ID:   ensureID(b.ID, "call_"),
				Type: "function",
				Function: openai.FunctionCall{
					Name:      b.Name,
					Arguments: string(validObjectOrEmpty(b.Input)),
				},
			})
		}
	}

	if reasoning != "" {
		msg.ReasoningContent = reasoning
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		if text != "" {
			msg.Content = jsonString(text)
		}
	} else {
		msg.Content = jsonString(text)
	}

	finish := anthropicStopToOpenAI(resp.StopReason)
	out := &openai.Response{
		ID:      ensureID(resp.ID, "chatcmpl-"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openai.Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: &finish,
		}},
		Usage: &openai.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
	return out
}
