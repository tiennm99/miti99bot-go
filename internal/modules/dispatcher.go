package modules

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/metrics"
)

// Auth gates Protected/Private commands by sender Telegram user ID. Public
// commands are always allowed. A zero BotOwnerID + empty AdminUserIDs means
// every Protected/Private command is denied — the safe default for an
// unconfigured deployment.
type Auth struct {
	BotOwnerID   int64          // owner is implicitly an admin; receives Private + Protected
	AdminUserIDs map[int64]bool // additional users allowed to run Protected commands
}

// Permits reports whether the sender of update may run a command of visibility v.
// Denies are silent — callers must NOT reply to denied requests, otherwise the
// existence of a Protected/Private command is leaked to unprivileged users.
func (a Auth) Permits(v Visibility, update *models.Update) bool {
	if v == VisibilityPublic {
		return true
	}
	if update == nil || update.Message == nil || update.Message.From == nil {
		return false
	}
	senderID := update.Message.From.ID
	switch v {
	case VisibilityPrivate:
		return a.BotOwnerID != 0 && senderID == a.BotOwnerID
	case VisibilityProtected:
		if a.BotOwnerID != 0 && senderID == a.BotOwnerID {
			return true
		}
		return a.AdminUserIDs[senderID]
	}
	return false
}

// Install registers every command in the registry with the Telegram bot.
//
// MatchTypeCommand expects the bare command name without the leading slash;
// the library compares against entity bytes after the "/" prefix.
//
// auth gates Protected/Private commands; pass a zero-value Auth to deny all
// Protected/Private commands (the right answer for a misconfigured deploy).
func Install(b *bot.Bot, reg *Registry, auth Auth) {
	for name, cmd := range reg.AllCommands {
		cmdCopy := cmd // capture by value for the closure
		b.RegisterHandler(
			bot.HandlerTypeMessageText,
			name,
			bot.MatchTypeCommand,
			func(ctx context.Context, b *bot.Bot, update *models.Update) {
				if !auth.Permits(cmdCopy.Visibility, update) {
					return // silent — do not leak existence of gated commands
				}
				metrics.IncCommand(cmdCopy.Name)
				if err := cmdCopy.Handler(ctx, b, update); err != nil {
					metrics.IncError("handler-error")
					log.Error("command failed", "command", cmdCopy.Name, "err", err)
				}
			},
		)
	}
}
