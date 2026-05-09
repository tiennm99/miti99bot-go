package loldle

import "strings"

// normalize folds a name into a comparable form: lowercase, alphanumeric only.
// JS parity with util/normalize-name.js — `String(s).toLowerCase().replace(/[^a-z0-9]/g, "")`.
//
// Used for case/space/punctuation-insensitive name lookup so "Kai'Sa",
// "kaisa", and "KAI SA" all collapse to the same key.
func normalize(s string) string {
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
