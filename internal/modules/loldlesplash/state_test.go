package loldlesplash

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// gameState gains a `skinId` field — locks the chosen splash at round start.
func TestGameState_IncludesSkinIDField(t *testing.T) {
	g := gameState{Target: "Aatrox", SkinID: 3, Guesses: []string{}}
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"target":"Aatrox","skinId":3,"guesses":[],"startedAt":null}`
	if string(b) != want {
		t.Errorf("marshal:\ngot  %s\nwant %s", b, want)
	}
}

// JS-wire-format decode parity: a record written by the JS bot must decode
// directly. Locks the skinId field name + null-startedAt round-trip.
func TestGameState_DecodeFromJSWire(t *testing.T) {
	var g gameState
	raw := []byte(`{"target":"Ahri","skinId":7,"guesses":["Akali"],"startedAt":1700000000000}`)
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if g.Target != "Ahri" || g.SkinID != 7 || len(g.Guesses) != 1 || g.StartedAt == nil || *g.StartedAt != 1700000000000 {
		t.Errorf("decoded: %+v", g)
	}
}

func TestGetMaxGuesses_DefaultsToFour(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	if n, _ := getMaxGuesses(ctx, kv, "u1"); n != MaxGuesses {
		t.Errorf("default = %d, want %d", n, MaxGuesses)
	}
	if MaxGuesses != 4 {
		t.Errorf("MaxGuesses = %d, want 4 (parity with JS — splash is harder than ability)", MaxGuesses)
	}
}

func TestRecordResult_StreakSequence(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	s, _ := recordResult(ctx, kv, "u1", true)
	if s.Streak != 1 || s.Wins != 1 {
		t.Errorf("first win: %+v", s)
	}
	s, _ = recordResult(ctx, kv, "u1", false)
	if s.Streak != 0 || s.BestStreak != 1 {
		t.Errorf("loss after streak=1: %+v", s)
	}
}

func TestSaveLoadClear_RoundTrip(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	at := int64(42)
	want := &gameState{Target: "Aatrox", SkinID: 5, Guesses: []string{"Ahri"}, StartedAt: &at}
	if err := saveGame(ctx, kv, "u1", want); err != nil {
		t.Fatal(err)
	}
	got, _ := loadGame(ctx, kv, "u1")
	if got == nil || got.SkinID != 5 {
		t.Errorf("round-trip lost skinId: %+v", got)
	}
	if err := clearGame(ctx, kv, "u1"); err != nil {
		t.Fatal(err)
	}
	got, _ = loadGame(ctx, kv, "u1")
	if got != nil {
		t.Errorf("after clear, got %+v, want nil", got)
	}
}
