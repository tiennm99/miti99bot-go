package lolschedule

import (
	"strings"
	"testing"
	"time"
)

// fixed reference now: 2026-05-09 12:00 UTC = 19:00 ICT (still May 9 ICT).
var refNow = time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

func TestParseScheduleDate_EmptyReturnsToday(t *testing.T) {
	got := ParseScheduleDate("", refNow)
	if !got.OK {
		t.Fatalf("empty should be OK; err=%q", got.Error)
	}
	want := time.Date(2026, 5, 9, 0, 0, 0, 0, IctLocation).UTC()
	if !got.Date.Equal(want) {
		t.Errorf("empty → %v, want %v", got.Date, want)
	}
}

func TestParseScheduleDate_FullFormats(t *testing.T) {
	cases := []string{"15-06-2026", "15/06/2026", "15062026"}
	want := time.Date(2026, 6, 15, 0, 0, 0, 0, IctLocation).UTC()
	for _, in := range cases {
		got := ParseScheduleDate(in, refNow)
		if !got.OK {
			t.Errorf("%q: not OK: %s", in, got.Error)
			continue
		}
		if !got.Date.Equal(want) {
			t.Errorf("%q → %v, want %v", in, got.Date, want)
		}
	}
}

func TestParseScheduleDate_DayOnly_DefaultsMonthYear(t *testing.T) {
	got := ParseScheduleDate("15", refNow)
	if !got.OK {
		t.Fatalf("not OK: %s", got.Error)
	}
	want := time.Date(2026, 5, 15, 0, 0, 0, 0, IctLocation).UTC()
	if !got.Date.Equal(want) {
		t.Errorf("15 → %v, want %v", got.Date, want)
	}
}

func TestParseScheduleDate_DayMonth_DefaultsYear(t *testing.T) {
	got := ParseScheduleDate("15-06", refNow)
	if !got.OK {
		t.Fatalf("not OK: %s", got.Error)
	}
	want := time.Date(2026, 6, 15, 0, 0, 0, 0, IctLocation).UTC()
	if !got.Date.Equal(want) {
		t.Errorf("15-06 → %v, want %v", got.Date, want)
	}
	// 4-digit unbroken form: ddmm
	got2 := ParseScheduleDate("1506", refNow)
	if !got2.OK || !got2.Date.Equal(want) {
		t.Errorf("1506 → %v ok=%v, want %v", got2.Date, got2.OK, want)
	}
}

func TestParseScheduleDate_RejectsImpossibleDate(t *testing.T) {
	got := ParseScheduleDate("31-04-2026", refNow) // April has only 30 days
	if got.OK {
		t.Errorf("31-04-2026 should be rejected; got %v", got.Date)
	}
	if !strings.Contains(got.Error, "does not exist") {
		t.Errorf("error should mention non-existent date: %q", got.Error)
	}
}

func TestParseScheduleDate_RejectsInvalidValues(t *testing.T) {
	tests := []struct {
		in   string
		hint string
	}{
		{"abc", "dd-mm-yyyy"},
		{"32-01-2026", "must be 1–31"},
		{"15-13-2026", "must be 1–12"},
		{"15-06-1900", "Invalid year"},
		{"-15", "Invalid date"}, // empty leading part
		{"15--06", "Invalid date"},
	}
	for _, tt := range tests {
		got := ParseScheduleDate(tt.in, refNow)
		if got.OK {
			t.Errorf("%q should be rejected", tt.in)
			continue
		}
		if !strings.Contains(got.Error, tt.hint) {
			t.Errorf("%q error %q missing hint %q", tt.in, got.Error, tt.hint)
		}
	}
}

func TestIctDayStartOf(t *testing.T) {
	// 2026-05-09 19:00 ICT == 12:00 UTC. Start of ICT day = 2026-05-09 00:00 ICT
	// = 2026-05-08 17:00 UTC.
	got := ictDayStartOf(refNow)
	want := time.Date(2026, 5, 8, 17, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("ictDayStartOf = %v, want %v", got, want)
	}
}

func TestAddDays(t *testing.T) {
	base := time.Date(2026, 5, 9, 12, 30, 0, 0, time.UTC)
	got := addDays(base, 3)
	want := time.Date(2026, 5, 12, 12, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("addDays(3) = %v, want %v", got, want)
	}
}
