package trading

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestPriceClient(t *testing.T, handler http.HandlerFunc) (*PriceClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &PriceClient{
		HTTP: &http.Client{Timeout: 2 * time.Second},
		URL:  srv.URL,
	}, srv
}

func TestPriceClient_HappyPath(t *testing.T) {
	c, _ := newTestPriceClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/TCB/data_day") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("sdate") == "" || r.URL.Query().Get("edate") == "" {
			t.Errorf("missing sdate/edate: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data_day":[{"c":24500}, {"c":24300}]}`))
	})
	got, err := c.FetchPrice(context.Background(), "TCB")
	if err != nil {
		t.Fatalf("FetchPrice: %v", err)
	}
	if got != 24500 {
		t.Errorf("price: got %v, want 24500 (latest bar = data_day[0])", got)
	}
}

func TestPriceClient_NoData_ReturnsErrNoPrice(t *testing.T) {
	c, _ := newTestPriceClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data_day":[]}`))
	})
	_, err := c.FetchPrice(context.Background(), "NOPE")
	if !errors.Is(err, ErrNoPrice) {
		t.Errorf("got %v, want ErrNoPrice", err)
	}
}

func TestPriceClient_4xx_ReturnsErrNoPrice(t *testing.T) {
	c, _ := newTestPriceClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := c.FetchPrice(context.Background(), "BADTICKER")
	if !errors.Is(err, ErrNoPrice) {
		t.Errorf("got %v, want ErrNoPrice", err)
	}
}

func TestPriceClient_NegativeClose_ReturnsErrNoPrice(t *testing.T) {
	c, _ := newTestPriceClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data_day":[{"c":-1}]}`))
	})
	_, err := c.FetchPrice(context.Background(), "WEIRD")
	if !errors.Is(err, ErrNoPrice) {
		t.Errorf("got %v, want ErrNoPrice", err)
	}
}

func TestPriceClient_EmptyTicker(t *testing.T) {
	c := &PriceClient{}
	_, err := c.FetchPrice(context.Background(), "")
	if err == nil {
		t.Error("empty ticker: expected error, got nil")
	}
}

func TestKBSFormatDate(t *testing.T) {
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), "10-05-2026"},
		{time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), "05-01-2026"},
		{time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC), "31-12-2026"},
	}
	for _, c := range cases {
		if got := kbsFormatDate(c.in); got != c.want {
			t.Errorf("kbsFormatDate(%v): got %q, want %q", c.in, got, c.want)
		}
	}
}
