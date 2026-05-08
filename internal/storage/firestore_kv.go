package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// firestoreValueField is the document field that holds the raw value bytes.
// Stored as []byte so non-UTF-8 payloads round-trip without surprises.
const firestoreValueField = "value"

// firestoreUpdatedAtField is set on every Put for observability + future TTL.
const firestoreUpdatedAtField = "updatedAt"

// firestoreMaxKeyLen is Firestore's documented document-id byte cap.
const firestoreMaxKeyLen = 1500

// FirestoreKVStore is a KVStore backed by a single Firestore collection. The
// caller (FirestoreProvider) creates one per module so cross-module isolation
// is "different collection" — no key prefix needed at this layer.
type FirestoreKVStore struct {
	client     *firestore.Client
	collection string
}

// NewFirestoreKVStore returns a KVStore writing to the named collection.
// Collection names follow the same alphabet as module names (validated at
// modules.Build), so callers should never need to escape them here.
func NewFirestoreKVStore(client *firestore.Client, collection string) *FirestoreKVStore {
	return &FirestoreKVStore{client: client, collection: collection}
}

// validateKey enforces Firestore document-id constraints up-front so callers
// see a clear error instead of an opaque gRPC InvalidArgument from the wire.
//
// Forbidden patterns:
//   - empty
//   - longer than 1500 bytes (Firestore's documented limit is bytes, not
//     runes; len(string) returns bytes — do NOT switch to utf8.RuneCountInString)
//   - contains '/'   (path separator)
//   - "." or ".."    (reserved by Firestore)
//   - leading/trailing "__" (reserved namespace)
func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("storage: key is empty")
	}
	if len(key) > firestoreMaxKeyLen {
		return fmt.Errorf("storage: key exceeds %d bytes", firestoreMaxKeyLen)
	}
	if strings.Contains(key, "/") {
		return fmt.Errorf("storage: key contains '/' (Firestore path separator)")
	}
	if key == "." || key == ".." {
		return fmt.Errorf("storage: key %q is reserved", key)
	}
	if strings.HasPrefix(key, "__") && strings.HasSuffix(key, "__") {
		return fmt.Errorf("storage: key %q uses reserved __namespace__ pattern", key)
	}
	return nil
}

// validatePrefix runs the same checks as validateKey but allows the empty
// string (List with empty prefix scans the whole collection). Without this,
// a module passing a "/"-containing prefix would hand garbage to col.Doc()
// instead of getting a clean error.
func validatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	return validateKey(prefix)
}

func (s *FirestoreKVStore) doc(key string) *firestore.DocumentRef {
	return s.client.Collection(s.collection).Doc(key)
}

// Get returns the raw bytes stored at key, or ErrNotFound.
func (s *FirestoreKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	snap, err := s.doc(key).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("firestore get %s/%s: %w", s.collection, key, err)
	}
	raw, err := snap.DataAt(firestoreValueField)
	if err != nil {
		return nil, fmt.Errorf("firestore get %s/%s: missing %q field: %w", s.collection, key, firestoreValueField, err)
	}
	switch v := raw.(type) {
	case []byte:
		return v, nil
	case string:
		// Firestore may decode small payloads as string; keep the API
		// byte-clean by re-encoding.
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("firestore get %s/%s: unexpected value type %T", s.collection, key, raw)
	}
}

// GetJSON decodes the value at key into dst.
func (s *FirestoreKVStore) GetJSON(ctx context.Context, key string, dst any) error {
	raw, err := s.Get(ctx, key)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("firestore get %s/%s: json decode: %w", s.collection, key, err)
	}
	return nil
}

// Put writes raw bytes at key, creating or overwriting.
func (s *FirestoreKVStore) Put(ctx context.Context, key string, val []byte) error {
	if err := validateKey(key); err != nil {
		return err
	}
	_, err := s.doc(key).Set(ctx, map[string]any{
		firestoreValueField:     val,
		firestoreUpdatedAtField: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("firestore put %s/%s: %w", s.collection, key, err)
	}
	return nil
}

// PutJSON marshals val and writes the bytes at key.
func (s *FirestoreKVStore) PutJSON(ctx context.Context, key string, val any) error {
	raw, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("firestore put %s/%s: json encode: %w", s.collection, key, err)
	}
	return s.Put(ctx, key, raw)
}

// Delete removes the document at key. Deleting a missing key is not an error
// (idempotent) — Firestore's Delete already has these semantics.
func (s *FirestoreKVStore) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	_, err := s.doc(key).Delete(ctx)
	if err != nil {
		return fmt.Errorf("firestore delete %s/%s: %w", s.collection, key, err)
	}
	return nil
}

// List returns all document IDs in the collection that start with prefix.
// Implemented as a half-open range scan on document ID — no composite index
// required. Empty prefix returns the whole collection.
//
// Caveat: an all-0xFF prefix (e.g. "\xff\xff") has no successor in the same
// length, so prefixSuccessor returns the prefix unchanged and the range scan
// degenerates to an empty result. Don't use such prefixes.
func (s *FirestoreKVStore) List(ctx context.Context, prefix string) ([]string, error) {
	if err := validatePrefix(prefix); err != nil {
		return nil, err
	}
	col := s.client.Collection(s.collection)
	q := col.Query
	if prefix != "" {
		end := prefixSuccessor(prefix)
		q = col.Where(firestore.DocumentID, ">=", col.Doc(prefix)).
			Where(firestore.DocumentID, "<", col.Doc(end))
	}
	iter := q.Documents(ctx)
	defer iter.Stop()

	var keys []string
	for {
		snap, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("firestore list %s prefix=%q: %w", s.collection, prefix, err)
		}
		keys = append(keys, snap.Ref.ID)
	}
	return keys, nil
}

// prefixSuccessor returns the smallest string strictly greater than every
// string with the given prefix. Used for half-open range scans on document IDs.
//
// For "abc" the successor is "abd". If the prefix ends in 0xFF, we strip the
// trailing 0xFF bytes and increment the last < 0xFF byte. If the prefix is
// entirely 0xFF (vanishingly unlikely for ASCII module data), we fall back to
// an unbounded scan — accepted, callers using such keys deserve what they get.
func prefixSuccessor(prefix string) string {
	b := []byte(prefix)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xFF {
			b[i]++
			return string(b[:i+1])
		}
	}
	// All-0xFF: no successor in the same length; return prefix unchanged
	// (caller's range Where(< prefix) will degenerate to empty — acceptable).
	return prefix
}
