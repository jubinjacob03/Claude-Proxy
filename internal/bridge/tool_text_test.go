package bridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// buildToolText assembles the shape a model produces when it writes tool calls
// as text. It is built from parts so the literal markup cannot confuse tooling
// that reads this file.
func buildToolText(name string, params map[string]string) string {
	open := "<" + "invoke name=" + fmt.Sprintf("%q", name) + ">"
	closeTag := "</" + "invoke>"
	var b strings.Builder
	b.WriteString("<" + "function_calls>")
	b.WriteString(open)
	for k, v := range params {
		b.WriteString("<" + "parameter name=" + fmt.Sprintf("%q", k) + ">")
		b.WriteString(v)
		b.WriteString("</" + "parameter>")
	}
	b.WriteString(closeTag)
	b.WriteString("</" + "function_calls>")
	return b.String()
}

// The exact failure seen in a real run: prose, then tool invocations written as
// text. The editor executes structured calls only, so as text the whole turn
// renders as prose and the agent loop stalls at the first step.
func realWorldToolText() string {
	return "I'll analyze the codebase and find a fix. Let me start by exploring the project structure." +
		buildToolText("LS", map[string]string{"path": `d:\My-Projects\Claude-Proxy`})
}

func TestParsesToolCallsTheModelWroteAsText(t *testing.T) {
	calls := parseTextToolCalls(realWorldToolText())
	if len(calls) != 1 {
		t.Fatalf("found %d tool calls, want 1", len(calls))
	}
	if calls[0].Name != "LS" {
		t.Errorf("call name = %q, want LS", calls[0].Name)
	}
	if got := calls[0].Args["path"]; got != `d:\My-Projects\Claude-Proxy` {
		t.Errorf("path = %q", got)
	}
}

func TestParsesMultipleTextToolCalls(t *testing.T) {
	text := buildToolText("LS", map[string]string{"path": "."}) +
		buildToolText("Grep", map[string]string{"pattern": "deadline"})

	calls := parseTextToolCalls(text)
	if len(calls) != 2 {
		t.Fatalf("found %d tool calls, want 2", len(calls))
	}
	if calls[0].Name != "LS" || calls[1].Name != "Grep" {
		t.Errorf("names = %q, %q", calls[0].Name, calls[1].Name)
	}
}

func TestTextToolCallsRenderInTheEditorsShape(t *testing.T) {
	encoded := toolCallsJSON(parseTextToolCalls(realWorldToolText()))
	if len(encoded) != 1 {
		t.Fatalf("encoded %d calls, want 1", len(encoded))
	}

	var got struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(encoded[0], &got); err != nil {
		t.Fatalf("unreadable tool call: %v", err)
	}
	if got.Type != "function" || got.Function.Name != "LS" || got.ID == "" {
		t.Errorf("malformed tool call: %+v", got)
	}

	// Arguments must be a JSON string; the API contract requires that shape.
	var args map[string]string
	if err := json.Unmarshal([]byte(got.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments are not a JSON string: %v", err)
	}
	if args["path"] == "" {
		t.Error("arguments lost the path")
	}
}

// The prose before the invocation is a real answer and must still stream.
func TestFilterStreamsProseAndCapturesToolText(t *testing.T) {
	f := &toolTextFilter{}
	var streamed strings.Builder
	for _, chunk := range strings.Split(realWorldToolText(), "") {
		streamed.WriteString(f.push(chunk))
	}
	trailing, calls := f.finish()
	streamed.WriteString(trailing)

	if len(calls) != 1 {
		t.Fatalf("filter captured %d calls, want 1", len(calls))
	}
	if !strings.Contains(streamed.String(), "exploring the project structure") {
		t.Errorf("the prose was swallowed: %q", streamed.String())
	}
	if strings.Contains(streamed.String(), "invoke name") {
		t.Errorf("raw tool markup leaked to the user: %q", streamed.String())
	}
}

// Ordinary text containing '<' must not be held back or mangled.
func TestFilterPassesOrdinaryAngleBracketsThrough(t *testing.T) {
	f := &toolTextFilter{}
	input := "use Map<String, int> and compare a < b"

	var streamed strings.Builder
	for _, chunk := range strings.Split(input, "") {
		streamed.WriteString(f.push(chunk))
	}
	trailing, calls := f.finish()
	streamed.WriteString(trailing)

	if len(calls) != 0 {
		t.Errorf("plain text was mistaken for tool calls: %+v", calls)
	}
	if streamed.String() != input {
		t.Errorf("text was altered:\n got %q\nwant %q", streamed.String(), input)
	}
}

// Code blocks are full of angle brackets and must survive untouched.
func TestFilterPreservesCodeContainingMarkup(t *testing.T) {
	f := &toolTextFilter{}
	input := "```go\nif a<b { fmt.Println(\"<div>\") }\n```"

	var streamed strings.Builder
	for _, chunk := range strings.Split(input, "") {
		streamed.WriteString(f.push(chunk))
	}
	trailing, _ := f.finish()
	streamed.WriteString(trailing)

	if streamed.String() != input {
		t.Errorf("code was altered:\n got %q\nwant %q", streamed.String(), input)
	}
}
