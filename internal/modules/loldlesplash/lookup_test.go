package loldlesplash

import (
	"strings"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/champname"
)

func TestLoadPool_SkinsNonEmpty(t *testing.T) {
	pool := loadPool()
	if n := len(pool); n < 150 || n > 200 {
		t.Errorf("pool size = %d, want ~172", n)
	}
	for _, c := range pool {
		if len(c.Skins) == 0 {
			t.Errorf("empty skins record leaked through filter: %s", c.ChampionName)
		}
	}
	got := champname.FindByExactName(pool, "Aatrox", championName)
	if got == nil {
		t.Fatal("expected Aatrox in pool")
	}
	if len(got.Skins) < 2 {
		t.Errorf("Aatrox should have multiple skins, got %d", len(got.Skins))
	}
	for _, s := range got.Skins {
		if !strings.HasPrefix(s.URL, "https://ddragon.leagueoflegends.com/cdn/img/champion/splash/") {
			t.Errorf("Aatrox skin %q URL is not a DDragon splash URL: %q", s.Name, s.URL)
		}
	}
	// Default skin (id=0) must always be present and named "Default".
	if got.Skins[0].ID != 0 || got.Skins[0].Name != "Default" {
		t.Errorf("Aatrox first skin = (%d, %q), want (0, Default)", got.Skins[0].ID, got.Skins[0].Name)
	}
}

func TestSkinByID(t *testing.T) {
	c := &SplashChampion{
		ChampionName: "Test",
		Skins: []Skin{
			{ID: 0, Name: "Default"},
			{ID: 3, Name: "Mecha"},
			{ID: 5, Name: "Sea Hunter"},
		},
	}
	if got := skinByID(c, 3); got == nil || got.Name != "Mecha" {
		t.Errorf("skinByID(3) = %v, want Mecha", got)
	}
	// Unknown id → nil (caller treats as refresh signal).
	if got := skinByID(c, 99); got != nil {
		t.Errorf("skinByID(99) = %v, want nil", got)
	}
}
