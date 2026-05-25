// Package lolschedule serves LoL esports match schedules via lolesports.com's
// persisted API plus a daily push to subscribers.
//
// Endpoint: https://esports-api.lolesports.com/persisted/gw/getSchedule
// Auth: x-api-key header — the public key embedded in lolesports.com's web
// client (no registration). If Riot ever rotates it, lift the new value
// from their public JS bundle.
//
// Cache strategy: KV-backed cacheRecord with a 120s fresh window and a
// 60-minute stale fallback (stale-while-error).
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
	"unicode/utf8"

	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/storage"
)

const (
	apiURL = "https://esports-api.lolesports.com/persisted/gw/getSchedule"
	// apiKey is the public lolesports.com web client key (not a secret).
	// gosec flags it as a hardcoded credential; the value is shipped in
	// Riot's own public web bundle and serves the live site too.
	// #nosec G101
	apiKey    = "0TvQnueqKa5mxJntVWt0w4LpLfEkrV1Ta8rQBb9Z"
	userAgent = "miti99bot/0.1 (https://t.me/miti99bot)"
	// CacheTTL: schedule data changes minute-by-minute during live events.
	cacheTTL = 120 * time.Second
	// staleMaxAge: how long to fall back to a cached payload when the
	// upstream call fails outright.
	staleMaxAge = 60 * 60 * time.Second
	// httpTimeout: keep upstream calls bounded so a hung lolesports edge
	// can't hold a Lambda instance.
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

// cacheRecord is the KV value: fetch timestamp (ms-epoch) + events.
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

// fetchSchedulePage retrieves one page of events. pageToken can be either a
// forward (`pages.newer`) or backward (`pages.older`) cursor — the upstream
// uses the same query param for both directions. Returns the page's events
// (sorted ascending by startTime) plus both cursor tokens for further
// navigation in either direction.
func (c *Client) fetchSchedulePage(ctx context.Context, pageToken string) ([]ScheduleEvent, string, string, error) {
	u, err := url.Parse(c.baseURL())
	if err != nil {
		return nil, "", "", fmt.Errorf("lolschedule parse url: %w", err)
	}
	q := u.Query()
	q.Set("hl", "en-US")
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("lolschedule build request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("lolschedule do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", fmt.Errorf("lolschedule read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Warn("lolschedule_fetch", "status", resp.StatusCode, "body", truncate(string(body), 500))
		return nil, "", "", fmt.Errorf("lolschedule API HTTP %d", resp.StatusCode)
	}
	var page schedulePage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", "", fmt.Errorf("lolschedule decode: %w", err)
	}
	// Drop pre/post-show segments; they aren't matches.
	out := make([]ScheduleEvent, 0, len(page.Data.Schedule.Events))
	for _, e := range page.Data.Schedule.Events {
		if e.Type == "show" {
			continue
		}
		out = append(out, e)
	}
	return out, page.Data.Schedule.Pages.Newer, page.Data.Schedule.Pages.Older, nil
}

// earliestStart returns the first parseable startTime in a page that is sorted
// ascending. ok=false when no event has a valid timestamp.
func earliestStart(events []ScheduleEvent) (time.Time, bool) {
	for _, e := range events {
		if t, err := time.Parse(time.RFC3339, e.StartTime); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// latestStart returns the last parseable startTime in a page that is sorted
// ascending. ok=false when no event has a valid timestamp.
func latestStart(events []ScheduleEvent) (time.Time, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if t, err := time.Parse(time.RFC3339, events[i].StartTime); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// fetchEventsInRange covers [from, to) by walking both pagination directions
// from the default page (anchored at "now"). Walks older pages until the
// earliest collected event is ≤ from, then walks newer pages until the
// latest is ≥ to. Page budget caps both directions combined to bound
// upstream calls during dense weeks.
func (c *Client) fetchEventsInRange(ctx context.Context, from, to time.Time, maxPages int) ([]ScheduleEvent, error) {
	if maxPages <= 0 {
		maxPages = 8
	}
	events, newer, older, err := c.fetchSchedulePage(ctx, "")
	if err != nil {
		return nil, err
	}
	collected := events
	pages := 1

	// Walk older while the window extends before what we've collected.
	for pages < maxPages && older != "" {
		if t, ok := earliestStart(collected); ok && !t.After(from) {
			break
		}
		olderEvents, _, prevOlder, err := c.fetchSchedulePage(ctx, older)
		if err != nil {
			return nil, err
		}
		// Each page is ascending and disjoint from the next, so prepending
		// preserves overall ascending order.
		collected = append(olderEvents, collected...)
		older = prevOlder
		pages++
	}

	// Walk newer while the window extends past what we've collected.
	for pages < maxPages && newer != "" {
		if t, ok := latestStart(collected); ok && !t.Before(to) {
			break
		}
		newerEvents, nextNewer, _, err := c.fetchSchedulePage(ctx, newer)
		if err != nil {
			return nil, err
		}
		collected = append(collected, newerEvents...)
		newer = nextNewer
		pages++
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

// truncate clips a string to a rune-boundary prefix whose byte length is
// <= maxLen, appending "..." if cut. Keeps log output bounded — lolesports
// occasionally returns multi-MB error pages, and team/player names mix in
// Korean/Chinese characters that a raw byte slice would split mid-codepoint
// (producing replacement glyphs in CloudWatch).
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// ErrEmptyResult is reserved for explicit "no events" scenarios where the
// fetch succeeded but returned zero matches. Currently unused outside tests
// but kept exported so callers can distinguish from network errors.
var ErrEmptyResult = errors.New("lolschedule: no events in range")
