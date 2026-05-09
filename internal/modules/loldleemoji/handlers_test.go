package loldleemoji

import (
	"context"
	"strings"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/storage"
	"github.com/tiennm99/miti99bot-go/internal/testutil"
)

// installEmoji wires the loldle-emoji module + auth (owner gates
// /loldle_emoji_setmax). seedTarget pre-seeds a game so guess outcomes are
// deterministic without hooking math/rand.
func installEmoji(t *testing.T, ownerID int64, seedSubject, seedTarget string) (*testutil.RecordingBot, storage.KVStore) {
	t.Helper()
	rb := testutil.NewRecordingBot(t)
	provider := storage.NewMemoryProvider()
	kv := provider.For("loldle-emoji")
	mod := New(modules.Deps{KV: kv})
	reg := &modules.Registry{
		Modules:     []modules.Module{{Name: "loldle-emoji", Commands: mod.Commands}},
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

func TestEmoji_NoArgShowsClue(t *testing.T) {
	rb, _ := installEmoji(t, 0, "1", "Aatrox")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_emoji"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "🎭") {
		t.Errorf("emoji clue marker missing: %q", got)
	}
}

func TestEmoji_Win(t *testing.T) {
	rb, _ := installEmoji(t, 0, "1", "Aatrox")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_emoji aatrox"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Got it") {
		t.Errorf("win reply missing 'Got it': %q", got)
	}
	if !strings.Contains(got, "Aatrox") {
		t.Errorf("win reply missing 'Aatrox': %q", got)
	}
}

func TestEmoji_UnknownChampion(t *testing.T) {
	rb, _ := installEmoji(t, 0, "1", "Aatrox")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_emoji ZilbeanZ"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Champion not found") {
		t.Errorf("unknown champion reject: %q", got)
	}
}

func TestEmoji_DuplicateGuessRejected(t *testing.T) {
	rb, _ := installEmoji(t, 0, "1", "Aatrox")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_emoji ahri"))
	rb.Reset()
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_emoji ahri"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "already guessed") {
		t.Errorf("duplicate-guess reply: %q", got)
	}
}

func TestEmojiGiveup_RevealsAnswer(t *testing.T) {
	rb, _ := installEmoji(t, 0, "1", "Aatrox")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_emoji_giveup"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Aatrox") {
		t.Errorf("/loldle_emoji_giveup should reveal Aatrox: %q", got)
	}
}

func TestEmojiStats_Empty(t *testing.T) {
	rb, _ := installEmoji(t, 0, "", "")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_emoji_stats"))

	got := rb.LastSent().Text()
	for _, want := range []string{"Played: 0", "Wins: 0 (0%)"} {
		if !strings.Contains(got, want) {
			t.Errorf("/loldle_emoji_stats empty missing %q; got %q", want, got)
		}
	}
}

func TestEmojiSetMax_OwnerSucceeds(t *testing.T) {
	rb, kv := installEmoji(t, 999, "", "")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/loldle_emoji_setmax 7"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "max guesses set to 7") {
		t.Errorf("/loldle_emoji_setmax reply: %q", got)
	}
	var cfg roundConfig
	if err := kv.GetJSON(context.Background(), configKey("999"), &cfg); err != nil {
		t.Fatalf("expected config persisted: %v", err)
	}
	if cfg.MaxGuesses != 7 {
		t.Errorf("MaxGuesses persisted = %d, want 7", cfg.MaxGuesses)
	}
}

func TestEmojiSetMax_DeniedToNonOwner(t *testing.T) {
	rb, _ := installEmoji(t, 999, "", "")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/loldle_emoji_setmax 5"))
	if calls := rb.Sent(); len(calls) != 0 {
		t.Errorf("non-owner /loldle_emoji_setmax replied: %+v", calls)
	}
}
