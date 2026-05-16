package migration

import (
	"fmt"
	"io"
	"sort"
)

// Report aggregates counts for one migration run. It is intentionally
// per-prefix rather than per-key so the runbook can compare totals to the
// Phase 01 inventory at a glance.
type Report struct {
	// Imported is keys actually written to DynamoDB.
	Imported map[string]int
	// SkippedExisting is keys already present (idempotent rerun).
	SkippedExisting map[string]int
	// SkippedPolicy is keys rejected by the Phase 01 allowlist; keyed by
	// reason (cache, retired, unknown, ephemeral).
	SkippedPolicy map[string]int
	// Failed is keys that errored mid-import.
	Failed map[string]int
}

func NewReport() *Report {
	return &Report{
		Imported:        map[string]int{},
		SkippedExisting: map[string]int{},
		SkippedPolicy:   map[string]int{},
		Failed:          map[string]int{},
	}
}

func (r *Report) AddImported(prefix string)        { r.Imported[prefix]++ }
func (r *Report) AddSkippedExisting(prefix string) { r.SkippedExisting[prefix]++ }
func (r *Report) AddSkippedPolicy(reason string)   { r.SkippedPolicy[reason]++ }
func (r *Report) AddFailed(prefix string)          { r.Failed[prefix]++ }

// Format writes a human-readable summary. Stable ordering (alphabetical) so
// rerun diffs stay clean. Write errors are ignored — callers pass os.Stdout
// or *bytes.Buffer, where short writes are not actionable.
func (r *Report) Format(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Migration report")
	_, _ = fmt.Fprintln(w, "================")
	writeSection(w, "Imported", r.Imported)
	writeSection(w, "Skipped (already present)", r.SkippedExisting)
	writeSection(w, "Skipped (policy)", r.SkippedPolicy)
	writeSection(w, "Failed", r.Failed)
	_, _ = fmt.Fprintf(w, "TOTAL imported=%d skipped_existing=%d skipped_policy=%d failed=%d\n",
		sum(r.Imported), sum(r.SkippedExisting), sum(r.SkippedPolicy), sum(r.Failed))
}

func writeSection(w io.Writer, label string, m map[string]int) {
	_, _ = fmt.Fprintf(w, "\n%s:\n", label)
	if len(m) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "  %-30s %d\n", k, m[k])
	}
}

func sum(m map[string]int) int {
	t := 0
	for _, v := range m {
		t += v
	}
	return t
}

// PrefixOf returns the longest known prefix from kvRules that matches key,
// or "unknown". Used by Report consumers to bucket counts.
func PrefixOf(key string) string {
	best := ""
	for _, r := range kvRules {
		if len(r.prefix) > len(best) && hasPrefix(key, r.prefix) {
			best = r.prefix
		}
	}
	if best == "" {
		return "unknown"
	}
	return best
}

func hasPrefix(s, p string) bool {
	if len(s) < len(p) {
		return false
	}
	return s[:len(p)] == p
}
