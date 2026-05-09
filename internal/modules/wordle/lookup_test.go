package wordle

import "testing"

func TestNormalizeWord(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"crane":      "crane",
		"CRANE":      "crane",
		"  crane  ":  "crane",
		"c-r-a-n-e":  "crane",
		"héllo":      "hllo", // strips non a-z (including the é and accented o-equivalent)
		"!@#$%":      "",
		"42 crane":   "crane",
	}
	for in, want := range cases {
		if got := normalizeWord(in); got != want {
			t.Errorf("normalizeWord(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateGuess(t *testing.T) {
	dict := map[string]struct{}{"crane": {}, "shale": {}}

	type want struct {
		ok     bool
		reason rejectReason
		word   string
	}
	cases := []struct {
		input string
		want  want
	}{
		{"", want{ok: false, reason: reasonEmpty, word: ""}},
		{"!!!", want{ok: false, reason: reasonEmpty, word: ""}},
		{"cat", want{ok: false, reason: reasonLength, word: "cat"}},
		{"craning", want{ok: false, reason: reasonLength, word: "craning"}},
		{"there", want{ok: false, reason: reasonUnknown, word: "there"}},
		{"crane", want{ok: true, word: "crane"}},
		{"  CRANE!", want{ok: true, word: "crane"}}, // normalization survives
	}
	for _, c := range cases {
		got := validateGuess(dict, c.input)
		if got.OK != c.want.ok || got.Reason != c.want.reason || got.Word != c.want.word {
			t.Errorf("validateGuess(%q) = %+v, want %+v", c.input, got, c.want)
		}
	}
}
