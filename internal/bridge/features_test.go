package bridge

import (
	"encoding/json"
	"testing"
)

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func TestApplyFeaturesFillsAbsentFields(t *testing.T) {
	raw := []byte(`{"model":"claude-opus-4-8","messages":[]}`)
	maxTokens := 2048
	temp := 0.5

	got := decode(t, applyFeatures(raw, "claude-opus-4-8", ModelFeatures{
		MaxTokens:   &maxTokens,
		Temperature: &temp,
	}))

	if got["max_tokens"] != float64(2048) {
		t.Errorf("max_tokens = %v, want 2048", got["max_tokens"])
	}
	if got["temperature"] != 0.5 {
		t.Errorf("temperature = %v, want 0.5", got["temperature"])
	}
}

func TestApplyFeaturesNeverOverridesClientValues(t *testing.T) {
	raw := []byte(`{"model":"claude-opus-4-8","max_tokens":100,"temperature":0.9}`)
	maxTokens := 4096
	temp := 0.1

	got := decode(t, applyFeatures(raw, "claude-opus-4-8", ModelFeatures{
		MaxTokens:   &maxTokens,
		Temperature: &temp,
	}))

	if got["max_tokens"] != float64(100) {
		t.Errorf("max_tokens = %v, want the client's 100", got["max_tokens"])
	}
	if got["temperature"] != 0.9 {
		t.Errorf("temperature = %v, want the client's 0.9", got["temperature"])
	}
}

func TestApplyFeaturesThinkingOnlyOnThinkingModels(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[]}`)
	on := true
	budget := 1024

	withThinking := decode(t, applyFeatures(raw, "claude-opus-4-8-thinking", ModelFeatures{
		Thinking: &on, ThinkingBudget: &budget,
	}))
	think, ok := withThinking["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking block missing: %v", withThinking)
	}
	if think["type"] != "enabled" || think["budget_tokens"] != float64(1024) {
		t.Errorf("thinking = %v, want enabled with budget 1024", think)
	}

	plain := decode(t, applyFeatures(raw, "claude-opus-4-8", ModelFeatures{Thinking: &on}))
	if _, exists := plain["thinking"]; exists {
		t.Errorf("thinking must not be injected into a non-thinking model: %v", plain)
	}
}

func TestApplyFeaturesNoopReturnsInputUnchanged(t *testing.T) {
	raw := []byte(`{"model":"claude-opus-4-8","max_tokens":10}`)
	got := applyFeatures(raw, "claude-opus-4-8", ModelFeatures{})
	if string(got) != string(raw) {
		t.Errorf("body was rewritten with no applicable features:\n got %s\nwant %s", got, raw)
	}
}

func TestRewriteModelPreservesOtherFields(t *testing.T) {
	raw := []byte(`{"model":"old","max_tokens":7,"stream":true}`)
	got := decode(t, rewriteModel(raw, "claude-opus-5"))

	if got["model"] != "claude-opus-5" {
		t.Errorf("model = %v, want claude-opus-5", got["model"])
	}
	if got["max_tokens"] != float64(7) || got["stream"] != true {
		t.Errorf("sibling fields lost: %v", got)
	}
}

func TestMapModelUsesExactThenWildcard(t *testing.T) {
	c := &Config{ModelMap: map[string]string{
		"claude-3-5-sonnet-20241022": "claude-opus-4-8",
		"*":                          "claude-opus-5",
	}}

	if got := c.MapModel("claude-3-5-sonnet-20241022"); got != "claude-opus-4-8" {
		t.Errorf("exact match = %q, want claude-opus-4-8", got)
	}
	if got := c.MapModel("anything-else"); got != "claude-opus-5" {
		t.Errorf("wildcard = %q, want claude-opus-5", got)
	}

	noWildcard := &Config{ModelMap: map[string]string{}}
	if got := noWildcard.MapModel("keep-me"); got != "keep-me" {
		t.Errorf("unmapped = %q, want passthrough", got)
	}
}

func TestIsLoopbackOriginRejectsRemoteSites(t *testing.T) {
	// A reflected Origin would let any site you visit drive the proxy from your
	// browser, so only loopback origins may be echoed.
	allowed := []string{
		"http://localhost:3001", "http://127.0.0.1:3001",
		"https://localhost", "http://[::1]:8080", "http://127.0.0.2:3001",
	}
	denied := []string{
		"https://evil.example", "http://attacker.test:3001",
		"http://169.254.169.254", "http://localhost.evil.com", "", "not-a-url",
		"http://10.0.0.5:3001",
	}

	for _, o := range allowed {
		if !isLoopbackOrigin(o) {
			t.Errorf("origin %q should be allowed", o)
		}
	}
	for _, o := range denied {
		if isLoopbackOrigin(o) {
			t.Errorf("origin %q must be rejected", o)
		}
	}
}

func TestRequiresClientAuth(t *testing.T) {
	guarded := []string{"/v1/messages", "/v1/chat/completions", "/v1/models", "/models", "/features", "/stop"}
	open := []string{"/", "/health", "/status", "/config", "/admin/stats", "/app.js"}

	for _, p := range guarded {
		if !requiresClientAuth(p) {
			t.Errorf("%s should require auth", p)
		}
	}
	for _, p := range open {
		if requiresClientAuth(p) {
			t.Errorf("%s should not require auth", p)
		}
	}
}

func TestCanonicalEndpointPath(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		changed bool
	}{
		{in: "/v1/chat/completions", want: "/v1/chat/completions", changed: false},
		{in: "/V1/chat/completions", want: "/v1/chat/completions", changed: true},
		{in: "/chat/completions", want: "/v1/chat/completions", changed: true},
		{in: "/V1/messages", want: "/v1/messages", changed: true},
		{in: "/messages/count_tokens", want: "/v1/messages/count_tokens", changed: true},
		{in: "/HEALTH", want: "/health", changed: true},
		{in: "/unknown", want: "/unknown", changed: false},
	}

	for _, c := range cases {
		got, changed := canonicalEndpointPath(c.in)
		if got != c.want || changed != c.changed {
			t.Errorf("canonicalEndpointPath(%q) = (%q, %v), want (%q, %v)", c.in, got, changed, c.want, c.changed)
		}
	}
}

func TestParseModelMapRoundTrip(t *testing.T) {
	m := ParseModelMap("a=1, b=2 ,,bad,c=3")
	if len(m) != 3 || m["a"] != "1" || m["b"] != "2" || m["c"] != "3" {
		t.Fatalf("parsed = %v, want a=1 b=2 c=3", m)
	}
	if got := ParseModelMap(ModelMapString(m)); len(got) != 3 {
		t.Errorf("round trip lost entries: %v", got)
	}
}
