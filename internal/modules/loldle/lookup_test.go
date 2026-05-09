package loldle

import (
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/champname"
)

// Generic name lookup primitives (Normalize, Find, FindByExactName) live in
// internal/champname and are tested there. This file only exercises the
// embedded champions.json — wiring + shape integration test.
func TestLoadChampions_EmbedIsValid(t *testing.T) {
	cs := loadChampions()
	if n := len(cs); n < 150 || n > 200 {
		t.Errorf("champion count = %d, want ~172", n)
	}
	got := champname.FindByExactName(cs, "Aatrox", championName)
	if got == nil {
		t.Fatal("expected Aatrox in embedded list")
	}
	if got.Gender != "Male" || got.Resource != "Manaless" {
		t.Errorf("Aatrox shape unexpected: %+v", got)
	}
}
