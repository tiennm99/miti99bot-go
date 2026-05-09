package storage

import (
	"regexp"

	"cloud.google.com/go/firestore"
)

// collectionNameRe mirrors modules.moduleNameRe. Defense-in-depth: callers
// should validate first (modules.Build does), but a junk collection name
// that escapes validation could let any caller drop docs into someone
// else's namespace. Match the canonical alphabet here too.
var collectionNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

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
// module. moduleName is re-validated against collectionNameRe — defense in
// depth against caller bugs that bypass modules.Build. An invalid name
// returns a store whose every operation errors with ErrInvalidModuleName,
// so the bug surfaces at first use rather than silently writing to a
// junk-named collection.
func (p *FirestoreProvider) For(moduleName string) KVStore {
	if !collectionNameRe.MatchString(moduleName) {
		return invalidStore{name: moduleName}
	}
	return NewFirestoreKVStore(p.client, moduleName)
}
