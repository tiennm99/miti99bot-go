package lolschedule

import (
	"strings"
	"testing"
	"time"
)

func mkEvent(state, slug, name, t1Code, t2Code, startISO string) ScheduleEvent {
	return ScheduleEvent{
		StartTime: startISO,
		State:     state,
		League:    League{Slug: slug, Name: name},
		Match: Match{
			Teams:    []Team{{Code: t1Code}, {Code: t2Code}},
			Strategy: Strategy{Type: "bestOf", Count: 3},
		},
	}
}

func TestFormatEventLine_Unstarted(t *testing.T) {
	e := mkEvent("unstarted", "lck", "LCK", "T1", "GEN", "2026-05-09T05:00:00Z")
	got := formatEventLine(e)
	if !strings.Contains(got, "🕒") {
		t.Errorf("missing clock emoji: %q", got)
	}
	if !strings.Contains(got, "T1 vs GEN") {
		t.Errorf("missing team labels: %q", got)
	}
	if !strings.Contains(got, "Bo3") {
		t.Errorf("missing Bo3: %q", got)
	}
	// 05:00 UTC == 12:00 ICT.
	if !strings.Contains(got, "12:00") {
		t.Errorf("ICT time wrong; got %q (expected 12:00)", got)
	}
}

func TestFormatEventLine_Completed_BoldsWinner(t *testing.T) {
	winResult := &struct {
		Outcome  string `json:"outcome,omitempty"`
		GameWins int    `json:"gameWins,omitempty"`
	}{Outcome: "win", GameWins: 3}
	loseResult := &struct {
		Outcome  string `json:"outcome,omitempty"`
		GameWins int    `json:"gameWins,omitempty"`
	}{Outcome: "loss", GameWins: 1}
	e := ScheduleEvent{
		StartTime: "2026-05-09T05:00:00Z",
		State:     "completed",
		League:    League{Slug: "lck", Name: "LCK"},
		Match: Match{
			Teams: []Team{
				{Code: "T1", Result: winResult},
				{Code: "GEN", Result: loseResult},
			},
			Strategy: Strategy{Count: 5},
		},
	}
	got := formatEventLine(e)
	if !strings.Contains(got, "✅") {
		t.Errorf("missing completed emoji: %q", got)
	}
	if !strings.Contains(got, "<b>T1</b>") {
		t.Errorf("winner not bolded: %q", got)
	}
	if !strings.Contains(got, "3–1") {
		t.Errorf("score missing: %q", got)
	}
	if strings.Contains(got, "<b>GEN</b>") {
		t.Errorf("loser should not be bolded: %q", got)
	}
}

func TestFormatEventLine_InProgress(t *testing.T) {
	w := &struct {
		Outcome  string `json:"outcome,omitempty"`
		GameWins int    `json:"gameWins,omitempty"`
	}{GameWins: 1}
	e := ScheduleEvent{
		StartTime: "2026-05-09T05:00:00Z",
		State:     "inProgress",
		League:    League{Slug: "lck"},
		Match: Match{
			Teams:    []Team{{Code: "T1", Result: w}, {Code: "GEN", Result: w}},
			Strategy: Strategy{Count: 5},
		},
	}
	got := formatEventLine(e)
	if !strings.Contains(got, "🔴 LIVE") {
		t.Errorf("missing LIVE marker: %q", got)
	}
	if !strings.Contains(got, "1–1") {
		t.Errorf("score missing: %q", got)
	}
}

func TestRenderToday_GroupsByLeagueInOrder(t *testing.T) {
	day := time.Date(2026, 5, 9, 0, 0, 0, 0, IctLocation)
	events := []ScheduleEvent{
		mkEvent("unstarted", "lcs", "LCS", "TL", "C9", "2026-05-09T18:00:00Z"),
		mkEvent("unstarted", "lck", "LCK", "T1", "GEN", "2026-05-09T05:00:00Z"),
		mkEvent("unstarted", "lpl", "LPL", "JDG", "BLG", "2026-05-09T08:00:00Z"),
	}
	got := RenderToday(events, day)
	// LEAGUE_ORDER puts LCK before LPL before LCS.
	idxLck := strings.Index(got, "<b>LCK</b>")
	idxLpl := strings.Index(got, "<b>LPL</b>")
	idxLcs := strings.Index(got, "<b>LCS</b>")
	if idxLck < 0 || idxLpl < 0 || idxLcs < 0 {
		t.Fatalf("missing league section; got:\n%s", got)
	}
	if idxLck >= idxLpl || idxLpl >= idxLcs {
		t.Errorf("league order wrong: lck=%d lpl=%d lcs=%d\n%s", idxLck, idxLpl, idxLcs, got)
	}
	// Header in ICT.
	if !strings.Contains(got, "LoL — Sat May 9</b> (ICT)") {
		t.Errorf("header wrong: %q", got)
	}
}

func TestRenderToday_EmptyShowsNoMatches(t *testing.T) {
	day := time.Date(2026, 5, 9, 0, 0, 0, 0, IctLocation)
	got := RenderToday(nil, day)
	if !strings.Contains(got, "No matches today.") {
		t.Errorf("empty render missing 'No matches today.': %q", got)
	}
}

func TestRenderWeek_GroupsByLeagueAndDay(t *testing.T) {
	from := time.Date(2026, 5, 9, 0, 0, 0, 0, IctLocation)
	to := from.AddDate(0, 0, 7)
	events := []ScheduleEvent{
		mkEvent("unstarted", "lck", "LCK", "T1", "GEN", "2026-05-09T05:00:00Z"),
		mkEvent("unstarted", "lck", "LCK", "DK", "KT", "2026-05-10T05:00:00Z"),
	}
	got := RenderWeek(events, from, to)
	if !strings.Contains(got, "<b>LCK</b>") {
		t.Errorf("missing LCK section: %q", got)
	}
	// Both days should appear under LCK.
	if !strings.Contains(got, "Sat May 9") {
		t.Errorf("missing Sat May 9: %q", got)
	}
	if !strings.Contains(got, "Sun May 10") {
		t.Errorf("missing Sun May 10: %q", got)
	}
}

func TestFilterMajor(t *testing.T) {
	events := []ScheduleEvent{
		{League: League{Slug: "lck"}},
		{League: League{Slug: "lpl"}},
		{League: League{Slug: "tcl"}}, // Turkish league — not in allowlist
		{League: League{Slug: "lja"}}, // Japan academy — not in allowlist
		{League: League{Slug: "msi"}},
	}
	got := FilterMajor(events)
	if len(got) != 3 {
		t.Errorf("filtered count = %d, want 3", len(got))
	}
	for _, e := range got {
		if e.League.Slug == "tcl" || e.League.Slug == "lja" {
			t.Errorf("non-major league leaked: %s", e.League.Slug)
		}
	}
}

func TestFormatEventLine_EscapesUserStrings(t *testing.T) {
	e := ScheduleEvent{
		StartTime: "2026-05-09T05:00:00Z",
		State:     "unstarted",
		BlockName: "<script>",
		League:    League{Slug: "lck"},
		Match: Match{
			Teams: []Team{
				{Name: "Tom & Jerry"},
				{Name: `"Quotes"`},
			},
			Strategy: Strategy{Count: 1},
		},
	}
	got := formatEventLine(e)
	if strings.Contains(got, "<script>") {
		t.Errorf("raw <script> leaked: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("BlockName not escaped: %q", got)
	}
	if strings.Contains(got, "Tom & Jerry") {
		// & should be escaped to &amp;
		t.Errorf("ampersand not escaped: %q", got)
	}
}
