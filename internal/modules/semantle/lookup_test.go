package semantle

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Hello ", "hello"},
		{"FooBar", "foobar"},
		{"  word  ", "word"},
		{"", ""},
		{"two   words", "two words"},
	}
	for _, c := range cases {
		if got := normalize(c.in); got != c.want {
			t.Errorf("normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsValidShape(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"hello", true},
		{"a", true},
		{"", false},
		{"two words", false},   // spaces not allowed in semantle
		{"hello1", false},      // digits not allowed
		{"hello!", false},      // punctuation not allowed
		{string(make([]byte, 65)), false}, // > 64 chars
	}
	for _, c := range cases {
		if got := isValidShape(c.in); got != c.want {
			t.Errorf("isValidShape(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLoadWords_HasContent(t *testing.T) {
	words, set := loadWords()
	if len(words) < 1000 {
		t.Errorf("loadWords: expected >1000 words, got %d", len(words))
	}
	if len(set) != len(words) {
		t.Errorf("loadWords: slice/set size mismatch: %d vs %d", len(words), len(set))
	}
	// "the" is the most common English word — sanity check.
	if _, ok := set["the"]; !ok {
		t.Errorf("loadWords: 'the' missing from vocab — list looks malformed")
	}
}
