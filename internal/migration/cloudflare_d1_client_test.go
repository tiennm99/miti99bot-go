package migration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestD1QueryHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/d1/database/db123/query") {
			t.Errorf("path=%s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["sql"] != "SELECT * FROM trading_trades" {
			t.Errorf("sql=%v", got["sql"])
		}
		_, _ = w.Write([]byte(`{
			"success": true,
			"result": [{
				"results": [
					{"id": 1, "user_id": 100, "symbol": "FPT"},
					{"id": 2, "user_id": 100, "symbol": "TCB"}
				],
				"success": true
			}]
		}`))
	}))
	defer srv.Close()

	c := NewCloudflareD1Client("acct", "db123", "tok")
	c.SetBaseURL(srv.URL)

	rows, err := c.Query(context.Background(), "SELECT * FROM trading_trades", nil)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0]["symbol"] != "FPT" || rows[1]["symbol"] != "TCB" {
		t.Errorf("rows=%v", rows)
	}
}

func TestD1QueryApiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success": false, "errors": [{"code": 8000, "message": "bad sql"}]}`))
	}))
	defer srv.Close()

	c := NewCloudflareD1Client("acct", "db", "tok")
	c.SetBaseURL(srv.URL)
	_, err := c.Query(context.Background(), "SELECT", nil)
	if err == nil || !strings.Contains(err.Error(), "api error") {
		t.Fatalf("got %v, want api error", err)
	}
}
