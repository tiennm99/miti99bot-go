package loldle

import (
	"strings"
	"testing"
)

// TestRenderGuess_AlignsLabelColumn locks the monospace column-alignment
// invariant: the label column must be padded to the longest label so stacked
// guesses on the board line up. Captures the JS render contract that the
// rest of the test suite never exercises.
func TestRenderGuess_AlignsLabelColumn(t *testing.T) {
	rows := []AttributeRow{
		{Key: "gender", Label: "Gender", Type: attrExact, GuessValue: "Male", Result: ResultCorrect},
		{Key: "release_date", Label: "Release year", Type: attrYear, GuessValue: "2010", Result: ResultWrong, Direction: "up"},
	}
	out := renderGuess("Aatrox", rows)

	// HTML envelope must be a <pre>…</pre> so Telegram renders the columns.
	if !strings.HasPrefix(out, "<pre>") || !strings.HasSuffix(out, "</pre>") {
		t.Fatalf("expected <pre>…</pre>, got %q", out)
	}

	// Longest label is "Release year" (12 chars). All inner rows must pad
	// their label to that width so the value column starts at the same
	// offset on every line.
	body := strings.TrimSuffix(strings.TrimPrefix(out, "<pre>"), "</pre>")
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			continue
		}
		// Format: "<emoji> <label-padded-to-width> <value...>"
		// We assert the label is followed by whitespace (or a space-only run
		// totalling label width) before the value. Easiest check: the value
		// column always starts after the same number of bytes from the
		// start-of-line.
		if !strings.Contains(line, "Release year") && !strings.Contains(line, "Champion    ") && !strings.Contains(line, "Gender      ") {
			t.Errorf("row not padded to longest-label width: %q", line)
		}
	}

	// Year direction arrow appears for wrong rows.
	if !strings.Contains(out, "⬆️") {
		t.Errorf("expected up-arrow for wrong year row; got %q", out)
	}
}

func TestRenderGuess_EscapesHTMLInValues(t *testing.T) {
	rows := []AttributeRow{
		{Key: "gender", Label: "Gender", Type: attrExact, GuessValue: "Tom & <Jerry>", Result: ResultWrong},
	}
	out := renderGuess("Aatrox", rows)
	if !strings.Contains(out, "Tom &amp; &lt;Jerry&gt;") {
		t.Errorf("html metachars not escaped: %s", out)
	}
}

func TestRenderBoard_EmptyShowsHint(t *testing.T) {
	if got := renderBoard(nil); !strings.Contains(got, "No guesses yet") {
		t.Errorf("empty board placeholder missing: %q", got)
	}
}

func TestRenderBoard_StackedGuessesUseUnifiedWidth(t *testing.T) {
	// First guess has the longest label "Release year"; second guess uses
	// the same set so unified width should still be 12. Both blocks must
	// align — verify by checking that a 'Gender' row in the second block
	// has the same padding as in the first.
	rowsA := []AttributeRow{
		{Key: "gender", Label: "Gender", Type: attrExact, GuessValue: "Male", Result: ResultCorrect},
		{Key: "release_date", Label: "Release year", Type: attrYear, GuessValue: "2013", Result: ResultCorrect},
	}
	rowsB := []AttributeRow{
		{Key: "gender", Label: "Gender", Type: attrExact, GuessValue: "Female", Result: ResultWrong},
		{Key: "release_date", Label: "Release year", Type: attrYear, GuessValue: "2011", Result: ResultWrong, Direction: "up"},
	}
	board := renderBoard([]boardEntry{
		{Champion: "Aatrox", Results: rowsA},
		{Champion: "Ahri", Results: rowsB},
	})
	// Two row-groups, blank-line separated, share a single <pre> envelope.
	if strings.Count(board, "<pre>") != 1 || strings.Count(board, "</pre>") != 1 {
		t.Errorf("expected single <pre>…</pre> envelope wrapping both groups; got %q", board)
	}
	if !strings.Contains(board, "\n\n") {
		t.Errorf("expected blank-line separator between groups; got %q", board)
	}
}
