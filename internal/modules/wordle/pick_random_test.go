package wordle

import (
	"math/rand"
	"sync"
	"testing"
)

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
