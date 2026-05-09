package loldleemoji

import (
	"github.com/tiennm99/miti99bot-go/internal/modules"
)

// New is the loldle-emoji module Factory. Loads the embedded pool once and
// shares it (plus the per-subject lock map) across all handlers.
func New(deps modules.Deps) modules.Module {
	s := &state{kv: deps.KV, pool: loadPool()}
	return modules.Module{
		Commands: []modules.Command{
			{
				Name:        "loldle_emoji",
				Visibility:  modules.VisibilityPublic,
				Description: "Emoji loldle — guess the champion from emojis",
				Handler:     s.handleEmoji,
			},
			{
				Name:        "loldle_emoji_giveup",
				Visibility:  modules.VisibilityPublic,
				Description: "Reveal the current emoji loldle answer",
				Handler:     s.handleGiveup,
			},
			{
				Name:        "loldle_emoji_stats",
				Visibility:  modules.VisibilityPublic,
				Description: "Show your emoji loldle stats (wins, streak)",
				Handler:     s.handleStats,
			},
			{
				Name:        "loldle_emoji_setmax",
				Visibility:  modules.VisibilityPrivate,
				Description: "Override emoji loldle max guesses per round (1-10)",
				Handler:     s.handleSetMax,
			},
		},
	}
}
