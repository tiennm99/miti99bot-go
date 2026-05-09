package loldlesplash

import (
	"context"
	"strings"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/storage"
	"github.com/tiennm99/miti99bot-go/internal/testutil"
)

// installSplash wires the loldle-splash module + auth (owner gates
// /loldle_splash_setmax). seedTarget + seedSkinID pre-seed a game so guess
// outcomes are deterministic without hooking math/rand.
func installSplash(t *testing.T, ownerID int64, seedSubject, seedTarget string, seedSkinID int) (*testutil.RecordingBot, storage.KVStore) {
	t.Helper()
	rb := testutil.NewRecordingBot(t)
	provider := storage.NewMemoryProvider()
	kv := provider.For("loldle-splash")
	mod := New(modules.Deps{KV: kv})
	reg := &modules.Registry{
		Modules:     []modules.Module{{Name: "loldle-splash", Commands: mod.Commands}},
		AllCommands: map[string]modules.Command{},
	}
	for _, c := range mod.Commands {
		reg.AllCommands[c.Name] = c
	}
	modules.Install(rb.Bot, reg, modules.Auth{BotOwnerID: ownerID})

	if seedTarget != "" {
		g := &gameState{Target: seedTarget, SkinID: seedSkinID, Guesses: []string{}}
		if err := saveGame(context.Background(), kv, seedSubject, g); err != nil {
			t.Fatalf("seed game: %v", err)
		}
	}
	return rb, kv
}

func TestSplash_NoArgSendsPhoto(t *testing.T) {
	rb, _ := installSplash(t, 0, "1", "Aatrox", 0)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_splash"))

	calls := rb.Sent()
	if len(calls) == 0 {
		t.Fatal("/loldle_splash produced no reply")
	}
	last := calls[len(calls)-1]
	if last.Method != "sendPhoto" {
		t.Errorf("method = %q, want sendPhoto", last.Method)
	}
	photo := last.Form["photo"]
	if !strings.Contains(photo, "Aatrox_0.jpg") {
		t.Errorf("photo = %q, want Aatrox_0 splash URL", photo)
	}
	caption := last.Form["caption"]
	if !strings.Contains(caption, "splash art") {
		t.Errorf("caption missing prompt: %q", caption)
	}
}

func TestSplash_Win(t *testing.T) {
	rb, _ := installSplash(t, 0, "1", "Aatrox", 0)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_splash aatrox"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Got it") {
		t.Errorf("win reply missing 'Got it': %q", got)
	}
	if !strings.Contains(got, "Aatrox") {
		t.Errorf("win reply missing 'Aatrox': %q", got)
	}
	// Skin label format: "in <i>Default</i> skin"
	if !strings.Contains(got, "Default") {
		t.Errorf("win reply missing skin name: %q", got)
	}
}

func TestSplash_UnknownChampion(t *testing.T) {
	rb, _ := installSplash(t, 0, "1", "Aatrox", 0)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_splash ZilbeanZ"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Champion not found") {
		t.Errorf("unknown champion reject: %q", got)
	}
}

func TestSplashGiveup_RevealsAnswerAndSkin(t *testing.T) {
	rb, _ := installSplash(t, 0, "1", "Aatrox", 0)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_splash_giveup"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Aatrox") {
		t.Errorf("/loldle_splash_giveup should reveal Aatrox: %q", got)
	}
	if !strings.Contains(got, "Default") {
		t.Errorf("/loldle_splash_giveup should include skin label: %q", got)
	}
}

func TestSplashStats_Empty(t *testing.T) {
	rb, _ := installSplash(t, 0, "", "", 0)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/loldle_splash_stats"))

	got := rb.LastSent().Text()
	for _, want := range []string{"Played: 0", "Wins: 0 (0%)"} {
		if !strings.Contains(got, want) {
			t.Errorf("/loldle_splash_stats empty missing %q; got %q", want, got)
		}
	}
}

func TestSplashSetMax_OwnerSucceeds(t *testing.T) {
	rb, kv := installSplash(t, 999, "", "", 0)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/loldle_splash_setmax 5"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "max guesses set to 5") {
		t.Errorf("/loldle_splash_setmax reply: %q", got)
	}
	var cfg roundConfig
	if err := kv.GetJSON(context.Background(), configKey("999"), &cfg); err != nil {
		t.Fatalf("expected config persisted: %v", err)
	}
	if cfg.MaxGuesses != 5 {
		t.Errorf("MaxGuesses persisted = %d, want 5", cfg.MaxGuesses)
	}
}

func TestSplashSetMax_DeniedToNonOwner(t *testing.T) {
	rb, _ := installSplash(t, 999, "", "", 0)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/loldle_splash_setmax 5"))
	if calls := rb.Sent(); len(calls) != 0 {
		t.Errorf("non-owner /loldle_splash_setmax replied: %+v", calls)
	}
}
