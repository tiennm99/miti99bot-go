package storage

import (
	"cloud.google.com/go/firestore"
)

// FirestoreProvider is a KVProvider that creates one collection per module.
// No key prefix wrapping is needed — collection-per-module IS the isolation.
type FirestoreProvider struct {
	client *firestore.Client
}

// NewFirestoreProvider returns a provider over the given client. The client
// must outlive every KVStore the provider hands out; callers own its Close.
func NewFirestoreProvider(client *firestore.Client) *FirestoreProvider {
	return &FirestoreProvider{client: client}
}

// For returns a FirestoreKVStore writing to a collection named after the
// module. Module names are validated by modules.Build before reaching here,
// so we don't sanitize again.
func (p *FirestoreProvider) For(moduleName string) KVStore {
	return NewFirestoreKVStore(p.client, moduleName)
}
