---
phase: 1
title: "Source inventory and migration policy"
status: completed
priority: P1
effort: "2-3h"
dependencies: []
completed: 2026-05-16
---

# Phase 01: Source inventory and migration policy

## Overview
Identify the exact Cloudflare KV namespaces and D1 tables still carrying production data, then lock a per-key policy: migrate, skip, or archive. The goal is to prevent a noisy "copy everything" migration that drags stale caches, retired modules, or incompatible schemas into DynamoDB.

## Requirements
- Functional: produce a concrete inventory of live CF data sources, active key prefixes, D1 tables, and the AWS target shape for each kept dataset.
- Non-functional: decisions are explicit, reversible, and tied to current code paths — not guesses from old plans.

## Architecture
- Inspect current AWS consumers first: `wordle stats:*`, `loldle stats:*` / `config:*`, `twentyq stats:*`, `lolschedule subscribers`, `misc:last_ping`, `trading user:*` portfolios.
- Inspect legacy CF sources second: KV namespace(s), D1 trading tables, and any retired-module prefixes still present.
- Lock default policy:
  - **Migrate:** long-lived, user-visible state.
  - **Skip:** `game:*`, `matches:*`, `sym:*`, other caches.
  - **Archive-only:** retired modules and optional historical trade rows not consumed by current AWS runtime.

## Related Code Files
- Create: `docs/cf-to-aws-migration-runbook.md`
- Modify: `plans/260510-0114-aws-port/phase-07-cutover.md`
- Read only: `internal/modules/wordle/state.go`, `internal/modules/loldle/state.go`, `internal/modules/twentyq/state.go`, `internal/modules/lolschedule/subscribers.go`, `internal/modules/trading/portfolio.go`

## Implementation Steps
1. Enumerate current AWS key shapes from live code.
2. Pull a source inventory from Cloudflare KV and D1 using operator credentials.
3. Build a migration matrix: source dataset → target DynamoDB key → action (`migrate|skip|archive`).
4. Lock the exact D1 source tables/columns for `Portfolio.Meta.CreatedAt` and `Portfolio.Meta.Invested`.
5. Mark retired namespaces explicitly so they are not silently reintroduced.
6. Update the AWS cutover phase to say final webhook flip is gated on this migration matrix.

## Success Criteria
- [x] Every live CF dataset is classified as migrate, skip, or archive. (matrix in `docs/cf-to-aws-migration-runbook.md`)
- [x] Every migrated dataset has an explicit AWS target key shape.
- [x] Trading `meta.createdAt` and `meta.invested` have authoritative source fields. (KV `trading:user:<id>` — flat JSON snapshot, no D1 derivation)
- [x] Retired-module data is excluded by policy. (`doantu`, `loldle-ability`, `loldle-emoji`, `semantle` stats keys skipped; no archive)
- [x] The cutover plan references this migration gate. (`plans/260510-0114-aws-port/phase-07-cutover.md` lines 13, 20, 42, 72)

## Outcome notes (2026-05-16)
- Live inventory taken via wrangler against prod CF account `miti99` (D1 `miti99bot-db`, KV `f7f190fcb2fa42eb84a05542911334b0`).
- D1: only `trading_trades` exists (11 rows, 1 user). No `users` / `holdings` tables.
- KV: 21 keys total. 9 durable, 7 cache, 5 retired-module stats. `misc:last_ping` does not exist upstream.
- Trading transform branch invalidated — JS Worker already stores final `Portfolio` JSON in KV. Phase 03 rewritten to flat KV copy (effort 4-6h → ~2h).

## Risk Assessment
Main risk is misclassifying a dataset as disposable when users still care about it. Mitigation: classify by current runtime consumers first, then validate Cloudflare inventory against those exact consumers before any tooling is written.
