package loldleability

import (
	"strings"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/champname"
)

func TestLoadPool_AbilitiesNonEmpty(t *testing.T) {
	pool := loadPool()
	if n := len(pool); n < 150 || n > 200 {
		t.Errorf("pool size = %d, want ~172", n)
	}
	for _, c := range pool {
		if len(c.Abilities) == 0 {
			t.Errorf("empty abilities record leaked through filter: %s", c.ChampionName)
		}
	}
	got := champname.FindByExactName(pool, "Aatrox", championName)
	if got == nil {
		t.Fatal("expected Aatrox in pool")
	}
	// Aatrox should have all 5 standard ability slots present.
	slots := map[string]bool{}
	for _, a := range got.Abilities {
		slots[a.Slot] = true
		if !strings.HasPrefix(a.Icon, "https://ddragon.leagueoflegends.com/cdn/") {
			t.Errorf("Aatrox ability %s icon is not a DDragon URL: %q", a.Slot, a.Icon)
		}
	}
	for _, want := range []string{"P", "Q", "W", "E", "R"} {
		if !slots[want] {
			t.Errorf("Aatrox missing slot %q", want)
		}
	}
}

func TestAbilityBySlot(t *testing.T) {
	c := &AbilityChampion{
		ChampionName: "Test",
		Abilities: []Ability{
			{Slot: "P", Name: "Passive"},
			{Slot: "Q", Name: "Q ability"},
			{Slot: "R", Name: "R ability"},
		},
	}
	if got := abilityBySlot(c, "Q"); got == nil || got.Name != "Q ability" {
		t.Errorf("abilityBySlot(Q) = %v, want 'Q ability'", got)
	}
	// Unknown slot → nil (caller treats as refresh signal).
	if got := abilityBySlot(c, "W"); got != nil {
		t.Errorf("abilityBySlot(W) = %v, want nil (slot not present)", got)
	}
}
