package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// MemoryKVStore is an in-process KVStore for tests and local smoke runs.
// It is the only implementation available until Phase 04 adds Firestore.
type MemoryKVStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

// NewMemoryKVStore returns an empty in-memory store.
func NewMemoryKVStore() *MemoryKVStore {
	return &MemoryKVStore{m: make(map[string][]byte)}
}

func (s *MemoryKVStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[key]
	if !ok {
		return nil, ErrNotFound
	}
	out := make([]byte, len(v))
	copy(out, v)
	return out, nil
}

func (s *MemoryKVStore) GetJSON(ctx context.Context, key string, dst any) error {
	raw, err := s.Get(ctx, key)
	if err != nil {
		return err
	}
	return json.NewDecoder(bytes.NewReader(raw)).Decode(dst)
}

func (s *MemoryKVStore) Put(_ context.Context, key string, val []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := make([]byte, len(val))
	copy(stored, val)
	s.m[key] = stored
	return nil
}

func (s *MemoryKVStore) PutJSON(ctx context.Context, key string, val any) error {
	raw, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return s.Put(ctx, key, raw)
}

func (s *MemoryKVStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *MemoryKVStore) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0)
	for k := range s.m {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}
