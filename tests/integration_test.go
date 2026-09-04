package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claude-proxy/internal/bridge"
)

// mockGoRouter stands in for GoRouter's Anthropic-compatible endpoint. It records
// the last request body and headers it saw so tests can assert on forwarding.
type mockGoRouter struct {
	server   *httptest.Server
	lastBody map[string]any
	lastHdr  http.Header
}

type mockOpenAIUpstream struct {
	server   *httptest.Server
	lastBody map[string]any
	lastHdr  http.Header
}

func newMockGoRouter(t *testing.T) *mockGoRouter {
	t.Helper()
	m := &mockGoRouter{}
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"list","data":[{"id":"claude-opus-4-8"},{"id":"claude-opus-5"}]}`)
	})

	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		m.lastHdr = r.Header.Clone()
		m.lastBody = map[string]any{}
		_ = json.Unmarshal(raw, &m.lastBody)

		if stream, _ := m.lastBody["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fl, _ := w.(http.Flusher)
			for _, e := range []string{
				`data: {"type":"message_start","message":{"id":"msg_mock","usage":{"input_tokens":6}}}`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
				`data: {"type":"content_block_stop","index":0}`,
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
				`data: {"type":"message_stop"}`,
			} {
				io.WriteString(w, e+"\n\n")
				if fl != nil {
					fl.Flush()
				}
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_mock","type":"message","role":"assistant","model":"claude-opus-4-8",`+
			`"content":[{"type":"text","text":"Hello from GoRouter"}],"stop_reason":"end_turn","stop_sequence":null,`+
			`"usage":{"input_tokens":6,"output_tokens":4}}`)
	})

	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	return m
}

func newMockOpenAIUpstream(t *testing.T) *mockOpenAIUpstream {
	t.Helper()
	m := &mockOpenAIUpstream{}
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		m.lastHdr = r.Header.Clone()
		m.lastBody = map[string]any{}
		_ = json.Unmarshal(raw, &m.lastBody)

		if stream, _ := m.lastBody["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fl, _ := w.(http.Flusher)
			for _, e := range []string{
				`data: {"id":"chatcmpl_claude","object":"chat.completion.chunk","created":1,"model":"claude-3-opus-20240229","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
				`data: {"id":"chatcmpl_claude","object":"chat.completion.chunk","created":1,"model":"claude-3-opus-20240229","choices":[{"index":0,"delta":{"content":"Hello from Claude"},"finish_reason":null}]}`,
				`data: {"id":"chatcmpl_claude","object":"chat.completion.chunk","created":1,"model":"claude-3-opus-20240229","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
				`data: [DONE]`,
			} {
				io.WriteString(w, e+"\n\n")
				if fl != nil {
					fl.Flush()
				}
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"chatcmpl_claude","object":"chat.completion","created":1,"model":"claude-3-opus-20240229",`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":"Hello from Claude"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}`)
	})

	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	return m
}

func newBridge(t *testing.T, upstream string, mutate func(*bridge.Config)) *httptest.Server {
	t.Helper()
	cfg := &bridge.Config{
		Host:             "127.0.0.1",
		Port:             0,
		UpstreamBaseURL:  upstream,
		UpstreamFormat:   bridge.FormatAnthropic,
		UpstreamAPIKey:   "upstream-key",
		DefaultModel:     "claude-opus-4-8",
		ModelMap:         map[string]string{},
		DefaultMaxTokens: 4096,
		StreamIdlePing:   0,
		RequestTimeout:   30 * time.Second,
	}
	if mutate != nil {
		mutate(cfg)
	}
	srv := httptest.NewServer(bridge.NewServer(cfg, "test").Handler())
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, url, body string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, string(raw)
}

func getJSON(t *testing.T, url string, hdr map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp, out
}

func TestMessagesPassthroughEndToEnd(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, nil)

	resp, body := postJSON(t, br.URL+"/v1/messages",
		`{"model":"claude-opus-4-8","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["type"] != "message" || out["role"] != "assistant" {
		t.Errorf("not an Anthropic envelope: %s", body)
	}
	// the upstream key must be injected, never the client's absent one
	if got := mock.lastHdr.Get("x-api-key"); got != "upstream-key" {
		t.Errorf("upstream x-api-key = %q, want upstream-key", got)
	}
	if got := mock.lastHdr.Get("anthropic-version"); got == "" {
		t.Error("anthropic-version must be set upstream")
	}
}

func TestMessagesStreamingProducesAnthropicSSE(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, nil)

	_, body := postJSON(t, br.URL+"/v1/messages",
		`{"model":"claude-opus-4-8","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`, nil)

	for _, want := range []string{"message_start", "content_block_delta", `"text":"Hi"`, "message_stop"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in stream:\n%s", want, body)
		}
	}
}

func TestChatCompletionsTranslatesToAnthropicUpstream(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, nil)

	resp, body := postJSON(t, br.URL+"/v1/chat/completions",
		`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(body), &out)
	if out["object"] != "chat.completion" {
		t.Errorf("want an OpenAI envelope back, got: %s", body)
	}
	// the bridge must have spoken Anthropic upstream
	if _, ok := mock.lastBody["max_tokens"]; !ok {
		t.Errorf("max_tokens must be injected for Anthropic: %v", mock.lastBody)
	}
}

func TestAuthTokenGatesAPIButNotHealth(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, func(c *bridge.Config) { c.AuthToken = "secret" })

	resp, _ := postJSON(t, br.URL+"/v1/messages",
		`{"model":"claude-opus-4-8","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no key: status = %d, want 401", resp.StatusCode)
	}

	resp, _ = postJSON(t, br.URL+"/v1/messages",
		`{"model":"claude-opus-4-8","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`,
		map[string]string{"x-api-key": "secret"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("x-api-key: status = %d, want 200", resp.StatusCode)
	}

	resp, _ = postJSON(t, br.URL+"/v1/messages",
		`{"model":"claude-opus-4-8","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`,
		map[string]string{"Authorization": "Bearer secret"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("bearer: status = %d, want 200", resp.StatusCode)
	}

	if resp, _ := getJSON(t, br.URL+"/health", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("/health must stay open, got %d", resp.StatusCode)
	}
}

func TestModelMapRewritesUpstreamModel(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, func(c *bridge.Config) {
		c.ModelMap = map[string]string{"claude-3-5-sonnet-20241022": "claude-opus-4-8"}
	})

	postJSON(t, br.URL+"/v1/messages",
		`{"model":"claude-3-5-sonnet-20241022","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, nil)

	if got := mock.lastBody["model"]; got != "claude-opus-4-8" {
		t.Errorf("upstream model = %v, want the mapped claude-opus-4-8", got)
	}
}

func TestDefaultModelUsedWhenOmitted(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, func(c *bridge.Config) { c.DefaultModel = "claude-opus-5" })

	postJSON(t, br.URL+"/v1/messages",
		`{"max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, nil)

	if got := mock.lastBody["model"]; got != "claude-opus-5" {
		t.Errorf("upstream model = %v, want the default claude-opus-5", got)
	}
}

func TestModelsEndpointsServeBothShapes(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, nil)

	_, v1 := getJSON(t, br.URL+"/v1/models", nil)
	data, ok := v1["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("/v1/models data missing: %v", v1)
	}
	first := data[0].(map[string]any)
	for _, key := range []string{"id", "object", "display_name", "architecture"} {
		if _, ok := first[key]; !ok {
			t.Errorf("/v1/models entry missing %q: %v", key, first)
		}
	}

	_, compact := getJSON(t, br.URL+"/models", nil)
	if _, ok := compact["models"]; !ok {
		t.Errorf("/models must return a models array: %v", compact)
	}
	if compact["currentModel"] != "claude-opus-4-8" {
		t.Errorf("currentModel = %v", compact["currentModel"])
	}
}

func TestFeaturesOverrideReachesUpstream(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, nil)

	resp, body := postJSON(t, br.URL+"/features",
		`{"model":"claude-opus-4-8","temperature":0.25,"max_tokens":333}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /features = %d: %s", resp.StatusCode, body)
	}

	// the client omits both fields, so the stored override should fill them in
	postJSON(t, br.URL+"/v1/messages",
		`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`, nil)

	if got := mock.lastBody["temperature"]; got != 0.25 {
		t.Errorf("temperature = %v, want the override 0.25", got)
	}
	if got := mock.lastBody["max_tokens"]; got != float64(333) {
		t.Errorf("max_tokens = %v, want the override 333", got)
	}

	// an explicit client value must still win
	postJSON(t, br.URL+"/v1/messages",
		`{"model":"claude-opus-4-8","max_tokens":7,"messages":[{"role":"user","content":"hi"}]}`, nil)
	if got := mock.lastBody["max_tokens"]; got != float64(7) {
		t.Errorf("max_tokens = %v, want the client's 7", got)
	}
}

func TestCORSIsNotReflectedToRemoteOrigins(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, nil)

	// A malicious page must not get a CORS grant, otherwise it could drive the
	// proxy from the victim's browser and repoint the upstream.
	req, _ := http.NewRequest(http.MethodOptions, br.URL+"/config", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("remote origin was granted CORS: %q", got)
	}

	// A loopback origin is still allowed.
	req, _ = http.NewRequest(http.MethodOptions, br.URL+"/config", nil)
	req.Header.Set("Origin", "http://127.0.0.1:3001")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	defer resp2.Body.Close()
	if got := resp2.Header.Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:3001" {
		t.Errorf("loopback origin should be allowed, got %q", got)
	}
}

func TestConfigMutationRequiresAuthTokenWhenSet(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, func(c *bridge.Config) { c.AuthToken = "secret" })

	// Repointing the upstream is how a key gets stolen; it must need the token.
	resp, _ := postJSON(t, br.URL+"/config", `{"upstream_base_url":"https://evil.example"}`, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /config POST = %d, want 401", resp.StatusCode)
	}
	_, cfg := getJSON(t, br.URL+"/config", nil)
	if cfg["upstream_base_url"] != mock.server.URL {
		t.Errorf("upstream was changed without auth: %v", cfg["upstream_base_url"])
	}

	// With the token it succeeds.
	resp, body := postJSON(t, br.URL+"/config", `{"default_model":"claude-opus-5"}`,
		map[string]string{"x-api-key": "secret"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("authenticated /config POST = %d: %s", resp.StatusCode, body)
	}
}

func TestConfigUpdateAppliesLive(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, nil)

	resp, body := postJSON(t, br.URL+"/config",
		`{"default_model":"claude-opus-5","persist":false}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /config = %d: %s", resp.StatusCode, body)
	}

	_, cfg := getJSON(t, br.URL+"/config", nil)
	if cfg["default_model"] != "claude-opus-5" {
		t.Errorf("default_model = %v, want the live update", cfg["default_model"])
	}
	// secrets must never be echoed back
	if _, leaked := cfg["upstream_api_key"]; leaked {
		t.Error("/config must not return the raw upstream key")
	}
}

func TestUpstreamErrorSurfacesInAnthropicShape(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"slow down"}}`)
	}))
	defer bad.Close()

	br := newBridge(t, bad.URL, nil)
	resp, body := postJSON(t, br.URL+"/v1/messages",
		`{"model":"claude-opus-4-8","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, nil)

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want the upstream 429 preserved", resp.StatusCode)
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(body), &out)
	if out["type"] != "error" {
		t.Errorf("want an Anthropic error envelope: %s", body)
	}
}

func TestAnthropicShapedUpstreamErrorIsForwardedVerbatim(t *testing.T) {
	// Claude Code matches on upstream error wording to decide whether to retry
	// with a capability disabled, so a correctly shaped error must not be rewrapped.
	upstreamBody := `{"type":"error","error":{"type":"invalid_request_error","message":"thinking: not supported"}}`
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, upstreamBody)
	}))
	defer bad.Close()

	br := newBridge(t, bad.URL, nil)
	resp, body := postJSON(t, br.URL+"/v1/messages",
		`{"model":"claude-opus-4-8","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, nil)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	if strings.TrimSpace(body) != upstreamBody {
		t.Errorf("error body was modified:\n got %s\nwant %s", body, upstreamBody)
	}
}

func TestCountTokensEstimatesOnOpenAIUpstream(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, func(c *bridge.Config) { c.UpstreamFormat = bridge.FormatOpenAI })

	_, body := postJSON(t, br.URL+"/v1/messages/count_tokens",
		`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hello there"}]}`, nil)

	var out map[string]any
	_ = json.Unmarshal([]byte(body), &out)
	n, ok := out["input_tokens"].(float64)
	if !ok || n < 1 {
		t.Errorf("input_tokens = %v, want a positive estimate", out["input_tokens"])
	}
}

func TestAdminEndpoints(t *testing.T) {
	mock := newMockGoRouter(t)
	br := newBridge(t, mock.server.URL, nil)

	resp, err := http.Get(br.URL + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET / = %d, want 404 now that no UI is served", resp.StatusCode)
	}

	if _, status := getJSON(t, br.URL+"/status", nil); status["name"] != "claude-proxy" {
		t.Errorf("/status = %v", status)
	}
	if _, stats := getJSON(t, br.URL+"/admin/stats", nil); stats["total_requests"] == nil {
		t.Errorf("/admin/stats missing counters: %v", stats)
	}
	if _, clients := getJSON(t, br.URL+"/admin/clients", nil); clients["count"] == nil {
		t.Errorf("/admin/clients missing count: %v", clients)
	}
}
