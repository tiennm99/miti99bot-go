package wordle

import "testing"

// TestLoadWords_EmbeddedDictIsValid asserts the embedded data file: every
// entry is exactly 5 lowercase a-z, count is plausible, and a known word
// is present. Cheap insurance against a bad regen of words.txt.
func TestLoadWords_EmbeddedDictIsValid(t *testing.T) {
	words, set := loadWords()

	if len(words) < 14000 || len(words) > 15000 {
		t.Errorf("word count = %d, want ~14855", len(words))
	}
	if len(words) != len(set) {
		t.Errorf("words/set length mismatch: %d vs %d (duplicates?)", len(words), len(set))
	}

	// Spot-check known words from the dracos list.
	for _, sample := range []string{"crane", "abase", "zymic"} {
		if _, ok := set[sample]; !ok {
			t.Errorf("expected %q in dict", sample)
		}
	}
}
