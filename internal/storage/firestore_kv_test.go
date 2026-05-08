package storage

import (
	"context"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

// requireEmulator skips the test unless FIRESTORE_EMULATOR_HOST is set. The
// Firestore SDK auto-routes to the emulator when this env var is present.
//
// CI does not run the emulator today; these tests run locally via:
//
//	make test-emulator
func requireEmulator(t *testing.T) *firestore.Client {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set; skipping Firestore emulator test")
	}
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if project == "" {
		project = "miti99bot-go-test"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := firestore.NewClient(ctx, project)
	if err != nil {
		t.Fatalf("firestore.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// uniqueCollection returns a per-test collection name so parallel tests don't
// collide on emulator state.
func uniqueCollection(t *testing.T) string {
	t.Helper()
	return "test_" + t.Name()
}

// drainCollection deletes every document in a collection so the next test
// starts clean. The emulator does not support collection-level delete, so we
// iterate.
func drainCollection(t *testing.T, c *firestore.Client, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	docs, err := c.Collection(name).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("drain list: %v", err)
	}
	for _, d := range docs {
		if _, err := d.Ref.Delete(ctx); err != nil {
			t.Fatalf("drain delete %s: %v", d.Ref.ID, err)
		}
	}
}

func TestFirestoreKV_PutGetRoundTrip(t *testing.T) {
	c := requireEmulator(t)
	col := uniqueCollection(t)
	defer drainCollection(t, c, col)
	store := NewFirestoreKVStore(c, col)

	ctx := context.Background()
	if err := store.Put(ctx, "score", []byte("42")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Get(ctx, "score")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "42" {
		t.Errorf("Get score = %q, want 42", got)
	}
}

func TestFirestoreKV_GetMissingReturnsErrNotFound(t *testing.T) {
	c := requireEmulator(t)
	col := uniqueCollection(t)
	defer drainCollection(t, c, col)
	store := NewFirestoreKVStore(c, col)

	if _, err := store.Get(context.Background(), "missing"); err != ErrNotFound {
		t.Errorf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestFirestoreKV_PutGetJSON(t *testing.T) {
	c := requireEmulator(t)
	col := uniqueCollection(t)
	defer drainCollection(t, c, col)
	store := NewFirestoreKVStore(c, col)

	type point struct{ X, Y int }
	want := point{X: 3, Y: 4}
	if err := store.PutJSON(context.Background(), "pt", want); err != nil {
		t.Fatalf("PutJSON: %v", err)
	}
	var got point
	if err := store.GetJSON(context.Background(), "pt", &got); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if got != want {
		t.Errorf("GetJSON = %+v, want %+v", got, want)
	}
}

func TestFirestoreKV_DeleteIdempotent(t *testing.T) {
	c := requireEmulator(t)
	col := uniqueCollection(t)
	defer drainCollection(t, c, col)
	store := NewFirestoreKVStore(c, col)

	ctx := context.Background()
	if err := store.Put(ctx, "x", []byte("y")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete(ctx, "x"); err != nil {
		t.Fatalf("Delete existing: %v", err)
	}
	if err := store.Delete(ctx, "x"); err != nil {
		t.Fatalf("Delete missing should be idempotent: %v", err)
	}
	if _, err := store.Get(ctx, "x"); err != ErrNotFound {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
}

func TestFirestoreKV_ListByPrefix(t *testing.T) {
	c := requireEmulator(t)
	col := uniqueCollection(t)
	defer drainCollection(t, c, col)
	store := NewFirestoreKVStore(c, col)

	ctx := context.Background()
	for _, k := range []string{"u_1", "u_2", "u_3", "session_a", "session_b"} {
		if err := store.Put(ctx, k, []byte("x")); err != nil {
			t.Fatalf("Put %q: %v", k, err)
		}
	}

	got, err := store.List(ctx, "u_")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sort.Strings(got)
	want := []string{"u_1", "u_2", "u_3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List u_ = %v, want %v", got, want)
	}
}

func TestFirestoreKV_RejectsInvalidKeys(t *testing.T) {
	store := NewFirestoreKVStore(nil, "any") // client unused — validation runs first

	cases := map[string]string{
		"empty":         "",
		"slash":         "a/b",
		"dot":           ".",
		"dotdot":        "..",
		"reserved__":    "__namespace__",
		"too long 1501": string(make([]byte, firestoreMaxKeyLen+1)),
	}
	for label, key := range cases {
		t.Run(label, func(t *testing.T) {
			if _, err := store.Get(context.Background(), key); err == nil {
				t.Errorf("Get %q: expected validation error", key)
			}
			if err := store.Put(context.Background(), key, []byte("x")); err == nil {
				t.Errorf("Put %q: expected validation error", key)
			}
		})
	}
}

func TestFirestoreKV_ListRejectsInvalidPrefix(t *testing.T) {
	store := NewFirestoreKVStore(nil, "any") // validation runs before any client call

	for _, prefix := range []string{"a/b", "..", "__x__"} {
		t.Run(prefix, func(t *testing.T) {
			if _, err := store.List(context.Background(), prefix); err == nil {
				t.Errorf("List %q: expected validation error", prefix)
			}
		})
	}
}

func TestPrefixSuccessor(t *testing.T) {
	cases := map[string]string{
		"abc":   "abd",
		"a":     "b",
		"":      "",
		"\xff":  "\xff",   // all-0xFF: degenerates
		"a\xff": "b",      // strip trailing 0xFF, increment
	}
	for in, want := range cases {
		if got := prefixSuccessor(in); got != want {
			t.Errorf("prefixSuccessor(%q) = %q, want %q", in, got, want)
		}
	}
}
