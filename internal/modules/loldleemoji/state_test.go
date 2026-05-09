package loldleemoji

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

func TestGameState_StartedAtNullByDefault(t *testing.T) {
	g := gameState{Target: "Aatrox", Guesses: []string{}}
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"target":"Aatrox","guesses":[],"startedAt":null}`
	if string(b) != want {
		t.Errorf("marshal:\ngot  %s\nwant %s", b, want)
	}
}

func TestGameState_StartedAtAsNumber(t *testing.T) {
	at := int64(1700000000000)
	g := gameState{Target: "Aatrox", Guesses: []string{"Ahri"}, StartedAt: &at}
	b, _ := json.Marshal(g)
	want := `{"target":"Aatrox","guesses":["Ahri"],"startedAt":1700000000000}`
	if string(b) != want {
		t.Errorf("marshal:\ngot  %s\nwant %s", b, want)
	}
}

func TestStats_NoLastResultAt(t *testing.T) {
	// Parity with classic loldle: emoji stats also omit lastResultAt.
	b, _ := json.Marshal(stats{})
	want := `{"played":0,"wins":0,"streak":0,"bestStreak":0}`
	if string(b) != want {
		t.Errorf("marshal:\ngot  %s\nwant %s", b, want)
	}
}

func TestRecordResult_StreakSequence(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()

	s, err := recordResult(ctx, kv, "u1", true)
	if err != nil {
		t.Fatal(err)
	}
	if s.Streak != 1 || s.BestStreak != 1 || s.Wins != 1 {
		t.Errorf("first win: %+v", s)
	}
	s, _ = recordResult(ctx, kv, "u1", true)
	if s.Streak != 2 || s.BestStreak != 2 {
		t.Errorf("two wins: %+v", s)
	}
	s, _ = recordResult(ctx, kv, "u1", false)
	if s.Streak != 0 || s.BestStreak != 2 {
		t.Errorf("loss: %+v", s)
	}
}

func TestGetMaxGuesses_DefaultsToFive(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	if n, _ := getMaxGuesses(ctx, kv, "u1"); n != MaxGuesses {
		t.Errorf("default = %d, want %d", n, MaxGuesses)
	}
	if MaxGuesses != 5 {
		t.Errorf("MaxGuesses = %d, want 5 (parity with JS)", MaxGuesses)
	}
}

func TestSetGetMaxGuesses_RoundTripAndValidation(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	if err := setMaxGuesses(ctx, kv, "u1", 3); err != nil {
		t.Fatal(err)
	}
	if n, _ := getMaxGuesses(ctx, kv, "u1"); n != 3 {
		t.Errorf("after set(3): %d", n)
	}
	for _, n := range []int{0, -1, MaxGuessesCap + 1} {
		if err := setMaxGuesses(ctx, kv, "u1", n); err == nil {
			t.Errorf("setMaxGuesses(%d) should error", n)
		}
	}
}

// TestStateShapes_DecodeFromJSWire locks the migration contract: a raw
// record written by the JS bot must decode into the Go structs without a
// custom decoder. Captures byte-shape (not just our own round-trip).
func TestStateShapes_DecodeFromJSWire(t *testing.T) {
	t.Run("game with null startedAt", func(t *testing.T) {
		var g gameState
		raw := []byte(`{"target":"Aatrox","guesses":[],"startedAt":null}`)
		if err := json.Unmarshal(raw, &g); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if g.Target != "Aatrox" || len(g.Guesses) != 0 || g.StartedAt != nil {
			t.Errorf("decoded: %+v", g)
		}
	})

	t.Run("game with numeric startedAt and guesses", func(t *testing.T) {
		var g gameState
		raw := []byte(`{"target":"Ahri","guesses":["Aatrox","Akali"],"startedAt":1700000000000}`)
		if err := json.Unmarshal(raw, &g); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if g.Target != "Ahri" || len(g.Guesses) != 2 || g.StartedAt == nil || *g.StartedAt != 1700000000000 {
			t.Errorf("decoded: %+v (StartedAt=%v)", g, g.StartedAt)
		}
	})

	t.Run("stats", func(t *testing.T) {
		var s stats
		raw := []byte(`{"played":7,"wins":4,"streak":2,"bestStreak":3}`)
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if s.Played != 7 || s.Wins != 4 || s.Streak != 2 || s.BestStreak != 3 {
			t.Errorf("decoded: %+v", s)
		}
	})

	t.Run("config", func(t *testing.T) {
		var c roundConfig
		raw := []byte(`{"maxGuesses":7}`)
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if c.MaxGuesses != 7 {
			t.Errorf("decoded: %+v", c)
		}
	})
}

func TestSaveLoadClear_RoundTrip(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	at := int64(42)
	want := &gameState{Target: "Aatrox", Guesses: []string{"Ahri"}, StartedAt: &at}
	if err := saveGame(ctx, kv, "u1", want); err != nil {
		t.Fatal(err)
	}
	got, _ := loadGame(ctx, kv, "u1")
	if got == nil || got.Target != "Aatrox" || got.StartedAt == nil || *got.StartedAt != 42 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if err := clearGame(ctx, kv, "u1"); err != nil {
		t.Fatal(err)
	}
	got, _ = loadGame(ctx, kv, "u1")
	if got != nil {
		t.Errorf("after clear, got %+v, want nil", got)
	}
}
