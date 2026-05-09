package loldleemoji

import (
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/champname"
)

// Generic lookup primitives live in internal/champname. This file only
// exercises the embedded pool — wiring + filter integration test.
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
	if got := champname.FindByExactName(pool, "Aatrox", championName); got == nil {
		t.Error("expected Aatrox in pool")
	}
}
