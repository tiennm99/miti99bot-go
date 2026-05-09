package semantle

import (
	"crypto/rand"
	"encoding/binary"
	mrand "math/rand/v2"

	"github.com/tiennm99/miti99bot-go/internal/ai"
	"github.com/tiennm99/miti99bot-go/internal/modules"
)

// New is the semantle module Factory. Loads the embedded wordlist once,
// captures Embedder via Deps, and registers /semantle, /semantle_giveup,
// /semantle_stats. If Deps.Embedder is nil (GEMINI_API_KEY unset) the module
// still loads and the handlers reply with a config-error message — keeping
// the rest of the bot functional.
func New(deps modules.Deps) modules.Module {
	words, set := loadWords()
	s := &state{
		kv:       deps.KV,
		embedder: deps.Embedder,
		limiter:  ai.NewPerUserLimiter(5.0/60.0, 5), // 5 guesses per 60s burst
		words:    words,
		vocab:    set,
		rng:      newRNG(),
	}
	return modules.Module{
		Commands: []modules.Command{
			{
				Name:        "semantle",
				Visibility:  modules.VisibilityPublic,
				Description: "Semantle — guess the hidden word (unlimited tries)",
				Handler:     s.handleSemantle,
			},
			{
				Name:        "semantle_giveup",
				Visibility:  modules.VisibilityPublic,
				Description: "Reveal the current semantle answer (auto-starts a fresh round)",
				Handler:     s.handleGiveup,
			},
			{
				Name:        "semantle_stats",
				Visibility:  modules.VisibilityPublic,
				Description: "Show your semantle stats",
				Handler:     s.handleStats,
			},
		},
	}
}

// newRNG returns a crypto-seeded math/rand v2 PCG. We use math/rand for the
// hot path (target pick) because crypto/rand on every call is wasteful, but
// seeding from crypto/rand prevents the deterministic-seed footgun that bit
// wordle/loldle in earlier reviews.
func newRNG() *mrand.Rand {
	var seed [32]byte
	_, _ = rand.Read(seed[:])
	var s1, s2 uint64
	s1 = binary.LittleEndian.Uint64(seed[0:8])
	s2 = binary.LittleEndian.Uint64(seed[8:16])
	return mrand.New(mrand.NewPCG(s1, s2))
}
