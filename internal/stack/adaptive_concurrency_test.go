package stack

import "testing"

func TestAdaptiveConcurrency_RampsUpAfterSuccesses(t *testing.T) {
	a := NewAdaptiveConcurrency(4)
	if a.Target != 1 {
		t.Fatalf("expected target=1, got %d", a.Target)
	}
	// Needs 2 successes per +1.
	a.OnSuccess()
	if a.Target != 1 {
		t.Fatalf("expected target=1 after 1 success, got %d", a.Target)
	}
	a.OnSuccess()
	if a.Target != 2 {
		t.Fatalf("expected target=2 after 2 successes, got %d", a.Target)
	}
	a.OnSuccess()
	a.OnSuccess()
	if a.Target != 3 {
		t.Fatalf("expected target=3 after 4 successes, got %d", a.Target)
	}
}

func TestAdaptiveConcurrency_ShrinksOnRateLimit(t *testing.T) {
	a := NewAdaptiveConcurrency(8)
	a.Target = 8
	a.OnFailure("RATE_LIMIT")
	if a.Target != 4 {
		t.Fatalf("expected target=4 after rate-limit shrink, got %d", a.Target)
	}
	// Cooldown blocks ramping up even with successes.
	for i := 0; i < 10; i++ {
		a.OnSuccess()
		if i < 4 && a.Target != 4 {
			t.Fatalf("expected target=4 during cooldown, got %d at i=%d", a.Target, i)
		}
	}
}

func TestAdaptiveConcurrency_ShrinksOnConflictMildly(t *testing.T) {
	a := NewAdaptiveConcurrency(4)
	a.Target = 3
	a.OnFailure("CONFLICT")
	if a.Target != 3 {
		t.Fatalf("expected target unchanged after single conflict, got %d", a.Target)
	}
	a.OnFailure("CONFLICT")
	if a.Target != 2 {
		t.Fatalf("expected target=2 after repeated conflicts, got %d", a.Target)
	}
}

func TestAdaptiveConcurrency_DoesNotOscillateOnPermanentErrors(t *testing.T) {
	a := NewAdaptiveConcurrency(4)
	a.Target = 3
	a.OnFailure("OTHER")
	if a.Target != 3 {
		t.Fatalf("expected target unchanged on OTHER, got %d", a.Target)
	}
	// Needs cooldown success + two clean successes to ramp.
	a.OnSuccess()
	a.OnSuccess()
	a.OnSuccess()
	if a.Target != 4 {
		t.Fatalf("expected target to ramp after successes, got %d", a.Target)
	}
}
