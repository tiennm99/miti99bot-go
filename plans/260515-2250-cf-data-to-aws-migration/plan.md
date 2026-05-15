---
title: "Migrate Cloudflare data to AWS DynamoDB"
description: "Export durable data from the legacy Cloudflare Worker stack, import it into the live AWS DynamoDB store, verify parity, and gate final cutover on a proven migration runbook."
status: pending
priority: P1
effort: 1-2d
branch: main
tags: [migration, cloudflare, aws, dynamodb, cutover, data]
created: 2026-05-15
blockedBy: []
blocks: [260510-0114-aws-port]
---

# Plan: Cloudflare data → AWS DynamoDB

This plan adds the missing data-migration leg to the in-progress AWS port. AWS runtime + DynamoDB already exist; the gap is getting durable user data out of the legacy Cloudflare KV/D1 stack before final decommission.

## Why separate plan
- `plans/260510-0114-aws-port/` covers runtime + deploy cutover.
- This plan covers source-data inventory, export/import tooling, parity verification, and rollback.
- `aws-port` should not be considered done until this plan passes.

## Locked decisions
- Migrate **durable user-visible data only**.
- Skip ephemeral or disposable data: in-flight game state, schedule caches, stale price caches.
- Keep `misc:last_ping` unless the user explicitly accepts that reset; `/mstats` reads it today.
- No admin HTTP routes. Migration runs as operator-invoked one-shot tooling.
- Rehearsal uses a staging DynamoDB table only. No wipe-and-rerun flow is allowed against the live table.
- Trading import is a **transform**, not a table copy: old D1 rows must become current KV portfolio JSON in the `trading` module.
- After the first AWS-served write, rollback is forward-fix only unless a reverse-sync path is built later.
- Retired module namespaces from the old CF stack are archived or ignored, not revived into AWS.

## Source data classes to classify in Phase 01
- Migrate: `wordle stats:*`, `loldle stats:*`, `loldle config:*`, `twentyq stats:*`, `lolschedule subscribers`, `misc:last_ping`, trading balances/holdings.
- Skip by default: `game:*`, `matches:*`, `sym:*`.
- Decide explicitly: historical trading ledger rows, retired-module data, and the authoritative D1 fields for trading `meta.createdAt` + `meta.invested`.

## Related current code
- `cmd/server/main.go:167` — runtime storage backend selection (`dynamodb|firestore|memory`)
- `internal/storage/dynamodb_provider.go:7` — live DynamoDB partitioning (`pk = moduleName`)
- `internal/storage/dynamodb_kv.go:24` — live DynamoDB sort-key contract (`sk = caller key`)
- `internal/modules/wordle/state.go:50` — `game:*` + `stats:*`
- `internal/modules/loldle/state.go:48` — `game:*`, `stats:*`, `config:*`
- `internal/modules/twentyq/state.go:37` — `game:*` + `stats:*`
- `internal/modules/lolschedule/subscribers.go:14` — `subscribers`
- `internal/modules/trading/portfolio.go:39` — current AWS target shape: per-user KV portfolio JSON
- `plans/260508-2222-go-port-cloud-run/phase-12-cutover.md:37` — prior CF→Go cutover notes (trading-only import assumption)
- `plans/260510-0114-aws-port/phase-07-cutover.md:13` — current AWS final cutover phase

## Phases

| # | Phase | Status | Effort | Key deliverable |
|---|-------|--------|--------|-----------------|
| 01 | [Source inventory and migration policy](phase-01-source-inventory-and-migration-policy.md) | pending | 2-3h | exact CF namespaces/tables mapped to migrate vs skip vs archive |
| 02 | [Backfill toolchain and safety rails](phase-02-backfill-toolchain-and-safety-rails.md) | pending | 3-4h | operator-run export/import binaries + dry-run support |
| 03 | [Trading and durable KV import](phase-03-trading-and-durable-kv-import.md) | pending | 4-6h | transformed trading portfolios + durable KV records loaded into DynamoDB |
| 04 | [Parity verification and rehearsal](phase-04-parity-verification-and-rehearsal.md) | pending | 2-3h | repeatable verifier, mismatch report, rollback drill |
| 05 | [Cutover integration and Cloudflare decommission](phase-05-cutover-integration-and-cloudflare-decommission.md) | pending | 2-3h | AWS cutover checklist updated; CF teardown gated on verified migration |

## Key dependencies
- Blocks: `plans/260510-0114-aws-port/phase-07-cutover.md`
- Uses the already-live AWS target from `plans/260510-0114-aws-port/`
- Should finish before deleting CF Worker/KV/D1 resources referenced in `plans/260508-2222-go-port-cloud-run/phase-12-cutover.md`

## Success bar
- Durable CF data imported into DynamoDB with counts + sampled payload parity.
- Trading balances/holdings and required portfolio metadata match the old system after transform.
- Cutover runbook explicitly distinguishes pre-flip rollback from post-flip forward-fix semantics.
- CF resources are not deleted until parity report is green.
