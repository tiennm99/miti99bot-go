package twentyq

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Judgement is the canonical shape after parse + normalize.
// Answer ∈ {"yes","no"}.
type Judgement struct {
	IsGuess bool   `json:"is_guess"`
	Answer  string `json:"answer"`
	Hint    string `json:"hint"`
}

// RoundStart is the LLM's reply for the round-start prompt.
type RoundStart struct {
	Category    string `json:"category"`
	InitialHint string `json:"initialHint"`
}

const defaultHint = "I couldn't fully parse that — try a clear yes/no question."

// fenceRe strips ```json / ``` code fences if the model disobeys the prompt.
var fenceRe = regexp.MustCompile("(?i)```(?:json)?")

// parseJSON returns the first balanced {...} JSON object found in `text`.
// Returns nil on parse failure — tolerant by design since LLM output is
// untrusted and we'd rather fall back to defaults than 500.
func parseJSON(text string) map[string]any {
	if text == "" {
		return nil
	}
	unfenced := strings.ReplaceAll(fenceRe.ReplaceAllString(text, ""), "```", "")
	start := strings.IndexByte(unfenced, '{')
	if start < 0 {
		return nil
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(unfenced); i++ {
		ch := unfenced[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				slice := unfenced[start : i+1]
				var out map[string]any
				if err := json.Unmarshal([]byte(slice), &out); err != nil {
					return nil
				}
				return out
			}
		}
	}
	return nil
}

// normalizeJudgement coerces a parsed payload to the canonical Judgement
// shape with safe defaults (answer="no", hint=defaultHint, is_guess=false).
func normalizeJudgement(payload map[string]any) Judgement {
	out := Judgement{Answer: "no", Hint: defaultHint}
	if payload == nil {
		return out
	}
	if v, ok := payload["is_guess"].(bool); ok {
		out.IsGuess = v
	}
	if v, ok := payload["answer"].(string); ok {
		if strings.EqualFold(v, "yes") {
			out.Answer = "yes"
		}
	}
	if v, ok := payload["hint"].(string); ok {
		if t := strings.TrimSpace(v); t != "" {
			out.Hint = t
		}
	}
	return out
}

// redactSecret blanks any whole-word case-insensitive match of `target` in
// `hint`. Defense-in-depth — the prompt forbids it, but never trust the
// model. Word boundaries: ASCII-letter neighbours.
func redactSecret(hint, target string) string {
	if target == "" {
		return hint
	}
	pattern := `(?i)\b` + regexp.QuoteMeta(target) + `\b`
	re, err := regexp.Compile(pattern)
	if err != nil {
		return hint
	}
	return re.ReplaceAllString(hint, "(redacted)")
}

// parseRoundStart returns (category, initialHint, ok). ok=false on any
// failure so the caller can substitute fallbacks rather than serve garbage.
func parseRoundStart(payload map[string]any) (string, string, bool) {
	if payload == nil {
		return "", "", false
	}
	cat, _ := payload["category"].(string)
	hint, _ := payload["initialHint"].(string)
	cat = strings.TrimSpace(cat)
	hint = strings.TrimSpace(hint)
	if cat == "" || hint == "" {
		return "", "", false
	}
	return cat, hint, true
}
