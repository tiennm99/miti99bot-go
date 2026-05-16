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
- ~~Keep `misc:last_ping`~~ → skip; KV inventory shows it was never written by the JS Worker (`/mstats` resets on AWS).
- No admin HTTP routes. Migration runs as operator-invoked one-shot tooling.
- **Pre-cutover bulk import may target the live table directly** while the Telegram webhook still points to Cloudflare and the AWS bot has served zero writes (amended 2026-05-16). Rationale: until webhook flip, the AWS DynamoDB table is empty and no real user traffic depends on it, so a separate staging table adds setup cost without de-risking anything. **After webhook cutover, any re-import must use a staging table first.** The `attribute_not_exists` idempotency guard is the only safe write path at any time — no wipe-and-rerun flow is allowed against the live table.
- ~~Trading import is a transform~~ → **flat KV copy**. CF KV `trading:user:<id>` already holds the final `Portfolio` JSON shape; no D1 derivation needed. (Phase 01 inventory, 2026-05-16.)
- After the first AWS-served write, rollback is forward-fix only unless a reverse-sync path is built later.
- Retired module namespaces from the old CF stack are skipped (no archive — operator decision 2026-05-16).

## Source data classified in Phase 01 (closed 2026-05-16)
- **Migrate (9 keys):** `wordle:stats:*` (1), `loldle:stats:*` (4), `loldle:config:*` (1), `twentyq:stats:*` (1), `lolschedule:subscribers` (1), `trading:user:*` (1).
- **Skip (cache + missing + retired):** `trading:sym:*` (7), `misc:last_ping` (0), `doantu:stats:*` (2), `loldle-ability:stats:*` (1), `loldle-emoji:stats:*` (1), `semantle:stats:*` (1).
- **Archive-only (optional, operator-elective):** D1 `trading_trades` (11 rows, 1 user) — audit dump, not import input.

Full matrix lives in `docs/cf-to-aws-migration-runbook.md`.

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
| 01 | [Source inventory and migration policy](phase-01-source-inventory-and-migration-policy.md) | completed | 2-3h | exact CF namespaces/tables mapped to migrate vs skip vs archive |
| 02 | [Backfill toolchain and safety rails](phase-02-backfill-toolchain-and-safety-rails.md) | completed | 3-4h | operator-run `cmd/migrate_cf_data` binary (inventory, kv-import, trading-audit-dump) + dry-run + idempotency |
| 03 | [Durable KV import (trading included)](phase-03-trading-and-durable-kv-import.md) | pending | 2h | flat KV→DynamoDB copy for 9 durable keys + optional D1 audit dump |
| 04 | [Parity verification and rehearsal](phase-04-parity-verification-and-rehearsal.md) | pending | 2-3h | repeatable verifier, mismatch report, rollback drill |
| 05 | [Cutover integration and Cloudflare decommission](phase-05-cutover-integration-and-cloudflare-decommission.md) | pending | 2-3h | AWS cutover checklist updated; CF teardown gated on verified migration |

## Key dependencies
- Blocks: `plans/260510-0114-aws-port/phase-07-cutover.md`
- Uses the already-live AWS target from `plans/260510-0114-aws-port/`
- Should finish before deleting CF Worker/KV/D1 resources referenced in `plans/260508-2222-go-port-cloud-run/phase-12-cutover.md`

## Success bar
- Durable CF data imported into DynamoDB with counts + sampled payload parity.
- Trading balances/holdings and required portfolio metadata match the old system byte-for-byte (flat KV copy).
- Cutover runbook explicitly distinguishes pre-flip rollback from post-flip forward-fix semantics.
- CF resources are not deleted until parity report is green.
