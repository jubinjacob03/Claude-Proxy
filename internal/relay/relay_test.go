package relay_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"claude-proxy/internal/license"
	"claude-proxy/internal/relay"
)

type upstream struct {
	server  *httptest.Server
	lastHdr http.Header
	calls   int
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	return newUpstreamWithStatus(t, http.StatusOK)
}

func newUpstreamWithStatus(t *testing.T, status int) *upstream {
	t.Helper()
	u := &upstream{}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.calls++
		u.lastHdr = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status >= 200 && status < 300 {
			io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5",`+
				`"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","stop_sequence":null,`+
				`"usage":{"input_tokens":1,"output_tokens":1}}`)
			return
		}
		io.WriteString(w, `{"error":{"message":"nope"}}`)
	}))
	t.Cleanup(u.server.Close)
	return u
}

func newRelay(t *testing.T, up *upstream, quota license.Money) (*httptest.Server, *license.Store, *license.License) {
	t.Helper()
	dir := t.TempDir()
	store, err := license.Open(dir, []byte("test-db-key"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if _, err := store.AddPoolKey("pool", "sk-pooled-secret", license.ProviderClaude, 100000); err != nil {
		t.Fatalf("add pool key: %v", err)
	}
	lic, err := store.CreateLicense(quota, "test")
	if err != nil {
		t.Fatalf("create licence: %v", err)
	}

	srv := httptest.NewServer(relay.New(relay.Config{
		DataDir:       dir,
		TokenSecret:   "test-secret",
		AdminToken:    "admin-secret",
		ClaudeBaseURL: up.server.URL,
		DefaultQuota:  quota,
	}, store).Handler())
	t.Cleanup(srv.Close)
	return srv, store, lic
}

func post(t *testing.T, url, body string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
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

func activate(t *testing.T, relayURL, key, hwid string) string {
	t.Helper()
	resp, body := post(t, relayURL+"/v1/activate",
		`{"key":"`+key+`","hwid":"`+hwid+`"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("activate = %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	if out.Token == "" {
		t.Fatalf("no token returned: %s", body)
	}
	return out.Token
}

const messageBody = `{"model":"claude-opus-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`

func TestActivationBindsAndRejectsOtherMachines(t *testing.T) {
	up := newUpstream(t)
	srv, _, lic := newRelay(t, up, 7000)

	activate(t, srv.URL, lic.Key, "machine-a")

	resp, body := post(t, srv.URL+"/v1/activate",
		`{"key":"`+lic.Key+`","hwid":"machine-b"}`, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("second machine = %d, want 403: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "another machine") {
		t.Errorf("unhelpful message: %s", body)
	}
}

func TestActivationRejectsUnknownKey(t *testing.T) {
	up := newUpstream(t)
	srv, _, _ := newRelay(t, up, 7000)

	resp, _ := post(t, srv.URL+"/v1/activate", `{"key":"CP-DEAD-BEEF-0000-0000-0000","hwid":"m"}`, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestProxyRequiresALicenceToken(t *testing.T) {
	up := newUpstream(t)
	srv, _, _ := newRelay(t, up, 7000)

	resp, body := post(t, srv.URL+"/v1/messages", messageBody, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401: %s", resp.StatusCode, body)
	}
	if up.calls != 0 {
		t.Error("an unlicensed request reached the upstream")
	}
	// the client speaks Anthropic here, so the error must be Anthropic shaped
	var out map[string]any
	_ = json.Unmarshal([]byte(body), &out)
	if out["type"] != "error" {
		t.Errorf("want an Anthropic error envelope: %s", body)
	}
}

func TestPooledKeyIsInjectedAndNeverReturned(t *testing.T) {
	up := newUpstream(t)
	srv, _, lic := newRelay(t, up, 7000)
	token := activate(t, srv.URL, lic.Key, "machine-a")

	resp, body := post(t, srv.URL+"/v1/messages", messageBody,
		map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if got := up.lastHdr.Get("x-api-key"); got != "sk-pooled-secret" {
		t.Errorf("upstream x-api-key = %q, want the pooled secret", got)
	}
	if strings.Contains(body, "sk-pooled-secret") {
		t.Fatalf("the pooled secret leaked to the client: %s", body)
	}
}

func TestQuotaIsEnforcedAndReportsClearly(t *testing.T) {
	up := newUpstream(t)
	// 40 cents buys exactly two 20 cent opus calls.
	srv, _, lic := newRelay(t, up, 40)
	token := activate(t, srv.URL, lic.Key, "machine-a")
	auth := map[string]string{"Authorization": "Bearer " + token}

	for i := 0; i < 2; i++ {
		if resp, body := post(t, srv.URL+"/v1/messages", messageBody, auth); resp.StatusCode != http.StatusOK {
			t.Fatalf("call %d = %d: %s", i, resp.StatusCode, body)
		}
	}

	resp, body := post(t, srv.URL+"/v1/messages", messageBody, auth)
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("exhausted call = %d, want 402: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Usage quota exhausted") {
		t.Errorf("the user needs a clear message, got: %s", body)
	}
	if up.calls != 2 {
		t.Errorf("upstream served %d calls, want 2: quota was not enforced", up.calls)
	}
}

func TestSpendIsTrackedPerLicence(t *testing.T) {
	up := newUpstream(t)
	srv, store, first := newRelay(t, up, 7000)
	second, err := store.CreateLicense(7000, "other seat")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	tokenA := activate(t, srv.URL, first.Key, "machine-a")
	tokenB := activate(t, srv.URL, second.Key, "machine-b")

	for i := 0; i < 3; i++ {
		post(t, srv.URL+"/v1/messages", messageBody, map[string]string{"Authorization": "Bearer " + tokenA})
	}
	post(t, srv.URL+"/v1/messages", messageBody, map[string]string{"Authorization": "Bearer " + tokenB})

	gotA, _ := store.Get(first.ID)
	gotB, _ := store.Get(second.ID)
	if gotA.SpentCents != 60 {
		t.Errorf("licence A spent %v, want 60", gotA.SpentCents)
	}
	if gotB.SpentCents != 20 {
		t.Errorf("licence B spent %v, want 20 (usage must not pool across users)", gotB.SpentCents)
	}
}

func TestPausedLicenceIsRefused(t *testing.T) {
	up := newUpstream(t)
	srv, store, lic := newRelay(t, up, 7000)
	token := activate(t, srv.URL, lic.Key, "machine-a")

	if err := store.SetActive(lic.ID, false); err != nil {
		t.Fatalf("pause: %v", err)
	}
	resp, body := post(t, srv.URL+"/v1/messages", messageBody,
		map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403: %s", resp.StatusCode, body)
	}
}

func TestForgedTokenIsRejected(t *testing.T) {
	up := newUpstream(t)
	srv, _, lic := newRelay(t, up, 7000)

	forged := license.Sign([]byte("not-the-relay-secret"), lic.ID, "machine-a")
	resp, _ := post(t, srv.URL+"/v1/messages", messageBody,
		map[string]string{"Authorization": "Bearer " + forged})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if up.calls != 0 {
		t.Error("a forged token reached the upstream")
	}
}

func TestUpstreamFailureIsRefunded(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":{"message":"upstream exploded"}}`)
	}))
	defer broken.Close()

	dir := t.TempDir()
	store, err := license.Open(dir, []byte("test-db-key"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	defer store.Close()
	store.AddPoolKey("pool", "sk-pooled-secret", license.ProviderClaude, 100000)
	lic, _ := store.CreateLicense(7000, "")

	srv := httptest.NewServer(relay.New(relay.Config{
		DataDir: dir, TokenSecret: "test-secret",
		ClaudeBaseURL: broken.URL, DefaultQuota: 7000,
	}, store).Handler())
	defer srv.Close()

	token := activate(t, srv.URL, lic.Key, "machine-a")
	post(t, srv.URL+"/v1/messages", messageBody, map[string]string{"Authorization": "Bearer " + token})

	got, _ := store.Get(lic.ID)
	if got.SpentCents != 0 {
		t.Errorf("spent = %v, want 0: a failed upstream call must not be billed", got.SpentCents)
	}
}

// A bad pooled key is the operator's problem. Billing the customer for the
// resulting 401 would silently drain the quota of everyone using that key.
func TestUpstreamAuthFailureIsNotBilled(t *testing.T) {
	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusPaymentRequired,
		http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
	} {
		refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			io.WriteString(w, `{"error":{"message":"nope"}}`)
		}))

		dir := t.TempDir()
		store, err := license.Open(dir, []byte("test-db-key"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		store.AddPoolKey("pool", "sk-pooled-secret", license.ProviderClaude, 100000)
		lic, _ := store.CreateLicense(7000, "")

		srv := httptest.NewServer(relay.New(relay.Config{
			DataDir: dir, TokenSecret: "test-secret",
			ClaudeBaseURL: refusing.URL, DefaultQuota: 7000,
		}, store).Handler())

		token := activate(t, srv.URL, lic.Key, "machine-a")
		post(t, srv.URL+"/v1/messages", messageBody, map[string]string{"Authorization": "Bearer " + token})

		got, _ := store.Get(lic.ID)
		if got.SpentCents != 0 {
			t.Errorf("upstream %d billed the customer %v; it must be refunded", status, got.SpentCents)
		}

		srv.Close()
		refusing.Close()
		store.Close()
	}
}

func TestPoolKeyStaysActiveOnUpstreamAuthOrBalanceFailure(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusPaymentRequired} {
		up := newUpstreamWithStatus(t, status)
		srv, store, lic := newRelay(t, up, 7000)
		token := activate(t, srv.URL, lic.Key, "machine-a")

		post(t, srv.URL+"/v1/messages", messageBody, map[string]string{"Authorization": "Bearer " + token})

		keys := store.ListPoolKeys()
		if len(keys) != 1 {
			t.Fatalf("keys = %d, want 1", len(keys))
		}
		if !keys[0].Active {
			t.Fatalf("status %d disabled the pool key", status)
		}
	}
}

func TestEnablePoolKeyDisablesOtherKeysInGroup(t *testing.T) {
	dir := t.TempDir()
	store, err := license.Open(dir, []byte("test-db-key"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	first, err := store.AddPoolKeyInGroup("first", "sk-first", license.ProviderClaude, "group-a", 1000)
	if err != nil {
		t.Fatalf("add first: %v", err)
	}
	second, err := store.AddPoolKeyInGroup("second", "sk-second", license.ProviderClaude, "group-a", 1000)
	if err != nil {
		t.Fatalf("add second: %v", err)
	}
	if err := store.SetPoolKeyActive(first.ID, true); err != nil {
		t.Fatalf("enable first: %v", err)
	}
	if err := store.SetPoolKeyActive(second.ID, true); err != nil {
		t.Fatalf("enable second: %v", err)
	}
	one, err := store.GetPoolKey(first.ID)
	if err != nil {
		t.Fatalf("get first: %v", err)
	}
	two, err := store.GetPoolKey(second.ID)
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if one.Active {
		t.Fatalf("first key stayed active after enabling second")
	}
	if !two.Active {
		t.Fatalf("second key was not active after enabling it")
	}
}

func TestAdminAPIRequiresItsToken(t *testing.T) {
	up := newUpstream(t)
	srv, _, lic := newRelay(t, up, 7000)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin/licenses", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("admin request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated admin = %d, want 401", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/admin/licenses", nil)
	req.Header.Set("X-Admin-Token", "admin-secret")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("admin request: %v", err)
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("admin = %d: %s", resp2.StatusCode, body)
	}
	// A hint is fine for identification; the usable key must never be listed.
	if strings.Contains(string(body), lic.Key) {
		t.Errorf("the licence list exposed a full key: %s", body)
	}
}
