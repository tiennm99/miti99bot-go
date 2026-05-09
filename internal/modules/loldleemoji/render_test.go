package loldleemoji

import (
	"strings"
	"testing"
)

func TestRenderBoard_EmptyShowsHint(t *testing.T) {
	out := renderBoard("⚔️ 🌍", nil, 5)
	if !strings.Contains(out, "🎭 ⚔️ 🌍") {
		t.Errorf("emoji clue missing: %q", out)
	}
	if !strings.Contains(out, "No guesses yet") {
		t.Errorf("placeholder missing: %q", out)
	}
}

func TestRenderBoard_ListsGuessesWithCounter(t *testing.T) {
	out := renderBoard("⚔️ 🌍", []string{"Aatrox", "Ahri"}, 5)
	if !strings.Contains(out, "Guesses (2/5)") {
		t.Errorf("counter missing: %q", out)
	}
	if !strings.Contains(out, "  • Aatrox  ❌") {
		t.Errorf("first guess line missing: %q", out)
	}
	if !strings.Contains(out, "  • Ahri  ❌") {
		t.Errorf("second guess line missing: %q", out)
	}
}

func TestRenderBoard_EscapesHTMLInGuessNames(t *testing.T) {
	// Champion names from the dict are static strings without HTML metachars,
	// but render escapes defensively. Prove it.
	out := renderBoard("⚔️", []string{"<script>"}, 5)
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("html metachars not escaped: %q", out)
	}
}
