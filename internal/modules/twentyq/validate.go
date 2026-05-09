package twentyq

import (
	"regexp"
	"strings"
)

const (
	minLen = 3
	maxLen = 200
)

// openEndedRe rejects open-ended questions before spending a Gemini call.
// JS-parity prefix list.
var openEndedRe = regexp.MustCompile(`(?i)^\s*(what|how|why|which|who|where|when|tell me|describe|explain)\b`)

// ValidateResult is the public outcome of validateQuestion. Either OK + the
// normalized form, or !OK + a user-visible reason (HTML allowed).
type ValidateResult struct {
	OK         bool
	Normalized string
	Reason     string
}

// validateQuestion strips/collapses whitespace and rejects too-short,
// too-long, or open-ended inputs. Reason strings carry HTML <code> markup
// so the dispatcher's ReplyHTML wrapping is required for those branches.
func validateQuestion(raw string) ValidateResult {
	if raw == "" {
		return ValidateResult{Reason: "Please send a yes/no question after the command."}
	}
	collapsed := strings.Join(strings.Fields(raw), " ")
	if len(collapsed) < minLen {
		return ValidateResult{Reason: "Question too short — try something like <code>is it big?</code>."}
	}
	if len(collapsed) > maxLen {
		return ValidateResult{Reason: "Question too long — keep it under 200 characters."}
	}
	if openEndedRe.MatchString(collapsed) {
		return ValidateResult{Reason: "Yes/no questions only — try <code>is it ...?</code> or <code>does it ...?</code>."}
	}
	return ValidateResult{OK: true, Normalized: collapsed}
}
