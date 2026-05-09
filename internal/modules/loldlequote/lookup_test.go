package loldlequote

import (
	"strings"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/champname"
)

// Generic lookup primitives live in internal/champname. This file only
// exercises the embedded pool — wiring + filter integration test.
func TestLoadPool_DropsEmptyQuoteRecords(t *testing.T) {
	pool := loadPool()
	if n := len(pool); n < 150 || n > 200 {
		t.Errorf("pool size = %d, want ~172", n)
	}
	for _, c := range pool {
		if strings.TrimSpace(c.Quote) == "" {
			t.Errorf("empty-quote record leaked through filter: %s", c.ChampionName)
		}
	}
	if got := champname.FindByExactName(pool, "Aatrox", championName); got == nil {
		t.Error("expected Aatrox in pool")
	} else if !strings.Contains(got.Quote, "___") {
		// JS source replaces the champion name with `___` as the redaction
		// marker. Lock the contract here so a future regen that forgets to
		// redact gets caught.
		t.Errorf("Aatrox quote missing `___` redaction marker: %q", got.Quote)
	}
}
