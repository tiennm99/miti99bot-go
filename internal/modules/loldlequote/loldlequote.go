package loldlequote

import (
	"github.com/tiennm99/miti99bot-go/internal/modules"
)

// New is the loldle-quote module Factory. Loads the embedded pool once and
// shares it (plus the per-subject lock map) across all handlers.
func New(deps modules.Deps) modules.Module {
	s := &state{kv: deps.KV, pool: loadPool()}
	return modules.Module{
		Commands: []modules.Command{
			{
				Name:        "loldle_quote",
				Visibility:  modules.VisibilityPublic,
				Description: "Quote loldle — guess the champion from a lore blurb",
				Handler:     s.handleQuote,
			},
			{
				Name:        "loldle_quote_giveup",
				Visibility:  modules.VisibilityPublic,
				Description: "Reveal the current quote loldle answer",
				Handler:     s.handleGiveup,
			},
			{
				Name:        "loldle_quote_stats",
				Visibility:  modules.VisibilityPublic,
				Description: "Show your quote loldle stats (wins, streak)",
				Handler:     s.handleStats,
			},
			{
				Name:        "loldle_quote_setmax",
				Visibility:  modules.VisibilityPrivate,
				Description: "Override quote loldle max guesses per round (1-10)",
				Handler:     s.handleSetMax,
			},
		},
	}
}
