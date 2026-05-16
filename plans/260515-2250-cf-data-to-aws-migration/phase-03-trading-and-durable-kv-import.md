---
phase: 3
title: "Durable KV import (trading included)"
status: pending
priority: P1
effort: "2h"
dependencies: [1, 2]
---

# Phase 03: Durable KV import (trading included)

## Overview
Copy the 9 durable Cloudflare KV records into DynamoDB under the live runtime key shape. Trading is included as a flat KV copy — the JS Worker already snapshots `Portfolio` JSON into `trading:user:*`, so no D1 transform is required (locked in Phase 01).

## Requirements
- Functional: each migrated KV key lands in DynamoDB at `(pk=moduleName, sk=callerKey)` with the original CF KV value placed in the `value` attribute, byte-for-byte where possible.
- Non-functional: idempotent — rerun must not duplicate or corrupt; skipped/failed records must be reported, not silently dropped.

## Architecture
- Single import mode: KV → DynamoDB. No D1 transform path.
- Durable key set (locked in Phase 01):
  - `wordle:stats:*`
  - `loldle:stats:*`
  - `loldle:config:*`
  - `twentyq:stats:*`
  - `lolschedule:subscribers`
  - `trading:user:*`
- Skip set: `trading:sym:*` (cache), retired modules (`doantu`, `loldle-ability`, `loldle-emoji`, `semantle` stats keys), `misc:last_ping` (never written upstream).
- Optional sub-task: dump `trading_trades` (D1) to JSONL for cold audit. Not an import input. Operator-elective.

## Related Code Files
- Modify: `cmd/migrate_cf_data/main.go` — wire up the KV-copy run mode
- Create: `internal/migration/kv_filter.go` — durable/skip allowlist driven by the Phase 01 matrix
- Create: `internal/migration/import_report.go` — counts for imported / skipped / failed
- Create: `internal/migration/trading_audit_dump.go` — optional D1 → JSONL exporter (operator-elective)
- Modify: `docs/cf-to-aws-migration-runbook.md` — append import command + report layout
- Read only: `internal/storage/dynamodb_kv.go`, `internal/modules/trading/portfolio.go`

## Implementation Steps
1. Wire the KV allowlist filter into the import binary so only Phase 01 durable keys are read.
2. For each durable KV key, read the raw value and write to DynamoDB at the matching `(pk, sk)`. Preserve original bytes.
3. Implement idempotency via conditional `PutItem` or `attribute_not_exists` guard, fall back to overwrite with operator flag.
4. Emit an import report (stdout + JSON file) with per-prefix counts: imported, skipped, failed.
5. Add the optional `--trading-audit-dump <path>` flag that streams `trading_trades` rows to a local JSONL file. Default off.
6. Update the runbook with the exact command operators run, and the expected report layout.

## Success Criteria
- [ ] All 9 durable keys land in DynamoDB at the runtime-expected `(pk, sk)`.
- [ ] `trading:user:<id>` round-trips through the Go runtime's `Portfolio` JSON unmarshal without modification (parity check).
- [ ] Re-running the import without flags is a no-op (no duplicates, no corruption).
- [ ] Skipped-by-policy keys (cache, retired modules) are listed in the report, not silently dropped.
- [ ] Optional `trading_trades` audit dump produces JSONL with one row per trade when flag is passed.

## Risk Assessment
- KV value encoding drift: CF KV may return values as strings while DynamoDB attribute typing prefers `B`/`S`. Mitigation: round-trip a known portfolio record and assert byte parity before bulk import.
- Idempotency: an overwrite-by-default rerun could silently revert post-cutover writes. Mitigation: default to `attribute_not_exists` guard and require explicit `--overwrite` flag.

## Notes
- Phase 03 used to assume a D1 → Portfolio transform. That was wrong: KV already holds the final shape. Earlier `trading_transform.go` work is dropped.
