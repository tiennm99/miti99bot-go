package migration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListKeysSinglePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header = %q", got)
		}
		if !strings.HasSuffix(r.URL.Path, "/storage/kv/namespaces/ns123/keys") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"result": [{"name":"wordle:stats:1"},{"name":"trading:sym:FPT"}],
			"success": true,
			"result_info": {"count": 2, "cursor": ""}
		}`))
	}))
	defer srv.Close()

	c := NewCloudflareKVClient("acct", "ns123", "test-token")
	c.SetBaseURL(srv.URL)

	keys, err := c.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 || keys[0] != "wordle:stats:1" || keys[1] != "trading:sym:FPT" {
		t.Fatalf("got %v", keys)
	}
}

func TestListKeysPaginates(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		switch page {
		case 1:
			_, _ = w.Write([]byte(`{
				"result": [{"name":"a"}],
				"success": true,
				"result_info": {"cursor": "p2"}
			}`))
		case 2:
			if got := r.URL.Query().Get("cursor"); got != "p2" {
				t.Errorf("cursor=%q want p2", got)
			}
			_, _ = w.Write([]byte(`{
				"result": [{"name":"b"}],
				"success": true,
				"result_info": {"cursor": ""}
			}`))
		default:
			t.Fatalf("too many pages")
		}
	}))
	defer srv.Close()

	c := NewCloudflareKVClient("acct", "ns", "tok")
	c.SetBaseURL(srv.URL)
	keys, err := c.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("got %v", keys)
	}
}

func TestGetValueReturns404AsErrKeyNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewCloudflareKVClient("acct", "ns", "tok")
	c.SetBaseURL(srv.URL)
	_, err := c.GetValue(context.Background(), "misc:last_ping")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("got %v, want ErrKeyNotFound", err)
	}
}

func TestGetValueRawBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"currency":{"VND":100},"meta":{"invested":0}}`))
	}))
	defer srv.Close()

	c := NewCloudflareKVClient("acct", "ns", "tok")
	c.SetBaseURL(srv.URL)
	val, err := c.GetValue(context.Background(), "trading:user:42")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	want := `{"currency":{"VND":100},"meta":{"invested":0}}`
	if string(val) != want {
		t.Errorf("got %q want %q", string(val), want)
	}
}
