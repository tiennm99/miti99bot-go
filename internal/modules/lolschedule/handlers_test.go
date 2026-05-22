package lolschedule

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/storage"
	"github.com/tiennm99/miti99bot/internal/testutil"
)

// installSchedule wires the lolschedule module to a recording bot, with a
// custom upstream HTTP server returning bodyJSON for every request. nowMs
// fixes the clock so date-based handlers are deterministic.
func installSchedule(t *testing.T, bodyJSON string, nowMs int64) (*testutil.RecordingBot, storage.KVStore) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bodyJSON))
	}))
	t.Cleanup(upstream.Close)

	rb := testutil.NewRecordingBot(t)
	provider := storage.NewMemoryProvider()
	kv := provider.For("lolschedule")

	s := &state{
		kv:     kv,
		client: &Client{HTTP: upstream.Client(), URL: upstream.URL},
		nowFn:  func() time.Time { return time.UnixMilli(nowMs).UTC() },
	}
	mod := modules.Module{
		Name: "lolschedule",
		Commands: []modules.Command{
			{Name: "lolschedule", Visibility: modules.VisibilityPublic, Description: "x", Handler: s.handleSchedule},
			{Name: "lolschedule_today", Visibility: modules.VisibilityPublic, Description: "x", Handler: s.handleToday},
			{Name: "lolschedule_week", Visibility: modules.VisibilityPublic, Description: "x", Handler: s.handleWeek},
			{Name: "lolschedule_subscribe", Visibility: modules.VisibilityPublic, Description: "x", Handler: s.handleSubscribe},
			{Name: "lolschedule_unsubscribe", Visibility: modules.VisibilityPublic, Description: "x", Handler: s.handleUnsubscribe},
		},
	}
	reg := &modules.Registry{
		Modules:     []modules.Module{mod},
		AllCommands: map[string]modules.Command{},
	}
	for _, c := range mod.Commands {
		reg.AllCommands[c.Name] = c
	}
	modules.Install(rb.Bot, reg, modules.Auth{})
	return rb, kv
}

// 2026-05-09 12:00 UTC = 19:00 ICT (still May 9 ICT day). Used as the fake
// "now" in handler tests.
const fakeNowMs int64 = 1778328000000 // 2026-05-09T12:00:00Z

const todayBody = `{
  "data": {
    "schedule": {
      "events": [
        {
          "startTime": "2026-05-09T05:00:00Z",
          "state": "unstarted",
          "league": {"slug": "lck", "name": "LCK"},
          "match": {"teams": [{"code":"T1"},{"code":"GEN"}], "strategy":{"count":3}}
        }
      ],
      "pages": {"newer": null}
    }
  }
}`

func TestHandleToday_RendersHTMLAndFiltersMajor(t *testing.T) {
	rb, _ := installSchedule(t, todayBody, fakeNowMs)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/lolschedule_today"))

	got := rb.LastSent()
	if got.Method != "sendMessage" {
		t.Errorf("method = %q, want sendMessage", got.Method)
	}
	if got.Form["parse_mode"] != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", got.Form["parse_mode"])
	}
	for _, want := range []string{"<b>LoL —", "(ICT)", "<b>LCK</b>", "T1 vs GEN"} {
		if !strings.Contains(got.Text(), want) {
			t.Errorf("missing %q in:\n%s", want, got.Text())
		}
	}
}

func TestHandleSchedule_BadDateInput(t *testing.T) {
	rb, _ := installSchedule(t, todayBody, fakeNowMs)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/lolschedule notadate"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Invalid date") {
		t.Errorf("expected parse error reply; got %q", got)
	}
}

func TestHandleWeek_RendersWeek(t *testing.T) {
	rb, _ := installSchedule(t, todayBody, fakeNowMs)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/lolschedule_week"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "→") {
		t.Errorf("week header missing arrow: %q", got)
	}
	// fakeNowMs is Sat 2026-05-09 ICT → calendar week is Mon May 4 → Sun May 10.
	for _, want := range []string{"Mon May 4", "Sun May 10"} {
		if !strings.Contains(got, want) {
			t.Errorf("week header missing %q in:\n%s", want, got)
		}
	}
}

func TestHandleSubscribe_AddsAndIsIdempotent(t *testing.T) {
	rb, kv := installSchedule(t, todayBody, fakeNowMs)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/lolschedule_subscribe"))
	if got := rb.LastSent().Text(); !strings.HasPrefix(got, "✅") {
		t.Errorf("first subscribe should confirm; got %q", got)
	}
	rb.Reset()
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/lolschedule_subscribe"))
	if got := rb.LastSent().Text(); !strings.Contains(got, "Already subscribed") {
		t.Errorf("duplicate subscribe should report Already; got %q", got)
	}
	ids, _ := listSubscribers(context.Background(), kv)
	if len(ids) != 1 || ids[0] != 7 {
		t.Errorf("subscribers = %v, want [7]", ids)
	}
}

func TestHandleUnsubscribe(t *testing.T) {
	rb, _ := installSchedule(t, todayBody, fakeNowMs)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/lolschedule_subscribe"))
	rb.Reset()
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/lolschedule_unsubscribe"))
	if got := rb.LastSent().Text(); got != "Unsubscribed." {
		t.Errorf("unsubscribe reply = %q, want 'Unsubscribed.'", got)
	}
	rb.Reset()
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/lolschedule_unsubscribe"))
	if got := rb.LastSent().Text(); !strings.Contains(got, "weren't subscribed") {
		t.Errorf("idempotent unsubscribe reply = %q", got)
	}
}

func TestHandleSchedule_UpstreamFailureGivesFriendlyError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	rb := testutil.NewRecordingBot(t)
	provider := storage.NewMemoryProvider()
	kv := provider.For("lolschedule")
	s := &state{
		kv:     kv,
		client: &Client{HTTP: upstream.Client(), URL: upstream.URL},
		nowFn:  func() time.Time { return time.UnixMilli(fakeNowMs).UTC() },
	}
	cmd := modules.Command{Name: "lolschedule_today", Visibility: modules.VisibilityPublic, Description: "x", Handler: s.handleToday}
	reg := &modules.Registry{
		Modules:     []modules.Module{{Name: "lolschedule", Commands: []modules.Command{cmd}}},
		AllCommands: map[string]modules.Command{cmd.Name: cmd},
	}
	modules.Install(rb.Bot, reg, modules.Auth{})

	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/lolschedule_today"))
	if got := rb.LastSent().Text(); !strings.Contains(got, "Could not fetch") {
		t.Errorf("expected friendly fetch-error reply; got %q", got)
	}
}
