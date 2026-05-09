package wordle

import (
	"strings"
	"testing"
)

// resultsLetters joins the .Result fields so test expectations stay readable.
func resultsLetters(rs []LetterScore) string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Result
	}
	return strings.Join(out, ",")
}

func TestCompareWords_AllCorrect(t *testing.T) {
	r := CompareWords("crane", "crane")
	if got, want := resultsLetters(r), "correct,correct,correct,correct,correct"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCompareWords_AllWrong(t *testing.T) {
	r := CompareWords("abcde", "fghij")
	if got, want := resultsLetters(r), "wrong,wrong,wrong,wrong,wrong"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCompareWords_CorrectOverPartial(t *testing.T) {
	// guess "slate" vs target "shale" → s correct, l partial, a correct,
	// t wrong, e correct
	r := CompareWords("slate", "shale")
	if got, want := resultsLetters(r), "correct,partial,correct,wrong,correct"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCompareWords_DuplicateExcessLettersWrong(t *testing.T) {
	// target "abbey", guess "babes": b@0 partial, a@1 partial, b@2 correct,
	// e@3 correct, s@4 wrong
	r := CompareWords("babes", "abbey")
	if got, want := resultsLetters(r), "partial,partial,correct,correct,wrong"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCompareWords_DuplicateGuessSingleTarget(t *testing.T) {
	// target "abide", guess "aahed": a@0 correct, a@1 wrong (pool exhausted),
	// h wrong, e@3 partial, d@4 partial
	r := CompareWords("aahed", "abide")
	if got, want := resultsLetters(r), "correct,wrong,wrong,partial,partial"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCompareWords_DuplicatesBothSides(t *testing.T) {
	// target "lever", guess "ebbed":
	// pass1: e@3=e correct (consume e). pool=[l,e,v,r]
	// pass2: e@0 partial (consume remaining e); b@1 wrong; b@2 wrong; d@4 wrong
	r := CompareWords("ebbed", "lever")
	if got, want := resultsLetters(r), "partial,wrong,wrong,correct,wrong"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCompareWords_AllSameTargetExhaustsPool(t *testing.T) {
	// target "aaaaa", guess "aabbb" → both 'a's are positional matches; the
	// remaining b's find nothing in the pool (already empty), so they're wrong.
	// Locks the "pool exhausted before pass 2" branch.
	r := CompareWords("aabbb", "aaaaa")
	if got, want := resultsLetters(r), "correct,correct,wrong,wrong,wrong"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCompareWords_PreservesGuessLetters(t *testing.T) {
	r := CompareWords("crane", "cloud")
	letters := make([]string, len(r))
	for i, x := range r {
		letters[i] = x.Letter
	}
	want := []string{"c", "r", "a", "n", "e"}
	for i := range want {
		if letters[i] != want[i] {
			t.Errorf("letter[%d] = %s, want %s", i, letters[i], want[i])
		}
	}
}
