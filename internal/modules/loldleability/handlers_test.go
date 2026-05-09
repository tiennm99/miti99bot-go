package loldleability

import (
	"context"
	"strings"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/storage"
	"github.com/tiennm99/miti99bot-go/internal/testutil"
)

// installAbility wires the loldle-ability module + auth (owner gates
// /loldle_ability_setmax). seedTarget + seedSlot pre-seed a game so guess
// outcomes are deterministic without hooking math/rand.
func installAbility(t *testing.T, ownerID int64, seedSubject, seedTarget, seedSlot string) (*testutil.RecordingBot, storage.KVStore) {
	t.Helper()
	rb := testutil.NewRecordingBot(t)
	provider := storage.NewMemoryProvider()
	kv := provider.For("loldle-ability")
	mod := New(modules.Deps{KV: kv})
	reg := &modules.Registry{
		Modules:     []modules.Module{{Name: "loldle-ability", Commands: mod.Commands}},
		AllCommands: map[string]modules.Command{},
	}
	for _, c := range mod.Commands {
		reg.AllCommands[c.Name] = c
	}
	modules.Install(rb.Bot, reg, modules.Auth{BotOwnerID: ownerID})

	if seedTarget != "" {
		g := &gameState{Target: seedTarget, Slot: seedSlot, Guesses: []string{}}
		if err := saveGame(context.Background(), kv, seedSubject, g); err != nil {
			t.Fatalf("seed game: %v", err)
		}
	}
	return rb, kv
}

// /loldle_ability with no arg sends a photo, not a text message.
func TestAbility_NoArgSendsPhoto(t *testing.T) {
	rb, _ := installAbility(t, 0, "1", "Aatrox", "Q")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_ability"))

	calls := rb.Sent()
	if len(calls) == 0 {
		t.Fatal("/loldle_ability produced no reply")
	}
	last := calls[len(calls)-1]
	if last.Method != "sendPhoto" {
		t.Errorf("method = %q, want sendPhoto", last.Method)
	}
	// Aatrox Q icon — DDragon URL pattern.
	photo := last.Form["photo"]
	if !strings.Contains(photo, "AatroxQ") || !strings.Contains(photo, "ddragon.leagueoflegends.com") {
		t.Errorf("photo = %q, want Aatrox Q DDragon URL", photo)
	}
	caption := last.Form["caption"]
	if !strings.Contains(caption, "Guess the champion") {
		t.Errorf("caption missing prompt: %q", caption)
	}
}

func TestAbility_Win(t *testing.T) {
	rb, _ := installAbility(t, 0, "1", "Aatrox", "Q")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_ability aatrox"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Got it") {
		t.Errorf("win reply missing 'Got it': %q", got)
	}
	if !strings.Contains(got, "Aatrox") {
		t.Errorf("win reply missing 'Aatrox': %q", got)
	}
	// Ability label format: <i>Name</i> (Slot) — the slot must surface so the
	// player sees which ability the bot was thinking of.
	if !strings.Contains(got, "(Q)") {
		t.Errorf("win reply missing slot tag (Q): %q", got)
	}
}

func TestAbility_UnknownChampion(t *testing.T) {
	rb, _ := installAbility(t, 0, "1", "Aatrox", "Q")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_ability ZilbeanZ"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Champion not found") {
		t.Errorf("unknown champion reject: %q", got)
	}
}

func TestAbility_DuplicateGuessRejected(t *testing.T) {
	rb, _ := installAbility(t, 0, "1", "Aatrox", "Q")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_ability ahri"))
	rb.Reset()
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_ability ahri"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "already guessed") {
		t.Errorf("duplicate-guess reply: %q", got)
	}
}

func TestAbilityGiveup_RevealsAnswerAndAbility(t *testing.T) {
	rb, _ := installAbility(t, 0, "1", "Aatrox", "Q")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_ability_giveup"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Aatrox") {
		t.Errorf("/loldle_ability_giveup should reveal Aatrox: %q", got)
	}
	if !strings.Contains(got, "(Q)") {
		t.Errorf("/loldle_ability_giveup should include slot label: %q", got)
	}
}

func TestAbilityStats_Empty(t *testing.T) {
	rb, _ := installAbility(t, 0, "", "", "")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_ability_stats"))

	got := rb.LastSent().Text()
	for _, want := range []string{"Played: 0", "Wins: 0 (0%)"} {
		if !strings.Contains(got, want) {
			t.Errorf("/loldle_ability_stats empty missing %q; got %q", want, got)
		}
	}
}

func TestAbilitySetMax_OwnerSucceeds(t *testing.T) {
	rb, kv := installAbility(t, 999, "", "", "")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/loldle_ability_setmax 3"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "max guesses set to 3") {
		t.Errorf("/loldle_ability_setmax reply: %q", got)
	}
	var cfg roundConfig
	if err := kv.GetJSON(context.Background(), configKey("999"), &cfg); err != nil {
		t.Fatalf("expected config persisted: %v", err)
	}
	if cfg.MaxGuesses != 3 {
		t.Errorf("MaxGuesses persisted = %d, want 3", cfg.MaxGuesses)
	}
}

func TestAbilitySetMax_DeniedToNonOwner(t *testing.T) {
	rb, _ := installAbility(t, 999, "", "", "")
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/loldle_ability_setmax 5"))
	if calls := rb.Sent(); len(calls) != 0 {
		t.Errorf("non-owner /loldle_ability_setmax replied: %+v", calls)
	}
}
