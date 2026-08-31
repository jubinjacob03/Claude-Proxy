package translate

import (
	"encoding/json"
	"fmt"

	"claude-proxy/internal/anthropic"
	"claude-proxy/internal/openai"
)

// AnthropicRequestToOpenAI converts an Anthropic /v1/messages request into an
// OpenAI /v1/chat/completions request. model is the already-mapped upstream
// model id.
func AnthropicRequestToOpenAI(r *anthropic.Request, model string) *openai.Request {
	out := &openai.Request{
		Model:       model,
		Stream:      r.Stream,
		Temperature: r.Temperature,
		TopP:        r.TopP,
	}

	if r.MaxTokens > 0 {
		mt := r.MaxTokens
		out.MaxTokens = &mt
	}
	if len(r.StopSequences) > 0 {
		if b, err := json.Marshal(r.StopSequences); err == nil {
			out.Stop = b
		}
	}
	if sys := anthropic.SystemText(r.System); sys != "" {
		out.Messages = append(out.Messages, openai.Message{Role: "system", Content: jsonString(sys)})
	}
	for _, m := range r.Messages {
		out.Messages = append(out.Messages, convertAnthropicMessage(m)...)
	}
	for _, t := range r.Tools {
		if t.Name == "" {
			continue
		}
		params := t.InputSchema
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out.Tools = append(out.Tools, openai.Tool{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	if r.ToolChoice != nil {
		out.ToolChoice = anthropicToolChoiceToOpenAI(r.ToolChoice)
	}
	if r.Stream {
		out.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	}
	return out
}

// convertAnthropicMessage expands one Anthropic message into one or more OpenAI
// messages. tool_result blocks become standalone tool messages; tool_use blocks
// on an assistant turn become tool_calls.
func convertAnthropicMessage(m anthropic.Message) []openai.Message {
	blocks := m.Blocks()
	if len(blocks) == 0 {
		return nil
	}

	switch m.Role {
	case "assistant":
		return []openai.Message{convertAnthropicAssistant(blocks)}
	default: // user (and any tool-result-bearing turn)
		return convertAnthropicUser(blocks)
	}
}

func convertAnthropicAssistant(blocks []anthropic.ContentBlock) openai.Message {
	msg := openai.Message{Role: "assistant"}
	text := ""
	var toolCalls []openai.ToolCall
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text += b.Text
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
		// thinking / redacted_thinking are dropped: OpenAI has no equivalent
		// on the request side.
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		if text != "" {
			msg.Content = jsonString(text)
		}
	} else {
		msg.Content = jsonString(text)
	}
	return msg
}

func convertAnthropicUser(blocks []anthropic.ContentBlock) []openai.Message {
	var out []openai.Message

	// tool_result blocks become tool messages and must precede fresh user
	// content so they stay adjacent to the assistant tool_calls that produced
	// them.
	for _, b := range blocks {
		if b.Type == "tool_result" {
			content := anthropic.ToolResultText(b.Content)
			if b.IsError && content != "" {
				content = "[tool error] " + content
			}
			out = append(out, openai.Message{
				Role:       "tool",
				ToolCallID: b.ToolUseID,
				Content:    jsonString(content),
			})
		}
	}

	parts, plainText, hasImage := collectUserContent(blocks)
	if hasImage {
		if b, err := json.Marshal(parts); err == nil {
			out = append(out, openai.Message{Role: "user", Content: b})
		}
	} else if plainText != "" {
		out = append(out, openai.Message{Role: "user", Content: jsonString(plainText)})
	}
	return out
}

// collectUserContent gathers text and image blocks. When images are present it
// returns OpenAI content parts; otherwise it returns the concatenated text.
func collectUserContent(blocks []anthropic.ContentBlock) (parts []openai.ContentPart, text string, hasImage bool) {
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text += b.Text
			parts = append(parts, openai.ContentPart{Type: "text", Text: b.Text})
		case "image":
			if url := imageBlockToURL(b.Source); url != "" {
				hasImage = true
				parts = append(parts, openai.ContentPart{
					Type:     "image_url",
					ImageURL: &openai.ImageURL{URL: url},
				})
			}
		}
	}
	return parts, text, hasImage
}

func imageBlockToURL(src *anthropic.Source) string {
	if src == nil {
		return ""
	}
	switch src.Type {
	case "base64":
		if src.Data == "" {
			return ""
		}
		mt := src.MediaType
		if mt == "" {
			mt = "image/png"
		}
		return fmt.Sprintf("data:%s;base64,%s", mt, src.Data)
	case "url":
		return src.URL
	}
	return ""
}

func anthropicToolChoiceToOpenAI(tc *anthropic.ToolChoice) json.RawMessage {
	switch tc.Type {
	case "auto":
		return json.RawMessage(`"auto"`)
	case "any":
		return json.RawMessage(`"required"`)
	case "none":
		return json.RawMessage(`"none"`)
	case "tool":
		if tc.Name != "" {
			b, _ := json.Marshal(map[string]any{
				"type":     "function",
				"function": map[string]string{"name": tc.Name},
			})
			return b
		}
	}
	return json.RawMessage(`"auto"`)
}

// OpenAIResponseToAnthropic converts a non-streaming OpenAI chat completion into
// an Anthropic message response. model is echoed back to the client.
func OpenAIResponseToAnthropic(resp *openai.Response, model string) *anthropic.Response {
	out := &anthropic.Response{
		ID:           ensureID(resp.ID, "msg_"),
		Type:         "message",
		Role:         "assistant",
		Model:        model,
		StopSequence: nil,
	}

	var choice *openai.Choice
	if len(resp.Choices) > 0 {
		choice = &resp.Choices[0]
	}

	if choice != nil && choice.Message != nil {
		msg := choice.Message
		if msg.ReasoningContent != "" {
			out.Content = append(out.Content, anthropic.ContentBlock{
				Type:     "thinking",
				Thinking: msg.ReasoningContent,
			})
		}
		if text := msg.ContentText(); text != "" {
			out.Content = append(out.Content, anthropic.ContentBlock{Type: "text", Text: text})
		}
		for _, tc := range msg.ToolCalls {
			out.Content = append(out.Content, anthropic.ContentBlock{
				Type:  "tool_use",
				ID:    ensureID(tc.ID, "toolu_"),
				Name:  tc.Function.Name,
				Input: validObjectOrEmpty(json.RawMessage(tc.Function.Arguments)),
			})
		}
	}

	if len(out.Content) == 0 {
		out.Content = []anthropic.ContentBlock{{Type: "text", Text: ""}}
	}

	finish := ""
	if choice != nil && choice.FinishReason != nil {
		finish = *choice.FinishReason
	}
	out.StopReason = openAIFinishToAnthropic(finish)

	if resp.Usage != nil {
		out.Usage = anthropic.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
	}
	return out
}
