package lolschedule

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ictOffset is the ICT (UTC+7) offset. All day boundaries in this module
// are anchored on ICT.
const ictOffset = 7 * time.Hour

// formatHint is the user-facing usage line appended to parse errors.
const formatHint = "Use dd-mm-yyyy, dd/mm/yyyy, or ddmmyyyy."

// IctLocation is the fixed-offset UTC+7 timezone.
var IctLocation = time.FixedZone("ICT", int(ictOffset/time.Second))

// parseDateResult is the outcome of ParseScheduleDate. Date is the start of
// the requested ICT day, expressed as a UTC instant.
type parseDateResult struct {
	OK    bool
	Date  time.Time
	Error string
}

var digitsOnly = regexp.MustCompile(`^\d+$`)

// ictDayStartOf returns the start of the ICT calendar day containing now,
// expressed as a UTC instant.
func ictDayStartOf(now time.Time) time.Time {
	ict := now.In(IctLocation)
	dayStart := time.Date(ict.Year(), ict.Month(), ict.Day(), 0, 0, 0, 0, IctLocation)
	return dayStart.UTC()
}

// ictWeekStartOf returns the start of the ICT calendar week (Monday 00:00 ICT)
// containing now, expressed as a UTC instant. Week boundary is ISO 8601:
// Monday is day 1, Sunday is day 7.
func ictWeekStartOf(now time.Time) time.Time {
	day := ictDayStartOf(now).In(IctLocation)
	// time.Weekday: Sunday=0, Monday=1, ..., Saturday=6.
	// Days since Monday: Mon→0, Tue→1, ..., Sun→6.
	daysFromMonday := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -daysFromMonday).UTC()
}

// addDays returns date + days, preserving time-of-day.
func addDays(date time.Time, days int) time.Time {
	return date.Add(time.Duration(days) * 24 * time.Hour)
}

// splitParts breaks the trimmed input into [dd, mm?, yyyy?] string parts.
// Mirrors JS splitParts: dash- or slash-separated, or 1/2/4/8-digit unbroken.
func splitParts(trimmed string) ([]string, string) {
	if strings.ContainsAny(trimmed, "-/") {
		// Replace both delimiters with a single one, then split.
		normalized := strings.ReplaceAll(trimmed, "/", "-")
		parts := strings.Split(normalized, "-")
		if len(parts) < 1 || len(parts) > 3 {
			return nil, fmt.Sprintf(`Invalid date %q. %s`, trimmed, formatHint)
		}
		for _, p := range parts {
			if p == "" || !digitsOnly.MatchString(p) {
				return nil, fmt.Sprintf(`Invalid date %q. %s`, trimmed, formatHint)
			}
		}
		return parts, ""
	}

	if !digitsOnly.MatchString(trimmed) {
		return nil, fmt.Sprintf(`Invalid date %q. %s`, trimmed, formatHint)
	}
	switch len(trimmed) {
	case 1, 2:
		return []string{trimmed}, ""
	case 4:
		return []string{trimmed[:2], trimmed[2:]}, ""
	case 8:
		return []string{trimmed[:2], trimmed[2:4], trimmed[4:]}, ""
	default:
		return nil, fmt.Sprintf(`Invalid date %q. %s`, trimmed, formatHint)
	}
}

// ParseScheduleDate parses a /lolschedule date argument. Empty input → today.
// Returns the start of the requested ICT day as a UTC instant.
func ParseScheduleDate(input string, now time.Time) parseDateResult {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return parseDateResult{OK: true, Date: ictDayStartOf(now)}
	}

	parts, errMsg := splitParts(trimmed)
	if errMsg != "" {
		return parseDateResult{Error: errMsg}
	}

	ictNow := now.In(IctLocation)
	day, _ := strconv.Atoi(parts[0])
	month := int(ictNow.Month())
	year := ictNow.Year()
	if len(parts) >= 2 {
		month, _ = strconv.Atoi(parts[1])
	}
	if len(parts) >= 3 {
		year, _ = strconv.Atoi(parts[2])
	}

	if day < 1 || day > 31 {
		return parseDateResult{Error: fmt.Sprintf(`Invalid day %q — must be 1–31.`, parts[0])}
	}
	if month < 1 || month > 12 {
		monthStr := ""
		if len(parts) >= 2 {
			monthStr = parts[1]
		}
		return parseDateResult{Error: fmt.Sprintf(`Invalid month %q — must be 1–12.`, monthStr)}
	}
	if year < 1970 || year > 2100 {
		yearStr := ""
		if len(parts) >= 3 {
			yearStr = parts[2]
		}
		return parseDateResult{Error: fmt.Sprintf(`Invalid year %q.`, yearStr)}
	}

	// Build the ICT-midnight instant. time.Date normalises out-of-range days
	// (e.g. April 31 → May 1) so we verify the round-trip below.
	candidate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, IctLocation)
	if candidate.Year() != year || int(candidate.Month()) != month || candidate.Day() != day {
		return parseDateResult{Error: fmt.Sprintf(`Invalid date — %d/%d/%d does not exist.`, day, month, year)}
	}

	return parseDateResult{OK: true, Date: candidate.UTC()}
}
