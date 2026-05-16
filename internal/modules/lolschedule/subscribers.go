package lolschedule

import (
	"context"
	"errors"
	"fmt"

	"github.com/tiennm99/miti99bot/internal/storage"
)

// subscribersKey is the KV slot holding the per-module subscriber list.
// Stored as a JSON array of int64 chat ids — same shape as JS so a
// cross-runtime KV migration round-trips byte-for-byte.
const subscribersKey = "subscribers"

// listSubscribers returns the current subscriber list, or an empty slice
// if none have ever subscribed.
func listSubscribers(ctx context.Context, kv storage.KVStore) ([]int64, error) {
	var ids []int64
	err := kv.GetJSON(ctx, subscribersKey, &ids)
	switch {
	case err == nil:
		return ids, nil
	case errors.Is(err, storage.ErrNotFound):
		return nil, nil
	default:
		return nil, fmt.Errorf("lolschedule listSubscribers: %w", err)
	}
}

// addSubscriber appends chatID if absent. Returns true on first-add, false
// when already subscribed (idempotent).
//
// Concurrency: the list lives in a single KV slot, so a concurrent
// Get→mutate→Put from two chats subscribing in the same millisecond would
// drop one write. Callers MUST serialize through state.subscribersMu (or an
// equivalent module-scoped lock) before calling this.
func addSubscriber(ctx context.Context, kv storage.KVStore, chatID int64) (bool, error) {
	ids, err := listSubscribers(ctx, kv)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == chatID {
			return false, nil
		}
	}
	ids = append(ids, chatID)
	if err := kv.PutJSON(ctx, subscribersKey, ids); err != nil {
		return false, fmt.Errorf("lolschedule addSubscriber: %w", err)
	}
	return true, nil
}

// removeSubscriber drops chatID from the list. Returns true when removed,
// false when chatID wasn't present (idempotent).
//
// Concurrency: same single-slot Get→mutate→Put as addSubscriber; callers
// must hold state.subscribersMu.
func removeSubscriber(ctx context.Context, kv storage.KVStore, chatID int64) (bool, error) {
	ids, err := listSubscribers(ctx, kv)
	if err != nil {
		return false, err
	}
	out := make([]int64, 0, len(ids))
	removed := false
	for _, id := range ids {
		if id == chatID {
			removed = true
			continue
		}
		out = append(out, id)
	}
	if !removed {
		return false, nil
	}
	if err := kv.PutJSON(ctx, subscribersKey, out); err != nil {
		return false, fmt.Errorf("lolschedule removeSubscriber: %w", err)
	}
	return true, nil
}
