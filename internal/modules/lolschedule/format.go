package lolschedule

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"
)

// leagueOrder lists the most-prestigious tournaments first. Anything not
// in this list is rendered in alphabetical order after the known ones.
var leagueOrder = []string{
	"worlds",
	"msi",
	"first_stand",
	"lck",
	"lpl",
	"lec",
	"lcs",
	"lcp",
	"cblol-brazil",
	"emea_masters",
}

// majorLeagueSlugs filters the lolesports response down to the headline
// tournaments most viewers care about. Without this filter the API
// returns 135+ events/week and replies blow past Telegram's 4096-char limit.
var majorLeagueSlugs = map[string]bool{
	"lck":           true,
	"lpl":           true,
	"lec":           true,
	"lcs":           true,
	"worlds":        true,
	"msi":           true,
	"first_stand":   true,
	"lcp":           true,
	"cblol-brazil":  true,
	"emea_masters": true,
}

// FilterMajor keeps only events whose league slug is in the major-league
// allowlist. Exposed for cron/handler reuse.
func FilterMajor(events []ScheduleEvent) []ScheduleEvent {
	out := make([]ScheduleEvent, 0, len(events))
	for _, e := range events {
		if majorLeagueSlugs[e.League.Slug] {
			out = append(out, e)
		}
	}
	return out
}

// formatIctTime returns "HH:MM" in ICT.
func formatIctTime(t time.Time) string {
	d := t.In(IctLocation)
	return fmt.Sprintf("%02d:%02d", d.Hour(), d.Minute())
}

// formatIctDayLabel returns "Mon Sep 12" in ICT.
func formatIctDayLabel(t time.Time) string {
	weekdays := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	d := t.In(IctLocation)
	return fmt.Sprintf("%s %s %d", weekdays[d.Weekday()], months[d.Month()-1], d.Day())
}

// ictDayKey returns "YYYY-MM-DD" in ICT — used to group week events by day.
func ictDayKey(t time.Time) string {
	d := t.In(IctLocation)
	return fmt.Sprintf("%04d-%02d-%02d", d.Year(), int(d.Month()), d.Day())
}

// teamLabel picks the best short identifier for a team. Empty team → "TBD".
func teamLabel(t Team) string {
	if t.Code != "" {
		return t.Code
	}
	if t.Name != "" {
		return t.Name
	}
	return "TBD"
}

// formatEventLine renders one match as a single line. Already HTML-safe;
// caller can join with "\n" inside a league section.
func formatEventLine(e ScheduleEvent) string {
	var t1, t2 Team
	if len(e.Match.Teams) > 0 {
		t1 = e.Match.Teams[0]
	}
	if len(e.Match.Teams) > 1 {
		t2 = e.Match.Teams[1]
	}
	t1Label := html.EscapeString(teamLabel(t1))
	t2Label := html.EscapeString(teamLabel(t2))
	block := ""
	if e.BlockName != "" {
		block = " (" + html.EscapeString(e.BlockName) + ")"
	}
	bo := ""
	if e.Match.Strategy.Count > 0 {
		bo = fmt.Sprintf(" · Bo%d", e.Match.Strategy.Count)
	}

	switch e.State {
	case "completed":
		var w1, w2 int
		if t1.Result != nil {
			w1 = t1.Result.GameWins
		}
		if t2.Result != nil {
			w2 = t2.Result.GameWins
		}
		left := t1Label
		if t1.Result != nil && t1.Result.Outcome == "win" {
			left = "<b>" + t1Label + "</b>"
		}
		right := t2Label
		if t2.Result != nil && t2.Result.Outcome == "win" {
			right = "<b>" + t2Label + "</b>"
		}
		return fmt.Sprintf("✅ %s %d–%d %s%s%s", left, w1, w2, right, bo, block)
	case "inProgress":
		var w1, w2 int
		if t1.Result != nil {
			w1 = t1.Result.GameWins
		}
		if t2.Result != nil {
			w2 = t2.Result.GameWins
		}
		return fmt.Sprintf("🔴 LIVE %s %d–%d %s%s%s", t1Label, w1, w2, t2Label, bo, block)
	default:
		t, err := time.Parse(time.RFC3339, e.StartTime)
		if err != nil {
			t = time.Time{}
		}
		return fmt.Sprintf("🕒 %s %s vs %s%s%s", formatIctTime(t), t1Label, t2Label, bo, block)
	}
}

// leagueGroup is a per-league bucket used by the formatters.
type leagueGroup struct {
	Slug   string
	Name   string
	Events []ScheduleEvent
}

// groupByLeague preserves leagueOrder for known slugs, then alphabetises
// unknowns by display name. Stable across calls.
func groupByLeague(events []ScheduleEvent) []leagueGroup {
	bySlug := map[string]*leagueGroup{}
	for _, e := range events {
		slug := e.League.Slug
		if slug == "" {
			slug = "unknown"
		}
		name := e.League.Name
		if name == "" {
			name = slug
		}
		g, ok := bySlug[slug]
		if !ok {
			g = &leagueGroup{Slug: slug, Name: name}
			bySlug[slug] = g
		}
		g.Events = append(g.Events, e)
	}

	knownIdx := map[string]int{}
	for i, slug := range leagueOrder {
		knownIdx[slug] = i
	}
	var known, unknown []leagueGroup
	for slug, g := range bySlug {
		if _, ok := knownIdx[slug]; ok {
			known = append(known, *g)
		} else {
			unknown = append(unknown, *g)
		}
	}
	sort.Slice(known, func(i, j int) bool {
		return knownIdx[known[i].Slug] < knownIdx[known[j].Slug]
	})
	sort.Slice(unknown, func(i, j int) bool {
		return unknown[i].Name < unknown[j].Name
	})
	return append(known, unknown...)
}

// renderLeagueSection renders header + lines for one league.
func renderLeagueSection(g leagueGroup) string {
	lines := make([]string, len(g.Events))
	for i, e := range g.Events {
		lines[i] = formatEventLine(e)
	}
	return "<b>" + html.EscapeString(g.Name) + "</b>\n" + strings.Join(lines, "\n")
}

// RenderToday renders the today reply — grouped by league. day may be any
// instant on the target ICT day.
func RenderToday(events []ScheduleEvent, day time.Time) string {
	header := "<b>LoL — " + html.EscapeString(formatIctDayLabel(day)) + "</b> (ICT)"
	if len(events) == 0 {
		return header + "\nNo matches today."
	}
	groups := groupByLeague(events)
	sections := make([]string, len(groups))
	for i, g := range groups {
		sections[i] = renderLeagueSection(g)
	}
	return header + "\n\n" + strings.Join(sections, "\n\n")
}

// RenderWeek renders the next-7-days reply — grouped by league → day.
// `to` is exclusive (the start of the day after the range), so the label
// uses to-1.
func RenderWeek(events []ScheduleEvent, from, to time.Time) string {
	fromLbl := html.EscapeString(formatIctDayLabel(from))
	toLbl := html.EscapeString(formatIctDayLabel(to.Add(-time.Millisecond)))
	header := "<b>LoL — " + fromLbl + " → " + toLbl + "</b> (ICT)"
	if len(events) == 0 {
		return header + "\nNo matches this week."
	}

	leagueBlocks := make([]string, 0, len(events))
	for _, league := range groupByLeague(events) {
		// Group this league's events by ICT day.
		type dayBucket struct {
			Label string
			Lines []string
		}
		days := map[string]*dayBucket{}
		for _, e := range league.Events {
			t, err := time.Parse(time.RFC3339, e.StartTime)
			if err != nil {
				continue
			}
			key := ictDayKey(t)
			d, ok := days[key]
			if !ok {
				d = &dayBucket{Label: formatIctDayLabel(t)}
				days[key] = d
			}
			d.Lines = append(d.Lines, formatEventLine(e))
		}
		// Sort day keys chronologically (lexical works since YYYY-MM-DD).
		keys := make([]string, 0, len(days))
		for k := range days {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		daySections := make([]string, len(keys))
		for i, k := range keys {
			d := days[k]
			daySections[i] = "<i>" + html.EscapeString(d.Label) + "</i>\n" + strings.Join(d.Lines, "\n")
		}
		block := "<b>" + html.EscapeString(league.Name) + "</b>\n" + strings.Join(daySections, "\n")
		leagueBlocks = append(leagueBlocks, block)
	}
	return header + "\n\n" + strings.Join(leagueBlocks, "\n\n")
}
