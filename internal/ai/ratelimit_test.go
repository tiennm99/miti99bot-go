package ai

import "testing"

func TestPerUserLimiter_BurstThenDrop(t *testing.T) {
	// 5 burst, 0 refill — second batch must drop.
	l := NewPerUserLimiter(0, 5)
	for i := 0; i < 5; i++ {
		if !l.Allow("user-1") {
			t.Fatalf("burst[%d]: want allow, got drop", i)
		}
	}
	if l.Allow("user-1") {
		t.Errorf("post-burst: want drop, got allow")
	}
}

func TestPerUserLimiter_PerSubjectIsolated(t *testing.T) {
	l := NewPerUserLimiter(0, 1)
	if !l.Allow("a") {
		t.Fatalf("user a first call dropped")
	}
	if l.Allow("a") {
		t.Errorf("user a second call: want drop")
	}
	// Different subject must have its own bucket.
	if !l.Allow("b") {
		t.Errorf("user b first call dropped — buckets not isolated")
	}
}

func TestPerUserLimiter_BurstFloor(t *testing.T) {
	// burst=0 → floored to 1 so the limiter never permanently blocks.
	l := NewPerUserLimiter(0, 0)
	if !l.Allow("x") {
		t.Errorf("burst=0 floored: want first call allowed")
	}
}
