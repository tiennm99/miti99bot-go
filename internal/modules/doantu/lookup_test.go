package doantu

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Con   chó  ", "con chó"},
		{"Máy Bay", "máy bay"},
		{"", ""},
		{"   ", ""},
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
		{"con chó", true},
		{"máy bay", true},
		{"hello", true},
		{"", false},
		{"abc 123", false}, // digits
		{"abc!", false},    // punctuation
		{"  ", false},      // empty after collapse
	}
	for _, c := range cases {
		if got := isValidShape(c.in); got != c.want {
			t.Errorf("isValidShape(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLooksVietnamese(t *testing.T) {
	cases := []struct {
		w    string
		want bool
	}{
		{"con", false},     // pure ASCII, no underscore
		{"chó", true},      // diacritic
		{"thanh_pho", true}, // compound
		{"hello", false},
	}
	for _, c := range cases {
		if got := looksVietnamese(c.w); got != c.want {
			t.Errorf("looksVietnamese(%q) = %v, want %v", c.w, got, c.want)
		}
	}
}
