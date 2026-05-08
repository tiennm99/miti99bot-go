package storage

import (
	"context"
	"strings"
)

// Prefixed returns a KVStore that transparently prepends prefix+":" to every
// key. List() strips the prefix from returned keys so callers see their own
// flat namespace. The prefix must be non-empty.
func Prefixed(inner KVStore, prefix string) KVStore {
	if prefix == "" {
		panic("storage: Prefixed requires non-empty prefix")
	}
	return &prefixedStore{inner: inner, prefix: prefix + ":"}
}

type prefixedStore struct {
	inner  KVStore
	prefix string
}

func (p *prefixedStore) k(key string) string { return p.prefix + key }

func (p *prefixedStore) Get(ctx context.Context, key string) ([]byte, error) {
	return p.inner.Get(ctx, p.k(key))
}

func (p *prefixedStore) GetJSON(ctx context.Context, key string, dst any) error {
	return p.inner.GetJSON(ctx, p.k(key), dst)
}

func (p *prefixedStore) Put(ctx context.Context, key string, val []byte) error {
	return p.inner.Put(ctx, p.k(key), val)
}

func (p *prefixedStore) PutJSON(ctx context.Context, key string, val any) error {
	return p.inner.PutJSON(ctx, p.k(key), val)
}

func (p *prefixedStore) Delete(ctx context.Context, key string) error {
	return p.inner.Delete(ctx, p.k(key))
}

func (p *prefixedStore) List(ctx context.Context, prefix string) ([]string, error) {
	keys, err := p.inner.List(ctx, p.k(prefix))
	if err != nil {
		return nil, err
	}
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = strings.TrimPrefix(k, p.prefix)
	}
	return out, nil
}
