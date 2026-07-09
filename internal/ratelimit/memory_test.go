package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestMemoryLimiter_BurstThenRefill(t *testing.T) {
	l := NewMemoryLimiter()
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	ctx := context.Background()

	// burst=3, rate=1 token/sec: the first 3 requests drain the bucket.
	for i := 0; i < 3; i++ {
		res, err := l.Allow(ctx, "k", 1, 3)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if !res.Allowed {
			t.Fatalf("request %d should be allowed", i)
		}
	}

	// 4th request is denied and reports a positive wait.
	res, _ := l.Allow(ctx, "k", 1, 3)
	if res.Allowed {
		t.Fatal("4th request should be denied")
	}
	if res.RetryAfter <= 0 {
		t.Fatalf("expected positive RetryAfter, got %v", res.RetryAfter)
	}

	// After 1 second, exactly one token refills.
	now = now.Add(time.Second)
	if res, _ := l.Allow(ctx, "k", 1, 3); !res.Allowed {
		t.Fatal("request after 1s refill should be allowed")
	}
	if res, _ := l.Allow(ctx, "k", 1, 3); res.Allowed {
		t.Fatal("only one token should have refilled")
	}
}

func TestMemoryLimiter_RefillCapsAtBurst(t *testing.T) {
	l := NewMemoryLimiter()
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	ctx := context.Background()

	l.Allow(ctx, "k", 1, 5) // create bucket, spend 1 (4 left)

	// Wait a long time; tokens must cap at burst, not overflow.
	now = now.Add(time.Hour)
	allowed := 0
	for i := 0; i < 10; i++ {
		if res, _ := l.Allow(ctx, "k", 1, 5); res.Allowed {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("expected exactly 5 allowed (capped at burst), got %d", allowed)
	}
}

func TestMemoryLimiter_PerKeyIsolation(t *testing.T) {
	l := NewMemoryLimiter()
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
