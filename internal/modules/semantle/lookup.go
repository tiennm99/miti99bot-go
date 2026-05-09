package semantle

import (
	"regexp"
	"strings"
)

// shapeRe enforces the JS lookup.js policy: ASCII letters only, no spaces,
// max 64 chars. Wordlist is ASCII at build time so anything else is OOV.
var shapeRe = regexp.MustCompile(`^[a-z]+$`)

// normalize collapses whitespace + lowercases. JS-parity.
func normalize(raw string) string {
	if raw == "" {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(raw)), " "))
}

// isValidShape mirrors JS isValidShape: non-empty, ≤64 chars, ASCII letters.
func isValidShape(word string) bool {
	if word == "" || len(word) > 64 {
		return false
	}
	return shapeRe.MatchString(word)
}
