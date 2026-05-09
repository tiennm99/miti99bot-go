package loldlequote

import (
	"strings"
	"testing"
)

func TestRenderBoard_EmptyShowsHint(t *testing.T) {
	got := renderBoard("the test quote", nil, 6)
	if !strings.Contains(got, "🎭 <i>the test quote</i>") {
		t.Errorf("missing italic clue line: %q", got)
	}
	if !strings.Contains(got, "/loldle_quote &lt;champion&gt;") {
		t.Errorf("missing usage hint: %q", got)
	}
}

func TestRenderBoard_GuessLines(t *testing.T) {
	got := renderBoard("clue", []string{"Aatrox", "Ahri"}, 6)
	for _, want := range []string{"Guesses (2/6):", "• Aatrox  ❌", "• Ahri  ❌"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// HTML in either the quote text or a guess name (unlikely but defensive)
// must be escaped before emission.
func TestRenderBoard_EscapesHTMLInQuote(t *testing.T) {
	got := renderBoard(`<script>alert("x")</script>`, nil, 6)
	if strings.Contains(got, "<script>") {
		t.Errorf("raw <script> leaked: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("expected escaped <script>; got: %q", got)
	}
}
