// Package loldle ports the JS loldle classic mode — guess the League
// champion from attribute hints (gender, species, regions, etc.).
package loldle

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Champion is one row of champions.json. Field tags match loldle.net's
// scraped schema verbatim so the embedded JSON file lifts unmodified from
// the JS source.
type Champion struct {
	ChampionName string   `json:"championName"`
	Gender       string   `json:"gender"`
	Positions    []string `json:"positions"`
	Species      []string `json:"species"`
	Resource     string   `json:"resource"`
	RangeType    []string `json:"range_type"`
	Regions      []string `json:"regions"`
	ReleaseDate  string   `json:"release_date"` // YYYY-MM-DD
}

// rawChampions holds the embedded JSON byte stream. The data file is copied
// byte-for-byte from src/modules/loldle/champions.json in the JS repo so
// dictionaries are identical across runtimes (no normalization at port time).
//
//go:embed data/champions.json
var rawChampions []byte

// loadChampions parses the embedded JSON. Panics on malformed data —
// a corrupt regen of champions.json is a build-time bug, not a runtime
// concern worth recovering from.
func loadChampions() []Champion {
	var cs []Champion
	if err := json.Unmarshal(rawChampions, &cs); err != nil {
		panic(fmt.Sprintf("loldle: cannot decode champions.json: %v", err))
	}
	if len(cs) == 0 {
		panic("loldle: champions.json contained no records")
	}
	return cs
}
