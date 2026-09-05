package license

import (
	"strings"
	"sync"
)

// Pricing turns a request into a number of cents.
//
// The upstream bills per request rather than per token, so a served call costs
// the same whether it carried ten words or ten thousand. That makes metering
// exact: no token counting, no estimation, no drift between what is charged and
// what is recorded.
type Pricing struct {
	mu       sync.RWMutex
	prices   map[string]Money
	fallback Money
}

// DefaultPricing mirrors the gateway's published per-request prices. Values are
// cents so they stay integral.
func DefaultPricing() *Pricing {
	return &Pricing{
		prices: map[string]Money{
			"claude-opus-4-8":            20,
			"claude-opus-4-8-thinking":   20,
			"claude-opus-5":              20,
			"claude-opus-5-thinking":     20,
			"claude-sonnet-4-5-20250929": 10,
			"claude-opus-4-5-20250929":   20,
			"claude-3-7-sonnet-20250219": 10,
			"claude-3-5-sonnet-20241022": 10,
			"claude-3-5-haiku-20241022":  2,
		},
		fallback: 20,
	}
}

// Cost prices one request. Unknown models fall back to the most expensive tier
// so a model we have not priced yet can never be billed as free.
func (p *Pricing) Cost(model string) Money {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if c, ok := p.prices[strings.ToLower(strings.TrimSpace(model))]; ok {
		return c
	}
	return p.fallback
}

// Set overrides or adds a model price.
func (p *Pricing) Set(model string, cents Money) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prices[strings.ToLower(strings.TrimSpace(model))] = cents
}

// All returns a copy of the price table.
func (p *Pricing) All() map[string]Money {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]Money, len(p.prices))
	for k, v := range p.prices {
		out[k] = v
	}
	return out
}

// TokenCost calculates the cost in cents for a token-based request.
// inputCostPerM and outputCostPerM are fractional cents per million tokens (stored as int64 * 1000 for precision).
// We use integer arithmetic to avoid floating point drift.
func TokenCost(inputTokens, outputTokens, inputCentsPer1M, outputCentsPer1M int64) Money {
	if inputCentsPer1M <= 0 && outputCentsPer1M <= 0 {
		return 0
	}
	// Calculate in milli-cents to preserve precision then round up.
	inputMilliCents := inputTokens * inputCentsPer1M
	outputMilliCents := outputTokens * outputCentsPer1M
	totalMilliCents := inputMilliCents + outputMilliCents
	// Divide by 1_000_000 (tokens per million) rounding up.
	cents := (totalMilliCents + 999_999) / 1_000_000
	if cents < 1 && totalMilliCents > 0 {
		cents = 1
	}
	return Money(cents)
}
