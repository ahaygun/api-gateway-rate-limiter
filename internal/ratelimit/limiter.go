// Package ratelimit implements per-client token-bucket rate limiting with
// two interchangeable backends: an in-memory limiter (single instance) and a
// Redis-backed limiter (shared across many gateway instances).
package ratelimit

import (
	"context"
	"time"
)

// Result is the outcome of a rate-limit check.
type Result struct {
	Allowed    bool
	Limit      int           // bucket capacity (burst)
	Remaining  int           // tokens left after this request
	RetryAfter time.Duration // how long until the next token, when rejected
}

// Limiter decides whether a request identified by key may proceed. rate is
// tokens added per second; burst is the bucket capacity.
type Limiter interface {
	Allow(ctx context.Context, key string, rate float64, burst int) (Result, error)
}
