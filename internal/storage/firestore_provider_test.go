package storage

import (
	"context"
	"errors"
	"testing"
)

// FirestoreProvider.For re-validates the module name as defense-in-depth.
// We can't actually exercise valid names without a Firestore client, but
// invalid names return invalidStore (no client touched), which is the
// branch worth locking.
func TestFirestoreProvider_For_RejectsInvalidName(t *testing.T) {
	p := &FirestoreProvider{client: nil}

	bogus := []string{
		"",                                // empty
		"with spaces",                     // not allowed
		"WITHCAPS",                        // not allowed
		"path/traversal",                  // attempted slash injection
		"../etc/passwd",                   // attempted traversal
		"way-too-long-for-our-32-char-limit-x", // exceeds 32 chars
		"with:colon",                      // explicit ban — colon is the prefixed-store delimiter
	}
	for _, name := range bogus {
		store := p.For(name)
		_, err := store.Get(context.Background(), "any-key")
		if !errors.Is(err, ErrInvalidModuleName) {
			t.Errorf("For(%q).Get → %v, want ErrInvalidModuleName", name, err)
		}
	}
}

func TestFirestoreProvider_For_AcceptsCanonicalNames(t *testing.T) {
	// Canonical names match the regex: lowercase + digits + underscore + hyphen,
	// 1..32 chars. We can't dereference the returned FirestoreKVStore (nil
	// client), but we can assert it's NOT an invalidStore — validation passed.
	p := &FirestoreProvider{client: nil}
	for _, name := range []string{"misc", "demo-mod", "wordle", "x", "a1_b-2"} {
		store := p.For(name)
		if _, ok := store.(invalidStore); ok {
			t.Errorf("For(%q) returned invalidStore; expected validation to pass", name)
		}
	}
}
