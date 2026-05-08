package storage

import (
	"context"
	"reflect"
	"testing"
)

func TestPrefixed_RoundTrip(t *testing.T) {
	ctx := context.Background()
	base := NewMemoryKVStore()
	a := Prefixed(base, "modA")
	b := Prefixed(base, "modB")

	if err := a.Put(ctx, "score", []byte("10")); err != nil {
		t.Fatalf("a.Put: %v", err)
	}
	if err := b.Put(ctx, "score", []byte("20")); err != nil {
		t.Fatalf("b.Put: %v", err)
	}

	got, err := a.Get(ctx, "score")
	if err != nil {
		t.Fatalf("a.Get: %v", err)
	}
	if string(got) != "10" {
		t.Errorf("a.Get score = %q, want %q", got, "10")
	}

	got, err = b.Get(ctx, "score")
	if err != nil {
		t.Fatalf("b.Get: %v", err)
	}
	if string(got) != "20" {
		t.Errorf("b.Get score = %q, want %q", got, "20")
	}
}

func TestPrefixed_ListStripsPrefix(t *testing.T) {
	ctx := context.Background()
	base := NewMemoryKVStore()
	mod := Prefixed(base, "wordle")

	for _, k := range []string{"u:1", "u:2", "session:abc"} {
		if err := mod.Put(ctx, k, []byte("x")); err != nil {
			t.Fatalf("Put %q: %v", k, err)
		}
	}

	got, err := mod.List(ctx, "u:")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"u:1", "u:2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List u: = %v, want %v", got, want)
	}
}

func TestPrefixed_NotFoundPropagates(t *testing.T) {
	ctx := context.Background()
	base := NewMemoryKVStore()
	mod := Prefixed(base, "modA")

	if _, err := mod.Get(ctx, "missing"); err != ErrNotFound {
		t.Errorf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestPrefixed_PanicsOnEmptyPrefix(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Prefixed(_, \"\") did not panic")
		}
	}()
	Prefixed(NewMemoryKVStore(), "")
}
