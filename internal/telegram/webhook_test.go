package telegram

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const testSecret = "super-secret-token"

// validUpdate is a minimal Telegram update payload that decodes cleanly. The
// bot has no handlers registered so ProcessUpdate is a no-op match.
const validUpdate = `{"update_id": 1}`

func mustBot(t *testing.T) *bot.Bot {
	t.Helper()
	b, err := NewBot("TEST:TOKEN")
	if err != nil {
		t.Fatalf("NewBot: %v", err)
	}
	return b
}

func TestWebhookHandler_RejectsNonPost(t *testing.T) {
	h := WebhookHandler(mustBot(t), testSecret)
	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestWebhookHandler_RejectsMissingSecret(t *testing.T) {
	h := WebhookHandler(mustBot(t), testSecret)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(validUpdate))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestWebhookHandler_RejectsWrongSecret(t *testing.T) {
	h := WebhookHandler(mustBot(t), testSecret)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(validUpdate))
	req.Header.Set(secretTokenHeader, "wrong")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestWebhookHandler_RejectsWrongSecretSamePrefix(t *testing.T) {
	// Locks the constant-time compare: a value sharing a prefix must still
	// 401, not silently succeed.
	h := WebhookHandler(mustBot(t), testSecret)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(validUpdate))
	req.Header.Set(secretTokenHeader, testSecret[:len(testSecret)-1]+"X")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestWebhookHandler_RejectsMalformedJSON(t *testing.T) {
	h := WebhookHandler(mustBot(t), testSecret)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("not-json"))
	req.Header.Set(secretTokenHeader, testSecret)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestWebhookHandler_RejectsOversizedBody(t *testing.T) {
	h := WebhookHandler(mustBot(t), testSecret)
	// Valid-prefixed JSON so the decoder doesn't bail on the first byte; the
	// long string field forces a read past maxWebhookBody, triggering
	// *http.MaxBytesError. Plain "aaaa…" without the JSON wrapper would fail
	// at byte 1 with a SyntaxError and never exercise the cap.
	body := bytes.Buffer{}
	body.WriteString(`{"update_id":1,"message":{"text":"`)
	body.Write(bytes.Repeat([]byte("a"), maxWebhookBody+1))
	body.WriteString(`"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", &body)
	req.Header.Set(secretTokenHeader, testSecret)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

func TestWebhookHandler_AcceptsValidUpdate(t *testing.T) {
	h := WebhookHandler(mustBot(t), testSecret)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(validUpdate))
	req.Header.Set(secretTokenHeader, testSecret)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestTruncateRunes_KeepsUTF8Valid(t *testing.T) {
	// Single-byte (ASCII): output must equal a byte slice when boundary aligns.
	if got := truncateRunes("hello world", 5); got != "hello" {
		t.Errorf("ascii: got %q, want %q", got, "hello")
	}
	// Multi-byte (Vietnamese): max=5 bytes, "ầ" is 3 bytes ("\xe1\xba\xa7").
	// "h" (1) + "ầ" (3) = 4 bytes; next rune would push to 7. truncate at 5
	// would land mid-rune; the helper must walk back to byte 4 so the slice
	// ends on a rune boundary and the result decodes cleanly.
	if got := truncateRunes("hầuhầuhầu", 5); got != "hầu" {
		t.Errorf("vietnamese: got %q (len %d), want %q (len %d)", got, len(got), "hầu", len("hầu"))
	}
	// Length-below-cap path: pass through unchanged.
	if got := truncateRunes("abc", 10); got != "abc" {
		t.Errorf("short: got %q, want %q", got, "abc")
	}
}

// panicUpdate matches the panicHandler registered below by /panic command.
const panicUpdate = `{"update_id":2,"message":{"message_id":1,"date":1,"chat":{"id":1,"type":"private"},"from":{"id":1,"is_bot":false,"first_name":"x"},"text":"/panic","entities":[{"type":"bot_command","offset":0,"length":6}]}}`

func TestWebhookHandler_RecoversPanicAndReturns200(t *testing.T) {
	// A panicking handler must NOT propagate to the http.Server (would close
	// the response mid-write and trigger Telegram's 24-hour retry storm on the
	// same poisoned update). Recovery returns 200; Telegram does not retry.
	b := mustBot(t)
	b.RegisterHandler(bot.HandlerTypeMessageText, "panic", bot.MatchTypeCommand,
		func(ctx context.Context, _ *bot.Bot, _ *models.Update) {
			panic("boom")
		})

	h := WebhookHandler(b, testSecret)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(panicUpdate))
	req.Header.Set(secretTokenHeader, testSecret)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 after recover", rec.Code)
	}
}
