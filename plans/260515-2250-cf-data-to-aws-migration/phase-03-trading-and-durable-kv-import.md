---
phase: 3
title: "Trading and durable KV import"
status: pending
priority: P1
effort: "4-6h"
dependencies: [1, 2]
---

# Phase 03: Trading and durable KV import

## Overview
Run the first real data movement into DynamoDB: import durable KV records and transform old D1 trading data into the current AWS portfolio shape. This phase intentionally does not move ephemeral state or try to preserve abandoned feature surfaces.

## Requirements
- Functional: import approved KV keys plus trading data into DynamoDB while preserving the full current `Portfolio` contract, not just balances/holdings.
- Non-functional: preserve current runtime schemas exactly, avoid duplicate writes on rerun, and keep historical-only data out of the hot path unless explicitly requested.

## Architecture
- KV import target uses the same live DynamoDB shape the runtime already expects: `pk = moduleName`, `sk = caller key`.
- Durable key policy from Phase 01 drives import filters:
  - keep `stats:*`, `config:*`, `subscribers`, `last_ping`, approved trading records
  - reject `game:*`, `matches:*`, `sym:*`
- Trading import is a transform:
  - source: D1 exports from the exact authoritative tables locked in Phase 01
  - target: `trading` module KV entries with keys `user:<telegram_id>`
  - value: `internal/modules/trading/portfolio.go` JSON shape (`currency`, `assets`, `meta`)
  - `meta.createdAt` and `meta.invested` must be mapped from explicit source fields or derivations approved in Phase 01 before this phase starts
- Historical trade rows are exported for audit only unless the user later asks to restore history as a separate feature.

## Related Code Files
- Modify: `cmd/migrate_cf_data/main.go` — add the trading import mode once Phase 01 locks authoritative source tables
- Create: `internal/migration/trading_transform.go`
- Create: `internal/migration/kv_filter.go`
- Create: `internal/migration/import_report.go`
- Modify: `docs/cf-to-aws-migration-runbook.md`
- Read only: `internal/modules/trading/portfolio.go`, `internal/modules/wordle/state.go`, `internal/modules/loldle/state.go`, `internal/modules/twentyq/state.go`, `internal/modules/lolschedule/subscribers.go`

## Implementation Steps
1. Implement KV filters from the Phase 01 migration matrix.
2. Build the D1-to-portfolio transform using the current `Portfolio` JSON contract.
3. Import durable KV records into DynamoDB using module/key parity.
4. Import transformed trading portfolios into the `trading` module namespace.
5. Emit an import report with counts for imported, skipped, archived, and failed records.
6. Capture raw trading exports locally for audit if retention is needed outside runtime state.

## Success Criteria
- [ ] Durable KV keys land in DynamoDB under the exact runtime key names.
- [ ] Trading users get correct `Portfolio` JSON records, including `meta.createdAt` and `meta.invested`.
- [ ] Re-running the import does not duplicate or corrupt data.
- [ ] Skipped datasets are reported explicitly, not silently dropped.
- [ ] Historical-only trading rows are archived or intentionally ignored by policy.

## Risk Assessment
Biggest risk is a wrong trading transform that gives users the wrong balances or holdings. Mitigation: derive the target object from the current `Portfolio` type only, spot-check several real users, and keep raw D1 exports so any discrepancy can be recomputed without touching Cloudflare again.
