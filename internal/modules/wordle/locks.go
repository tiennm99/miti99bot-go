package wordle

import "sync"

// subjectLocks serialises Get → mutate → Put compound operations on a
// GameState by per-subject mutex.
//
// Why: the KVStore guarantees atomicity on a single op (Get, Put, Delete) but
// not on a load-then-save sequence. With the bot dispatcher running each
// Telegram update in its own goroutine, two concurrent /wordle calls for the
// same chat (groups can have many active users) can interleave and silently
// drop one player's guess. The Cloudflare Workers source the JS bot ran in
// happens to serialise this for free; Go + Firestore does not.
//
// Implementation: one *sync.Mutex per subject, lazily created via sync.Map.
// The map grows unboundedly with distinct subjects but each entry is ~32 B,
// so 1M chats costs ~32 MB — acceptable for v1. Sharded eviction is a Phase
// 11 concern.
type subjectLocks struct {
	m sync.Map // key: subject string → val: *sync.Mutex
}

// acquire locks the per-subject mutex and returns the unlock func. Callers
// must `defer s.acquire(subject)()` at the top of any handler that reads,
// mutates, then writes the same KV record.
func (s *subjectLocks) acquire(subject string) func() {
	v, _ := s.m.LoadOrStore(subject, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
