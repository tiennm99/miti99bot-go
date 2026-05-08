package telegram

import (
	"github.com/go-telegram/bot"
)

// NewBot constructs a Telegram bot configured for webhook mode:
//
//   - WithSkipGetMe: avoid a 5s blocking call to Telegram during cold start.
//     Token validity surfaces on the first outgoing API call instead.
//   - WithNotAsyncHandlers: handlers run synchronously inside the dispatcher's
//     goroutine. The webhook handler can rely on r.Context() staying live for
//     the duration of dispatch, which a goroutine-spawning default would break.
//
// Callers may pass extra options that override these defaults.
func NewBot(token string, opts ...bot.Option) (*bot.Bot, error) {
	defaults := []bot.Option{
		bot.WithSkipGetMe(),
		bot.WithNotAsyncHandlers(),
	}
	return bot.New(token, append(defaults, opts...)...)
}
