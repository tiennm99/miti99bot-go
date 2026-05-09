package trading

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

func TestResolveSymbol_FirstTime_QueriesAndCaches(t *testing.T) {
	kv := storage.NewMemoryKVStore()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"data_day":[{"c":24500}]}`))
	}))
	defer srv.Close()
	prices := &PriceClient{URL: srv.URL}

	got, err := ResolveSymbol(context.Background(), kv, prices, "tcb")
	if err != nil {
		t.Fatalf("ResolveSymbol: %v", err)
	}
	if got.Symbol != "TCB" || got.Category != "stock" {
		t.Errorf("resolved: got %+v, want {TCB stock TCB}", got)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("KBS hits: got %d, want 1", hits)
	}

	// Second call should hit the cache, not KBS.
	_, err = ResolveSymbol(context.Background(), kv, prices, "TCB")
	if err != nil {
		t.Fatalf("ResolveSymbol (cached): %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("KBS hits after cache: got %d, want 1 (cached)", hits)
	}
}

func TestResolveSymbol_Unknown(t *testing.T) {
	kv := storage.NewMemoryKVStore()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data_day":[]}`))
	}))
	defer srv.Close()
	prices := &PriceClient{URL: srv.URL}

	_, err := ResolveSymbol(context.Background(), kv, prices, "NOPE")
	if !errors.Is(err, ErrUnknownTicker) {
		t.Errorf("got %v, want ErrUnknownTicker", err)
	}
}

func TestResolveSymbol_EmptyInput(t *testing.T) {
	_, err := ResolveSymbol(context.Background(), storage.NewMemoryKVStore(), &PriceClient{}, "  ")
	if !errors.Is(err, ErrUnknownTicker) {
		t.Errorf("got %v, want ErrUnknownTicker for empty input", err)
	}
}

func TestResolveSymbol_NormalizesCase(t *testing.T) {
	kv := storage.NewMemoryKVStore()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// KBS endpoint should receive the upper-cased ticker.
		if !strings.Contains(r.URL.Path, "/FPT/") {
			t.Errorf("ticker not upper-cased in URL: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data_day":[{"c":120000}]}`))
	}))
	defer srv.Close()
	prices := &PriceClient{URL: srv.URL}

	got, err := ResolveSymbol(context.Background(), kv, prices, "  fpt  ")
	if err != nil {
		t.Fatalf("ResolveSymbol: %v", err)
	}
	if got.Symbol != "FPT" {
		t.Errorf("normalised: got %q, want FPT", got.Symbol)
	}
}
