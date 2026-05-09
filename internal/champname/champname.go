// Package champname holds champion-name primitives shared by loldle and
// loldleemoji (and any future loldle variant). Normalize folds names to a
// comparable form; Find resolves user input to a single champion via exact
// or unique-prefix match. Generic over the champion type via a name-extractor
// closure so each module can keep its own data shape.
package champname

import "strings"

// Normalize folds a name to a comparable form: lowercase, alphanumeric only.
// JS-parity with util/normalize-name.js — `String(s).toLowerCase().replace(/[^a-z0-9]/g, "")`.
//
// Used for case/space/punctuation-insensitive lookup so "Kai'Sa", "kaisa",
// and "KAI SA" all collapse to the same key.
func Normalize(s string) string {
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

// Find resolves user input to a champion via:
//
//  1. Empty / non-alphanumeric input → no match (nil).
//  2. Exact normalised name → that champion.
//  3. Otherwise: unique normalised-prefix match → that champion.
//
// Ambiguous prefix or no hit → nil. Returning nil for ambiguous prefixes
// prevents "Ka" silently routing to whichever Ka- champion happens to be
// first in the data file.
//
// Generic over T so loldle/Champion and loldleemoji/EmojiChampion can both
// use it; nameOf extracts the champion's display name.
func Find[T any](pool []T, input string, nameOf func(*T) string) *T {
	q := Normalize(input)
	if q == "" {
		return nil
	}
	for i := range pool {
		if Normalize(nameOf(&pool[i])) == q {
			return &pool[i]
		}
	}
	var hit *T
	for i := range pool {
		if strings.HasPrefix(Normalize(nameOf(&pool[i])), q) {
			if hit != nil {
				return nil // ambiguous
			}
			hit = &pool[i]
		}
	}
	return hit
}

// FindByExactName looks up a champion by literal display name (no
// normalization). Used to rehydrate a stored game's target. Returns nil if
// the pool was refreshed and the target is no longer present.
func FindByExactName[T any](pool []T, name string, nameOf func(*T) string) *T {
	for i := range pool {
		if nameOf(&pool[i]) == name {
			return &pool[i]
		}
	}
	return nil
}
