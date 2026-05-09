package loldleemoji

import (
	"fmt"
	"html"
	"strings"
)

// renderBoard formats the emoji clue + the wrong-guess list. JS-faithful:
//
//	🎭 ⚔️ 🌍 💪
//
//	Guesses (2/5):
//	  • Aatrox  ❌
//	  • Ahri    ❌
//
// Empty board returns the placeholder hint. emojis is build-time data (embedded
// JSON) so practically safe, but we escape defensively — Telegram's HTML parse
// mode rejects unknown tags and the page-level invariant is "no unescaped
// user/data input in HTML output".
func renderBoard(emojis string, guesses []string, maxGuesses int) string {
	clue := "🎭 " + html.EscapeString(emojis)
	if len(guesses) == 0 {
		return clue + "\n\nNo guesses yet. Reply with <code>/loldle_emoji &lt;champion&gt;</code>."
	}
	lines := make([]string, len(guesses))
	for i, name := range guesses {
		lines[i] = "  • " + html.EscapeString(name) + "  ❌"
	}
	return fmt.Sprintf("%s\n\nGuesses (%d/%d):\n%s",
		clue, len(guesses), maxGuesses, strings.Join(lines, "\n"))
}
