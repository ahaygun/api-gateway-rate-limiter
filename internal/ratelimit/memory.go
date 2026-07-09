package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"
)

// MemoryLimiter is an in-process token-bucket limiter. It needs no external
// dependencies and is the right choice for a single gateway instance.
type MemoryLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time // injectable for tests
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewMemoryLimiter creates an empty in-memory limiter.
func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{
		buckets: make(map[string]*bucket),
		now:     time.Now,
	}
}

// Allow refills the bucket for key based on elapsed time, then tries to spend
// one token.
func (l *MemoryLimiter) Allow(_ context.Context, key string, rate float64, burst int) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(burst), last: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = math.Min(float64(burst), b.tokens+elapsed*rate)
		b.last = now
	}

	res := Result{Limit: burst}
	if b.tokens >= 1 {
		b.tokens--
		res.Allowed = true
		res.Remaining = int(b.tokens)
		return res, nil
	}

	deficit := 1 - b.tokens
	res.RetryAfter = time.Duration(deficit / rate * float64(time.Second))
	return res, nil
}
