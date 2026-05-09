package modules

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// Visibility classifies who may invoke a command. The dispatcher enforces
// this at command-handler entry: Public is unrestricted; Protected requires
// the sender to be in Auth.AdminUserIDs (or be the bot owner); Private
// requires the sender to be Auth.BotOwnerID. /help filters by the same field.
type Visibility int

const (
	VisibilityPublic Visibility = iota
	VisibilityProtected
	VisibilityPrivate
)

// CommandHandler runs in response to a Telegram command. Returning an error
// causes the dispatcher to log the failure. Telegram retries are governed by
// the webhook HTTP status (200), not handler errors — so the error return is
// purely for logging/metrics, not flow control.
type CommandHandler func(ctx context.Context, b *bot.Bot, update *models.Update) error

// CronHandler runs when Cloud Scheduler hits /cron/{name}. Crons receive the
// per-module-prefixed Deps via the registry; handlers should not capture the
// base Deps from the factory closure or KV writes will collide across modules.
type CronHandler func(ctx context.Context, deps Deps) error

// Command is a single Telegram bot command exposed by a module.
type Command struct {
	Name        string         // ^[a-z0-9_]{1,32}$ — Telegram BotFather rules
	Visibility  Visibility     // public/protected/private
	Description string         // shown in /help (required, non-empty)
	Handler     CommandHandler // required
}

// Cron is a single scheduled job exposed by a module.
type Cron struct {
	Schedule string      // documentation only; real schedule lives in Cloud Scheduler
	Name     string      // unique within module
	Handler  CronHandler // required
}

// Module is a self-contained feature unit: a name plus zero or more commands
// and crons. Modules are constructed by Factory functions that capture their
// per-module Deps via closure.
//
// Module.Name is overridden by the registry to its catalog key; factories may
// leave it blank.
type Module struct {
	Name     string
	Commands []Command
	Crons    []Cron
}

// Deps is the dependency bundle a Factory receives. Each field is added in the
// phase that introduces it; today KV, Env, and Registry exist (Gemini: Phase 07).
//
// Deps.Env is empty by default — process env does NOT auto-flow to modules
// (allowlist semantics). Phase 07+ introduces a per-module env declaration so
// keys flow only to declared consumers; this prevents a future API key from
// silently reaching every module.
//
// Deps.Registry is a pointer to the Registry being built. At factory call
// time the Registry is partially populated (only modules earlier in the
// MODULES env order); by the time any handler runs, it is fully populated.
// Modules that need to introspect commands (e.g. /help) capture this pointer
// in their handler closures.
type Deps struct {
	KV       storage.KVStore   // already prefixed with the module name when passed to a Factory
	Env      map[string]string // empty by default; per-module allowlist (Phase 07+)
	Registry *Registry         // populated by Build; safe to capture but read-only at module use
}

// Factory constructs a Module from its Deps. Spec deviation: Phase 03 plan
// defines `Factory func() Module` with a separate Init step. We pass Deps
// directly so handler closures can capture them — idiomatic Go and removes a
// lifecycle ordering trap.
type Factory func(deps Deps) Module
