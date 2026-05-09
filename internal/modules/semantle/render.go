package semantle

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// Telegram message limit is 4096 chars; cap the visible row count so a
// 200-guess game still fits. JS-parity constants.
const (
	maxRows       = 15
	latestMarker  = "➡️"
	plainMarker   = "  "
	maxWordWidth  = 20
)

// renderBoard returns the HTML-formatted board. Latest canonical (if any)
// gets the arrow marker even when sort order shuffles it down.
func renderBoard(guesses []Guess, latestCanonical string) string {
	count := len(guesses)
	header := fmt.Sprintf("🎯 Semantle — %d guess%s", count, plural(count))
	if count == 0 {
		return header + "\n🆕 Round ready — reply with <code>/semantle &lt;word&gt;</code>."
	}

	sorted := make([]Guess, len(guesses))
	copy(sorted, guesses)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Similarity > sorted[j].Similarity
	})
	if len(sorted) > maxRows {
		sorted = sorted[:maxRows]
	}

	wordWidth := 0
	for _, g := range sorted {
		if l := len(g.Canonical); l > wordWidth {
			wordWidth = l
		}
	}
	if wordWidth > maxWordWidth {
		wordWidth = maxWordWidth
	}

	var lines []string
	for i, g := range sorted {
		score := calibrate(g.Similarity)
		marker := plainMarker
		if g.Canonical == latestCanonical {
			marker = latestMarker
		}
		rank := padLeft(fmt.Sprintf("%d", i+1), 2)
		warmth := padLeft(formatWarmth(score), 3)
		word := html.EscapeString(padRight(g.Canonical, wordWidth))
		lines = append(lines, fmt.Sprintf("%s %s  %s  %s %s", marker, rank, warmth, word, warmthEmoji(score)))
	}

	body := "<pre>" + strings.Join(lines, "\n") + "</pre>"
	footer := ""
	if hidden := count - len(sorted); hidden > 0 {
		footer = fmt.Sprintf("\n…%d older guess%s hidden.", hidden, plural(hidden))
	}
	return header + "\n" + body + footer
}

// renderGuess: single-line summary used after a scored guess (above the board).
func renderGuess(g Guess) string {
	score := calibrate(g.Similarity)
	return fmt.Sprintf("<code>%s</code> → %s %s",
		html.EscapeString(g.Canonical), formatWarmth(score), warmthEmoji(score))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

func padLeft(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(s)) + s
}

func padRight(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}
