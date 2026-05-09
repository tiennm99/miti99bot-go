// Package loldlesplash ports the JS loldle-splash variant — guess the
// champion from a randomly-chosen splash art (any skin, including Default).
// Pool seeded from Riot Data Dragon. Uses Telegram's sendPhoto with the
// DDragon CDN URL directly — no binary embedding.
//
// Round state persists `{target, skinId, guesses, startedAt}` so the SAME
// splash shows across all turns until the round ends.
package loldlesplash

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Skin is one champion-skin record: numeric id, display name, splash URL.
type Skin struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"` // absolute DDragon CDN URL
}

// SplashChampion is one record of splashes.json — championName + the full
// skin list including the Default skin.
type SplashChampion struct {
	ChampionName string `json:"championName"`
	Skins        []Skin `json:"skins"`
}

//go:embed data/splashes.json
var rawSplashes []byte

// loadPool parses splashes.json and drops champions with no skins. Panics
// on malformed data — corrupt regen is a build-time bug.
func loadPool() []SplashChampion {
	var all []SplashChampion
	if err := json.Unmarshal(rawSplashes, &all); err != nil {
		panic(fmt.Sprintf("loldlesplash: cannot decode splashes.json: %v", err))
	}
	out := make([]SplashChampion, 0, len(all))
	for _, c := range all {
		if len(c.Skins) > 0 {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		panic("loldlesplash: splashes.json contained no usable records")
	}
	return out
}

// skinByID finds the skin with the given numeric id. Returns nil when id
// isn't present — caller treats that as a refresh signal (start over).
func skinByID(c *SplashChampion, id int) *Skin {
	for i := range c.Skins {
		if c.Skins[i].ID == id {
			return &c.Skins[i]
		}
	}
	return nil
}
