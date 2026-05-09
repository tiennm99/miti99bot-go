// Package loldleemoji ports the JS loldle-emoji variant — guess the
// champion from a short emoji clue. Binary right/wrong scoring (no attribute
// comparison); shares the per-subject lifecycle pattern with classic loldle
// but uses its own KV namespace so stats are isolated.
package loldleemoji

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// EmojiChampion is one record of emojis.json. Only championName + emojis
// matter for this variant; the JS source keeps the same shape so the embed
// file lifts unmodified.
type EmojiChampion struct {
	ChampionName string `json:"championName"`
	Emojis       string `json:"emojis"`
}

//go:embed data/emojis.json
var rawEmojis []byte

// loadPool parses emojis.json and drops any record with an empty `emojis`
// string (matching the JS `pool.filter(...)` at the top of handlers.js).
// Panics on malformed data — a corrupt regen of the data file is a build-
// time bug, not a runtime concern worth recovering from.
func loadPool() []EmojiChampion {
	var all []EmojiChampion
	if err := json.Unmarshal(rawEmojis, &all); err != nil {
		panic(fmt.Sprintf("loldleemoji: cannot decode emojis.json: %v", err))
	}
	out := make([]EmojiChampion, 0, len(all))
	for _, c := range all {
		if strings.TrimSpace(c.Emojis) != "" {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		panic("loldleemoji: emojis.json contained no usable records")
	}
	return out
}
