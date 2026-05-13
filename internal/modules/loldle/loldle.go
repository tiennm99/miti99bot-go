package loldle

import (
	"github.com/tiennm99/miti99bot/internal/modules"
)

// New is the loldle module Factory. Loads champions.json once at construction
// and shares the parsed slice + per-subject lock map across all handlers.
func New(deps modules.Deps) modules.Module {
	s := &state{
		kv:        deps.KV,
		champions: loadChampions(),
	}
	return modules.Module{
		Commands: []modules.Command{
			{
				Name:        "loldle",
				Visibility:  modules.VisibilityPublic,
				Description: "Classic loldle — guess the current champion",
				Handler:     s.handleLoldle,
			},
			{
				Name:        "loldle_giveup",
				Visibility:  modules.VisibilityPublic,
				Description: "Reveal the current loldle answer (auto-starts a fresh round)",
				Handler:     s.handleGiveup,
			},
			{
				Name:        "loldle_stats",
				Visibility:  modules.VisibilityPublic,
				Description: "Show your loldle stats (wins, streak)",
				Handler:     s.handleStats,
			},
			{
				Name:        "loldle_setmax",
				Visibility:  modules.VisibilityPrivate,
				Description: "Override max guesses per round (1-10) for this chat",
				Handler:     s.handleSetMax,
			},
		},
	}
}
