package licensing

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newActivationServer(t *testing.T, boundHWID string) *httptest.Server {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Key  string `json:"key"`
			HWID string `json:"hwid"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		if body.Key != "CP-VALID-KEY" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"status":"error","message":"That licence key was not recognised."}`)
			return
		}
		if boundHWID != "" && body.HWID != boundHWID {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"status":"error","message":"That licence is already in use on another machine."}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"ok","token":"tok_`+body.HWID+`"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEnsureActivatedFailsClosedWithoutAKey(t *testing.T) {
	srv := newActivationServer(t, "")
	_, err := EnsureActivated(NewClient(srv.URL), t.TempDir(), "")
	if err != ErrNotLicensed {
		t.Fatalf("err = %v, want ErrNotLicensed", err)
	}
}

func TestEnsureActivatedRejectsAnUnknownKey(t *testing.T) {
	srv := newActivationServer(t, "")
	_, err := EnsureActivated(NewClient(srv.URL), t.TempDir(), "CP-WRONG-KEY")
	if err == nil {
		t.Fatal("an unknown key must not activate")
	}
}

func TestEnsureActivatedCachesTheToken(t *testing.T) {
	srv := newActivationServer(t, "")
	dir := t.TempDir()
	client := NewClient(srv.URL)

	first, err := EnsureActivated(client, dir, "CP-VALID-KEY")
	if err != nil {
		t.Fatalf("first activation: %v", err)
	}
	if first == "" {
		t.Fatal("expected a token")
	}

	// A second run must not need the key at all: the cache should satisfy it
	// even if an empty key is passed, which mirrors normal startup after the
	// first run.
	second, err := EnsureActivated(client, dir, "")
	if err != nil {
		t.Fatalf("cached activation should not require the key again: %v", err)
	}
	if second != first {
		t.Errorf("token = %q, want the cached %q", second, first)
	}
}

func TestEnsureActivatedReactivatesWhenRelayChanges(t *testing.T) {
	dir := t.TempDir()
	srvA := newActivationServer(t, "")
	if _, err := EnsureActivated(NewClient(srvA.URL), dir, "CP-VALID-KEY"); err != nil {
		t.Fatalf("activate on relay A: %v", err)
	}

	srvB := newActivationServer(t, "")
	if _, err := EnsureActivated(NewClient(srvB.URL), dir, ""); err != ErrNotLicensed {
		t.Fatalf("switching relays without a key err = %v, want ErrNotLicensed", err)
	}
}

func TestHWIDIsStableAndOpaque(t *testing.T) {
	a, err := HWID()
	if err != nil {
		t.Fatalf("hwid: %v", err)
	}
	b, err := HWID()
	if err != nil {
		t.Fatalf("hwid: %v", err)
	}
	if a != b {
		t.Errorf("hwid is not stable across calls: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("hwid length = %d, want a 64-char hex sha256", len(a))
	}
}
