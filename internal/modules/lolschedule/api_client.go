// Package lolschedule ports the JS lolschedule module — LoL esports match
// schedule via lolesports.com's persisted API.
//
// Endpoint: https://esports-api.lolesports.com/persisted/gw/getSchedule
// Auth: x-api-key header (the public key embedded in lolesports.com's web
// client — no registration). If Riot ever rotates it, lift the new value
// from their public JS bundle.
//
// Cache strategy: KV-backed, 120s fresh window with 60-minute stale
// fallback. Same shape as the JS source so cross-runtime KV migration
// round-trips byte-for-byte.
package lolschedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/tiennm99/miti99bot-go/internal/log"
	"github.com/tiennm99/miti99bot-go/internal/storage"
)

const (
	apiURL = "https://esports-api.lolesports.com/persisted/gw/getSchedule"
	// apiKey is the public lolesports.com web client key (not a secret).
	// gosec flags it as a hardcoded credential; the value is shipped in
	// Riot's own public JS bundle and serves the live site too.
	// #nosec G101
	apiKey    = "0TvQnueqKa5mxJntVWt0w4LpLfEkrV1Ta8rQBb9Z"
	userAgent = "miti99bot-go/0.1 (https://t.me/miti99bot)"
	// CacheTTL: schedule data changes minute-by-minute during live events.
	cacheTTL = 120 * time.Second
	// staleMaxAge: how long to fall back to a cached payload when the
	// upstream call fails outright.
	staleMaxAge = 60 * 60 * time.Second
	// httpTimeout: keep upstream calls bounded so a hung lolesports edge
	// can't hold a Cloud Run instance.
	httpTimeout = 8 * time.Second
)

// Team is one side of a match. JSON shape matches the lolesports response.
type Team struct {
	Name   string `json:"name,omitempty"`
	Code   string `json:"code,omitempty"`
	Image  string `json:"image,omitempty"`
	Result *struct {
		Outcome  string `json:"outcome,omitempty"` // "win" or "loss"
		GameWins int    `json:"gameWins,omitempty"`
	} `json:"result,omitempty"`
	Record *struct {
		Wins   int `json:"wins,omitempty"`
		Losses int `json:"losses,omitempty"`
	} `json:"record,omitempty"`
}

// League holds the league-section-header info on each event.
type League struct {
	Name  string `json:"name,omitempty"`
	Slug  string `json:"slug,omitempty"`
	Image string `json:"image,omitempty"`
}

// Strategy is the bestOf descriptor (Bo1, Bo3, Bo5).
type Strategy struct {
	Type  string `json:"type,omitempty"`
	Count int    `json:"count,omitempty"`
}

// Match is the inner match metadata.
type Match struct {
	ID       string   `json:"id,omitempty"`
	Teams    []Team   `json:"teams,omitempty"`
	Strategy Strategy `json:"strategy,omitempty"`
}

// ScheduleEvent is one upcoming or past match. State is "unstarted",
// "inProgress", or "completed". Type is set to "show" for pre/post-show
// segments which we filter out.
type ScheduleEvent struct {
	StartTime string `json:"startTime"`
	State     string `json:"state,omitempty"`
	Type      string `json:"type,omitempty"`
	BlockName string `json:"blockName,omitempty"`
	League    League `json:"league,omitempty"`
	Match     Match  `json:"match,omitempty"`
}

// schedulePage is the inner shape of an upstream response.
type schedulePage struct {
	Data struct {
		Schedule struct {
			Events []ScheduleEvent `json:"events"`
			Pages  struct {
				Newer string `json:"newer,omitempty"`
				Older string `json:"older,omitempty"`
			} `json:"pages,omitempty"`
		} `json:"schedule"`
	} `json:"data"`
}

// cacheRecord is the KV value: timestamp + events. Same shape as JS so KV
// export/import migration round-trips.
type cacheRecord struct {
	Ts     int64           `json:"ts"` // ms-since-epoch when fetched
	Events []ScheduleEvent `json:"events"`
}

// Client is the lolesports API client. Default zero-value uses
// http.DefaultClient + http.DefaultTransport; tests inject a custom HTTP
// client (typically pointing at httptest.Server).
type Client struct {
	HTTP *http.Client
	URL  string // override for tests; empty falls back to apiURL
}

// httpClient returns the client to use, or a sensible default.
func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: httpTimeout}
}

func (c *Client) baseURL() string {
	if c.URL != "" {
		return c.URL
	}
	return apiURL
}

// fetchSchedulePage retrieves one page of events. pageToken is the forward
// cursor from a previous call's `pages.newer`.
func (c *Client) fetchSchedulePage(ctx context.Context, pageToken string) ([]ScheduleEvent, string, error) {
	u, err := url.Parse(c.baseURL())
	if err != nil {
		return nil, "", fmt.Errorf("lolschedule parse url: %w", err)
	}
	q := u.Query()
	q.Set("hl", "en-US")
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("lolschedule build request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("lolschedule do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("lolschedule read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Warn("lolschedule_fetch", "status", resp.StatusCode, "body", truncate(string(body), 500))
		return nil, "", fmt.Errorf("lolschedule API HTTP %d", resp.StatusCode)
	}
	var page schedulePage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", fmt.Errorf("lolschedule decode: %w", err)
	}
	// Drop pre/post-show segments; they aren't matches.
	out := make([]ScheduleEvent, 0, len(page.Data.Schedule.Events))
	for _, e := range page.Data.Schedule.Events {
		if e.Type == "show" {
			continue
		}
		out = append(out, e)
	}
	return out, page.Data.Schedule.Pages.Newer, nil
}

// fetchEventsInRange paginates forward until the supplied window is covered
// or maxPages is reached. Default page returns ~20 events; week view
// usually needs 1 extra page.
func (c *Client) fetchEventsInRange(ctx context.Context, from, to time.Time, maxPages int) ([]ScheduleEvent, error) {
	if maxPages <= 0 {
		maxPages = 3
	}
	var collected []ScheduleEvent
	pageToken := ""
	for i := 0; i < maxPages; i++ {
		events, newer, err := c.fetchSchedulePage(ctx, pageToken)
		if err != nil {
			return nil, err
		}
		collected = append(collected, events...)
		// If the latest event in the page is already past our window end, stop.
		if len(events) > 0 {
			lastT, parseErr := time.Parse(time.RFC3339, events[len(events)-1].StartTime)
			if parseErr == nil && !lastT.Before(to) {
				break
			}
		}
		if newer == "" {
			break
		}
		pageToken = newer
	}
	out := make([]ScheduleEvent, 0, len(collected))
	for _, e := range collected {
		t, err := time.Parse(time.RFC3339, e.StartTime)
		if err != nil {
			continue
		}
		if !t.Before(from) && t.Before(to) {
			out = append(out, e)
		}
	}
	return out, nil
}

// cacheKey is `matches:<from-iso>:<to-iso>` — a stable key for a date range.
func cacheKey(from, to time.Time) string {
	return "matches:" + from.UTC().Format(time.RFC3339) + ":" + to.UTC().Format(time.RFC3339)
}

// GetEventsCached is the cache-first lookup. Returns fresh cache within
// cacheTTL, else fetches upstream and writes back, else falls back to
// stale cache (within staleMaxAge), else propagates the error.
func (c *Client) GetEventsCached(ctx context.Context, kv storage.KVStore, from, to time.Time) ([]ScheduleEvent, error) {
	key := cacheKey(from, to)
	now := time.Now().UTC().UnixMilli()

	var cached cacheRecord
	cacheErr := kv.GetJSON(ctx, key, &cached)
	hasCached := cacheErr == nil
	if hasCached && now-cached.Ts < cacheTTL.Milliseconds() {
		return cached.Events, nil
	}

	events, fetchErr := c.fetchEventsInRange(ctx, from, to, 3)
	if fetchErr == nil {
		rec := cacheRecord{Ts: now, Events: events}
		if err := kv.PutJSON(ctx, key, rec); err != nil {
			log.Warn("lolschedule_kv_put_fail", "err", err)
		}
		return events, nil
	}

	// Upstream failed — fall back to stale cache if recent enough.
	if hasCached && cached.Events != nil && now-cached.Ts < staleMaxAge.Milliseconds() {
		log.Warn("lolschedule_stale_fallback", "err", fetchErr)
		return cached.Events, nil
	}
	return nil, fetchErr
}

// truncate clips a string to maxLen runes with "..." if cut. Keeps the log
// output bounded — lolesports occasionally returns multi-MB error pages.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ErrEmptyResult is reserved for explicit "no events" scenarios where the
// fetch succeeded but returned zero matches. Currently unused outside tests
// but kept exported so callers can distinguish from network errors.
var ErrEmptyResult = errors.New("lolschedule: no events in range")
