---
title: "Migrate miti99bot from CF KV+D1 to MongoDB Atlas M0"
description: "Dual-write migration to Atlas M0 with explicit cold-start abort threshold and Upstash pivot path."
status: superseded
priority: P2
effort: 22h
branch: main
tags: [storage, migration, mongodb, atlas, cloudflare-workers]
created: 2026-04-25
blockedBy: []
blocks: []
supersededBy: 260508-2222-go-port-cloud-run
---

> **SUPERSEDED 2026-05-08** — superseded by [260508-2222-go-port-cloud-run](../260508-2222-go-port-cloud-run/plan.md). Direction changed: rather than swap CF KV+D1 for MongoDB on the same Cloudflare Worker, the bot is being rewritten in Go and deployed to Google Cloud Run with Firestore Native as the storage backend (all free-tier). All architectural concerns this plan addressed (cold-start, dual-write, abort criteria) are re-addressed in the Go-port plan against the new stack.

# Plan: KV+D1 → MongoDB Atlas M0

User-chosen path despite research recommending Upstash. Goal: validate cold-start UX firsthand with safe rollback to KV/D1 (or pivot to Upstash) if M0 cold-start P95 exceeds derived threshold.

## Reports
- [Atlas fit + driver](../reports/researcher-260425-1924-mongodb-atlas-fit-and-driver.md)
- [Schema + migration mechanics](../reports/researcher-260425-1924-mongodb-schema-and-migration.md)
- [Free DB validation matrix](../reports/researcher-260425-1934-free-db-validation-matrix.md)
- [Brainstormer critique](../reports/brainstormer-260425-2034-atlas-plan-critique.md)
- [Code-reviewer correctness](../reports/code-reviewer-260425-2034-atlas-plan-correctness.md)
- [Debugger failure-modes](../reports/debugger-260425-2034-atlas-plan-failure-modes.md)

## Constraints (locked)
- Backend: MongoDB Atlas M0 free, region `aws-ap-southeast-1`.
- Driver: official `mongodb` npm v6.7+; `nodejs_compat_v2`; `compatibility_date >= 2025-03-20` (current `2025-10-01` already qualifies — no change needed).
- Strategy: dual-write → backfill → verify → read-flip → soak → decommission.
- Scope: BOTH `KV` (12 modules) and `D1` (`trading`) → MongoDB.

## Phases

| # | Phase | Status | Effort | Owner files |
|---|-------|--------|--------|-------------|
| 01 | [Atlas setup + wrangler config](phase-01-atlas-setup.md) | pending | 2h | `wrangler.toml`, `.env.deploy.example`, `scripts/check-secret-leaks.js` |
| 02 | [MongoKVStore implementation](phase-02-mongo-kv-store.md) | pending | 3h | `src/db/mongo-*.js` |
| 03 | [MongoTradesStore + trading refactor](phase-03-mongo-sql-store.md) | pending | 3h | `src/db/mongo-trades-store.js`, `src/modules/trading/*` |
| 04 | [Dual-write wrappers + flag + e2e](phase-04-dual-write-wrappers.md) | pending | 4h | `src/db/dual-*.js`, factories, `tests/e2e/*` |
| 05 | [Backfill + verification (local-only)](phase-05-backfill-scripts.md) | pending | 3h | `scripts/backfill-*.js` |
| 06 | [Staged deploy + soak (cold-start gate)](phase-06-staged-deploy-and-soak.md) | pending | 4h | runtime telemetry |
| 07 | [Cutover + decommission](phase-07-cutover-and-decommission.md) | pending | 3h | wrangler bindings |
| 07-ALT | [Pivot to Upstash (STANDBY)](phase-07-alt-pivot.md) | standby | (3-4d if triggered) | `src/db/upstash-*.js` |
| 08 | [Tests + docs](phase-08-tests-and-docs.md) | pending | 1h | `tests/`, `docs/` |

## Critical dependencies
- 01 → 02, 03 (Atlas creds + bundle-size gate required)
- 02 + 03 → 04 (wrappers need both stores; e2e lands here)
- 04 → 05 (dual-write must be live before backfill so concurrent writes hit Mongo too)
- 05 → 06 (no soak without verified data)
- 06 → 07 (cutover blocked on cold-start gate; if breached, **abort to [phase-07-alt-pivot.md](phase-07-alt-pivot.md)**)
- 02–05 land throughout; 08 finalizes after 07

## Abort criteria (must trigger pivot, not retry)
- Phase 06: cold-start P95 for `/wordle` or `/loldle` > `2.5 × P95(cold-ping baseline from Phase 01)` over 24h soak window.
- Phase 06: dual-write divergence rate > 1% sustained for >1h.
- Phase 06: M0 connection saturation events (>400 of 500 cap) observed during burst.
- Phase 06: Worker CPU-time exceeded errors observed on cold start (Free plan 50ms ceiling).
- Pivot path: leave dual-write running → revert `STORAGE_PRIMARY=kv` → execute [phase-07-alt-pivot.md](phase-07-alt-pivot.md).

## Rollback per phase
Every phase file lists explicit rollback. Headline: until Phase 07 deletes KV/D1 bindings, the original data path is one env-flag flip away. Phase 07 is the only irreversible cutover step.

## Alternatives considered (reviewer dissent)

Reviewers (see brainstormer + code-reviewer + debugger reports) recommended:
1. **Skip dual-write** in favor of a 10-minute maintenance window (saves ~6h, removes latency amplification).
2. **Defer trading migration** entirely (D1 free tier handles ~100 writes/day indefinitely).
3. **Single shared `kv` collection** instead of 12 per-module collections.
4. **Pivot to Upstash** before starting (smaller bundle, HTTP-native, no cold-start TLS cost).
5. **Direct `MongoTradesStore`** instead of SQL-pattern dispatcher (applied — see Phase 03).

User chose to proceed with full Atlas migration to validate cold-start UX firsthand. Reviewer correctness/safety findings have been applied throughout phases 01–08; architectural recommendations 1–4 are documented but not adopted. If the cold-start gate trips, [phase-07-alt-pivot.md](phase-07-alt-pivot.md) executes recommendation 4.
