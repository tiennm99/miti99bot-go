package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-telegram/bot"

	"github.com/tiennm99/miti99bot-go/internal/log"
	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/telegram"
)

// cronNameRe limits cron path segments to a safe alphabet so log injection via
// the route is impossible (newlines, ANSI escapes, etc. are rejected at the
// router boundary). Same shape as Telegram command names.
var cronNameRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// cronAuthHeader is the shared-secret header name. Replaced by OIDC in Phase 09.
const cronAuthHeader = "X-Cron-Token"

// Config wires the router's runtime dependencies.
type Config struct {
	Bot           *bot.Bot
	Registry      *modules.Registry
	WebhookSecret string

	// CronSecret is the shared-secret bridge until Phase 09 adds OIDC. Empty
	// means /cron/{name} is fully disabled (404). Required to prevent
	// unauthenticated triggering of billable side effects.
	CronSecret string
}

// New builds the application's HTTP handler. Routes:
//
//	GET  /                  → health
//	POST /webhook           → Telegram update intake (constant-time secret check)
//	POST /cron/{name}       → Cloud Scheduler entry (shared-secret check; OIDC in Phase 09)
//
// Anything else is 404. All routes pass through LogRequests so every
// request emits a structured `req` log line (Cloud Logging consumes them
// for 5xx-rate alerts and per-route latency).
func New(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", HealthHandler())
	mux.Handle("/webhook", telegram.WebhookHandler(cfg.Bot, cfg.WebhookSecret))
	mux.Handle("/cron/", cronHandler(cfg.Registry, cfg.CronSecret))
	return LogRequests(mux)
}

func cronHandler(reg *modules.Registry, secret string) http.HandlerFunc {
	secretBytes := []byte(secret)
	cronDisabled := secret == ""
	return func(w http.ResponseWriter, r *http.Request) {
		if cronDisabled {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		got := []byte(r.Header.Get(cronAuthHeader))
		if subtle.ConstantTimeCompare(got, secretBytes) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		name := strings.TrimPrefix(r.URL.Path, "/cron/")
		if !cronNameRe.MatchString(name) {
			http.NotFound(w, r)
			return
		}

		log.Info("cron triggered", "route", "/cron", "name", name)
		ctx, cancel := context.WithTimeout(r.Context(), defaultCronTimeout)
		defer cancel()

		if err := modules.DispatchScheduled(ctx, name, reg); err != nil {
			if errors.Is(err, modules.ErrCronNotFound) {
				http.NotFound(w, r)
				return
			}
			log.Error("cron failed", "route", "/cron", "name", name, "err", err)
			http.Error(w, "cron failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
