package loldlequote

import (
	"fmt"
	"html"
	"strings"
)

// renderBoard formats the quote clue + the wrong-guess list. JS-faithful:
//
//	🎭 <i>the Darkin Blade — Once honored defenders of Shurima against the Void, ___ ...</i>
//
//	Guesses (2/6):
//	  • Aatrox  ❌
//	  • Ahri    ❌
//
// Empty board returns the placeholder hint. The quote is HTML-escaped before
// wrapping in <i> so apostrophes / ampersands / stray angle brackets in the
// data source can't break Telegram's HTML parse mode.
func renderBoard(quote string, guesses []string, maxGuesses int) string {
	clue := "🎭 <i>" + html.EscapeString(quote) + "</i>"
	if len(guesses) == 0 {
		return clue + "\n\nNo guesses yet. Reply with <code>/loldle_quote &lt;champion&gt;</code>."
	}
	lines := make([]string, len(guesses))
	for i, name := range guesses {
		lines[i] = "  • " + html.EscapeString(name) + "  ❌"
	}
	return fmt.Sprintf("%s\n\nGuesses (%d/%d):\n%s",
		clue, len(guesses), maxGuesses, strings.Join(lines, "\n"))
}
