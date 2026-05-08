# Phase 01 — Atlas Setup + Wrangler Config

## Context Links
- [Atlas fit + driver report](../reports/researcher-260425-1924-mongodb-atlas-fit-and-driver.md) §"MongoDB Driver Specifics"
- [Schema report](../reports/researcher-260425-1924-mongodb-schema-and-migration.md) §1 (M0 ceiling), §4 (region)
- [Debugger failure-modes](../reports/debugger-260425-2034-atlas-plan-failure-modes.md) GAP-A, GAP-C, QW-1..5
- [Code-reviewer findings #5, #14, #19](../reports/code-reviewer-260425-2034-atlas-plan-correctness.md)
- `wrangler.toml:3` — current `compatibility_date = "2025-10-01"` (already satisfies ≥ 2025-03-20)
- `wrangler.toml:14-22` — KV + D1 bindings to retain through cutover
- `package.json:24-30` — current deps; will add only `mongodb`

## Overview
- **Priority:** P0 (gate for everything; bundle-size + CPU-time gates can abort the plan here)
- **Status:** pending
- **Description:** Provision Atlas M0 cluster, wire secrets, enable Node.js compat, run hard gates (bundle size + CPU time + auto-pause behavior). No code merged yet beyond config + secret-leak lint.

## Key Insights
- Atlas M0 only available regions: `aws-ap-southeast-1`, `aws-eu-west-1`, `aws-us-east-1`, `aws-us-west-2`, `aws-ap-southeast-2`. **Pick `aws-ap-southeast-1`** (closest to CF SEA PoPs; user is in VN).
- `nodejs_compat_v2` is the lever; without it `node:net`/`node:tls` are absent and the driver fails at import. v1 vs v2 are alternatives, not additive — switching alters process/Buffer/streams globals.
- M0 auto-pauses after **30 days of zero ops**. Bot has 6+ daily crons → not a real risk if any cron writes Mongo post-cutover. Phase 08 verifies.
- Connection limit: 500. Each cold isolate opens ≥1. Burst risk under deploy stampede; phase-06 tests pre-deploy.
- M0 has **no backups**. Source of truth during dual-write is still KV/D1.
- **Compatibility date** already `2025-10-01` — satisfies `≥ 2025-03-20` requirement (per debugger QW-5). No bump needed; verify only.
- **CF Worker Free plan: 50ms CPU limit**. Mongo TLS+SCRAM CPU cost is unknown; must measure (debugger GAP-A / QW-4).
- **Bundle-size gate** (code-reviewer #5): mongodb v6.7 is reported ~4-5 MB compressed; Free plan limit is 3 MiB. Hard gate before any further phase work.
- **0.0.0.0/0 IP allowlist** is permanent on M0 + Workers (no static egress IP without paid plan); document as risk, not as TODO.

## Requirements

### Functional
- Atlas project + cluster provisioned (region `aws-ap-southeast-1`, name `miti99bot-prod`).
- DB user `miti99bot-worker` with `readWrite` on db `miti99bot`.
- Network access list: **`0.0.0.0/0`** required (CF Workers do not have static IPs). Permanent risk; auth+TLS is the only barrier. Upgrade path: CF Workers paid static egress IP add-on (~$10/mo).
- `MONGODB_URI` set as CF secret AND in `.env.deploy`.
- `wrangler.toml` updated: `compatibility_flags = ["nodejs_compat_v2"]`. `compatibility_date` left at `2025-10-01` (already valid).
- `.env.deploy.example` updated with `MONGODB_URI=` placeholder + comment.
- **Secret-leak lint** (`scripts/check-secret-leaks.js`) introduced **here** (not phase-08 — per brainstormer #10) so any later phase commit cannot leak `MONGODB_URI`.
- **Node-API surface inventory** (per code-reviewer #14): grep `node:` imports + `process.env` + `Buffer.` uses; document each in `docs/using-mongodb.md`.
- **Atlas free-tier email alert** configured (debugger QW-2): cluster unavailability + connections > 400.

### Non-functional
- Connection string never logged (redact in any error path; lint enforces).
- README of repo notes M0 auto-pause behavior.
- One documented rollback procedure (delete cluster, revert wrangler flag, redeploy — KV/D1 still intact).

## Architecture

```
Cloudflare Worker (region: nearest CF PoP)
  │
  │  TCP/TLS over node:net (nodejs_compat_v2)
  │  SCRAM-SHA-256 auth
  ▼
MongoDB Atlas M0 (aws-ap-southeast-1)
  └─ db: miti99bot
       ├─ (collections created lazily by Phase 02/03)
```

Cold path: ~1500ms wall-clock (TLS + SCRAM + server selection). Worst-case ≈ 6.5s when server-selection timeout fires (5000ms) on a paused cluster (per code-reviewer #23). Warm path (memoized client per isolate): ~50–100ms.

## Related Code Files

### MODIFY
- `/config/workspace/tiennm99/miti99bot/wrangler.toml` — add `compatibility_flags`, keep KV/D1 bindings.
- `/config/workspace/tiennm99/miti99bot/.env.deploy.example` (or create if missing) — add `MONGODB_URI=`.
- `/config/workspace/tiennm99/miti99bot/package.json` — add `mongodb@^6.7.0`; add `lint` chain entry for `check-secret-leaks.js`.
- `/config/workspace/tiennm99/miti99bot/README.md` — add M0 auto-pause note.

### CREATE
- `/config/workspace/tiennm99/miti99bot/docs/using-mongodb.md` — operational runbook (cluster URL, auto-pause behavior, rotation, **node:* surface inventory**, `MongoServerSelectionError` catch path note for phase-02).
- `/config/workspace/tiennm99/miti99bot/scripts/check-secret-leaks.js` — fails build if any source file contains `console.log(env.MONGODB_URI)` or similar patterns for `MONGODB_URI`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEBHOOK_SECRET`, `ADMIN_TOKEN` (latter is now removed in phase-05 redesign — keep for defense-in-depth in case it returns).

### DELETE
- (none in this phase)

## Implementation Steps
1. Create Atlas account / project `miti99bot`. Provision M0 in `aws-ap-southeast-1`. Cluster name `miti99bot-prod`. Note: complete in one sitting (Atlas UI sessions expire; debugger #6).
2. Create DB user `miti99bot-worker` with `readWrite@miti99bot`. Strong random password (≥32 chars).
3. Network access list → add `0.0.0.0/0` (only viable choice for Workers). Document as permanent risk.
4. Configure Atlas free-tier email alerts: (a) cluster unavailable; (b) current connections > 400. (debugger QW-2; 5-min in Atlas UI; zero code.)
5. Copy SRV connection string `mongodb+srv://miti99bot-worker:<pass>@<host>/miti99bot?retryWrites=true&w=majority`.
6. `wrangler secret put MONGODB_URI` → paste string.
7. Add `MONGODB_URI=...` to `.env.deploy` (gitignored). Update `.env.deploy.example` with placeholder.
8. Verify `wrangler.toml` `compatibility_date` is `>= 2025-03-20` (current `2025-10-01` qualifies — **no edit**, per debugger QW-5). Add `compatibility_flags = ["nodejs_compat_v2"]`.
9. **Node-API surface inventory** (code-reviewer #14): `grep -rn "import.*node:\|process\.env\|Buffer\." src/ scripts/` → list each occurrence in `docs/using-mongodb.md` §"Node API surface". Confirms what `nodejs_compat_v2` must support.
10. Create `scripts/check-secret-leaks.js` (per brainstormer #10): grep src/ + scripts/ for `console.log(env.MONGODB_URI)`, `console.log(env.TELEGRAM_BOT_TOKEN)`, `console.log(env.TELEGRAM_WEBHOOK_SECRET)`, `console.log(env.ADMIN_TOKEN)`. Exit 1 on match. Wire into `npm run lint` chain in `package.json`.
11. `npm install mongodb@^6.7.0 --save`.
12. **HARD GATE — bundle size** (code-reviewer #5 / debugger QW-4):
    ```sh
    npx wrangler deploy --dry-run --outdir=./.tmp-deploy
    du -sh ./.tmp-deploy
    ```
    Abort if `> 2.7 MiB` on Free plan (3 MiB cap, 10% headroom) or `> 9 MiB` on paid (10 MiB cap).
    On abort: revert phase commits + execute `phase-07-alt-pivot.md`.
13. Smoke test from `wrangler dev`: drop a temporary route `/__mongo-ping` that connects, runs `db.runCommand({ping:1})`, returns `{wall_ms, cpu_note: "check CF dashboard CPU column"}`. Run 5+ cold cycles (10-min spaced). Record both wall-clock AND CF dashboard CPU time. **HARD GATE — CPU time** (debugger GAP-A / QW-1): if any cold ping reports CPU time near 50ms on Free plan, the migration is blocked on Free plan; document the result + escalate to user (paid plan or pivot). Save the cold P95 wall-clock as `BASELINE_COLD_PING_MS` — Phase 06 derives the abort threshold from this.
14. **Auto-pause behavior test** (debugger GAP-C): in Atlas UI, manually pause the cluster, then hit `/__mongo-ping`. Confirm driver throws a catchable `MongoServerSelectionError` after 5s (not a hang). Document the catch-path requirement for phase-02 §"Connection memoization".
15. Delete the temporary `/__mongo-ping` route before commit.
16. Write `docs/using-mongodb.md`: cluster name, region, auto-pause schedule, rotation procedure, rollback procedure, node-API surface inventory (step 9 output), baseline cold-ping P95 (step 13 output), `MongoServerSelectionError` catch requirement (step 14), Atlas alert config (step 4), `0.0.0.0/0` permanence + paid-IP upgrade path.
17. Run `npm run lint` (now includes `check-secret-leaks.js`). All clean.

## Todo List
- [ ] Atlas project + M0 cluster created (`aws-ap-southeast-1`)
- [ ] DB user `miti99bot-worker` created, password vaulted
- [ ] Network access `0.0.0.0/0` added with justification comment in Atlas UI
- [ ] Atlas email alerts configured (cluster unavailable + connections > 400)
- [ ] `MONGODB_URI` set via `wrangler secret put`
- [ ] `MONGODB_URI` mirrored in `.env.deploy` (NOT committed)
- [ ] `.env.deploy.example` updated
- [ ] `wrangler.toml` adds `compatibility_flags = ["nodejs_compat_v2"]` (compatibility_date unchanged)
- [ ] Node-API surface grep run + documented
- [ ] `scripts/check-secret-leaks.js` written + wired into `npm run lint`
- [ ] `mongodb@^6.7.0` installed
- [ ] **HARD GATE: bundle-size dry-run ≤ 2.7 MiB (Free) / ≤ 9 MiB (paid)**
- [ ] **HARD GATE: cold-ping CPU time well under 50ms** (or paid plan documented)
- [ ] Auto-pause behavior tested (catchable error confirmed)
- [ ] Temporary `/__mongo-ping` route deleted pre-commit
- [ ] Baseline cold-ping P95 recorded in `docs/using-mongodb.md` (drives Phase 06 gate)
- [ ] `docs/using-mongodb.md` written
- [ ] README mentions M0 auto-pause
- [ ] `npm run lint` passes (with secret-leak check)

## Success Criteria
- `wrangler dev` connects to Atlas, ping returns OK.
- Bundle-size gate passes.
- CPU-time gate passes (or operator escalates to paid plan).
- Auto-pause yields catchable error within 5s, not a hang.
- Cold-start ping P95 wall-clock recorded as `BASELINE_COLD_PING_MS` for Phase 06 abort threshold derivation.
- Rollback steps documented + verifiable (revert flag → redeploy → KV/D1 path intact).

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Connection string leaks via log | M | H | `check-secret-leaks.js` wired into lint at this phase. |
| `0.0.0.0/0` is exploited | L | H | SCRAM-SHA-256 + TLS; rotate password quarterly; non-guessable username. **Permanent risk** without paid CF static-egress add-on. |
| `nodejs_compat_v2` breaks existing modules | L | M | `compatibility_date 2025-10-01` (post-flag-stabilization). Step 9 inventory + `wrangler dev` smoke before any prod deploy. Vitest does NOT catch this (debugger #19); rely on smoke. |
| M0 auto-pause hits during low-traffic period | L | M | Bot has 6+ daily crons (any one writing Mongo prevents pause). Phase 02 catches `MongoServerSelectionError` → 503. Phase 08 confirms. |
| Bundle exceeds Worker size cap | M | H | **Hard gate at step 12**. Abort to phase-07-alt-pivot if exceeded. |
| Cold-start CPU exceeds 50ms (Free plan) | M | CATASTROPHIC | **Hard gate at step 13**. Free plan blocked → escalate to paid OR pivot. |
| Atlas API session / MFA expires mid-provisioning | L | M | Step 1 note: complete in one sitting; API token TTL ~30d. |

## Security Considerations
- `MONGODB_URI` contains user+password. Treat as secret-tier (same handling as `TELEGRAM_BOT_TOKEN`).
- Password ≥32 chars random.
- DB user has `readWrite` only — NOT `dbAdmin` or `clusterAdmin`.
- Atlas IP allow-list cannot be tightened without CF Workers static-IP add-on (paid) — accept as documented risk.
- All traffic TLS; driver bundles root CA, no manual setup.
- Secret-leak lint runs on every `npm run lint` from this phase forward.

## Rollback (this phase only)
1. `wrangler secret delete MONGODB_URI`.
2. Revert `wrangler.toml` (remove `compatibility_flags`).
3. `npm uninstall mongodb`.
4. `npm run deploy` — bot continues on KV/D1 unchanged.
5. (Optional) Delete Atlas cluster from UI.
6. (Optional) Revert `scripts/check-secret-leaks.js` if migration abandoned entirely. Keep otherwise — rule applies to other secrets too.

## Next Steps
- **Blocks:** Phase 02 (MongoKVStore) needs `MONGODB_URI` available + bundle/CPU gates passed.
- **Unblocks:** Phase 02, Phase 03.
