package lolschedule

import (
	"context"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/modules/util/chathelper"
	"github.com/tiennm99/miti99bot/internal/storage"
)

// state captures everything a lolschedule handler needs at runtime.
type state struct {
	kv     storage.KVStore
	client *Client
	// nowFn allows tests to inject a deterministic clock. Production code
	// uses time.Now via the default zero-value.
	nowFn func() time.Time
}

func (s *state) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now()
}

// handleSchedule is /lolschedule [date] — matches for one ICT day.
// Empty arg → today.
func (s *state) handleSchedule(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	arg := chathelper.ArgAfterCommand(msg.Text)
	parsed := ParseScheduleDate(arg, s.now())
	if !parsed.OK {
		return chathelper.Reply(ctx, b, msg.Chat.ID, parsed.Error)
	}
	return s.replyForRange(ctx, b, msg.Chat.ID, parsed.Date, addDays(parsed.Date, 1), false)
}

// handleToday is /lolschedule_today — today's matches.
func (s *state) handleToday(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	from := ictDayStartOf(s.now())
	return s.replyForRange(ctx, b, msg.Chat.ID, from, addDays(from, 1), false)
}

// handleWeek is /lolschedule_week — next 7 ICT days.
func (s *state) handleWeek(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	from := ictDayStartOf(s.now())
	return s.replyForRange(ctx, b, msg.Chat.ID, from, addDays(from, 7), true)
}

// replyForRange fetches + filters + renders a date window. week=true uses
// RenderWeek; false uses RenderToday.
func (s *state) replyForRange(ctx context.Context, b *bot.Bot, chatID int64, from, to time.Time, week bool) error {
	events, err := s.client.GetEventsCached(ctx, s.kv, from, to)
	if err != nil {
		log.Error("lolschedule_fetch_fail", "err", err, "from", from, "to", to)
		hint := "Could not fetch matches. Try again later."
		if week {
			hint = "Could not fetch this week's matches. Try again later."
		}
		return chathelper.Reply(ctx, b, chatID, hint)
	}
	filtered := FilterMajor(events)
	var text string
	if week {
		text = RenderWeek(filtered, from, to)
	} else {
		text = RenderToday(filtered, from)
	}
	return chathelper.ReplyHTML(ctx, b, chatID, text)
}

// handleSubscribe is /lolschedule_subscribe — opt the chat into the daily
// digest delivered by the EventBridge Scheduler cron handler.
func (s *state) handleSubscribe(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	added, err := addSubscriber(ctx, s.kv, msg.Chat.ID)
	if err != nil {
		return err
	}
	if added {
		return chathelper.Reply(ctx, b, msg.Chat.ID,
			"✅ Subscribed. You'll get today's LoL schedule at 08:00 ICT (push activates with the cron rollout).")
	}
	return chathelper.Reply(ctx, b, msg.Chat.ID, "Already subscribed.")
}

// handleUnsubscribe is /lolschedule_unsubscribe — opt out.
func (s *state) handleUnsubscribe(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	removed, err := removeSubscriber(ctx, s.kv, msg.Chat.ID)
	if err != nil {
		return err
	}
	if removed {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "Unsubscribed.")
	}
	return chathelper.Reply(ctx, b, msg.Chat.ID, "You weren't subscribed.")
}
