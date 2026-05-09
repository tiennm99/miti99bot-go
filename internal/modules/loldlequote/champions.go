// Package loldlequote ports the JS loldle-quote variant — guess the champion
// from a one-sentence lore blurb. Binary right/wrong scoring (no attribute
// comparison); shares the per-subject lifecycle pattern with classic loldle
// + loldle-emoji but uses its own KV namespace so stats are isolated.
package loldlequote

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// QuoteChampion is one record of quotes.json. The JS source keeps the same
// shape so the embed file lifts unmodified — championName + a one-sentence
// lore blurb where the champion's name is replaced with `___`.
type QuoteChampion struct {
	ChampionName string `json:"championName"`
	Quote        string `json:"quote"`
}

//go:embed data/quotes.json
var rawQuotes []byte

// loadPool parses quotes.json and drops any record with an empty/whitespace
// `quote` field (matching the JS `pool.filter(...)` at the top of
// handlers.js). Panics on malformed data — a corrupt regen of the data file
// is a build-time bug, not a runtime concern.
func loadPool() []QuoteChampion {
	var all []QuoteChampion
	if err := json.Unmarshal(rawQuotes, &all); err != nil {
		panic(fmt.Sprintf("loldlequote: cannot decode quotes.json: %v", err))
	}
	out := make([]QuoteChampion, 0, len(all))
	for _, c := range all {
		if strings.TrimSpace(c.Quote) != "" {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		panic("loldlequote: quotes.json contained no usable records")
	}
	return out
}
