# Phase 07-ALT — Pivot to Upstash Redis (STANDBY)

## Context Links
- [Free DB validation matrix](../reports/researcher-260425-1934-free-db-validation-matrix.md) §"Final Recommendation" + §"Next Steps (If Upstash Recommended)"
- [Brainstormer Finding #4](../reports/brainstormer-260425-2034-atlas-plan-critique.md) — phantom abort path
- [Debugger GAP-D / QW-6, #23](../reports/debugger-260425-2034-atlas-plan-failure-modes.md) — pre-write Upstash plan
- Phase 06 — abort criteria

## Overview
- **Priority:** STANDBY (execute only if Phase 06 ABORT)
- **Status:** standby
- **Description:** Skeleton plan to pivot from Atlas → Upstash Redis when the cold-start gate trips. Eliminates "draft new plan under outage pressure" by pre-writing the steps. Estimated ~3-4 days net (smaller than Atlas migration because Upstash is HTTP-native and dual-write doesn't have cold-start amplification).

## Trigger
Execute when ANY Phase-06 abort criterion fires:
- Cold-start P95 > derived gate over the soak window.
- Dual-write divergence > 1% sustained for >1h.
- M0 connection saturation events (>400/500).
- M0 auto-pause occurs unexpectedly during soak.
- Atlas outage > 5 min during soak.
- Verifier reports >0.5% data drift.
- Worker CPU-time exceeded errors observed.

## Key Insights
- Upstash Redis is **HTTP-native** (REST API + `@upstash/redis` SDK). No TLS+SCRAM cold-start cost — first request is sub-100ms.
- No long-lived connection pool to manage; no `serverSelectionTimeoutMS` hangs; no auto-pause behavior.
- Free tier: 10K commands/day, 256MB storage. Sufficient for KV scope (~615KB total data).
- Trading (D1) STAYS — Upstash is KV-only; Mongo trading work is dropped or reverted.

## Approach (5 steps)

### Step 1 — Provision + secrets (30 min)
1. Sign up at https://upstash.com (free).
2. Create Redis database in nearest region (likely `us-east-1` or `eu-west-1` since Upstash free tier doesn't offer SEA).
3. Copy `UPSTASH_REDIS_REST_URL` + `UPSTASH_REDIS_REST_TOKEN`.
4. `wrangler secret put UPSTASH_REDIS_REST_URL`
5. `wrangler secret put UPSTASH_REDIS_REST_TOKEN`
6. Mirror in `.env.deploy`.

### Step 2 — UpstashKVStore (~60 LOC; 1-2h)
- New file `src/db/upstash-kv-store.js`. Wraps `Redis.fromEnv()` to satisfy the `KVStore` interface.
- Methods: `get/put/delete/list/getJSON/putJSON`.
- TTL via `EX` flag on `SET`. List via `SCAN` with `MATCH prefix:*`.
- `npm install @upstash/redis`. Verify bundle size remains under CF Workers cap (~50KB; very small).

### Step 3 — Fake + tests (1h)
- New `tests/fakes/fake-upstash.js` — Map-backed; satisfy the surface used (`set/get/del/scan/expire`).
- Mirror Phase 02 test scaffolding for `UpstashKVStore`.
- Reuse the e2e storage-roundtrip test pattern (Phase 04) for KV path only (trading stays D1).

### Step 4 — Factory wiring (30 min)
- Edit `src/db/create-store.js`: add `STORAGE_PRIMARY=upstash` branch.
- Drop or revert MongoKVStore branches (depending on what shipped).
- Trading: `src/db/create-sql-store.js` returns CFSqlStore (D1) only. Revert phase-03 MongoTradesStore work in `src/modules/trading/*.js` IF shipped, OR drop unshipped.

### Step 5 — Re-run dual-write + cutover (1-2 days)
- Dual-write phase: KV (primary) + Upstash (secondary). Same `DualKVStore` from Phase 04 (parameterized — secondary is now UpstashKVStore).
- Backfill from KV → Upstash using the same local-node script pattern as Phase 05 (now writes to Upstash via REST).
- Soak 24h (no cold-start risk on Upstash; Phase 06's gate is irrelevant here).
- Cutover: `STORAGE_PRIMARY=upstash` → `DUAL_WRITE=0` after 24h overlap → delete CF KV namespace via guarded script (Phase 07 step 19 pattern).

## Drop / revert from Atlas plan

### If Phase 02–04 SHIPPED but Phase 07 cutover NOT executed (typical abort timing)
- Revert: `src/db/mongo-*.js`, `src/db/dual-*.js` (or re-parameterize for Upstash), `src/cron/drift-verifier.js`.
- Revert: `src/modules/trading/*` (if Mongo refactor landed) — restore D1-only path.
- Revert: `wrangler.toml` Mongo config + `STORAGE_PRIMARY` flag enum (now includes `upstash`).
- Keep: `scripts/check-secret-leaks.js` (general secret hygiene).
- Mongo cluster: leave running 7 days for forensic analysis, then `mongoexport` snapshot + delete.

### Mark plan.md updates
- Change `STORAGE_PRIMARY` enum to include `"upstash"`.
- Mark Atlas-related files for deletion in Phase 07-ALT cutover.
- Add cross-link from plan.md "Abort criteria" section.

## Estimated Effort
- Step 1: 30 min
- Step 2: 1-2h
- Step 3: 1h
- Step 4: 30 min
- Step 5: 1-2 days (mostly soak)
- **Total: ~3-4 days net**

## Success Criteria
- `UpstashKVStore` passes `KVStore` interface contract tests.
- Backfill verifier reports PASS for all 12 KV modules.
- Soak shows no error spikes; cold-start latency < 200ms (Upstash HTTP fundamental).
- Atlas cluster decommissioned after 7-day forensic window.

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Upstash region latency higher than Atlas SEA | M | L | Free-tier regions are US/EU — adds ~150ms vs SEA-routed Atlas, BUT HTTP avoids cold TLS+SCRAM, so net cold path is still faster. |
| `@upstash/redis` bundle pushes Worker over size cap | L | H | Bundle is ~50KB; far smaller than mongodb (~4-5 MB). Verify via `wrangler deploy --dry-run` (same gate pattern as Phase 01). |
| Free-tier 10K commands/day cap hit | L | M | Audit current ops/day before pivot; if borderline, escalate to paid ($0.20/100K). |
| Operator panics mid-pivot, mixes Mongo + Upstash code | M | H | This file IS the pre-written runbook (debugger QW-6); follow steps 1–5 in order. |

## Rollback
- Same as Phase 06 pivot rollback: `STORAGE_PRIMARY=kv`, `DUAL_WRITE=0`. KV remains authoritative until Upstash backfill verified.

## Next Steps (when triggered)
- Inform user: cold-start gate failed; executing Upstash pivot.
- Begin Step 1.
- Update plan.md status: `Atlas migration: ABORTED on YYYY-MM-DD; Upstash pivot in progress.`
