package loldle

import (
	"fmt"
	"html"
	"strings"
)

// guessRow is a single per-attribute render line built up from a comparison.
type guessRow struct {
	marker string
	label  string
	value  string
}

const (
	markerCorrect = "✅"
	markerPartial = "🟨"
	markerWrong   = "❌"
	markerName    = "🎯"
	arrowUp       = "⬆️"
	arrowDown     = "⬇️"
	nameLabel     = "Champion"
)

func markerFor(result string) string {
	switch result {
	case ResultCorrect:
		return markerCorrect
	case ResultPartial:
		return markerPartial
	default:
		return markerWrong
	}
}

func arrowFor(direction string) string {
	switch direction {
	case "up":
		return arrowUp
	case "down":
		return arrowDown
	}
	return ""
}

// buildRows produces the labelled rows for a single guess: a name header
// followed by one row per classic attribute. release_date wrong rows append
// the direction arrow.
func buildRows(championName string, results []AttributeRow) []guessRow {
	rows := make([]guessRow, 0, 1+len(results))
	rows = append(rows, guessRow{marker: markerName, label: nameLabel, value: strings.ToUpper(championName)})
	for _, r := range results {
		value := r.GuessValue
		if r.Key == "release_date" && r.Result != ResultCorrect {
			if a := arrowFor(r.Direction); a != "" {
				value = value + " " + a
			}
		}
		rows = append(rows, guessRow{marker: markerFor(r.Result), label: r.Label, value: value})
	}
	return rows
}

// formatRowGroups joins one or more guess-row groups into a single <pre>
// block. Label column width is the max label across ALL groups, so stacked
// guesses on a board align with each other (JS parity).
func formatRowGroups(groups [][]guessRow) string {
	width := 0
	for _, g := range groups {
		for _, r := range g {
			if n := len(r.label); n > width {
				width = n
			}
		}
	}
	blocks := make([]string, len(groups))
	for i, g := range groups {
		lines := make([]string, len(g))
		for j, r := range g {
			lines[j] = fmt.Sprintf("%s %s %s",
				r.marker, padRight(r.label, width), html.EscapeString(r.value))
		}
		blocks[i] = strings.Join(lines, "\n")
	}
	return "<pre>" + strings.Join(blocks, "\n\n") + "</pre>"
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// renderGuess formats a single guess as the HTML <pre> block.
func renderGuess(championName string, results []AttributeRow) string {
	return formatRowGroups([][]guessRow{buildRows(championName, results)})
}

// boardEntry is one prior guess on the rehydrated board.
type boardEntry struct {
	Champion string
	Results  []AttributeRow
}

// renderBoard formats every prior guess in one aligned <pre> block. Empty
// board returns the placeholder hint instead of an empty <pre>.
func renderBoard(entries []boardEntry) string {
	if len(entries) == 0 {
		return "No guesses yet. Reply with <code>/loldle &lt;champion&gt;</code>."
	}
	groups := make([][]guessRow, len(entries))
	for i, e := range entries {
		groups[i] = buildRows(e.Champion, e.Results)
	}
	return formatRowGroups(groups)
}
