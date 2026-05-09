package misc

import (
	"context"
	"testing"
	"time"

	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// We test the per-command KV behaviour directly — the bot/Telegram side is
// thin (single SendMessage) and exercising it would require a fake bot HTTP
// server. The KV interaction is the part with logic worth locking down.

func TestNew_RegistersExpectedCommands(t *testing.T) {
	deps := modules.Deps{KV: storage.NewMemoryKVStore()}
	mod := New(deps)

	want := map[string]modules.Visibility{
		"ping":     modules.VisibilityPublic,
		"mstats":   modules.VisibilityProtected,
		"fortytwo": modules.VisibilityPrivate,
	}
	if len(mod.Commands) != len(want) {
		t.Fatalf("commands count = %d, want %d", len(mod.Commands), len(want))
	}
	for _, c := range mod.Commands {
		v, ok := want[c.Name]
		if !ok {
			t.Errorf("unexpected command %q", c.Name)
			continue
		}
		if c.Visibility != v {
			t.Errorf("command %q visibility = %d, want %d", c.Name, c.Visibility, v)
		}
		if c.Handler == nil {
			t.Errorf("command %q has nil handler", c.Name)
		}
	}
}

func TestPing_WritesLastPingKV(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()

	// Drive the KV side directly: lock the wire format (ms-epoch number, not
	// RFC3339 string). A JS-written {at: 1700000000000} must round-trip into
	// the Go struct without a custom decoder.
	if err := kv.PutJSON(ctx, lastPingKey, lastPing{At: time.Now().UTC().UnixMilli()}); err != nil {
		t.Fatalf("PutJSON: %v", err)
	}

	var got lastPing
	if err := kv.GetJSON(ctx, lastPingKey, &got); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if got.At <= 0 {
		t.Errorf("read-back lastPing.At = %d, want positive ms-epoch", got.At)
	}

	// Also verify a value with the JS-shape decodes correctly.
	if err := kv.Put(ctx, lastPingKey, []byte(`{"at":1700000000000}`)); err != nil {
		t.Fatal(err)
	}
	got = lastPing{}
	if err := kv.GetJSON(ctx, lastPingKey, &got); err != nil {
		t.Fatalf("GetJSON js-shape: %v", err)
	}
	if got.At != 1700000000000 {
		t.Errorf("js-shape round-trip: At = %d, want 1700000000000", got.At)
	}
}

func TestMstats_MissingKVReturnsErrNotFound(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()

	var dst lastPing
	if err := kv.GetJSON(ctx, lastPingKey, &dst); err != storage.ErrNotFound {
		t.Errorf("GetJSON missing = %v, want ErrNotFound", err)
	}
}
