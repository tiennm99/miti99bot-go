// Package migration provides operator-run tooling that moves data from the
// legacy Cloudflare KV/D1 stack into the live AWS DynamoDB store.
//
// The package is intentionally small. It exposes:
//   - Policy: classify a CF KV key as migrate/skip/archive and resolve its
//     target DynamoDB (pk, sk).
//   - CloudflareKVClient / CloudflareD1Client: thin REST readers.
//   - DynamoDBWriter: idempotent writes against the runtime KV table shape.
//   - Report: per-prefix counts for an import run.
//
// The runtime production code (cmd/server) never imports this package.
package migration

import "strings"

// Action is the per-source-key migration decision locked in Phase 01.
type Action string

const (
	ActionMigrate Action = "migrate"
	ActionSkip    Action = "skip"
)

// Decision is the resolved migration decision for one CF KV key.
type Decision struct {
	Action Action
	// PK and SK are populated only when Action == ActionMigrate.
	PK string
	SK string
	// Reason explains a Skip (cache, retired, missing, etc.).
	Reason string
}

// kvRule is one entry in the static allowlist. Order matters: rules are
// matched top-down with HasPrefix so the most specific prefix wins when
// shorter prefixes would otherwise swallow a longer one.
type kvRule struct {
	prefix string
	module string // DynamoDB pk; empty when action != migrate
	skip   string // non-empty marks the rule as skip; value is the reason
}

// kvRules is the locked Phase 01 inventory. Adding a new live CF prefix
// requires re-running the Phase 01 inventory and updating this list.
var kvRules = []kvRule{
	// Durable user data — migrate.
	{prefix: "wordle:stats:", module: "wordle"},
	{prefix: "loldle:stats:", module: "loldle"},
	{prefix: "loldle:config:", module: "loldle"},
	{prefix: "twentyq:stats:", module: "twentyq"},
	{prefix: "lolschedule:subscribers", module: "lolschedule"},
	{prefix: "trading:user:", module: "trading"},

	// Caches — skip.
	{prefix: "trading:sym:", skip: "cache"},
	{prefix: "wordle:game:", skip: "ephemeral"},
	{prefix: "loldle:game:", skip: "ephemeral"},
	{prefix: "twentyq:game:", skip: "ephemeral"},
	{prefix: "lolschedule:matches:", skip: "cache"},

	// Retired modules — operator chose not to archive (Phase 01, 2026-05-16).
	{prefix: "doantu:", skip: "retired"},
	{prefix: "loldle-ability:", skip: "retired"},
	{prefix: "loldle-emoji:", skip: "retired"},
	{prefix: "loldle-quote:", skip: "retired"},
	{prefix: "loldle-splash:", skip: "retired"},
	{prefix: "semantle:", skip: "retired"},
}

// Classify returns the migration decision for a CF KV key.
// Unknown keys default to skip with reason "unknown" so new upstream prefixes
// are visible in the inventory report instead of being silently imported.
func Classify(cfKey string) Decision {
	for _, r := range kvRules {
		if !strings.HasPrefix(cfKey, r.prefix) {
			continue
		}
		if r.skip != "" {
			return Decision{Action: ActionSkip, Reason: r.skip}
		}
		// Migrate. Target sk is the CF key with the leading "<module>:" stripped.
		// Live runtime stores e.g. wordle/stats:<subject>, not wordle/wordle:stats:<subject>.
		sk := strings.TrimPrefix(cfKey, r.module+":")
		return Decision{Action: ActionMigrate, PK: r.module, SK: sk}
	}
	return Decision{Action: ActionSkip, Reason: "unknown"}
}

// DurablePrefixes returns the migrate-action prefixes for use by inventory
// and reporting. Order matches kvRules.
func DurablePrefixes() []string {
	out := make([]string, 0, len(kvRules))
	for _, r := range kvRules {
		if r.skip == "" {
			out = append(out, r.prefix)
		}
	}
	return out
}
