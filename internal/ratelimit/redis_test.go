package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// newTestRedisLimiter spins up an in-process Redis (miniredis) and returns a
// limiter pointed at it, with a controllable clock.
func newTestRedisLimiter(t *testing.T) (*RedisLimiter, *miniredis.Miniredis, *time.Time) {
	t.Helper()
	mr := miniredis.RunT(t)
	l, err := NewRedisLimiter(context.Background(), mr.Addr())
	if err != nil {
		t.Fatalf("new redis limiter: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	return l, mr, &now
}

func TestRedisLimiter_BurstThenRefill(t *testing.T) {
	l, _, now := newTestRedisLimiter(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		res, err := l.Allow(ctx, "k", 1, 3)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if !res.Allowed {
			t.Fatalf("request %d should be allowed", i)
		}
	}

	res, err := l.Allow(ctx, "k", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed {
		t.Fatal("4th request should be denied")
	}
	if res.RetryAfter <= 0 {
		t.Fatalf("expected positive RetryAfter, got %v", res.RetryAfter)
	}

	*now = now.Add(time.Second)
	if res, _ := l.Allow(ctx, "k", 1, 3); !res.Allowed {
		t.Fatal("request after 1s refill should be allowed")
	}
}

func TestRedisLimiter_IndependentKeys(t *testing.T) {
	l, _, _ := newTestRedisLimiter(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		l.Allow(ctx, "a", 1, 2)
	}
	if res, _ := l.Allow(ctx, "a", 1, 2); res.Allowed {
		t.Fatal("key a should be exhausted")
	}
	if res, _ := l.Allow(ctx, "b", 1, 2); !res.Allowed {
		t.Fatal("key b should have its own bucket")
	}
}
