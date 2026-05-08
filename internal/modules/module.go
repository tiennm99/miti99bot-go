package modules

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// Visibility classifies who may invoke a command. The dispatcher does not
// enforce visibility today; the field exists so /help and chat-scoping can
// filter consistently in later phases.
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
// phase that introduces it; today only KV + Env exist (Firestore: Phase 04,
// Gemini: Phase 07).
//
// Deps.Env is the process environment with sensitive keys stripped. Modules
// must not assume Env contains every variable — see cmd/server.envForModules.
type Deps struct {
	KV  storage.KVStore   // already prefixed with the module name when passed to a Factory
	Env map[string]string // process env minus known-sensitive keys
}

// Factory constructs a Module from its Deps. Spec deviation: Phase 03 plan
// defines `Factory func() Module` with a separate Init step. We pass Deps
// directly so handler closures can capture them — idiomatic Go and removes a
// lifecycle ordering trap.
type Factory func(deps Deps) Module
