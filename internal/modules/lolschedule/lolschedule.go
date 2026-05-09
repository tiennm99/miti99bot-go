package lolschedule

import (
	"github.com/tiennm99/miti99bot-go/internal/modules"
)

// New is the lolschedule module Factory. The 5 user-facing commands plus the
// daily-push cron (lolschedule_daily_push at 08:00 ICT, fan-out to
// subscribers) are wired here. The cron handler reads deps.Bot at invoke
// time — main.go must wire BuildOptions.Bot for the cron to function;
// without it the handler fails fast with a clear error.
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
		Crons: []modules.Cron{s.dailyPushCron()},
	}
}
