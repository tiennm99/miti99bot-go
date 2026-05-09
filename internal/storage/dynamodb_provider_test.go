package storage

import (
	"context"
	"errors"
	"testing"
)

func TestDynamoDBProvider_CrossModuleIsolation(t *testing.T) {
	client, table, cleanup := dynamoDBLocalSetup(t)
	defer cleanup()

	ctx := context.Background()
	p := NewDynamoDBProvider(client, table)

	wordle := p.For("wordle")
	loldle := p.For("loldle")

	if err := wordle.Put(ctx, "k", []byte("from-wordle")); err != nil {
		t.Fatalf("wordle.Put: %v", err)
	}
	if err := loldle.Put(ctx, "k", []byte("from-loldle")); err != nil {
		t.Fatalf("loldle.Put: %v", err)
	}

	got, err := wordle.Get(ctx, "k")
	if err != nil {
		t.Fatalf("wordle.Get: %v", err)
	}
	if string(got) != "from-wordle" {
		t.Errorf("wordle.Get: got %q, want from-wordle", got)
	}

	got, err = loldle.Get(ctx, "k")
	if err != nil {
		t.Fatalf("loldle.Get: %v", err)
	}
	if string(got) != "from-loldle" {
		t.Errorf("loldle.Get: got %q, want from-loldle", got)
	}

	// List should not leak across modules.
	wkeys, err := wordle.List(ctx, "")
	if err != nil {
		t.Fatalf("wordle.List: %v", err)
	}
	if len(wkeys) != 1 || wkeys[0] != "k" {
		t.Errorf("wordle.List: got %v, want [k]", wkeys)
	}
}

func TestDynamoDBProvider_InvalidModuleName(t *testing.T) {
	// No DynamoDB needed — invalidStore short-circuits before any SDK call.
	p := NewDynamoDBProvider(nil, "any")
	store := p.For("../../etc/passwd")

	_, err := store.Get(context.Background(), "k")
	if !errors.Is(err, ErrInvalidModuleName) {
		t.Errorf("Get: got %v, want ErrInvalidModuleName", err)
	}
}
