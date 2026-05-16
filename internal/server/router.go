package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/go-telegram/bot"

	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/telegram"
)

// cronNameRe limits cron path segments to a safe alphabet so log injection via
// the route is impossible (newlines, ANSI escapes, etc. are rejected at the
// router boundary). Same shape as Telegram command names.
var cronNameRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// cronAuthHeader is the shared-secret header EventBridge Scheduler attaches when
// invoking /cron/{name}.
const cronAuthHeader = "X-Cron-Token"

// Config wires the router's runtime dependencies.
type Config struct {
	Bot           *bot.Bot
	Registry      *modules.Registry
	WebhookSecret string

	// CronSecret protects /cron/{name} against unauthenticated calls; EventBridge
	// Scheduler attaches it as the X-Cron-Token header. Empty means /cron/{name}
	// is fully disabled (404).
	CronSecret string
}

// New builds the application's HTTP handler. Routes:
//
//	GET  /                  → health
//	POST /webhook           → Telegram update intake (constant-time secret check)
//	POST /cron/{name}       → EventBridge Scheduler entry (shared-secret check)
//
// Anything else is 404. All routes pass through LogRequests so every
// request emits a structured `req` log line (CloudWatch Logs consumes them
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
		// Rejection paths use bare status codes (no response body) so a
		// scanner hitting /cron/ can't fingerprint the route from the
		// response text. Status codes remain distinct for CloudWatch
		// metric filters; structured log lines carry the reason for
		// operator triage.
		if cronDisabled {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			log.Warn("cron rejected", "reason", "method", "method", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		got := []byte(r.Header.Get(cronAuthHeader))
		if subtle.ConstantTimeCompare(got, secretBytes) != 1 {
			log.Warn("cron rejected", "reason", "secret_mismatch")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		name := strings.TrimPrefix(r.URL.Path, "/cron/")
		if !cronNameRe.MatchString(name) {
			log.Warn("cron rejected", "reason", "bad_name", "name", name)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		log.Info("cron triggered", "route", "/cron", "name", name)
		ctx, cancel := context.WithTimeout(r.Context(), defaultCronTimeout)
		defer cancel()

		// Recover panics with cron-name context BEFORE the LogRequests
		// middleware's safety-net recover sees them — otherwise CloudWatch
		// would just show "middleware recovered panic" with no clue which
		// scheduled job blew up. EventBridge sees 500 either way.
		var dispatchErr error
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("cron handler panic",
						"route", "/cron",
						"name", name,
						"panic", rec,
						"stack", string(debug.Stack()))
					dispatchErr = errors.New("cron handler panicked")
				}
			}()
			dispatchErr = modules.DispatchScheduled(ctx, name, reg)
		}()
		if dispatchErr != nil {
			if errors.Is(dispatchErr, modules.ErrCronNotFound) {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			log.Error("cron failed", "route", "/cron", "name", name, "err", dispatchErr)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
