package loldle

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"Aatrox":   "aatrox",
		"Kai'Sa":   "kaisa",
		"KAI SA":   "kaisa",
		"Twisted Fate": "twistedfate",
		"!@#":      "",
		"42 Vi":    "42vi",
	}
	for in, want := range cases {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindChampion_ExactNormalizedMatch(t *testing.T) {
	cs := []Champion{
		{ChampionName: "Aatrox"},
		{ChampionName: "Ahri"},
		{ChampionName: "Akali"},
	}
	for _, in := range []string{"aatrox", "AATROX", "Aatrox", "A-A-T-R-O-X"} {
		got := findChampion(cs, in)
		if got == nil || got.ChampionName != "Aatrox" {
			t.Errorf("input %q → %v, want Aatrox", in, got)
		}
	}
}

func TestFindChampion_UniquePrefix(t *testing.T) {
	cs := []Champion{
		{ChampionName: "Aatrox"},
		{ChampionName: "Ahri"},
		{ChampionName: "Akali"},
	}
	got := findChampion(cs, "aat")
	if got == nil || got.ChampionName != "Aatrox" {
		t.Errorf("aat → %v, want Aatrox", got)
	}
}

func TestFindChampion_AmbiguousPrefixReturnsNil(t *testing.T) {
	cs := []Champion{
		{ChampionName: "Akali"},
		{ChampionName: "Akshan"},
	}
	if got := findChampion(cs, "ak"); got != nil {
		t.Errorf("ambiguous ak should be nil; got %v", got)
	}
}

func TestFindChampion_EmptyInputReturnsNil(t *testing.T) {
	cs := []Champion{{ChampionName: "Aatrox"}}
	if got := findChampion(cs, ""); got != nil {
		t.Errorf("empty input should be nil; got %v", got)
	}
	if got := findChampion(cs, "!!!"); got != nil {
		t.Errorf("non-alphanumeric input should be nil; got %v", got)
	}
}

func TestFindByExactName(t *testing.T) {
	cs := []Champion{{ChampionName: "Aatrox"}, {ChampionName: "Ahri"}}
	if got := findByExactName(cs, "Ahri"); got == nil || got.ChampionName != "Ahri" {
		t.Errorf("exact Ahri → %v", got)
	}
	// findByExactName is literal — no case-folding.
	if got := findByExactName(cs, "ahri"); got != nil {
		t.Errorf("lowercase should not match exact name lookup; got %v", got)
	}
}

func TestLoadChampions_EmbedIsValid(t *testing.T) {
	cs := loadChampions()
	if n := len(cs); n < 150 || n > 200 {
		t.Errorf("champion count = %d, want ~172", n)
	}
	// Spot-check a known champion that the JS test suite depends on.
	if got := findByExactName(cs, "Aatrox"); got == nil {
		t.Error("expected Aatrox in embedded list")
	} else if got.Gender != "Male" || got.Resource != "Manaless" {
		t.Errorf("Aatrox shape unexpected: %+v", got)
	}
}
