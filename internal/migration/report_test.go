package migration

import (
	"bytes"
	"strings"
	"testing"
)

func TestReportFormat(t *testing.T) {
	r := NewReport()
	r.AddImported("wordle:stats:")
	r.AddImported("wordle:stats:")
	r.AddImported("trading:user:")
	r.AddSkippedExisting("loldle:stats:")
	r.AddSkippedPolicy("cache")
	r.AddSkippedPolicy("cache")
	r.AddSkippedPolicy("retired")
	r.AddFailed("twentyq:stats:")

	var buf bytes.Buffer
	r.Format(&buf)
	got := buf.String()

	// Spot-check structure and totals.
	for _, want := range []string{
		"Imported:",
		"wordle:stats:                  2",
		"trading:user:                  1",
		"Skipped (already present):",
		"loldle:stats:                  1",
		"Skipped (policy):",
		"cache                          2",
		"retired                        1",
		"Failed:",
		"twentyq:stats:                 1",
		"TOTAL imported=3 skipped_existing=1 skipped_policy=3 failed=1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestReportEmptySection(t *testing.T) {
	r := NewReport()
	r.AddImported("wordle:stats:")
	var buf bytes.Buffer
	r.Format(&buf)
	if !strings.Contains(buf.String(), "Failed:\n  (none)") {
		t.Errorf("empty section missing (none) marker:\n%s", buf.String())
	}
}
