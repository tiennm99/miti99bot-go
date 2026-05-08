package telegram

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
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
	body := bytes.Repeat([]byte("a"), maxWebhookBody+1)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set(secretTokenHeader, testSecret)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("oversized body should not return 200; got %d", rec.Code)
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
