package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-telegram/bot"
)

// SentCall captures one outbound Telegram API call (sendMessage, sendSticker,
// etc.) made by the bot during dispatch. Method is the API method ("sendMessage")
// and Form is the multipart form fields collapsed to plain string→string —
// the bot library only emits primitive scalars for our handler call sites.
type SentCall struct {
	Method string
	Form   map[string]string
}

// Text is a convenience accessor for the most common assertion: SendMessage's
// "text" field. Returns "" if the call wasn't a sendMessage.
func (c SentCall) Text() string { return c.Form["text"] }

// ChatID returns the "chat_id" form field as-is (string form). Empty string
// if absent.
func (c SentCall) ChatID() string { return c.Form["chat_id"] }

// RecordingBot wraps a *bot.Bot wired to an httptest server that captures
// outbound API calls instead of contacting Telegram. Always Close() in a
// defer to release the test server.
type RecordingBot struct {
	Bot    *bot.Bot
	Server *httptest.Server

	mu    sync.Mutex
	calls []SentCall
}

// NewRecordingBot constructs a recording bot. The bot uses a synthetic token
// and disables async handlers so dispatch is deterministic in tests.
func NewRecordingBot(t *testing.T) *RecordingBot {
	t.Helper()
	rb := &RecordingBot{}
	rb.Server = httptest.NewServer(http.HandlerFunc(rb.handle))
	t.Cleanup(rb.Server.Close)

	b, err := bot.New("test-token",
		bot.WithSkipGetMe(),
		bot.WithNotAsyncHandlers(),
		bot.WithServerURL(rb.Server.URL),
	)
	if err != nil {
		t.Fatalf("recording bot init: %v", err)
	}
	rb.Bot = b
	return rb
}

// Sent returns a copy of all calls captured so far, in chronological order.
func (rb *RecordingBot) Sent() []SentCall {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	out := make([]SentCall, len(rb.calls))
	copy(out, rb.calls)
	return out
}

// LastSent returns the most-recent recorded call, or zero-value SentCall if
// none have been made yet.
func (rb *RecordingBot) LastSent() SentCall {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if len(rb.calls) == 0 {
		return SentCall{}
	}
	return rb.calls[len(rb.calls)-1]
}

// Reset drops all captured calls. Useful between sub-tests sharing one bot.
func (rb *RecordingBot) Reset() {
	rb.mu.Lock()
	rb.calls = nil
	rb.mu.Unlock()
}

// handle is the httptest server's request handler. Path shape is
// "/bot<token>/<method>" per the go-telegram/bot URL builder. We extract the
// method, parse the multipart form, record, and respond with a minimal-ok
// JSON body shaped to satisfy whichever method was called.
func (rb *RecordingBot) handle(w http.ResponseWriter, r *http.Request) {
	method := apiMethodFromPath(r.URL.Path)

	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	form := make(map[string]string, len(r.MultipartForm.Value))
	for k, vs := range r.MultipartForm.Value {
		if len(vs) > 0 {
			form[k] = vs[0]
		}
	}

	rb.mu.Lock()
	rb.calls = append(rb.calls, SentCall{Method: method, Form: form})
	rb.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(okResponseFor(method)))
}

// apiMethodFromPath extracts the API method from "/bot<token>/<method>".
// Returns "" on shapes that don't match (which the test still records as a
// call to "" — surfaces accidentally weird URLs in test output).
func apiMethodFromPath(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return ""
	}
	return p[idx+1:]
}

// okResponseFor returns a minimal `{ok:true, result:...}` payload that the
// bot library will accept for the named API method. SendMessage / SendSticker
// expect a Message; most others accept a bool.
func okResponseFor(method string) string {
	switch method {
	case "sendMessage", "sendSticker", "sendPhoto", "sendDocument", "sendVideo":
		// Minimal shape: id, date, chat. Bot library decodes via json so
		// extra fields are ignored.
		msg := map[string]any{
			"message_id": 1,
			"date":       0,
			"chat": map[string]any{
				"id":   1,
				"type": "private",
			},
		}
		body := map[string]any{"ok": true, "result": msg}
		out, _ := json.Marshal(body)
		return string(out)
	default:
		return `{"ok":true,"result":true}`
	}
}

// AssertSentText fails the test if no recorded sendMessage contains the
// substring needle. Matches the most common assertion pattern: "did the
// handler include this phrase?"
func (rb *RecordingBot) AssertSentText(t *testing.T, needle string) {
	t.Helper()
	for _, c := range rb.Sent() {
		if c.Method == "sendMessage" && strings.Contains(c.Text(), needle) {
			return
		}
	}
	t.Errorf("no sendMessage contained %q. Sent calls: %s", needle, rb.dumpCalls())
}

// dumpCalls renders the captured calls for error messages.
func (rb *RecordingBot) dumpCalls() string {
	calls := rb.Sent()
	var parts []string
	for i, c := range calls {
		parts = append(parts, fmt.Sprintf("[%d] %s text=%q chat=%s", i, c.Method, c.Text(), c.ChatID()))
	}
	if len(parts) == 0 {
		return "(no calls)"
	}
	return strings.Join(parts, "; ")
}
