package wordle

import "strings"

// normalizeWord lowercases input and strips anything outside a-z. JS parity:
// `String(input).toLowerCase().replace(/[^a-z]/g, "")`.
func normalizeWord(input string) string {
	lower := strings.ToLower(input)
	out := make([]byte, 0, len(lower))
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if c >= 'a' && c <= 'z' {
			out = append(out, c)
		}
	}
	return string(out)
}

// rejectReason classifies why validateGuess returned not-ok. Values match the
// JS source's discriminated-union strings so the user-facing reply mapping
// (handlers.rejectReason) stays parallel.
type rejectReason string

const (
	reasonEmpty   rejectReason = "empty"
	reasonLength  rejectReason = "length"
	reasonUnknown rejectReason = "unknown"
)

// guessResult mirrors JS's `{ok: true, word} | {ok: false, reason, word}`.
// Word is always populated (the normalized form), even on failure, so callers
// can include it in error messages if desired.
type guessResult struct {
	OK     bool
	Word   string
	Reason rejectReason
}

// validateGuess normalizes input then checks length + dictionary membership.
//
// Reasons in priority order: empty (post-normalize blank) > length > unknown.
func validateGuess(dict map[string]struct{}, input string) guessResult {
	w := normalizeWord(input)
	if w == "" {
		return guessResult{OK: false, Word: w, Reason: reasonEmpty}
	}
	if len(w) != WordLength {
		return guessResult{OK: false, Word: w, Reason: reasonLength}
	}
	if _, ok := dict[w]; !ok {
		return guessResult{OK: false, Word: w, Reason: reasonUnknown}
	}
	return guessResult{OK: true, Word: w}
}
