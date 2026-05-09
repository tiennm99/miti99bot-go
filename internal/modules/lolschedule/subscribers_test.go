package lolschedule

import (
	"context"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

func TestSubscribers_AddRemoveListIdempotent(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()

	got, _ := listSubscribers(ctx, kv)
	if len(got) != 0 {
		t.Errorf("empty list = %v, want []", got)
	}

	// First add → true.
	added, err := addSubscriber(ctx, kv, 42)
	if err != nil || !added {
		t.Fatalf("first add: added=%v err=%v", added, err)
	}
	// Idempotent re-add → false.
	added, _ = addSubscriber(ctx, kv, 42)
	if added {
		t.Errorf("re-add should be no-op")
	}

	got, _ = listSubscribers(ctx, kv)
	if len(got) != 1 || got[0] != 42 {
		t.Errorf("after add(42): %v, want [42]", got)
	}

	// Add second.
	if _, err := addSubscriber(ctx, kv, 7); err != nil {
		t.Fatal(err)
	}
	got, _ = listSubscribers(ctx, kv)
	if len(got) != 2 {
		t.Errorf("after add(7): %v, want len 2", got)
	}

	// Remove → true.
	removed, err := removeSubscriber(ctx, kv, 42)
	if err != nil || !removed {
		t.Fatalf("remove(42): removed=%v err=%v", removed, err)
	}
	// Idempotent re-remove → false.
	removed, _ = removeSubscriber(ctx, kv, 42)
	if removed {
		t.Errorf("re-remove should be no-op")
	}

	got, _ = listSubscribers(ctx, kv)
	if len(got) != 1 || got[0] != 7 {
		t.Errorf("after remove(42): %v, want [7]", got)
	}
}
