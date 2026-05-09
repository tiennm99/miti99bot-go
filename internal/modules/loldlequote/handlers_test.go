package loldlequote

import (
	"context"
	"strings"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/storage"
	"github.com/tiennm99/miti99bot-go/internal/testutil"
)

// installQuote wires the loldle-quote module + auth (owner gates
// /loldle_quote_setmax). seedTarget pre-seeds a game so guess outcomes are
// deterministic without hooking math/rand.
func installQuote(t *testing.T, ownerID int64, seedSubject, seedTarget string) (*testutil.RecordingBot, storage.KVStore) {
	t.Helper()
	rb := testutil.NewRecordingBot(t)
	provider := storage.NewMemoryProvider()
	kv := provider.For("loldle-quote")
	mod := New(modules.Deps{KV: kv})
	reg := &modules.Registry{
		Modules:     []modules.Module{{Name: "loldle-quote", Commands: mod.Commands}},
		AllCommands: map[string]modules.Command{},
	}
	for _, c := range mod.Commands {
		reg.AllCommands[c.Name] = c
	}
	modules.Install(rb.Bot, reg, modules.Auth{BotOwnerID: ownerID})

	if seedTarget != "" {
		g := &gameState{Target: seedTarget, Guesses: []string{}}
		if err := saveGame(context.Background(), kv, seedSubject, g); err != nil {
			t.Fatalf("seed game: %v", err)
		}
	}
	return rb, kv
}

func TestQuote_NoArgShowsClue(t *testing.T) {
	rb, _ := installQuote(t, 0, "1", "Aatrox")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_quote"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "🎭 <i>") {
		t.Errorf("quote clue marker missing: %q", got)
	}
	if !strings.Contains(got, "</i>") {
		t.Errorf("quote italic close tag missing: %q", got)
	}
}

func TestQuote_Win(t *testing.T) {
	rb, _ := installQuote(t, 0, "1", "Aatrox")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_quote aatrox"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Nailed it") {
		t.Errorf("win reply missing 'Nailed it': %q", got)
	}
	if !strings.Contains(got, "Aatrox") {
		t.Errorf("win reply missing 'Aatrox': %q", got)
	}
}

func TestQuote_UnknownChampion(t *testing.T) {
	rb, _ := installQuote(t, 0, "1", "Aatrox")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_quote ZilbeanZ"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Champion not found") {
		t.Errorf("unknown champion reject: %q", got)
	}
}

func TestQuote_DuplicateGuessRejected(t *testing.T) {
	rb, _ := installQuote(t, 0, "1", "Aatrox")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_quote ahri"))
	rb.Reset()
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_quote ahri"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "already guessed") {
		t.Errorf("duplicate-guess reply: %q", got)
	}
}

func TestQuoteGiveup_RevealsAnswer(t *testing.T) {
	rb, _ := installQuote(t, 0, "1", "Aatrox")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_quote_giveup"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Aatrox") {
		t.Errorf("/loldle_quote_giveup should reveal Aatrox: %q", got)
	}
}

func TestQuoteStats_Empty(t *testing.T) {
	rb, _ := installQuote(t, 0, "", "")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_quote_stats"))

	got := rb.LastSent().Text()
	for _, want := range []string{"Played: 0", "Wins: 0 (0%)"} {
		if !strings.Contains(got, want) {
			t.Errorf("/loldle_quote_stats empty missing %q; got %q", want, got)
		}
	}
}

func TestQuoteSetMax_OwnerSucceeds(t *testing.T) {
	rb, kv := installQuote(t, 999, "", "")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/loldle_quote_setmax 4"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "max guesses set to 4") {
		t.Errorf("/loldle_quote_setmax reply: %q", got)
	}
	var cfg roundConfig
	if err := kv.GetJSON(context.Background(), configKey("999"), &cfg); err != nil {
		t.Fatalf("expected config persisted: %v", err)
	}
	if cfg.MaxGuesses != 4 {
		t.Errorf("MaxGuesses persisted = %d, want 4", cfg.MaxGuesses)
	}
}

func TestQuoteSetMax_DeniedToNonOwner(t *testing.T) {
	rb, _ := installQuote(t, 999, "", "")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/loldle_quote_setmax 5"))
	if calls := rb.Sent(); len(calls) != 0 {
		t.Errorf("non-owner /loldle_quote_setmax replied: %+v", calls)
	}
}
