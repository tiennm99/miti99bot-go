package loldlesplash

import (
	"github.com/tiennm99/miti99bot-go/internal/modules"
)

// New is the loldle-splash module Factory. Loads the embedded pool once
// and shares it (plus the per-subject lock map) across all handlers.
func New(deps modules.Deps) modules.Module {
	s := &state{kv: deps.KV, pool: loadPool()}
	return modules.Module{
		Commands: []modules.Command{
			{
				Name:        "loldle_splash",
				Visibility:  modules.VisibilityPublic,
				Description: "Splash loldle — guess the champion from a splash art",
				Handler:     s.handleSplash,
			},
			{
				Name:        "loldle_splash_giveup",
				Visibility:  modules.VisibilityPublic,
				Description: "Reveal the current splash loldle answer",
				Handler:     s.handleGiveup,
			},
			{
				Name:        "loldle_splash_stats",
				Visibility:  modules.VisibilityPublic,
				Description: "Show your splash loldle stats (wins, streak)",
				Handler:     s.handleStats,
			},
			{
				Name:        "loldle_splash_setmax",
				Visibility:  modules.VisibilityPrivate,
				Description: "Override splash loldle max guesses per round (1-10)",
				Handler:     s.handleSetMax,
			},
		},
	}
}
