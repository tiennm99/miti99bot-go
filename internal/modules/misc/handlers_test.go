package misc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/storage"
	"github.com/tiennm99/miti99bot-go/internal/testutil"
)

// installMisc wires the misc module to a recording bot with a fresh
// in-memory KV. Returns the bot, the kv (so tests can pre-seed or read),
// and an Auth that permits Owner + Admin so /mstats /fortytwo dispatch.
func installMisc(t *testing.T, ownerID int64) (*testutil.RecordingBot, storage.KVStore) {
	t.Helper()
	rb := testutil.NewRecordingBot(t)
	provider := storage.NewMemoryProvider()
	kv := provider.For("misc")
	mod := New(modules.Deps{KV: kv})

	reg := &modules.Registry{
		Modules:     []modules.Module{{Name: "misc", Commands: mod.Commands}},
		AllCommands: map[string]modules.Command{},
	}
	for _, c := range mod.Commands {
		reg.AllCommands[c.Name] = c
	}
	auth := modules.Auth{BotOwnerID: ownerID}
	modules.Install(rb.Bot, reg, auth)
	return rb, kv
}

func TestPing_RepliesPongAndWritesKV(t *testing.T) {
	rb, kv := installMisc(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/ping"))

	if got := rb.LastSent().Text(); got != "pong" {
		t.Errorf("ping reply = %q, want %q", got, "pong")
	}
	var stored lastPing
	if err := kv.GetJSON(context.Background(), lastPingKey, &stored); err != nil {
		t.Fatalf("expected lastPing in KV: %v", err)
	}
	if stored.At <= 0 {
		t.Errorf("lastPing.At = %d, want positive", stored.At)
	}
	// Sanity: timestamp is within a minute of now (rules out stale fixture).
	if delta := time.Now().UTC().UnixMilli() - stored.At; delta > 60_000 || delta < 0 {
		t.Errorf("lastPing.At delta from now = %dms, want within 60s", delta)
	}
}

func TestMstats_NeverWhenKVEmpty(t *testing.T) {
	rb, _ := installMisc(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/mstats"))

	if got := rb.LastSent().Text(); got != "last ping: never" {
		t.Errorf("mstats reply = %q, want 'last ping: never'", got)
	}
}

func TestMstats_AfterPing(t *testing.T) {
	rb, _ := installMisc(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/ping"))
	rb.Reset()
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/mstats"))

	got := rb.LastSent().Text()
	if !strings.HasPrefix(got, "last ping: ") {
		t.Errorf("mstats reply = %q, want 'last ping: ...'", got)
	}
	if strings.Contains(got, "never") {
		t.Errorf("mstats still says 'never' after /ping: %q", got)
	}
}

func TestMstats_DeniedToNonAdmin(t *testing.T) {
	rb, _ := installMisc(t, 999) // owner = 999
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/mstats"))

	if calls := rb.Sent(); len(calls) != 0 {
		t.Errorf("non-admin /mstats produced replies: %+v", calls)
	}
}

func TestFortytwo_OwnerOnly(t *testing.T) {
	rb, _ := installMisc(t, 999)

	// Non-owner: silent denial
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/fortytwo"))
	if calls := rb.Sent(); len(calls) != 0 {
		t.Errorf("non-owner /fortytwo replied: %+v", calls)
	}

	// Owner: reply
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/fortytwo"))
	if got := rb.LastSent().Text(); got != "The answer." {
		t.Errorf("owner /fortytwo reply = %q, want 'The answer.'", got)
	}
}
