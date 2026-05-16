package lolschedule

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/modules"
)

// terminalSendErrorMarkers are substrings of Telegram API errors that mean the
// chat will never accept messages again (blocked, deactivated, kicked, chat
// gone). Detecting these lets the daily-push handler prune the subscriber
// list so dead chats stop consuming the 30-msg/s global budget.
//
// String matching is fragile by nature, but the bot library surfaces these
// directly in err.Error() and Telegram has used the same wording for years.
// The false-negative path (we miss a new wording, dead chat lingers) is
// strictly safer than the false-positive path (we wrongly prune a live chat).
var terminalSendErrorMarkers = []string{
	"bot was blocked by the user",
	"user is deactivated",
	"bot is not a member",
	"chat not found",
	"group chat was upgraded",
	"have no rights to send",
	"chat was deleted",
}

// isTerminalSendError reports whether err indicates the chat is permanently
// unreachable. Used by runDailyPush to drive auto-unsubscription.
func isTerminalSendError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, m := range terminalSendErrorMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// dailyPushCronName is the cron route segment + EventBridge schedule key.
// Must match the regex in internal/server/router.go (^[a-z0-9_]{1,32}$).
const dailyPushCronName = "lolschedule_daily_push"

// dailyPushSchedule is documentation only — the real fire time lives in
// EventBridge Scheduler. Cron expression is UTC;
// 01:00 UTC == 08:00 ICT.
const dailyPushSchedule = "0 1 * * *"

// telegramRateLimitThreshold is the subscriber count above which we throttle
// sends to stay under Telegram's global 30 msg/sec cap. Below it we send hot.
const telegramRateLimitThreshold = 30

// telegramRateLimitDelay is the inter-send pause when above the threshold.
// 50ms = ~20 msg/sec, well clear of the 30/s ceiling with margin for jitter.
const telegramRateLimitDelay = 50 * time.Millisecond

// messageSender is the subset of *bot.Bot the cron handler uses. Defining it
// as an interface lets tests inject a mock without spinning up a fake
// Telegram API server.
type messageSender interface {
	SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error)
}

// dailyPushCron returns the cron registration. Schedule is documentation only.
func (s *state) dailyPushCron() modules.Cron {
	return modules.Cron{
		Name:     dailyPushCronName,
		Schedule: dailyPushSchedule,
		Handler:  s.dailyPushHandler,
	}
}

// dailyPushHandler is invoked by the cron dispatcher. It pulls Bot from Deps
// (set in main.go via BuildOptions.Bot) and delegates to runDailyPush so the
// core logic is testable without an actual *bot.Bot.
func (s *state) dailyPushHandler(ctx context.Context, deps modules.Deps) error {
	if deps.Bot == nil {
		return errors.New("lolschedule daily push: deps.Bot is nil (BuildOptions.Bot not wired)")
	}
	return runDailyPush(ctx, s, deps.Bot)
}

// runDailyPush is the testable core: fetch subscribers, fetch today's matches,
// fan out to every subscriber. Per-chat send failures are logged but do not
// abort the batch — one bad chat does not deny the rest.
func runDailyPush(ctx context.Context, s *state, sender messageSender) error {
	subs, err := listSubscribers(ctx, s.kv)
	if err != nil {
		return fmt.Errorf("lolschedule daily push: list subscribers: %w", err)
	}
	if len(subs) == 0 {
		log.Info("lolschedule daily push: no subscribers, skipping")
		return nil
	}

	from := ictDayStartOf(s.now())
	to := addDays(from, 1)
	events, err := s.client.GetEventsCached(ctx, s.kv, from, to)
	if err != nil {
		return fmt.Errorf("lolschedule daily push: fetch matches: %w", err)
	}
	filtered := FilterMajor(events)
	text := RenderToday(filtered, from)

	throttle := len(subs) > telegramRateLimitThreshold
	var sent, failed int
	var deadChats []int64
	for i, chatID := range subs {
		if throttle && i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(telegramRateLimitDelay):
			}
		}
		if _, err := sender.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      text,
			ParseMode: models.ParseModeHTML,
		}); err != nil {
			log.Warn("lolschedule daily push send failed", "chat", chatID, "err", err)
			failed++
			if isTerminalSendError(err) {
				deadChats = append(deadChats, chatID)
			}
			continue
		}
		sent++
	}

	// Best-effort prune. Failure here just leaves the dead chats in the list
	// for tomorrow's push — same behaviour as before this code existed, so
	// strictly an improvement even when the writes fail.
	pruned := pruneDeadSubscribers(ctx, s, deadChats)

	log.Info("lolschedule daily push complete",
		"subscribers", len(subs),
		"sent", sent,
		"failed", failed,
		"pruned", pruned,
		"throttled", throttle)
	return nil
}

// pruneDeadSubscribers removes chatIDs flagged as permanently unreachable.
// Serializes through state.subscribersMu so a concurrent /subscribe handler
// doesn't lose its write. Returns the number actually removed (idempotent if
// a subscriber unsubscribed between the failed send and this call).
func pruneDeadSubscribers(ctx context.Context, s *state, deadChats []int64) int {
	if len(deadChats) == 0 {
		return 0
	}
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()
	removed := 0
	for _, chatID := range deadChats {
		ok, err := removeSubscriber(ctx, s.kv, chatID)
		if err != nil {
			log.Warn("lolschedule prune dead subscriber failed", "chat", chatID, "err", err)
			continue
		}
		if ok {
			removed++
		}
	}
	return removed
}
