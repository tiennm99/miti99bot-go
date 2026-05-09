package semantle

import (
	_ "embed"
	"strings"
)

// rawWords is the embedded google-10000-english list, byte-for-byte from the
// JS source's words-data.js (extracted with scripts/build-semantle-words.js).
// The pool doubles as the OOV vocabulary — anything not in here gets the
// "not in vocabulary" reply rather than a noisy embedding score.
//
//go:embed data/words.txt
var rawWords string

// loadWords parses the embedded list into (slice, set). The slice preserves
// JS pick order (target = LINES[Math.floor(Math.random()*LINES.length)]) and
// the set is for O(1) membership checks.
func loadWords() ([]string, map[string]struct{}) {
	lines := strings.Split(strings.TrimSpace(rawWords), "\n")
	words := make([]string, 0, len(lines))
	set := make(map[string]struct{}, len(lines))
	for _, w := range lines {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		words = append(words, w)
		set[w] = struct{}{}
	}
	return words, set
}
