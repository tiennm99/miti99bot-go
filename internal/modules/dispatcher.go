package modules

import (
	"context"
	"log"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Install registers every command in the registry with the Telegram bot.
//
// MatchTypeCommand expects the bare command name without the leading slash;
// the library compares against entity bytes after the "/" prefix.
func Install(b *bot.Bot, reg *Registry) {
	for name, cmd := range reg.AllCommands {
		cmdCopy := cmd // capture by value for the closure
		b.RegisterHandler(
			bot.HandlerTypeMessageText,
			name,
			bot.MatchTypeCommand,
			func(ctx context.Context, b *bot.Bot, update *models.Update) {
				if err := cmdCopy.Handler(ctx, b, update); err != nil {
					log.Printf("command /%s failed: %v", cmdCopy.Name, err)
				}
			},
		)
	}
}
