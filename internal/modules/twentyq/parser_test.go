package twentyq

import "testing"

func TestParseJSON_Plain(t *testing.T) {
	got := parseJSON(`{"is_guess": true, "answer": "yes", "hint": "ok"}`)
	if got == nil {
		t.Fatal("parseJSON returned nil")
	}
	if got["is_guess"] != true || got["answer"] != "yes" {
		t.Errorf("got %+v", got)
	}
}

func TestParseJSON_WithFences(t *testing.T) {
	in := "```json\n{\"is_guess\": false, \"answer\": \"no\", \"hint\": \"x\"}\n```"
	got := parseJSON(in)
	if got == nil {
		t.Fatalf("parseJSON returned nil for fenced input")
	}
	if got["answer"] != "no" {
		t.Errorf("got %+v", got)
	}
}

func TestParseJSON_PreNoise(t *testing.T) {
	in := "Sure! Here's the json: {\"is_guess\": false, \"answer\": \"yes\", \"hint\": \"clue\"}"
	got := parseJSON(in)
	if got == nil || got["answer"] != "yes" {
		t.Errorf("parseJSON pre-noise: got %+v", got)
	}
}

func TestParseJSON_Malformed(t *testing.T) {
	if got := parseJSON("not json at all"); got != nil {
		t.Errorf("malformed input returned %+v, want nil", got)
	}
	if got := parseJSON(""); got != nil {
		t.Errorf("empty input returned %+v, want nil", got)
	}
	if got := parseJSON("{ unclosed"); got != nil {
		t.Errorf("unclosed brace returned %+v", got)
	}
}

func TestNormalizeJudgement_Defaults(t *testing.T) {
	j := normalizeJudgement(nil)
	if j.Answer != "no" || j.IsGuess || j.Hint == "" {
		t.Errorf("nil payload: got %+v", j)
	}

	// "YES" lowercased; missing hint → default; bad is_guess type → false.
	j = normalizeJudgement(map[string]any{
		"is_guess": "true", // wrong type intentionally
		"answer":   "YES",
		"hint":     "  ",
	})
	if j.IsGuess {
		t.Errorf("string is_guess should not coerce to true: %+v", j)
	}
	if j.Answer != "yes" {
		t.Errorf("answer YES: got %q, want yes", j.Answer)
	}
	if j.Hint == "" {
		t.Errorf("blank hint should fall back to default")
	}
}

func TestRedactSecret(t *testing.T) {
	cases := []struct {
		hint, target, want string
	}{
		{"this is a guitar", "guitar", "this is a (redacted)"},
		{"GUITAR is a thing", "guitar", "(redacted) is a thing"},
		{"guitarist works here", "guitar", "guitarist works here"}, // word boundary
		{"clean hint", "guitar", "clean hint"},
		{"empty target", "", "empty target"},
	}
	for _, c := range cases {
		got := redactSecret(c.hint, c.target)
		if got != c.want {
			t.Errorf("redactSecret(%q,%q) = %q, want %q", c.hint, c.target, got, c.want)
		}
	}
}

func TestParseRoundStart(t *testing.T) {
	cat, hint, ok := parseRoundStart(map[string]any{
		"category":    "  instrument  ",
		"initialHint": " a clue ",
	})
	if !ok || cat != "instrument" || hint != "a clue" {
		t.Errorf("got cat=%q hint=%q ok=%v", cat, hint, ok)
	}
	if _, _, ok := parseRoundStart(nil); ok {
		t.Errorf("nil should not parse")
	}
	if _, _, ok := parseRoundStart(map[string]any{"category": ""}); ok {
		t.Errorf("empty category should not parse")
	}
}

func TestValidateQuestion(t *testing.T) {
	cases := []struct {
		raw    string
		wantOK bool
	}{
		{"is it big?", true},
		{"  is it round  ?", true},
		{"  ab", false},                  // too short
		{string(make([]byte, 250)), false}, // too long
		{"What color is it?", false},      // open-ended
		{"why is it heavy?", false},       // open-ended
		{"", false},
	}
	for _, c := range cases {
		v := validateQuestion(c.raw)
		if v.OK != c.wantOK {
			t.Errorf("validate(%q) ok=%v, want %v (reason=%s)", c.raw, v.OK, c.wantOK, v.Reason)
		}
	}
}
