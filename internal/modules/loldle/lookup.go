package loldle

import "strings"

// findChampion resolves user input to a Champion. JS-faithful semantics:
//
//  1. Empty / non-alphanumeric input → no match (nil).
//  2. Exact normalised name → that champion.
//  3. Otherwise: unique normalised-prefix match → that champion. Ambiguous or
//     no prefix match → nil. Returning nil for ambiguous prefixes prevents
//     "Ka" silently routing to whichever Ka- champion happens to be first
//     in champions.json.
func findChampion(champions []Champion, input string) *Champion {
	q := normalize(input)
	if q == "" {
		return nil
	}

	for i := range champions {
		if normalize(champions[i].ChampionName) == q {
			return &champions[i]
		}
	}

	var prefixHit *Champion
	for i := range champions {
		if strings.HasPrefix(normalize(champions[i].ChampionName), q) {
			if prefixHit != nil {
				// Second prefix match → ambiguous; bail.
				return nil
			}
			prefixHit = &champions[i]
		}
	}
	return prefixHit
}

// findByExactName looks up a champion by literal championName (no normalization).
// Used by handlers when restoring a stored game's target — the JS `find` over
// raw championName.
func findByExactName(champions []Champion, name string) *Champion {
	for i := range champions {
		if champions[i].ChampionName == name {
			return &champions[i]
		}
	}
	return nil
}
