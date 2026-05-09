// Package wordle ports the JS wordle module — classic 5-letter word-guess
// game, scored letter-by-letter green/yellow/grey.
package wordle

// WordLength is wordle's fixed 5. Exposed so render.go and tests can reuse it
// without magic numbers.
const WordLength = 5

// LetterResult labels a single guessed letter's state. Values match the JS
// wire format byte-for-byte: "correct" | "partial" | "wrong".
const (
	ResultCorrect = "correct"
	ResultPartial = "partial"
	ResultWrong   = "wrong"
)

// LetterScore is the JSON shape stored in KV per guess. Field tags match JS
// exactly so a saved JS game round-trips through Go without a custom decoder.
type LetterScore struct {
	Letter string `json:"letter"`
	Result string `json:"result"`
}

// CompareWords scores guess against target letter-by-letter. Both are assumed
// lowercase a-z and exactly WordLength long; callers validate via
// validateGuess before reaching here.
//
// Two-pass marking is required to handle duplicate letters correctly:
//   - pass 1: positional matches → "correct"; consume those slots from the
//     target's available pool.
//   - pass 2: remaining guess letters → "partial" if still in the pool (and
//     consume), else "wrong".
//
// Example: target "abbey", guess "babes" →
//
//	b@0 partial, a@1 partial, b@2 correct, e@3 correct, s@4 wrong.
func CompareWords(guess, target string) []LetterScore {
	out := make([]LetterScore, WordLength)
	pool := make([]byte, 0, WordLength)

	// Pass 1 — positional matches.
	for i := 0; i < WordLength; i++ {
		if guess[i] == target[i] {
			out[i] = LetterScore{Letter: string(guess[i]), Result: ResultCorrect}
		} else {
			pool = append(pool, target[i])
		}
	}

	// Pass 2 — partial matches against the remaining-pool, with consumption.
	for i := 0; i < WordLength; i++ {
		if out[i].Result == ResultCorrect {
			continue
		}
		idx := indexOfByte(pool, guess[i])
		if idx >= 0 {
			pool = append(pool[:idx], pool[idx+1:]...)
			out[i] = LetterScore{Letter: string(guess[i]), Result: ResultPartial}
		} else {
			out[i] = LetterScore{Letter: string(guess[i]), Result: ResultWrong}
		}
	}
	return out
}

// indexOfByte returns the first index of c in s, or -1.
// (bytes.IndexByte gives the same answer; inlined to keep this file
// dependency-free and emphasize the JS-parity origin.)
func indexOfByte(s []byte, c byte) int {
	for i, b := range s {
		if b == c {
			return i
		}
	}
	return -1
}
