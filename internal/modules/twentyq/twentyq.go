package twentyq

import (
	"crypto/rand"
	"encoding/binary"
	mrand "math/rand/v2"

	"github.com/tiennm99/miti99bot/internal/ai"
	"github.com/tiennm99/miti99bot/internal/modules"
)

// New is the twentyq module Factory. If Deps.Chatter is nil
// (GEMINI_API_KEY unset) the module loads but every command replies with a
// config-error message — keeps the rest of the bot functional.
func New(deps modules.Deps) modules.Module {
	s := &state{
		kv:      deps.KV,
		chatter: deps.Chatter,
		limiter: ai.NewPerUserLimiter(5.0/60.0, 5),
		rng:     newRNG(),
	}
	return modules.Module{
		Commands: []modules.Command{
			{
				Name:        "twentyq",
				Visibility:  modules.VisibilityPublic,
				Description: "20 questions — bot picks an object, you ask yes/no questions",
				Handler:     s.handleTwentyq,
			},
			{
				Name:        "twentyq_giveup",
				Visibility:  modules.VisibilityPublic,
				Description: "Reveal the current twentyq answer (auto-starts a fresh round)",
				Handler:     s.handleGiveup,
			},
			{
				Name:        "twentyq_stats",
				Visibility:  modules.VisibilityPublic,
				Description: "Show your twentyq stats (played, solved, best round)",
				Handler:     s.handleStats,
			},
		},
	}
}

func newRNG() *mrand.Rand {
	var seed [16]byte
	_, _ = rand.Read(seed[:])
	s1 := binary.LittleEndian.Uint64(seed[0:8])
	s2 := binary.LittleEndian.Uint64(seed[8:16])
	return mrand.New(mrand.NewPCG(s1, s2))
}
