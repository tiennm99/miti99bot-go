// Package chathelper consolidates per-module Telegram helpers (SubjectFor,
// ArgAfterCommand, NowMillis, Reply, ReplyHTML, WinRate) that would
// otherwise be duplicated across every module. Single source here; modules
// import.
package chathelper

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// SubjectFor returns the identity key per-module state should be scoped by:
// group/supergroup → chat ID (shared game state), otherwise → user ID.
// Returns "" when no usable id is present (caller should reply with a
// "cannot identify chat" error). Channels and unknown chat types fall
// through to From.ID.
func SubjectFor(msg *models.Message) string {
	if msg == nil {
		return ""
	}
	switch msg.Chat.Type {
	case models.ChatTypeGroup, models.ChatTypeSupergroup:
		return strconv.FormatInt(msg.Chat.ID, 10)
	default:
		if msg.From != nil {
			return strconv.FormatInt(msg.From.ID, 10)
		}
	}
	return ""
}

// ArgAfterCommand returns everything after the first space in text, trimmed.
// Works for `/cmd arg`, `/cmd@bot arg`, etc.
func ArgAfterCommand(text string) string {
	if text == "" {
		return ""
	}
	idx := strings.IndexByte(text, ' ')
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(text[idx+1:])
}

// NowMillis returns current UTC ms-since-epoch.
func NowMillis() int64 { return time.Now().UTC().UnixMilli() }

// Reply sends a plain-text response to the chat the inbound message came from.
//
// Forwards MessageThreadID so replies in a forum-supergroup topic stay in the
// same topic. Telegram routes outgoing messages with an absent/zero
// message_thread_id to the General topic — that mis-routing is the precise
// reason this helper takes the whole message instead of just a chat ID.
func Reply(ctx context.Context, b *bot.Bot, msg *models.Message, text string) error {
	if msg == nil {
		return nil
	}
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          msg.Chat.ID,
		MessageThreadID: msg.MessageThreadID,
		Text:            text,
	})
	return err
}

// ReplyHTML sends a Telegram HTML-formatted response to the chat the inbound
// message came from. Forwards MessageThreadID — see Reply for rationale.
func ReplyHTML(ctx context.Context, b *bot.Bot, msg *models.Message, text string) error {
	if msg == nil {
		return nil
	}
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          msg.Chat.ID,
		MessageThreadID: msg.MessageThreadID,
		Text:            text,
		ParseMode:       models.ParseModeHTML,
	})
	return err
}

// WinRate computes wins/played as a percentage rounded to nearest int.
// Uses math.Round (round half away from zero for positive inputs) so 2/3
// renders as 67%, not 66% as plain int(...) truncation would give.
// Returns 0 when played == 0 (avoids NaN).
func WinRate(wins, played int) int {
	if played <= 0 {
		return 0
	}
	return int(math.Round(float64(wins) / float64(played) * 100))
}
