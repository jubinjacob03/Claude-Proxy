// Package license is the relay's source of truth: who may use the proxy, on
// which machine, and how much of their allowance is left.
//
// Every rule here is enforced server-side on purpose. The client is assumed to
// be hostile: it never holds an upstream API key and never decides its own
// quota, so tampering with the local install cannot buy free usage.
package license

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Money is a whole number of cents. Floating point is deliberately avoided so
// that repeated debits cannot drift a balance.
type Money int64

func (m Money) String() string {
	sign := ""
	if m < 0 {
		sign = "-"
		m = -m
	}
	return fmt.Sprintf("%s$%d.%02d", sign, int64(m)/100, int64(m)%100)
}

// Status values reported to the client. The client only ever needs to know
// whether it may proceed, so the wire vocabulary is intentionally small.
const (
	StatusOK        = "ok"
	StatusExhausted = "quota_exhausted"
)

var (
	ErrNotFound     = errors.New("license not found")
	ErrInactive     = errors.New("license is not active")
	ErrHWIDMismatch = errors.New("license is bound to another machine")
	ErrHWIDMissing  = errors.New("hardware id is required")
	ErrExhausted    = errors.New("usage quota exhausted")
)

// License is a single sold seat.
type License struct {
	ID         string
	Key        string // only populated when the licence is first created
	RawKey     string `json:"raw_key"` // stored in DB for copying
	KeyHint    string
	HWID       string
	QuotaCents Money
	SpentCents Money
	Active     bool
	Note       string
	CreatedAt  time.Time
	BoundAt    *time.Time
	LastSeenAt *time.Time
}

// Remaining never reports below zero, so a single overspending request cannot
// make the next one look like it has credit.
func (l *License) Remaining() Money {
	if l.SpentCents >= l.QuotaCents {
		return 0
	}
	return l.QuotaCents - l.SpentCents
}

func (l *License) Bound() bool { return l.HWID != "" }

// PoolKey is one upstream credential shared by every licensed user. These never
// leave the relay.
type PoolKey struct {
	ID           string
	Label        string
	Secret       string
	Provider     string
	PoolGroup    string
	BalanceCents Money
	SpentCents   Money
	Active       bool
	CreatedAt    time.Time
	LastUsed     *time.Time
	ExhaustedAt  *time.Time
}

type EndpointProfile struct {
	Name                string
	ClaudeBaseURL       string
	PoolGroup           string
	Active              bool
	CreatedAt           time.Time
	BillingMode         string
	PerRequestCostCents Money
	InputCostPerM       int64
	OutputCostPerM      int64
}

const (
	BillingModePerRequest  = "per_request"
	BillingModeTokenBased  = "token_based"
	DefaultPerRequestCents = 30
)

// Remaining reports the credit left on a pooled key.
func (k *PoolKey) Remaining() Money {
	if k.SpentCents >= k.BalanceCents {
		return 0
	}
	return k.BalanceCents - k.SpentCents
}

// Providers a pooled key can serve.
const (
	ProviderClaude = "claude"
)

// UsageEvent is one metered request. Rows are append-only so that a user's
// spend can always be reconciled against what was actually served.
type UsageEvent struct {
	ID           string
	LicenseID    string
	PoolKeyID    string
	Provider     string
	Model        string
	CostCents    Money
	InputTokens  int64
	OutputTokens int64
	Streamed      bool
	StatusCode   int
	CreatedAt    time.Time
}

// NewID returns a random identifier suitable for a primary key.
func NewID() string {
	return "id_" + randomHex(12)
}

// NewKey formats a fresh license key. The grouped layout is only for humans
// retyping it; entropy comes from the 20 random hex characters.
func NewKey() string {
	raw := strings.ToUpper(randomHex(10))
	return fmt.Sprintf("CP-%s-%s-%s-%s-%s",
		raw[0:4], raw[4:8], raw[8:12], raw[12:16], raw[16:20])
}

// NormalizeKey makes key lookup forgiving about case and padding without
// weakening it: the stored form is always upper case and trimmed.
func NormalizeKey(key string) string {
	return strings.ToUpper(strings.TrimSpace(key))
}

// KeyHint is the only part of a key kept for display once it has been issued.
func KeyHint(key string) string {
	key = NormalizeKey(key)
	if len(key) <= 8 {
		return key
	}
	return key[:8] + "..."
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("license: system entropy unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
