// Package misc is a small stub module that proves the framework end-to-end:
// /ping (public, exercises KV write), /mstats (protected, exercises KV read),
// /fortytwo (private easter egg).
package misc

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/modules/util/chathelper"
	"github.com/tiennm99/miti99bot/internal/storage"
)

// lastPingKey is the per-module KV key /ping writes and /mstats reads.
const lastPingKey = "last_ping"

// defaultTarget is the substituted "investigator" name when /trongtruonghop is
// invoked without an argument.
const defaultTarget = "VNG"

// trongTruongHopTemplate is the disclaimer rendered by /trongtruonghop. Three
// %s slots: target (escaped), sender mention, sender mention.
const trongTruongHopTemplate = "Trong trường hợp nhóm này bị điều tra bởi %s, %s khẳng định không liên quan tới nhóm hoặc những cá nhân khác trong nhóm này. %s không rõ tại sao lại có mặt ở đây vào thời điểm này, có lẽ tài khoản đã được thêm bởi một bên thứ ba."

// lastPing mirrors the JS bot's wire format: { at: <ms-since-epoch number> }.
// Stored as int64 ms-epoch (not time.Time → RFC3339) so a future cross-runtime
// KV export/import migration round-trips byte-for-byte.
type lastPing struct {
	At int64 `json:"at"`
}

// New is the module Factory. Captures the per-module Deps via closure so each
// command handler has direct access to its KV store.
func New(deps modules.Deps) modules.Module {
	return modules.Module{
		Commands: []modules.Command{
			pingCommand(deps),
			mstatsCommand(deps),
			fortytwoCommand(),
			trongTruongHopCommand(),
		},
	}
}

func pingCommand(deps modules.Deps) modules.Command {
	return modules.Command{
		Name:        "ping",
		Visibility:  modules.VisibilityPublic,
		Description: "Health check — replies pong and records last ping",
		Handler: func(ctx context.Context, b *bot.Bot, update *models.Update) error {
			if update.Message == nil {
				return nil
			}
			// Best-effort write — if KV is unavailable, still reply.
			payload := lastPing{At: chathelper.NowMillis()}
			if err := deps.KV.PutJSON(ctx, lastPingKey, payload); err != nil {
				log.Error("kv put failed", "module", "misc", "command", "ping", "key", lastPingKey, "err", err)
			}
			return chathelper.Reply(ctx, b, update.Message, "pong")
		},
	}
}

func mstatsCommand(deps modules.Deps) modules.Command {
	return modules.Command{
		Name:        "mstats",
		Visibility:  modules.VisibilityProtected,
		Description: "Show the timestamp of the last /ping",
		Handler: func(ctx context.Context, b *bot.Bot, update *models.Update) error {
			if update.Message == nil {
				return nil
			}
			var last lastPing
			text := "last ping: never"
			err := deps.KV.GetJSON(ctx, lastPingKey, &last)
			switch {
			case err == nil && last.At > 0:
				text = fmt.Sprintf("last ping: %s",
					time.UnixMilli(last.At).UTC().Format(time.RFC3339))
			case err != nil && !errors.Is(err, storage.ErrNotFound):
				// User-visible reply mirrors how trading/wordle/loldle handle
				// transient KV failures — returning the error here would leave
				// the user with no reply at all.
				log.Error("kv get failed", "module", "misc", "command", "mstats", "key", lastPingKey, "err", err)
				text = "Could not load stats. Try again later."
			}
			return chathelper.Reply(ctx, b, update.Message, text)
		},
	}
}

// senderMention renders the mention used inside the trongtruonghop template.
// Prefer @username (Telegram resolves it server-side and enforces a safe
// charset). Fall back to a tg://user?id link with the user's display name when
// the account has no username; escape the name because first/last names can
// legitimately contain '<' or '&'.
func senderMention(u *models.User) string {
	if u == nil {
		return "thành viên"
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = "thành viên"
	}
	return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, u.ID, html.EscapeString(name))
}

func trongTruongHopCommand() modules.Command {
	return modules.Command{
		Name:        "trongtruonghop",
		Visibility:  modules.VisibilityPublic,
		Description: "Phát biểu disclaimer cho thành viên hiện tại",
		Handler: func(ctx context.Context, b *bot.Bot, update *models.Update) error {
			if update.Message == nil || update.Message.From == nil {
				return nil
			}
			arg := strings.TrimSpace(chathelper.ArgAfterCommand(update.Message.Text))
			if arg == "" {
				arg = defaultTarget
			}
			mention := senderMention(update.Message.From)
			text := fmt.Sprintf(trongTruongHopTemplate, html.EscapeString(arg), mention, mention)
			return chathelper.ReplyHTML(ctx, b, update.Message, text)
		},
	}
}

func fortytwoCommand() modules.Command {
	return modules.Command{
		Name:        "fortytwo",
		Visibility:  modules.VisibilityPrivate,
		Description: "Easter egg — the answer",
		Handler: func(ctx context.Context, b *bot.Bot, update *models.Update) error {
			if update.Message == nil {
				return nil
			}
			return chathelper.Reply(ctx, b, update.Message, "The answer.")
		},
	}
}
