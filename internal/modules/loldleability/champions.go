// Package loldleability ports the JS loldle-ability variant — guess the
// champion from a single ability icon. Pool seeded from Riot Data Dragon
// (passive + Q/W/E/R for each champion). Uses Telegram's sendPhoto with the
// DDragon CDN URL as the file source — no binary embedding.
//
// Round state persists `{target, slot, guesses, startedAt}` so the SAME
// ability icon shows across all turns until the round ends.
package loldleability

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Ability is one of P/Q/W/E/R for a champion.
type Ability struct {
	Slot string `json:"slot"` // P, Q, W, E, R
	Name string `json:"name"`
	Icon string `json:"icon"` // absolute DDragon CDN URL
}

// AbilityChampion is one record of abilities.json — championName + the full
// ability list.
type AbilityChampion struct {
	ChampionName string    `json:"championName"`
	Key          string    `json:"key"` // DDragon internal id; not used by handlers but kept for parity
	Abilities    []Ability `json:"abilities"`
}

//go:embed data/abilities.json
var rawAbilities []byte

// loadPool parses abilities.json and drops champions with no abilities.
// Panics on malformed data — corrupt regen is a build-time bug.
func loadPool() []AbilityChampion {
	var all []AbilityChampion
	if err := json.Unmarshal(rawAbilities, &all); err != nil {
		panic(fmt.Sprintf("loldleability: cannot decode abilities.json: %v", err))
	}
	out := make([]AbilityChampion, 0, len(all))
	for _, c := range all {
		if len(c.Abilities) > 0 {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		panic("loldleability: abilities.json contained no usable records")
	}
	return out
}

// abilityBySlot finds the ability with the given slot ("Q", "W", ...).
// Returns nil when the slot is unknown — caller treats that as a refresh
// signal (start over).
func abilityBySlot(c *AbilityChampion, slot string) *Ability {
	for i := range c.Abilities {
		if c.Abilities[i].Slot == slot {
			return &c.Abilities[i]
		}
	}
	return nil
}
