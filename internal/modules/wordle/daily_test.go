package wordle

import (
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestTodayUTC_FormatStable(t *testing.T) {
	now := time.Date(2026, 5, 9, 9, 30, 0, 0, time.UTC)
	if got := todayUTC(now); got != "2026-05-09" {
		t.Errorf("todayUTC = %s, want 2026-05-09", got)
	}
}

func TestPickDaily_DeterministicForSameSeed(t *testing.T) {
	words := []string{"alpha", "bravo", "delta", "gamma"}
	a, err := pickDaily(words, "2026-05-09")
	if err != nil {
		t.Fatal(err)
	}
	b, err := pickDaily(words, "2026-05-09")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("daily picks differ: %s vs %s", a, b)
	}
}

func TestPickDaily_DifferentSeedsDiffer(t *testing.T) {
	// Not strictly required by spec — but a useful smoke that the hash isn't
	// degenerate. Use a long word list so collisions are unlikely.
	words := []string{
		"alpha", "bravo", "delta", "gamma", "echo", "fox",
		"hotel", "india", "juliet", "kilo", "lima", "mike",
	}
	a, _ := pickDaily(words, "2026-05-09")
	b, _ := pickDaily(words, "2026-05-10")
	if a == b {
		t.Logf("warn: same daily for different seeds (acceptable but rare): %s", a)
	}
}

func TestPickDaily_EmptyErrors(t *testing.T) {
	if _, err := pickDaily(nil, "x"); err == nil {
		t.Error("expected error for empty list")
	}
}

func TestPickRandom_UsesInjectedRNG(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	words := []string{"a", "b", "c", "d", "e"}
	first, err := pickRandom(words, rng)
	if err != nil {
		t.Fatal(err)
	}
	// Re-seed with same source → same sequence; lock determinism.
	rng = rand.New(rand.NewSource(1))
	again, _ := pickRandom(words, rng)
	if first != again {
		t.Errorf("seeded RNG should be deterministic: %s vs %s", first, again)
	}
}

func TestPickRandom_EmptyErrors(t *testing.T) {
	if _, err := pickRandom(nil, nil); err == nil {
		t.Error("expected error for empty list")
	}
}

// TestPickRandom_NilRNGIsRaceFree exercises the production path (rng==nil)
// from many goroutines under -race. A regression to a non-thread-safe RNG
// would flag here. Cheap insurance for the hot handler path.
func TestPickRandom_NilRNGIsRaceFree(t *testing.T) {
	words := []string{"a", "b", "c", "d", "e"}
	const goroutines = 64
	const itersEach = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < itersEach; j++ {
				if _, err := pickRandom(words, nil); err != nil {
					t.Errorf("pickRandom: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}
