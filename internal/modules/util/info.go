package util

import (
	"context"
	"fmt"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/modules/util/chathelper"
)

// infoCommand returns /info — replies plain text with chat / thread / sender
// IDs, with "n/a" fallbacks. Used to debug bot routing in groups + topics.
func infoCommand() modules.Command {
	return modules.Command{
		Name:        "info",
		Visibility:  modules.VisibilityPublic,
		Description: "Show chat id, thread id, and sender id (debug helper)",
		Handler: func(ctx context.Context, b *bot.Bot, update *models.Update) error {
			msg := update.Message
			if msg == nil {
				// Today the dispatcher only routes message-text commands, but
				// guard so /info can be safely reused from other update paths.
				return nil
			}
			chatID := fmt.Sprintf("%d", msg.Chat.ID)
			// Telegram omits message_thread_id outside forum topics, so a 0
			// here is "no thread", same as JS's `?? "n/a"`.
			threadID := "n/a"
			if msg.MessageThreadID != 0 {
				threadID = fmt.Sprintf("%d", msg.MessageThreadID)
			}
			senderID := "n/a"
			if msg.From != nil {
				senderID = fmt.Sprintf("%d", msg.From.ID)
			}
			text := fmt.Sprintf("chat id: %s\nthread id: %s\nsender id: %s", chatID, threadID, senderID)
			return chathelper.Reply(ctx, b, msg, text)
		},
	}
}
