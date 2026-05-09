package ai

import (
	"sync"

	"golang.org/x/time/rate"
)

// PerUserLimiter is a tiny wrapper around x/time/rate.Limiter that gives each
// subject (user-id or chat-id, formatted as a string) its own token bucket.
//
// Why per-user: the Gemini free tier is 15 RPM / 1500 RPD shared across the
// entire bot. A single chatty user could exhaust the daily quota in minutes;
// a soft per-user cap (default 5/min) cushions everyone else.
//
// Why we don't enforce daily caps here: x/time/rate is a token bucket, not
// a fixed-window counter. Per-day caps need a different abstraction; if we
// hit RPD limits in practice we'll add a Firestore-backed counter. Phase 11
// soak data will tell us if it's needed.
type PerUserLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rate.Limiter
	r       rate.Limit // refill rate per second
	burst   int        // bucket capacity
}

// NewPerUserLimiter returns a limiter that allows `burst` requests
// instantaneously and refills at `perSec` requests per second.
//
// Defaults for the bot: burst=5, perSec=5/60 → 5 reqs in any 60s window.
func NewPerUserLimiter(perSec float64, burst int) *PerUserLimiter {
	if burst < 1 {
		burst = 1
	}
	return &PerUserLimiter{
		buckets: map[string]*rate.Limiter{},
		r:       rate.Limit(perSec),
		burst:   burst,
	}
}

// Allow consumes one token from the subject's bucket. Returns false if the
// bucket is empty — caller should reply with a "slow down" message and NOT
// invoke the upstream model.
func (p *PerUserLimiter) Allow(subject string) bool {
	p.mu.Lock()
	b, ok := p.buckets[subject]
	if !ok {
		b = rate.NewLimiter(p.r, p.burst)
		p.buckets[subject] = b
	}
	p.mu.Unlock()
	return b.Allow()
}
