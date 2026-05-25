package loldle

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tiennm99/miti99bot/internal/storage"
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

func TestStats_NoLastResultAtField(t *testing.T) {
	// loldle's stats schema deliberately differs from wordle's — no
	// lastResultAt field. Lock that, since adding the field would silently
	// change the on-disk shape for every existing player.
	b, _ := json.Marshal(stats{})
	want := `{"played":0,"wins":0,"streak":0,"bestStreak":0}`
	if string(b) != want {
		t.Errorf("marshal:\ngot  %s\nwant %s", b, want)
	}
}

func TestRecordResult_WinAndLossSequence(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()

	s, err := recordResult(ctx, kv, "u1", true)
	if err != nil {
		t.Fatal(err)
	}
	if s.Played != 1 || s.Wins != 1 || s.Streak != 1 || s.BestStreak != 1 {
		t.Errorf("first win: %+v", s)
	}
	s, _ = recordResult(ctx, kv, "u1", true)
	if s.Streak != 2 || s.BestStreak != 2 {
		t.Errorf("two wins: %+v", s)
	}
	s, _ = recordResult(ctx, kv, "u1", false)
	if s.Streak != 0 {
		t.Errorf("loss should reset streak, got %d", s.Streak)
	}
	if s.BestStreak != 2 {
		t.Errorf("best streak should persist, got %d", s.BestStreak)
	}
}

func TestGetMaxGuesses_DefaultsAndOverrides(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()

	if n, _ := getMaxGuesses(ctx, kv, "u1"); n != MaxGuesses {
		t.Errorf("missing config → %d, want %d", n, MaxGuesses)
	}

	if err := setMaxGuesses(ctx, kv, "u1", 5); err != nil {
		t.Fatal(err)
	}
	if n, _ := getMaxGuesses(ctx, kv, "u1"); n != 5 {
		t.Errorf("after setMax(5): %d, want 5", n)
	}
}

func TestSetMaxGuesses_ValidatesRange(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	for _, n := range []int{0, -1, MaxGuessesCap + 1, 100} {
		if err := setMaxGuesses(ctx, kv, "u1", n); err == nil {
			t.Errorf("setMaxGuesses(%d) should error", n)
		}
	}
	if err := setMaxGuesses(ctx, kv, "u1", 1); err != nil {
		t.Errorf("setMaxGuesses(1) should succeed: %v", err)
	}
	if err := setMaxGuesses(ctx, kv, "u1", MaxGuessesCap); err != nil {
		t.Errorf("setMaxGuesses(cap) should succeed: %v", err)
	}
}

func TestGetMaxGuesses_OutOfRangeIgnored(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	// Inject a corrupt config to simulate manual KV tampering or a stale
	// schema; getMaxGuesses must fall back to the default rather than
	// returning a wild value to handlers.
	if err := kv.PutJSON(ctx, configKey("u1"), roundConfig{MaxGuesses: 100}); err != nil {
		t.Fatal(err)
	}
	if n, _ := getMaxGuesses(ctx, kv, "u1"); n != MaxGuesses {
		t.Errorf("out-of-range config → %d, want %d (default)", n, MaxGuesses)
	}
}

func TestLoadGame_MissingReturnsNil(t *testing.T) {
	g, err := loadGame(context.Background(), storage.NewMemoryKVStore(), "nobody")
	if err != nil {
		t.Errorf("missing should not error: %v", err)
	}
	if g != nil {
		t.Errorf("expected nil, got %+v", g)
	}
}

func TestSaveLoadClearGame_RoundTrip(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	at := int64(42)
	want := &gameState{Target: "Aatrox", Guesses: []string{"Ahri"}, StartedAt: &at}
	if err := saveGame(ctx, kv, "u1", want); err != nil {
		t.Fatal(err)
	}
	got, err := loadGame(ctx, kv, "u1")
	if err != nil || got == nil {
		t.Fatalf("loadGame: %v, %v", got, err)
	}
	if got.Target != "Aatrox" || len(got.Guesses) != 1 || got.StartedAt == nil || *got.StartedAt != 42 {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	if err := clearGame(ctx, kv, "u1"); err != nil {
		t.Fatal(err)
	}
	got, _ = loadGame(ctx, kv, "u1")
	if got != nil {
		t.Errorf("after clearGame, expected nil, got %+v", got)
	}
}
