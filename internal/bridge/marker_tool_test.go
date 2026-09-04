package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

// The relay teaches models without a native tool API a marker protocol, while
// the editor's own prompt teaches an XML style. A model may follow either, so
// both must translate into structured calls.
func TestMarkerProtocolToolCallIsTranslated(t *testing.T) {
	text := "Let me look.\n" + markerToolOpen +
		`{"name":"view_file","arguments":{"path":"main.go","limit":100}}` +
		markerToolClose

	calls := parseTextToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("found %d calls, want 1", len(calls))
	}
	if calls[0].Name != "view_file" {
		t.Errorf("name = %q, want view_file", calls[0].Name)
	}

	encoded := toolCallsJSON(calls)
	var got struct {
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(encoded[0], &got); err != nil {
		t.Fatalf("unreadable tool call: %v", err)
	}

	// Non-string argument values must survive rather than being stringified.
	var args struct {
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(got.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments are not valid JSON: %v", err)
	}
	if args.Path != "main.go" || args.Limit != 100 {
		t.Errorf("arguments were mangled: %+v", args)
	}
}

// The marker must be held back like the XML form, so it never reaches the user
// as raw text.
func TestMarkerProtocolIsHeldBackFromTheUser(t *testing.T) {
	text := "Working." + markerToolOpen + `{"name":"LS","arguments":{}}` + markerToolClose

	f := &toolTextFilter{}
	var streamed strings.Builder
	for _, chunk := range strings.Split(text, "") {
		streamed.WriteString(f.push(chunk))
	}
	trailing, calls := f.finish()
	streamed.WriteString(trailing)

	if len(calls) != 1 {
		t.Fatalf("captured %d calls, want 1", len(calls))
	}
	if strings.Contains(streamed.String(), "TOOL_CALL") {
		t.Errorf("the marker leaked to the user: %q", streamed.String())
	}
	if !strings.Contains(streamed.String(), "Working.") {
		t.Errorf("the prose was lost: %q", streamed.String())
	}
}

// Both dialects in one reply must both be delivered.
func TestBothToolDialectsTranslate(t *testing.T) {
	text := buildToolText("Grep", map[string]string{"pattern": "deadline"}) +
		markerToolOpen + `{"name":"LS","arguments":{"path":"."}}` + markerToolClose

	calls := parseTextToolCalls(text)
	if len(calls) != 2 {
		t.Fatalf("found %d calls, want 2", len(calls))
	}

	names := map[string]bool{}
	for _, c := range calls {
		names[c.Name] = true
	}
	if !names["Grep"] || !names["LS"] {
		t.Errorf("missing a dialect: %v", names)
	}
}
