package storage

import (
	"context"
	"fmt"
)

// ErrInvalidModuleName is returned by every operation on an invalidStore —
// the sentinel emitted when FirestoreProvider.For is asked for a module
// whose name fails collectionNameRe.
var ErrInvalidModuleName = fmt.Errorf("storage: invalid module name")

// invalidStore is a KVStore that errors on every call. Returned by
// FirestoreProvider.For when the requested module name doesn't validate.
// Callers see a real KVStore but every op errors at use, surfacing the
// configuration bug at the first read/write rather than silently writing
// to an attacker-controllable collection name.
type invalidStore struct {
	name string
}

func (s invalidStore) wrap(op string) error {
	return fmt.Errorf("%w: %q (op=%s)", ErrInvalidModuleName, s.name, op)
}

func (s invalidStore) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, s.wrap("Get")
}
func (s invalidStore) GetJSON(_ context.Context, _ string, _ any) error { return s.wrap("GetJSON") }
func (s invalidStore) Put(_ context.Context, _ string, _ []byte) error  { return s.wrap("Put") }
func (s invalidStore) PutJSON(_ context.Context, _ string, _ any) error { return s.wrap("PutJSON") }
func (s invalidStore) Delete(_ context.Context, _ string) error         { return s.wrap("Delete") }
func (s invalidStore) List(_ context.Context, _ string) ([]string, error) {
	return nil, s.wrap("List")
}
