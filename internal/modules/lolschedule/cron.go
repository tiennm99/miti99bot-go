package lolschedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/modules"
)

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
			continue
		}
		sent++
	}
	log.Info("lolschedule daily push complete",
		"subscribers", len(subs),
		"sent", sent,
		"failed", failed,
		"throttled", throttle)
	return nil
}
