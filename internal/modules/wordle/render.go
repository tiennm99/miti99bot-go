package wordle

import (
	"strings"
)

// markerFor maps a LetterScore.Result to the NYT-Wordle share emoji.
func markerFor(result string) string {
	switch result {
	case ResultCorrect:
		return "🟩"
	case ResultPartial:
		return "🟨"
	default:
		return "⬜"
	}
}

// renderGuess formats one guess as the NYT share-pattern: word on one line,
// emoji marker row below.
//
//	CRANE
//	🟩🟨⬜🟩🟩
func renderGuess(word string, results []LetterScore) string {
	var markers strings.Builder
	for _, r := range results {
		markers.WriteString(markerFor(r.Result))
	}
	return strings.ToUpper(word) + "\n" + markers.String()
}

// renderBoard joins all prior guesses, blank-line separated. Used when a
// player asks for `/wordle` mid-round.
func renderBoard(guesses []GuessRecord) string {
	if len(guesses) == 0 {
		return "No guesses yet. Reply with `/wordle <word>`."
	}
	rows := make([]string, len(guesses))
	for i, g := range guesses {
		rows[i] = renderGuess(g.Word, g.Results)
	}
	return strings.Join(rows, "\n\n")
}
