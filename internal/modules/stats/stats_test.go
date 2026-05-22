package stats

import (
	"context"
	"strings"
	"testing"

	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/storage"
	"github.com/tiennm99/miti99bot/internal/testutil"
)

func TestNew_RegistersExpectedCommands(t *testing.T) {
	deps := modules.Deps{KV: storage.NewMemoryKVStore()}
	mod := New(deps)

	if len(mod.Commands) != 1 {
		t.Fatalf("commands count = %d, want 1", len(mod.Commands))
	}
	cmd := mod.Commands[0]
	if cmd.Name != "stats" {
		t.Errorf("command name = %q, want %q", cmd.Name, "stats")
	}
	if cmd.Visibility != modules.VisibilityPublic {
		t.Errorf("command visibility = %d, want Public", cmd.Visibility)
	}
	if cmd.Handler == nil {
		t.Error("command handler is nil")
	}
	if mod.CommandHook == nil {
		t.Error("CommandHook is nil")
	}
}

func TestInc_PersistsCountInKV(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	c := &counter{kv: kv}

	c.Inc(ctx, "ping")
	c.Inc(ctx, "ping")
	c.Inc(ctx, "wordle")

	var entry countEntry
	if err := kv.GetJSON(ctx, countKey("ping"), &entry); err != nil {
		t.Fatalf("GetJSON ping: %v", err)
	}
	if entry.N != 2 {
		t.Errorf("ping count = %d, want 2", entry.N)
	}

	entry = countEntry{}
	if err := kv.GetJSON(ctx, countKey("wordle"), &entry); err != nil {
		t.Fatalf("GetJSON wordle: %v", err)
	}
	if entry.N != 1 {
		t.Errorf("wordle count = %d, want 1", entry.N)
	}
}

func installStats(t *testing.T) (*testutil.RecordingBot, *counter) {
	t.Helper()
	rb := testutil.NewRecordingBot(t)
	kv := storage.NewMemoryKVStore()
	c := &counter{kv: kv}
	mod := modules.Module{
		Commands: []modules.Command{statsCommand(c)},
	}
	reg := &modules.Registry{
		AllCommands: map[string]modules.Command{},
	}
	for _, cmd := range mod.Commands {
		reg.AllCommands[cmd.Name] = cmd
	}
	modules.Install(rb.Bot, reg, modules.Auth{})
	return rb, c
}

func TestStats_NoDataRepliesEmpty(t *testing.T) {
	rb, _ := installStats(t)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/stats"))

	got := rb.LastSent().Text()
	if got != "No command stats yet." {
		t.Errorf("empty stats reply = %q, want 'No command stats yet.'", got)
	}
}

func TestStats_ShowsCountsSortedByPopularity(t *testing.T) {
	ctx := context.Background()
	rb, c := installStats(t)

	c.Inc(ctx, "ping")
	c.Inc(ctx, "wordle")
	c.Inc(ctx, "wordle")
	c.Inc(ctx, "wordle")
	c.Inc(ctx, "loldle")
	c.Inc(ctx, "loldle")

	rb.Bot.ProcessUpdate(ctx, testutil.NewPrivateMessage(1, "/stats"))
	got := rb.LastSent().Text()

	if !strings.HasPrefix(got, "Command usage:") {
		t.Errorf("reply missing header: %q", got)
	}
	// Verify descending order: wordle(3) > loldle(2) > ping(1)
	wordlePos := strings.Index(got, "/wordle:")
	loLdlePos := strings.Index(got, "/loldle:")
	pingPos := strings.Index(got, "/ping:")
	if wordlePos < 0 || loLdlePos < 0 || pingPos < 0 {
		t.Fatalf("reply missing expected commands: %q", got)
	}
	if wordlePos >= loLdlePos || loLdlePos >= pingPos {
		t.Errorf("commands not in descending count order: wordle=%d loldle=%d ping=%d in %q",
			wordlePos, loLdlePos, pingPos, got)
	}
}

func TestCommandHook_FiredThroughModulesBuild(t *testing.T) {
	ctx := context.Background()
	provider := storage.NewMemoryProvider()

	reg, err := modules.Build(
		[]string{"stats"},
		map[string]modules.Factory{"stats": New},
		provider,
		modules.BuildOptions{},
	)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(reg.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(reg.Modules))
	}

	rb := testutil.NewRecordingBot(t)
	modules.Install(rb.Bot, reg, modules.Auth{})

	// Dispatch /ping — not a registered command, so nothing replies.
	// But RunCommandHooks should have fired and incremented count:ping.
	reg.RunCommandHooks(ctx, "ping")

	statsKV := provider.For("stats")
	var entry countEntry
	if err := statsKV.GetJSON(ctx, countKey("ping"), &entry); err != nil {
		t.Fatalf("expected count:ping in KV after hook: %v", err)
	}
	if entry.N != 1 {
		t.Errorf("count:ping = %d, want 1", entry.N)
	}
}
