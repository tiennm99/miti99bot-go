package lolschedule

import (
	"github.com/tiennm99/miti99bot-go/internal/modules"
)

// New is the lolschedule module Factory. The 5 user-facing commands are
// wired here. The daily-push cron (08:00 ICT, fan-out to subscribers) is
// deferred to Phase 09 of the port plan: Cloud Scheduler will hit
// /cron/lolschedule_daily_push, and the cron handler needs a *bot.Bot
// reference which today's Deps doesn't expose. Subscribers are still
// collected by /lolschedule_subscribe so the push can light up the moment
// the cron infra lands.
func New(deps modules.Deps) modules.Module {
	s := &state{kv: deps.KV, client: &Client{}}
	return modules.Module{
		Commands: []modules.Command{
			{
				Name:        "lolschedule",
				Visibility:  modules.VisibilityPublic,
				Description: "LoL matches for a date (dd-mm-yyyy, dd/mm/yyyy, ddmmyyyy; default today)",
				Handler:     s.handleSchedule,
			},
			{
				Name:        "lolschedule_today",
				Visibility:  modules.VisibilityPublic,
				Description: "Today's LoL esports matches (scores if played)",
				Handler:     s.handleToday,
			},
			{
				Name:        "lolschedule_week",
				Visibility:  modules.VisibilityPublic,
				Description: "LoL esports matches for the next 7 days",
				Handler:     s.handleWeek,
			},
			{
				Name:        "lolschedule_subscribe",
				Visibility:  modules.VisibilityPublic,
				Description: "Get the daily LoL schedule digest at 08:00 ICT",
				Handler:     s.handleSubscribe,
			},
			{
				Name:        "lolschedule_unsubscribe",
				Visibility:  modules.VisibilityPublic,
				Description: "Stop receiving the daily LoL schedule digest",
				Handler:     s.handleUnsubscribe,
			},
		},
	}
}
