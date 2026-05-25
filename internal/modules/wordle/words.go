package wordle

import (
	_ "embed"
	"strings"
)

// rawWords holds the raw words.txt bytes embedded at compile time. One word
// per line, lowercase, exactly WordLength a-z; see loadWords for validation.
//
//go:embed data/words.txt
var rawWords string

// loadWords parses the embedded list into a slice plus a membership set. Both
// outputs share the same backing strings, so memory is roughly the dict size
// (≈90 KiB) — well under the binary-size budget.
//
// Words are validated to be exactly WordLength a-z; any malformed line panics
// at startup so a bad regen of the data file is caught immediately, not on
// the first /wordle.
func loadWords() ([]string, map[string]struct{}) {
	lines := strings.Split(strings.TrimSpace(rawWords), "\n")
	words := make([]string, 0, len(lines))
	set := make(map[string]struct{}, len(lines))
	for _, w := range lines {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if !validWord(w) {
			panic("wordle: invalid word in embedded list: " + w)
		}
		words = append(words, w)
		set[w] = struct{}{}
	}
	return words, set
}

func validWord(w string) bool {
	if len(w) != WordLength {
		return false
	}
	for i := 0; i < len(w); i++ {
		c := w[i]
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}
