package lolschedule

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tiennm99/miti99bot/internal/storage"
)

// mkServer spins an httptest.Server returning the supplied JSON body for
// every page request. callCount counts upstream hits so cache tests can
// assert "1 fetch, then no more".
func mkServer(t *testing.T, body string) (*httptest.Server, *int32) {
	t.Helper()
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

const sampleBody = `{
  "data": {
    "schedule": {
      "events": [
        {
          "startTime": "2026-05-09T05:00:00Z",
          "state": "unstarted",
          "league": {"slug": "lck", "name": "LCK"},
          "match": {"teams": [{"code":"T1"},{"code":"GEN"}], "strategy":{"count":3}}
        },
        {
          "startTime": "2026-05-09T08:00:00Z",
          "state": "unstarted",
          "type": "show",
          "league": {"slug": "lck", "name": "LCK"},
          "match": {"teams": [], "strategy":{}}
        }
      ],
      "pages": {"newer": null}
    }
  }
}`

func TestGetEventsCached_FirstHitFetchesUpstream(t *testing.T) {
	srv, count := mkServer(t, sampleBody)
	c := &Client{HTTP: srv.Client(), URL: srv.URL}
	kv := storage.NewMemoryKVStore()
	from := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	events, err := c.GetEventsCached(context.Background(), kv, from, to)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("events = %d, want 1 (show filtered out)", len(events))
	}
	if events[0].League.Slug != "lck" {
		t.Errorf("event slug = %q, want lck", events[0].League.Slug)
	}
	if atomic.LoadInt32(count) != 1 {
		t.Errorf("upstream calls = %d, want 1", *count)
	}
}

func TestGetEventsCached_SecondHitUsesCache(t *testing.T) {
	srv, count := mkServer(t, sampleBody)
	c := &Client{HTTP: srv.Client(), URL: srv.URL}
	kv := storage.NewMemoryKVStore()
	from := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	// First fetch primes the cache.
	if _, err := c.GetEventsCached(context.Background(), kv, from, to); err != nil {
		t.Fatal(err)
	}
	// Second fetch within TTL must NOT hit upstream.
	if _, err := c.GetEventsCached(context.Background(), kv, from, to); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(count); got != 1 {
		t.Errorf("upstream calls = %d, want 1 (cache should serve second call)", got)
	}
}

func TestGetEventsCached_StaleFallback(t *testing.T) {
	// Prime KV with a stale-but-still-fresh-enough cache record.
	kv := storage.NewMemoryKVStore()
	from := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	staleEvents := []ScheduleEvent{
		{StartTime: "2026-05-09T05:00:00Z", League: League{Slug: "lck", Name: "LCK"}},
	}
	// 10 minutes ago — past the 120s fresh window but well inside 60-min stale.
	staleTs := time.Now().UTC().Add(-10 * time.Minute).UnixMilli()
	if err := kv.PutJSON(context.Background(), cacheKey(from, to), cacheRecord{Ts: staleTs, Events: staleEvents}); err != nil {
		t.Fatal(err)
	}

	// Upstream errors — server returns 500.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), URL: srv.URL}

	got, err := c.GetEventsCached(context.Background(), kv, from, to)
	if err != nil {
		t.Fatalf("stale fallback should succeed: %v", err)
	}
	if len(got) != 1 || got[0].League.Slug != "lck" {
		t.Errorf("stale fallback returned wrong events: %+v", got)
	}
}

func TestGetEventsCached_HardFailureWhenNoCache(t *testing.T) {
	kv := storage.NewMemoryKVStore()
	from := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), URL: srv.URL}

	_, err := c.GetEventsCached(context.Background(), kv, from, to)
	if err == nil {
		t.Errorf("expected error when upstream fails AND no cache")
	}
}

func TestFetchSchedulePage_DropsShowEvents(t *testing.T) {
	srv, _ := mkServer(t, sampleBody)
	c := &Client{HTTP: srv.Client(), URL: srv.URL}

	events, _, err := c.fetchSchedulePage(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Type == "show" {
			t.Errorf("show event leaked: %+v", e)
		}
	}
}

func TestFetchSchedulePage_NonJSONErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), URL: srv.URL}
	_, _, err := c.fetchSchedulePage(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("non-JSON should produce decode error; got %v", err)
	}
}

// truncate is internal but worth a smoke test — log payloads use it.
func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate short = %q, want unchanged", got)
	}
	got := truncate("a long enough string", 5)
	if got != "a lon..." {
		t.Errorf("truncate = %q, want 'a lon...'", got)
	}
}

// Smoke: ErrEmptyResult is exported and distinct from generic errors.
func TestErrEmptyResult_Identity(t *testing.T) {
	if errors.Is(ErrEmptyResult, errors.New("other")) {
		t.Error("ErrEmptyResult should not match arbitrary errors")
	}
}
