package lolschedule

import (
	"context"
	"testing"

	"github.com/tiennm99/miti99bot/internal/storage"
)

func TestSubscribers_AddRemoveListIdempotent(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()

	got, _ := listSubscribers(ctx, kv)
	if len(got) != 0 {
		t.Errorf("empty list = %v, want []", got)
	}

	added, err := addSubscriber(ctx, kv, 42, 0)
	if err != nil || !added {
		t.Fatalf("first add: added=%v err=%v", added, err)
	}
	added, _ = addSubscriber(ctx, kv, 42, 0)
	if added {
		t.Errorf("re-add should be no-op")
	}

	got, _ = listSubscribers(ctx, kv)
	if len(got) != 1 || got[0] != (Subscriber{ChatID: 42}) {
		t.Errorf("after add(42,0): %v, want [{42 0}]", got)
	}

	if _, err := addSubscriber(ctx, kv, 7, 0); err != nil {
		t.Fatal(err)
	}
	got, _ = listSubscribers(ctx, kv)
	if len(got) != 2 {
		t.Errorf("after add(7,0): %v, want len 2", got)
	}

	removed, err := removeSubscriber(ctx, kv, 42, 0)
	if err != nil || !removed {
		t.Fatalf("remove(42,0): removed=%v err=%v", removed, err)
	}
	removed, _ = removeSubscriber(ctx, kv, 42, 0)
	if removed {
		t.Errorf("re-remove should be no-op")
	}

	got, _ = listSubscribers(ctx, kv)
	if len(got) != 1 || got[0].ChatID != 7 {
		t.Errorf("after remove(42,0): %v, want [{7 0}]", got)
	}
}

// TestSubscribers_TopicsAreDistinct locks in the forum-topic fix: the same
// chat can subscribe in multiple topics independently, and removing one
// topic must not affect the others.
func TestSubscribers_TopicsAreDistinct(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()

	for _, tid := range []int{0, 5, 9} {
		added, err := addSubscriber(ctx, kv, 100, tid)
		if err != nil || !added {
			t.Fatalf("add(100,%d): added=%v err=%v", tid, added, err)
		}
	}
	// Same (chat, thread) is rejected as duplicate.
	if added, _ := addSubscriber(ctx, kv, 100, 5); added {
		t.Errorf("duplicate (100,5) should be no-op")
	}

	got, _ := listSubscribers(ctx, kv)
	if len(got) != 3 {
		t.Fatalf("after 3 adds: %v, want len 3", got)
	}

	// Removing one topic leaves the other two intact.
	if removed, _ := removeSubscriber(ctx, kv, 100, 5); !removed {
		t.Error("remove(100,5) should report removed=true")
	}
	got, _ = listSubscribers(ctx, kv)
	if len(got) != 2 {
		t.Errorf("after remove(100,5): %v, want len 2", got)
	}
	for _, s := range got {
		if s.ChatID == 100 && s.ThreadID == 5 {
			t.Errorf("(100,5) still present: %v", got)
		}
	}
}

// TestSubscribers_RemoveAllForChat covers the chat-wide prune used when a
// terminal error means every topic in that chat is dead too.
func TestSubscribers_RemoveAllForChat(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()

	for _, tid := range []int{0, 5, 9} {
		if _, err := addSubscriber(ctx, kv, 100, tid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := addSubscriber(ctx, kv, 200, 0); err != nil {
		t.Fatal(err)
	}

	n, err := removeAllForChat(ctx, kv, 100)
	if err != nil {
		t.Fatalf("removeAllForChat(100): %v", err)
	}
	if n != 3 {
		t.Errorf("removeAllForChat(100) returned %d, want 3", n)
	}

	got, _ := listSubscribers(ctx, kv)
	if len(got) != 1 || got[0].ChatID != 200 {
		t.Errorf("after wipe(100): %v, want only chat 200", got)
	}

	// Idempotent: second call removes nothing.
	if n, _ := removeAllForChat(ctx, kv, 100); n != 0 {
		t.Errorf("second removeAllForChat(100) = %d, want 0", n)
	}
}

// TestSubscribers_LegacyDecode locks in backward-compat reads of rows
// written before topic-aware subscriptions (raw []int64 JSON).
func TestSubscribers_LegacyDecode(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()

	// Write legacy shape directly.
	if err := kv.Put(ctx, subscribersKey, []byte(`[11,22,33]`)); err != nil {
		t.Fatal(err)
	}

	got, err := listSubscribers(ctx, kv)
	if err != nil {
		t.Fatalf("listSubscribers: %v", err)
	}
	want := []Subscriber{{ChatID: 11}, {ChatID: 22}, {ChatID: 33}}
	if len(got) != len(want) {
		t.Fatalf("legacy decode: got %v, want %v", got, want)
	}
	for i, s := range got {
		if s != want[i] {
			t.Errorf("legacy[%d]: got %v, want %v", i, s, want[i])
		}
	}

	// Next mutation rewrites in the new shape — verify by reading raw.
	if _, err := addSubscriber(ctx, kv, 44, 7); err != nil {
		t.Fatal(err)
	}
	raw, _ := kv.Get(ctx, subscribersKey)
	if string(raw) == `[11,22,33,44]` {
		t.Errorf("expected rewrite to new shape, still legacy: %s", raw)
	}
}
