package loldleemoji

import "strings"

// normalize folds names to a comparable form: lowercase, alphanumeric only.
// JS-parity with util/normalize-name.js. Same logic also lives in the
// classic loldle package — duplication accepted for now (two callers); a
// shared `internal/champname` helper makes sense once the next loldle
// variant lands.
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
