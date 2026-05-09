package loldleemoji

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"Aatrox":       "aatrox",
		"Kai'Sa":       "kaisa",
		"KAI SA":       "kaisa",
		"Twisted Fate": "twistedfate",
		"!@#":          "",
		"42 Vi":        "42vi",
	}
	for in, want := range cases {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindChampion_ExactAndPrefixAndAmbiguous(t *testing.T) {
	pool := []EmojiChampion{
		{ChampionName: "Aatrox", Emojis: "⚔️"},
		{ChampionName: "Akali", Emojis: "🥷"},
		{ChampionName: "Akshan", Emojis: "🪝"},
	}

	// Exact normalised match.
	if got := findChampion(pool, "AATROX"); got == nil || got.ChampionName != "Aatrox" {
		t.Errorf("AATROX → %v, want Aatrox", got)
	}
	// Unique prefix.
	if got := findChampion(pool, "aat"); got == nil || got.ChampionName != "Aatrox" {
		t.Errorf("aat → %v, want Aatrox", got)
	}
	// Ambiguous prefix → nil.
	if got := findChampion(pool, "ak"); got != nil {
		t.Errorf("ambiguous ak → %v, want nil", got)
	}
	// Empty / non-alphanumeric → nil.
	if got := findChampion(pool, ""); got != nil {
		t.Errorf("empty input → %v, want nil", got)
	}
	if got := findChampion(pool, "!!!"); got != nil {
		t.Errorf("!!! → %v, want nil", got)
	}
}

func TestFindByExactName(t *testing.T) {
	pool := []EmojiChampion{{ChampionName: "Aatrox"}, {ChampionName: "Ahri"}}
	if got := findByExactName(pool, "Ahri"); got == nil || got.ChampionName != "Ahri" {
		t.Errorf("exact Ahri → %v", got)
	}
	if got := findByExactName(pool, "ahri"); got != nil {
		t.Errorf("lowercase should not match exact lookup, got %v", got)
	}
}

func TestLoadPool_DropsEmptyEmojiRecords(t *testing.T) {
	pool := loadPool()
	if n := len(pool); n < 150 || n > 200 {
		t.Errorf("pool size = %d, want ~172", n)
	}
	for _, c := range pool {
		if c.Emojis == "" {
			t.Errorf("empty-emoji record leaked through filter: %s", c.ChampionName)
		}
	}
	// Spot-check a known champion.
	if got := findByExactName(pool, "Aatrox"); got == nil {
		t.Error("expected Aatrox in pool")
	}
}
