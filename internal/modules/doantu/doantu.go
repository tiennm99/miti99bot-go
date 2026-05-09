package doantu

import (
	"crypto/rand"
	"encoding/binary"
	mrand "math/rand/v2"

	"github.com/tiennm99/miti99bot-go/internal/modules"
)

// defaultAPI is the production phow2sim instance. Override via env
// PHOW2SIM_API_URL (allowlisted in cmd/server/main.go).
const defaultAPI = "https://phow2sim.sg.miti99.com"

// New is the doantu module Factory. Reads PHOW2SIM_API_URL from Deps.Env
// (falls back to defaultAPI). The module is always loadable — the upstream
// service handles uptime, not us.
func New(deps modules.Deps) modules.Module {
	base := defaultAPI
	if v, ok := deps.Env["PHOW2SIM_API_URL"]; ok && v != "" {
		base = v
	}
	s := &state{
		kv:  deps.KV,
		api: NewClient(base, 0),
		rng: newRNG(),
	}
	return modules.Module{
		Commands: []modules.Command{
			{
				Name:        "doantu",
				Visibility:  modules.VisibilityPublic,
				Description: "Đoán từ — Vietnamese semantic word guessing (unlimited tries)",
				Handler:     s.handleDoantu,
			},
			{
				Name:        "doantu_hint",
				Visibility:  modules.VisibilityPublic,
				Description: "Reveal 3 related words (not the answer) to nudge your guessing",
				Handler:     s.handleHint,
			},
			{
				Name:        "doantu_giveup",
				Visibility:  modules.VisibilityPublic,
				Description: "Reveal the current doantu answer (auto-starts a fresh round)",
				Handler:     s.handleGiveup,
			},
			{
				Name:        "doantu_stats",
				Visibility:  modules.VisibilityPublic,
				Description: "Show your doantu stats",
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
