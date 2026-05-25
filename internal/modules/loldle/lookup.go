package loldle

import "strings"

// normalizeName folds a name to a comparable form: lowercase, alphanumeric
// only. Used for case/space/punctuation-insensitive lookup so "Kai'Sa",
// "kaisa", and "KAI SA" all collapse to the same key.
func normalizeName(s string) string {
	lower := strings.ToLower(s)
	out := make([]byte, 0, len(lower))
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			out = append(out, c)
		}
	}
	return string(out)
}

// findChampion resolves user input to a champion via:
//
//  1. Empty / non-alphanumeric input → no match (nil).
//  2. Exact normalised name → that champion.
//  3. Otherwise: unique normalised-prefix match → that champion.
//
// Ambiguous prefix or no hit → nil. Returning nil for ambiguous prefixes
// prevents "Ka" silently routing to whichever Ka- champion happens to be
// first in the data file.
func findChampion(pool []Champion, input string) *Champion {
	q := normalizeName(input)
	if q == "" {
		return nil
	}
	for i := range pool {
		if normalizeName(pool[i].ChampionName) == q {
			return &pool[i]
		}
	}
	var hit *Champion
	for i := range pool {
		if strings.HasPrefix(normalizeName(pool[i].ChampionName), q) {
			if hit != nil {
				return nil // ambiguous
			}
			hit = &pool[i]
		}
	}
	return hit
}

// findChampionByExactName looks up a champion by literal display name (no
// normalization). Used to rehydrate a stored game's target. Returns nil if
// the pool was refreshed and the target is no longer present.
func findChampionByExactName(pool []Champion, name string) *Champion {
	for i := range pool {
		if pool[i].ChampionName == name {
			return &pool[i]
		}
	}
	return nil
}
