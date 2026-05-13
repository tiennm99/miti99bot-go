package wordle

import (
	"github.com/tiennm99/miti99bot/internal/modules"
)

// New is the wordle module Factory. Loads the embedded dictionary once,
// captures the per-module KV via closure, and registers all four commands.
func New(deps modules.Deps) modules.Module {
	words, set := loadWords()
	s := &state{kv: deps.KV, words: words, set: set}

	return modules.Module{
		Commands: []modules.Command{
			{
				Name:        "wordle",
				Visibility:  modules.VisibilityPublic,
				Description: "Classic wordle — guess the 5-letter word",
				Handler:     s.handleWordle,
			},
			{
				Name:        "wordle_new",
				Visibility:  modules.VisibilityPublic,
				Description: "Start a new round (auto-gives-up any in-progress one)",
				Handler:     s.handleNew,
			},
			{
				Name:        "wordle_giveup",
				Visibility:  modules.VisibilityPublic,
				Description: "Reveal the current wordle answer",
				Handler:     s.handleGiveup,
			},
			{
				Name:        "wordle_stats",
				Visibility:  modules.VisibilityPublic,
				Description: "Show your wordle stats (wins, streak)",
				Handler:     s.handleStats,
			},
		},
	}
}
