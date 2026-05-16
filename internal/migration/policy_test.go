package migration

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		key      string
		wantAct  Action
		wantPK   string
		wantSK   string
		wantSkip string
	}{
		// Durable migrate paths — these are the 6 prefixes Phase 01 locked.
		{"wordle:stats:-1001760292100", ActionMigrate, "wordle", "stats:-1001760292100", ""},
		{"loldle:stats:1064111334", ActionMigrate, "loldle", "stats:1064111334", ""},
		{"loldle:config:-1001760292100", ActionMigrate, "loldle", "config:-1001760292100", ""},
		{"twentyq:stats:-1001760292100", ActionMigrate, "twentyq", "stats:-1001760292100", ""},
		{"lolschedule:subscribers", ActionMigrate, "lolschedule", "subscribers", ""},
		{"trading:user:1064111334", ActionMigrate, "trading", "user:1064111334", ""},

		// Cache + ephemeral — skip.
		{"trading:sym:FPT", ActionSkip, "", "", "cache"},
		{"wordle:game:abc", ActionSkip, "", "", "ephemeral"},
		{"lolschedule:matches:2026", ActionSkip, "", "", "cache"},

		// Retired modules — skip.
		{"doantu:stats:1064111334", ActionSkip, "", "", "retired"},
		{"semantle:stats:-1001760292100", ActionSkip, "", "", "retired"},
		{"loldle-emoji:stats:-1001760292100", ActionSkip, "", "", "retired"},

		// Unknown prefix — skip with reason "unknown" so it surfaces in reports.
		{"newmodule:foo", ActionSkip, "", "", "unknown"},
	}

	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			got := Classify(c.key)
			if got.Action != c.wantAct {
				t.Fatalf("action=%v want %v", got.Action, c.wantAct)
			}
			if got.PK != c.wantPK {
				t.Errorf("pk=%q want %q", got.PK, c.wantPK)
			}
			if got.SK != c.wantSK {
				t.Errorf("sk=%q want %q", got.SK, c.wantSK)
			}
			if got.Reason != c.wantSkip {
				t.Errorf("reason=%q want %q", got.Reason, c.wantSkip)
			}
		})
	}
}

func TestDurablePrefixes(t *testing.T) {
	got := DurablePrefixes()
	want := []string{
		"wordle:stats:",
		"loldle:stats:",
		"loldle:config:",
		"twentyq:stats:",
		"lolschedule:subscribers",
		"trading:user:",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("idx %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestPrefixOfLongestMatch(t *testing.T) {
	// loldle:stats: and loldle:config: both share the loldle: super-prefix.
	// Longest match wins so report buckets stay precise.
	if got := PrefixOf("loldle:stats:1234"); got != "loldle:stats:" {
		t.Errorf("loldle:stats:1234 → %q, want loldle:stats:", got)
	}
	if got := PrefixOf("loldle:config:5"); got != "loldle:config:" {
		t.Errorf("loldle:config:5 → %q, want loldle:config:", got)
	}
	if got := PrefixOf("zzz:unknown"); got != "unknown" {
		t.Errorf("unknown bucket: got %q", got)
	}
}
