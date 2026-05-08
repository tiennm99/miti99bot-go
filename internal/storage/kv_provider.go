package storage

// KVProvider yields a per-module KVStore. Implementations decide how isolation
// is achieved: MemoryProvider wraps a single in-process store with a key
// prefix, FirestoreProvider uses one collection per module.
//
// Modules never construct KVStores directly — they receive one through their
// factory's Deps. This keeps the storage backend swappable without touching
// any module code.
type KVProvider interface {
	For(moduleName string) KVStore
}

// MemoryProvider is a KVProvider backed by a single in-process MemoryKVStore.
// Each module sees a Prefixed view that prevents cross-module key collisions.
//
// Intended for tests, local smoke runs, and the no-Firestore fallback. State
// is lost when the process exits.
type MemoryProvider struct {
	base *MemoryKVStore
}

// NewMemoryProvider returns a fresh in-process provider.
func NewMemoryProvider() *MemoryProvider {
	return &MemoryProvider{base: NewMemoryKVStore()}
}

// For returns a per-module view of the shared in-memory store.
func (m *MemoryProvider) For(moduleName string) KVStore {
	return Prefixed(m.base, moduleName)
}

// Base exposes the underlying unprefixed store. Tests use this to assert
// cross-module isolation by reading raw keys; production code must not.
func (m *MemoryProvider) Base() *MemoryKVStore { return m.base }
