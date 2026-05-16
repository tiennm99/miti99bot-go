package lolschedule

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/storage"
)

// fakeSender records every SendMessage call. errOn returns a transient
// failure for the configured chat IDs; terminalErrOn returns a
// permanent-failure message string the dead-subscriber pruner recognises.
// All others succeed.
type fakeSender struct {
	mu            sync.Mutex
	calls         []bot.SendMessageParams
	errOn         map[int64]bool
	terminalErrOn map[int64]bool
}

func (f *fakeSender) SendMessage(_ context.Context, p *bot.SendMessageParams) (*models.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, *p)
	id, ok := p.ChatID.(int64)
	if ok && f.terminalErrOn[id] {
		// String shape that isTerminalSendError matches; verifies the marker
		// list works against a realistic Telegram error message.
		return nil, errors.New("Forbidden: bot was blocked by the user")
	}
	if ok && f.errOn[id] {
		return nil, errors.New("fakeSender: induced failure for chat " + chatIDString(id))
	}
	return &models.Message{}, nil
}

func chatIDString(id int64) string {
	return time.Unix(id, 0).Format("00") // arbitrary stringification; only used in error msg
}

// fixedNow returns a deterministic clock for the cron tests. Picked to land
// inside one ICT day cleanly so cache key + filter logic are stable.
func fixedNow() time.Time {
	// 2026-05-10 12:00 ICT == 05:00 UTC
	return time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
}

// seedFreshCache writes a cacheRecord with `now` as timestamp so the first
// GetEventsCached call returns it without hitting the network.
func seedFreshCache(t *testing.T, kv storage.KVStore, events []ScheduleEvent) {
	t.Helper()
	from := ictDayStartOf(fixedNow())
	to := addDays(from, 1)
	rec := cacheRecord{
		Ts:     time.Now().UTC().UnixMilli(),
		Events: events,
	}
	if err := kv.PutJSON(context.Background(), cacheKey(from, to), rec); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
}

func newTestState(t *testing.T) *state {
	t.Helper()
	kv := storage.NewMemoryKVStore()
	return &state{
		kv:     kv,
		client: &Client{}, // zero value; tests must seed cache to avoid HTTP
		nowFn:  fixedNow,
	}
}

func TestRunDailyPush_NoSubscribers(t *testing.T) {
	s := newTestState(t)
	seedFreshCache(t, s.kv, nil)

	sender := &fakeSender{}
	if err := runDailyPush(context.Background(), s, sender); err != nil {
		t.Fatalf("runDailyPush: %v", err)
	}
	if len(sender.calls) != 0 {
		t.Errorf("expected 0 sends, got %d", len(sender.calls))
	}
}

func TestRunDailyPush_SendsToAllSubscribers(t *testing.T) {
	s := newTestState(t)
	seedFreshCache(t, s.kv, nil) // empty schedule still produces a "no matches" message

	chatIDs := []int64{100, 200, 300}
	for _, id := range chatIDs {
		if _, err := addSubscriber(context.Background(), s.kv, id); err != nil {
			t.Fatalf("addSubscriber %d: %v", id, err)
		}
	}

	sender := &fakeSender{}
	if err := runDailyPush(context.Background(), s, sender); err != nil {
		t.Fatalf("runDailyPush: %v", err)
	}
	if len(sender.calls) != len(chatIDs) {
		t.Fatalf("expected %d sends, got %d", len(chatIDs), len(sender.calls))
	}
	for i, call := range sender.calls {
		gotID, ok := call.ChatID.(int64)
		if !ok {
			t.Errorf("send %d: ChatID not int64: %T", i, call.ChatID)
			continue
		}
		if gotID != chatIDs[i] {
			t.Errorf("send %d: chat got %d, want %d", i, gotID, chatIDs[i])
		}
		if call.ParseMode != models.ParseModeHTML {
			t.Errorf("send %d: parse mode got %v, want HTML", i, call.ParseMode)
		}
		if call.Text == "" {
			t.Errorf("send %d: empty text", i)
		}
	}
}

func TestRunDailyPush_PartialFailureContinues(t *testing.T) {
	s := newTestState(t)
	seedFreshCache(t, s.kv, nil)

	chatIDs := []int64{100, 200, 300}
	for _, id := range chatIDs {
		if _, err := addSubscriber(context.Background(), s.kv, id); err != nil {
			t.Fatalf("addSubscriber %d: %v", id, err)
		}
	}

	sender := &fakeSender{errOn: map[int64]bool{200: true}}
	if err := runDailyPush(context.Background(), s, sender); err != nil {
		t.Fatalf("runDailyPush: %v (should swallow per-chat failures)", err)
	}
	// All three chats should be attempted even though chat 200 failed.
	if len(sender.calls) != 3 {
		t.Errorf("expected 3 attempts (failure does not abort batch), got %d", len(sender.calls))
	}
}

// TestRunDailyPush_PrunesDeadSubscribers locks in the auto-cleanup of chats
// that have permanently blocked the bot. Recoverable (transient) errors
// MUST NOT trigger removal — only terminal Telegram errors do.
func TestRunDailyPush_PrunesDeadSubscribers(t *testing.T) {
	s := newTestState(t)
	seedFreshCache(t, s.kv, nil)

	chatIDs := []int64{100, 200, 300, 400}
	for _, id := range chatIDs {
		if _, err := addSubscriber(context.Background(), s.kv, id); err != nil {
			t.Fatalf("addSubscriber %d: %v", id, err)
		}
	}

	sender := &fakeSender{
		// 200 hit a transient failure → keep on list. 400 is permanently blocked
		// → prune from list.
		errOn:         map[int64]bool{200: true},
		terminalErrOn: map[int64]bool{400: true},
	}
	if err := runDailyPush(context.Background(), s, sender); err != nil {
		t.Fatalf("runDailyPush: %v", err)
	}

	remaining, err := listSubscribers(context.Background(), s.kv)
	if err != nil {
		t.Fatalf("listSubscribers: %v", err)
	}
	want := []int64{100, 200, 300} // 400 removed; 200 retained despite transient error
	if len(remaining) != len(want) {
		t.Fatalf("subscribers after prune: got %v, want %v", remaining, want)
	}
	for i, id := range want {
		if remaining[i] != id {
			t.Errorf("subscriber[%d]: got %d, want %d", i, remaining[i], id)
		}
	}
}

func TestIsTerminalSendError(t *testing.T) {
	terminals := []string{
		"Forbidden: bot was blocked by the user",
		"Forbidden: user is deactivated",
		"Bad Request: chat not found",
		"Bad Request: group chat was upgraded to a supergroup chat",
	}
	for _, msg := range terminals {
		if !isTerminalSendError(errors.New(msg)) {
			t.Errorf("isTerminalSendError(%q) = false, want true", msg)
		}
	}
	transients := []string{
		"connection reset by peer",
		"Too Many Requests: retry after 30",
		"context deadline exceeded",
	}
	for _, msg := range transients {
		if isTerminalSendError(errors.New(msg)) {
			t.Errorf("isTerminalSendError(%q) = true, want false (transient)", msg)
		}
	}
	if isTerminalSendError(nil) {
		t.Error("isTerminalSendError(nil) = true, want false")
	}
}

func TestDailyPushHandler_NilBot_ReturnsError(t *testing.T) {
	s := newTestState(t)
	deps := modules.Deps{KV: s.kv} // Bot intentionally nil
	err := s.dailyPushHandler(context.Background(), deps)
	if err == nil {
		t.Fatal("expected error when deps.Bot is nil, got nil")
	}
}

func TestDailyPushCron_Registration(t *testing.T) {
	s := newTestState(t)
	c := s.dailyPushCron()
	if c.Name != dailyPushCronName {
		t.Errorf("Name: got %q, want %q", c.Name, dailyPushCronName)
	}
	if c.Schedule != dailyPushSchedule {
		t.Errorf("Schedule: got %q, want %q", c.Schedule, dailyPushSchedule)
	}
	if c.Handler == nil {
		t.Error("Handler is nil")
	}
}
