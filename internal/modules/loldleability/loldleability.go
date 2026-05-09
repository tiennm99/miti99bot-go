package loldleability

import (
	"github.com/tiennm99/miti99bot-go/internal/modules"
)

// New is the loldle-ability module Factory. Loads the embedded pool once
// and shares it (plus the per-subject lock map) across all handlers.
func New(deps modules.Deps) modules.Module {
	s := &state{kv: deps.KV, pool: loadPool()}
	return modules.Module{
		Commands: []modules.Command{
			{
				Name:        "loldle_ability",
				Visibility:  modules.VisibilityPublic,
				Description: "Ability loldle — guess the champion from an ability icon",
				Handler:     s.handleAbility,
			},
			{
				Name:        "loldle_ability_giveup",
				Visibility:  modules.VisibilityPublic,
				Description: "Reveal the current ability loldle answer",
				Handler:     s.handleGiveup,
			},
			{
				Name:        "loldle_ability_stats",
				Visibility:  modules.VisibilityPublic,
				Description: "Show your ability loldle stats (wins, streak)",
				Handler:     s.handleStats,
			},
			{
				Name:        "loldle_ability_setmax",
				Visibility:  modules.VisibilityPrivate,
				Description: "Override ability loldle max guesses per round (1-10)",
				Handler:     s.handleSetMax,
			},
		},
	}
}
