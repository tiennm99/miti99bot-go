// Package keylock serialises compound operations that target the same key
// (typically a chat / user / subject identifier) across goroutines.
//
// Why a separate package: every game module needs a per-subject mutex to
// turn KVStore's single-op atomicity into safe Get→mutate→Put. The bot
// dispatcher runs each Telegram update in its own goroutine, and the JS
// source's Cloudflare Workers serialisation is not a property Go inherits.
//
// Trade-off: the underlying sync.Map grows unboundedly with distinct keys
// (~32 B each). At 1M keys that's ~32 MB — acceptable for the lifetime of
// a Cloud Run instance, which restarts well before reaching that scale.
// Eviction is a Phase 11 concern, not a v1 one.
package keylock

import "sync"

// Map gives each string key its own mutex, lazily created. Zero value is
// usable; do not copy after first use (sync.Map is non-copyable).
type Map struct {
	m sync.Map // key: string → val: *sync.Mutex
}

// Acquire locks the per-key mutex and returns its Unlock as a func so the
// caller can `defer m.Acquire(key)()` at the top of a critical section.
//
// Distinct keys never block each other; same-key callers run serially in the
// order Acquire was called.
func (m *Map) Acquire(key string) func() {
	v, _ := m.m.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
