package telegram

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/log"
)

// secretTokenHeader is the case-insensitive HTTP header Telegram sets when it
// POSTs an update to the webhook. It must equal the value passed to setWebhook.
// See: https://core.telegram.org/bots/api#setwebhook
// #nosec G101 — header name, not credential value
const secretTokenHeader = "X-Telegram-Bot-Api-Secret-Token"

// maxWebhookBody bounds inbound JSON. Telegram updates are well under 100 KiB
// even with media; 1 MiB is a defensive ceiling against malformed clients.
const maxWebhookBody = 1 << 20

// handlerTimeout caps a single Telegram update handler. Telegram retries after
// 60s of no 2xx; 10s leaves headroom for outbound API calls inside handlers
// without holding a Lambda instance long enough to block other updates.
const handlerTimeout = 10 * time.Second

// WebhookHandler returns an http.HandlerFunc that validates Telegram's secret
// token (constant-time) and dispatches the update synchronously to the bot.
//
// Dispatch is synchronous because the bot is constructed with
// bot.WithNotAsyncHandlers — handlers run inside this goroutine, so r.Context()
// stays live and bounded by handlerTimeout.
//
// secret must be non-empty; main is responsible for failing-fast at startup.
func WebhookHandler(b *bot.Bot, secret string) http.HandlerFunc {
	secretBytes := []byte(secret)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		got := []byte(r.Header.Get(secretTokenHeader))
		if subtle.ConstantTimeCompare(got, secretBytes) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBody)
		var update models.Update
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			// MaxBytesReader returns *http.MaxBytesError when the cap is hit;
			// surface 413 distinctly so Telegram (and ops dashboards) can
			// distinguish "body too big" from generic malformed JSON.
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		logDispatch(&update)

		ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
		defer cancel()
		// Recover panics so a buggy handler does not propagate up to the
		// http.Server (which would close the response mid-write and trigger
		// Telegram's 24-hour retry loop on the same poisoned update).
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("webhook handler panic",
						"panic", rec,
						"stack", string(debug.Stack()))
				}
			}()
			b.ProcessUpdate(ctx, &update)
		}()
		w.WriteHeader(http.StatusOK)
	}
}

// dispatchTextPreview caps message text in dispatch logs so chatty media
// captions or long DM threads don't bloat CloudWatch / drive up cost.
const dispatchTextPreview = 64

// logDispatch emits a single structured line per inbound update so the
// CloudWatch trail has chat type + command text without resorting to
// the library's pointer-printing debug mode. Cheap (no allocation when
// the message is short) and fires once per webhook hit.
func logDispatch(u *models.Update) {
	if u == nil || u.Message == nil {
		return
	}
	text := u.Message.Text
	if text == "" {
		text = u.Message.Caption
	}
	if len(text) > dispatchTextPreview {
		text = text[:dispatchTextPreview] + "…"
	}
	log.Info("dispatch",
		"update_id", u.ID,
		"chat_id", u.Message.Chat.ID,
		"chat_type", string(u.Message.Chat.Type),
		"text", text,
	)
}
