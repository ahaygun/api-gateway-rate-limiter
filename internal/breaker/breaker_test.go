package breaker

import (
	"testing"
	"time"
)

func TestBreaker_OpensAfterThreshold(t *testing.T) {
	b := New(3, time.Second)

	// Two failures stay closed.
	b.Failure()
	b.Failure()
	if got := b.State(); got != Closed {
		t.Fatalf("state after 2 failures = %v, want closed", got)
	}
	if !b.Allow() {
		t.Fatal("should still allow while closed")
	}

	// Third failure trips it open.
	b.Failure()
	if got := b.State(); got != Open {
		t.Fatalf("state after 3 failures = %v, want open", got)
	}
	if b.Allow() {
		t.Fatal("should reject while open")
	}
}

func TestBreaker_HalfOpenRecoveryAndReopen(t *testing.T) {
	now := time.Unix(1000, 0)
	b := New(2, 5*time.Second)
	b.now = func() time.Time { return now }

	b.Failure()
	b.Failure() // open
	if b.Allow() {
		t.Fatal("open breaker should reject before cooldown")
	}

	// After cooldown, one trial is allowed (half-open).
	now = now.Add(5 * time.Second)
	if !b.Allow() {
		t.Fatal("should allow a trial after cooldown")
	}
	if got := b.State(); got != HalfOpen {
		t.Fatalf("state = %v, want half-open", got)
	}

	// A failing trial re-opens immediately.
	b.Failure()
	if got := b.State(); got != Open {
		t.Fatalf("state after failed trial = %v, want open", got)
	}

	// Cooldown again, then a successful trial closes it.
	now = now.Add(5 * time.Second)
	b.Allow() // half-open
	b.Success()
	if got := b.State(); got != Closed {
		t.Fatalf("state after successful trial = %v, want closed", got)
	}
}

func TestBreaker_SuccessResetsFailures(t *testing.T) {
	b := New(3, time.Second)
	b.Failure()
	b.Failure()
	b.Success() // reset
	b.Failure()
	b.Failure()
	if got := b.State(); got != Closed {
		t.Fatalf("state = %v, want closed (success should have reset the count)", got)
	}
}

func TestBreaker_StateChangeCallback(t *testing.T) {
	var changes []State
	b := New(1, time.Second, WithOnStateChange(func(s State) {
		changes = append(changes, s)
	}))
	b.Failure() // -> open
	b.now = func() time.Time { return time.Now().Add(time.Hour) }
	b.Allow()   // -> half-open
	b.Success() // -> closed

	want := []State{Open, HalfOpen, Closed}
	if len(changes) != len(want) {
		t.Fatalf("changes = %v, want %v", changes, want)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Fatalf("changes = %v, want %v", changes, want)
		}
	}
}
