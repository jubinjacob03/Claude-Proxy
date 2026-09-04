package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"claude-proxy/internal/license"
	"claude-proxy/internal/relay"
)

func newRelayAdminHarness(t *testing.T) (http.Handler, *license.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := license.Open(dir, []byte("test-db-key"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server := relay.New(relay.Config{AdminToken: "admin-secret", DefaultQuota: 1000}, store)
	return server.Handler(), store
}

func adminPost(t *testing.T, server http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("X-Admin-Token", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)
	return rr
}

func adminGet(t *testing.T, server http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Admin-Token", "admin-secret")
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)
	return rr
}

func TestRelayAdminDeleteLicense(t *testing.T) {
	server, _ := newRelayAdminHarness(t)
	created := adminPost(t, server, "/admin/licenses", `{"quota_cents":500}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", created.Code, created.Body.String())
	}
	var payload struct {
		Created []struct {
			ID string `json:"id"`
		} `json:"created"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	id := payload.Created[0].ID

	deleted := adminPost(t, server, "/admin/licenses/"+id+"/delete", `{}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleted.Code, deleted.Body.String())
	}
	listed := adminGet(t, server, "/admin/licenses")
	if strings.Contains(listed.Body.String(), id) {
		t.Fatalf("deleted licence still present: %s", listed.Body.String())
	}
}

func TestRelayAdminTopUpAndRotatePoolKey(t *testing.T) {
	server, _ := newRelayAdminHarness(t)
	created := adminPost(t, server, "/admin/pool", `{"label":"main","secret":"sk-old","provider":"claude","balance_cents":1000}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", created.Code, created.Body.String())
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}

	topup := adminPost(t, server, "/admin/pool/"+payload.ID+"/topup", `{"balance_cents":250}`)
	if topup.Code != http.StatusOK {
		t.Fatalf("topup status = %d body=%s", topup.Code, topup.Body.String())
	}
	item := adminGet(t, server, "/admin/pool/"+payload.ID)
	if !strings.Contains(item.Body.String(), `"balance_cents":1250`) {
		t.Fatalf("topup not reflected: %s", item.Body.String())
	}

	rotated := adminPost(t, server, "/admin/pool/"+payload.ID+"/rotate", `{"label":"rotated","secret":"sk-new","balance_cents":900}`)
	if rotated.Code != http.StatusOK {
		t.Fatalf("rotate status = %d body=%s", rotated.Code, rotated.Body.String())
	}
	pool := adminGet(t, server, "/admin/pool")
	body := pool.Body.String()
	if !strings.Contains(body, `"label":"rotated"`) {
		t.Fatalf("rotated key missing: %s", body)
	}
	if !strings.Contains(body, `"active":false`) {
		t.Fatalf("old key should be disabled after rotation: %s", body)
	}
}

func TestRelayAdminLicenseAndUsageFilters(t *testing.T) {
	server, store := newRelayAdminHarness(t)
	created := adminPost(t, server, "/admin/licenses", `{"quota_cents":500,"note":"alpha seat"}`)
	var payload struct {
		Created []struct {
			ID string `json:"id"`
		} `json:"created"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	id := payload.Created[0].ID

	if err := store.SetActive(id, false); err != nil {
		t.Fatal(err)
	}
	filtered := adminGet(t, server, "/admin/licenses?q=alpha&status=paused")
	if !strings.Contains(filtered.Body.String(), id) {
		t.Fatalf("filtered licences missing expected record: %s", filtered.Body.String())
	}

	res := &license.Reservation{EventID: license.NewID(), LicenseID: id, PoolKeyID: "pool-1", Cost: 25}
	if err := store.Commit(res, "claude", "claude-opus-5", "pool-1", 200, true); err != nil {
		t.Fatal(err)
	}
	usage := adminGet(t, server, "/admin/usage?license_id="+id+"&q=opus&status=success")
	if !strings.Contains(usage.Body.String(), `"model":"claude-opus-5"`) {
		t.Fatalf("filtered usage missing expected event: %s", usage.Body.String())
	}
}

func TestRelayAdminEndpointProfiles(t *testing.T) {
	server, _ := newRelayAdminHarness(t)

	saved := adminPost(t, server, "/admin/endpoints", `{"name":"eu","claude_base_url":"https://eu.example/claude","pool_group":"eu","active":false}`)
	if saved.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", saved.Code, saved.Body.String())
	}

	list := adminGet(t, server, "/admin/endpoints")
	if !strings.Contains(list.Body.String(), `"name":"eu"`) {
		t.Fatalf("endpoint profile not listed: %s", list.Body.String())
	}

	active := adminPost(t, server, "/admin/endpoints/eu/activate", `{}`)
	if active.Code != http.StatusOK {
		t.Fatalf("activate status = %d body=%s", active.Code, active.Body.String())
	}

	item := adminGet(t, server, "/admin/endpoints/eu")
	if !strings.Contains(item.Body.String(), `"active":true`) {
		t.Fatalf("profile should be active: %s", item.Body.String())
	}
}

func TestRelayAdminPoolGroupFilter(t *testing.T) {
	server, _ := newRelayAdminHarness(t)

	createdA := adminPost(t, server, "/admin/pool", `{"label":"default","secret":"sk-default","provider":"claude","pool_group":"default","balance_cents":1000}`)
	if createdA.Code != http.StatusOK {
		t.Fatalf("create default key status = %d body=%s", createdA.Code, createdA.Body.String())
	}
	createdB := adminPost(t, server, "/admin/pool", `{"label":"eu","secret":"sk-eu","provider":"claude","pool_group":"eu","balance_cents":1000}`)
	if createdB.Code != http.StatusOK {
		t.Fatalf("create eu key status = %d body=%s", createdB.Code, createdB.Body.String())
	}

	filtered := adminGet(t, server, "/admin/pool?pool_group=eu")
	body := filtered.Body.String()
	if !strings.Contains(body, `"pool_group":"eu"`) {
		t.Fatalf("eu key missing in filtered pool: %s", body)
	}
	if strings.Contains(body, `"pool_group":"default"`) {
		t.Fatalf("default group key should not appear in eu filter: %s", body)
	}
}
