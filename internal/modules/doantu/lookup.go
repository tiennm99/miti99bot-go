package doantu

import (
	"regexp"
	"strings"
)

// shapeRe: Unicode letters + combining marks, single space between syllables
// for compound words (`con chó`, `máy bay`). Mirrors JS lookup.js.
var shapeRe = regexp.MustCompile(`^[\p{L}\p{M}]+(?: [\p{L}\p{M}]+)*$`)

func normalize(raw string) string {
	if raw == "" {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(raw)), " "))
}

func isValidShape(word string) bool {
	if word == "" || len(word) > 64 {
		return false
	}
	return shapeRe.MatchString(word)
}
