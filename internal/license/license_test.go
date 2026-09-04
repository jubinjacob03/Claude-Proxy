package license

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir(), []byte("test-encryption-key"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestActivateBindsFirstMachineAndRejectsOthers(t *testing.T) {
	s := newStore(t)
	l, err := s.CreateLicense(7000, "seat")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	bound, err := s.Activate(l.Key, "machine-a")
	if err != nil {
		t.Fatalf("first activation: %v", err)
	}
	if bound.HWID != "machine-a" {
		t.Errorf("hwid = %q, want machine-a", bound.HWID)
	}

	// A reinstall on the same machine must keep working.
	if _, err := s.Activate(l.Key, "machine-a"); err != nil {
		t.Errorf("re-activation on the bound machine failed: %v", err)
	}

	if _, err := s.Activate(l.Key, "machine-b"); err != ErrHWIDMismatch {
		t.Errorf("second machine err = %v, want ErrHWIDMismatch", err)
	}
}

func TestActivateIsCaseInsensitiveButRequiresHWID(t *testing.T) {
	s := newStore(t)
	l, _ := s.CreateLicense(7000, "")

	if _, err := s.Activate(l.Key, ""); err != ErrHWIDMissing {
		t.Errorf("missing hwid err = %v, want ErrHWIDMissing", err)
	}
	if _, err := s.Activate("  "+strings.ToLower(l.Key)+"  ", "machine-a"); err != nil {
		t.Errorf("padded lowercase key should activate: %v", err)
	}
	if _, err := s.Activate("CP-0000-0000-0000-0000-0000", "machine-a"); err != ErrNotFound {
		t.Errorf("unknown key err = %v, want ErrNotFound", err)
	}
}

func TestPausedLicenseCannotActivateOrSpend(t *testing.T) {
	s := newStore(t)
	l, _ := s.CreateLicense(7000, "")
	s.Activate(l.Key, "machine-a")

	if err := s.SetActive(l.ID, false); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if _, err := s.Authorize(l.ID, "machine-a"); err != ErrInactive {
		t.Errorf("authorize err = %v, want ErrInactive", err)
	}
	if _, err := s.Reserve(l.ID, "machine-a", 20, ProviderClaude, "claude-opus-5", "k"); err != ErrInactive {
		t.Errorf("reserve err = %v, want ErrInactive", err)
	}
}

func TestReserveStopsAtTheQuota(t *testing.T) {
	s := newStore(t)
	l, _ := s.CreateLicense(50, "small")
	s.Activate(l.Key, "machine-a")

	for i := 0; i < 2; i++ {
		if _, err := s.Reserve(l.ID, "machine-a", 20, ProviderClaude, "claude-opus-5", "k"); err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
	}
	// 40 of 50 spent; a 20c call must not be allowed to overdraw.
	if _, err := s.Reserve(l.ID, "machine-a", 20, ProviderClaude, "claude-opus-5", "k"); err != ErrExhausted {
		t.Fatalf("overdraw err = %v, want ErrExhausted", err)
	}
	// but a call that fits still goes through
	if _, err := s.Reserve(l.ID, "machine-a", 10, ProviderClaude, "claude-3-5-haiku-20241022", "k"); err != nil {
		t.Fatalf("exact fit: %v", err)
	}
	got, _ := s.Get(l.ID)
	if got.Remaining() != 0 {
		t.Errorf("remaining = %v, want 0", got.Remaining())
	}
	if _, err := s.Authorize(l.ID, "machine-a"); err != ErrExhausted {
		t.Errorf("authorize err = %v, want ErrExhausted", err)
	}
}

// The whole point of charging before forwarding is that parallel requests
// cannot both spend the same balance.
func TestConcurrentReservesNeverOverspend(t *testing.T) {
	s := newStore(t)
	l, _ := s.CreateLicense(100, "race")
	s.Activate(l.Key, "machine-a")

	const workers = 50
	var wg sync.WaitGroup
	granted := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Reserve(l.ID, "machine-a", 20, ProviderClaude, "claude-opus-5", "k"); err == nil {
				granted <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(granted)

	if n := len(granted); n != 5 {
		t.Errorf("%d reservations granted, want exactly 5 for a 100c quota at 20c each", n)
	}
	got, _ := s.Get(l.ID)
	if got.SpentCents != 100 {
		t.Errorf("spent = %v, want exactly the quota", got.SpentCents)
	}
}

func TestReleaseRefundsAFailedCall(t *testing.T) {
	s := newStore(t)
	l, _ := s.CreateLicense(100, "")
	s.Activate(l.Key, "machine-a")

	r, err := s.Reserve(l.ID, "machine-a", 20, ProviderClaude, "claude-opus-5", "k")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := s.Release(r); err != nil {
		t.Fatalf("release: %v", err)
	}
	got, _ := s.Get(l.ID)
	if got.SpentCents != 0 {
		t.Errorf("spent = %v, want 0 after a refund", got.SpentCents)
	}
}

func TestWrongMachineCannotSpend(t *testing.T) {
	s := newStore(t)
	l, _ := s.CreateLicense(100, "")
	s.Activate(l.Key, "machine-a")

	if _, err := s.Reserve(l.ID, "machine-b", 20, ProviderClaude, "claude-opus-5", "k"); err != ErrHWIDMismatch {
		t.Errorf("err = %v, want ErrHWIDMismatch", err)
	}
}

func TestResetHWIDAllowsANewMachine(t *testing.T) {
	s := newStore(t)
	l, _ := s.CreateLicense(100, "")
	s.Activate(l.Key, "machine-a")

	if err := s.ResetHWID(l.ID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := s.Activate(l.Key, "machine-b"); err != nil {
		t.Errorf("after reset, a new machine should bind: %v", err)
	}
}

func TestSpendSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	key := []byte("test-encryption-key")
	s, err := Open(dir, key)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	l, _ := s.CreateLicense(100, "")
	s.Activate(l.Key, "machine-a")
	s.Reserve(l.ID, "machine-a", 40, ProviderClaude, "claude-opus-5", "")
	s.Close()

	reopened, err := Open(dir, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	got, err := reopened.Get(l.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SpentCents != 40 {
		t.Errorf("spent = %v, want 40 to survive a restart", got.SpentCents)
	}
	if got.HWID != "machine-a" {
		t.Errorf("hwid = %q, want the binding to survive a restart", got.HWID)
	}
}

func TestPoolKeysRotateOnExhaustionAndHideSecrets(t *testing.T) {
	s := newStore(t)
	if _, err := s.TakePoolKey(ProviderClaude); err == nil {
		t.Error("an empty pool must not hand out a key")
	}

	small, _ := s.AddPoolKey("small", "sk-small", ProviderClaude, 40)
	big, _ := s.AddPoolKey("big", "sk-big", ProviderClaude, 500)

	// The key with the most credit is preferred.
	got, err := s.TakePoolKey(ProviderClaude)
	if err != nil || got.ID != big.ID {
		t.Fatalf("take = %v (err %v), want the key with most credit", got, err)
	}
	if got.Secret != "sk-big" {
		t.Errorf("secret did not survive the encryption round trip: %q", got.Secret)
	}

	for _, k := range s.ListPoolKeys() {
		if k.Secret != "" {
			t.Errorf("ListPoolKeys leaked a secret for %s", k.ID)
		}
	}

	// Draining the small key must retire it automatically.
	s.SetPoolKeyActive(big.ID, false)
	l, _ := s.CreateLicense(7000, "")
	s.Activate(l.Key, "machine-a")
	for i := 0; i < 2; i++ {
		if _, err := s.Reserve(l.ID, "machine-a", 20, ProviderClaude, "claude-opus-5", small.ID); err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
	}
	if _, err := s.TakePoolKey(ProviderClaude); err == nil {
		t.Error("a drained pool key must be retired, not handed out again")
	}
}

func TestPoolKeySecretsAreEncryptedAtRest(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, []byte("correct-key"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	s.AddPoolKey("pool", "sk-super-secret", ProviderClaude, 500)
	s.Close()

	raw, err := os.ReadFile(filepath.Join(dir, "license.db"))
	if err != nil {
		t.Fatalf("read db: %v", err)
	}
	if bytes.Contains(raw, []byte("sk-super-secret")) {
		t.Error("the pooled secret is stored in plaintext in the database file")
	}

	wrong, err := Open(dir, []byte("wrong-key"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer wrong.Close()
	if _, err := wrong.TakePoolKey(ProviderClaude); err == nil {
		t.Error("a wrong encryption key must not decrypt pooled secrets")
	}
}

func TestTokenRoundTripAndTampering(t *testing.T) {
	secret := []byte("relay-secret")
	token := Sign(secret, "id_123", "machine-a")

	got, err := Verify(secret, token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.LicenseID != "id_123" || got.HWID != "machine-a" {
		t.Errorf("round trip = %+v", got)
	}

	if _, err := Verify([]byte("other-secret"), token); err != ErrBadToken {
		t.Errorf("a token signed elsewhere must not verify: %v", err)
	}
	if _, err := Verify(secret, Sign(secret, "id_123", "machine-b")); err != nil {
		t.Fatalf("control token should verify: %v", err)
	}
	for _, bad := range []string{"", "garbage", "no-dot", "YWJj.zzzz"} {
		if _, err := Verify(secret, bad); err == nil {
			t.Errorf("malformed token %q verified", bad)
		}
	}
}

func TestUnknownModelIsNeverFree(t *testing.T) {
	p := DefaultPricing()
	if got := p.Cost("something-we-have-not-priced"); got <= 0 {
		t.Errorf("unknown model cost = %v, want the expensive fallback", got)
	}
	if got := p.Cost("CLAUDE-OPUS-5"); got != 20 {
		t.Errorf("pricing should be case-insensitive, got %v", got)
	}
}

func TestMoneyFormatting(t *testing.T) {
	cases := map[Money]string{0: "$0.00", 5: "$0.05", 7000: "$70.00", 1234: "$12.34"}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("Money(%d) = %s, want %s", int64(in), got, want)
		}
	}
}
