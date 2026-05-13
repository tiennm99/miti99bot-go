package twentyq

import (
	"context"
	"strings"
	"testing"

	"github.com/tiennm99/miti99bot/internal/ai"
	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/storage"
	"github.com/tiennm99/miti99bot/internal/testutil"
)

// scriptedChatter returns canned responses by call index. Tests script the
// exact sequence the handler will see (round-start then judge per turn).
type scriptedChatter struct {
	responses []string
	err       error
	calls     int
}

func (s *scriptedChatter) Generate(_ context.Context, _, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.calls >= len(s.responses) {
		return "{}", nil
	}
	r := s.responses[s.calls]
	s.calls++
	return r, nil
}

func install(t *testing.T, c ai.Chatter) *testutil.RecordingBot {
	t.Helper()
	rb := testutil.NewRecordingBot(t)
	reg, err := modules.Build([]string{"twentyq"},
		map[string]modules.Factory{"twentyq": New},
		storage.NewMemoryProvider(),
		modules.BuildOptions{Chatter: c})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	modules.Install(rb.Bot, reg, modules.Auth{})
	return rb
}

func TestTwentyq_NoChatterRefuses(t *testing.T) {
	rb := install(t, nil)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/twentyq"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "GEMINI_API_KEY") {
		t.Errorf("missing-key warning: got %q", last)
	}
}

func TestTwentyq_FreshRoundShowsIntro(t *testing.T) {
	c := &scriptedChatter{responses: []string{
		`{"category":"animal","initialHint":"a cryptic clue"}`,
	}}
	rb := install(t, c)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/twentyq"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "animal") || !strings.Contains(last, "cryptic clue") {
		t.Errorf("intro: got %q", last)
	}
}

func TestTwentyq_RoundStartFallback(t *testing.T) {
	c := &scriptedChatter{responses: []string{"unparseable garbage"}}
	rb := install(t, c)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/twentyq"))
	last := rb.LastSent().Text()
	// Fallback category=object, initialHint=everyday-life.
	if !strings.Contains(last, "object") || !strings.Contains(last, "everyday life") {
		t.Errorf("fallback intro: got %q", last)
	}
}

func TestTwentyq_GiveupNoActiveRound(t *testing.T) {
	c := &scriptedChatter{}
	rb := install(t, c)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/twentyq_giveup"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "No active round") {
		t.Errorf("giveup-no-round: got %q", last)
	}
}

func TestTwentyq_StatsEmpty(t *testing.T) {
	c := &scriptedChatter{}
	rb := install(t, c)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/twentyq_stats"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "No twentyq games") {
		t.Errorf("empty stats: got %q", last)
	}
}

func TestTwentyq_RateLimitedRoundStart(t *testing.T) {
	c := &scriptedChatter{err: ai.ErrRateLimited}
	rb := install(t, c)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/twentyq"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "rate-limited") {
		t.Errorf("rate-limited reply: got %q", last)
	}
}
