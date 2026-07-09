// Package breaker implements a per-upstream circuit breaker. It fails fast
// while an upstream is unhealthy instead of piling requests onto it, which
// prevents one failing dependency from dragging the whole gateway down.
package breaker

import (
	"sync"
	"time"
)

// State is the circuit breaker state.
type State int

const (
	// Closed lets requests through and counts failures.
	Closed State = iota
	// Open rejects requests immediately during the cooldown.
	Open
	// HalfOpen lets a trial request through to probe recovery.
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// Breaker is a consecutive-failure circuit breaker. After threshold failures
// it opens for cooldown; the next request after cooldown is a half-open trial
// that either closes the circuit (success) or re-opens it (failure).
type Breaker struct {
	threshold int
	cooldown  time.Duration

	mu       sync.Mutex
	state    State
	failures int
	openedAt time.Time

	now           func() time.Time
	onStateChange func(State)
}

// Option configures a Breaker.
type Option func(*Breaker)

// WithOnStateChange registers a callback fired whenever the state changes
// (useful for metrics and logging).
func WithOnStateChange(fn func(State)) Option {
	return func(b *Breaker) { b.onStateChange = fn }
}

// New creates a breaker that opens after threshold consecutive failures and
// stays open for cooldown.
func New(threshold int, cooldown time.Duration, opts ...Option) *Breaker {
	b := &Breaker{
		threshold: threshold,
		cooldown:  cooldown,
		state:     Closed,
		now:       time.Now,
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// Allow reports whether a request may proceed, transitioning Open → HalfOpen
// once the cooldown has elapsed.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == Open {
		if b.now().Sub(b.openedAt) >= b.cooldown {
			b.setState(HalfOpen)
			return true
		}
		return false
	}
	return true
}

// Success records a successful call, closing the circuit.
func (b *Breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures = 0
	if b.state != Closed {
		b.setState(Closed)
	}
}

// Failure records a failed call, opening the circuit once the threshold is
// reached (or immediately if a half-open trial fails).
func (b *Breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	if b.state == HalfOpen || b.failures >= b.threshold {
		b.openedAt = b.now()
		b.setState(Open)
	}
}

// State returns the current state.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// setState must be called with the lock held.
func (b *Breaker) setState(s State) {
	if b.state == s {
		return
	}
	b.state = s
	if b.onStateChange != nil {
		b.onStateChange(s)
	}
}
