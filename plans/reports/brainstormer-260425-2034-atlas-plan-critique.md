# Atlas Migration Plan — Architectural Critique

**Plan:** `plans/260425-1945-mongodb-atlas-migration/` (8 phases, 24h budgeted)
**Scope of critique:** YAGNI/over-engineering; cold-start gate; abort path realism; admin-route reintroduction; SQL-shaped wrapper around a doc store.

---

## TL;DR

1. Plan is over-engineered for ~615KB KV + ~300KB/mo trading. Dual-write + verifier + soak machinery costs ~13h to potentially throw away.
2. `MongoSqlStore` is a SQL-pattern dispatcher emulating 6 statements — phase-03 is a leaky abstraction; refactoring callers is simpler.
3. Phase-05 reintroduces `/__admin/*` HTTP routes that `docs/architecture.md` § 10 explicitly rejected. Direct violation of project posture.
4. Abort path (`phase-07-alt-pivot.md`) is a TODO. The "bail-out" is not actually planned — the user has no real escape hatch when the gate trips.
5. Trading migration earns nothing on M0 vs D1; phase-03 is ~3h of code with negative business value that just adds risk.

---

## Findings

### 1. Dual-write is over-engineered for this dataset size

**Smell.** Phase 04 (3h) + dual-write divergence handling in Phase 06 (parts of 4h) + reverse-backfill stub debt in Phase 07 (Stage 2 rollback) = ~5–6h of complexity, all of which is bypassable.

**Evidence.**
- `phase-04-dual-write-wrappers.md:15-21` — dual-write rationale: "keep secondary warm + ready for read-flip". Worth it for high-traffic continuous-availability systems. **Not a Telegram bot with ~615KB total data.**
- `phase-05-backfill-scripts.md:16` — order-of-operations: dual-write must precede backfill or "writes during backfill window go to KV only". This dependency is the entire reason dual-write exists. A maintenance-window cutover removes this constraint.
- `phase-04` risk table line `phase-04-dual-write-wrappers.md:159` — "Write amplification doubles latency on every put... Cold p99 = 1500ms". Dual-write WORSENS the very metric Phase 06 is gating on. The plan creates the latency problem then measures it.
- KV `list()` is paginated 1000/page (phase-05 line 17). Total dataset spans ~615KB. A one-shot dump is minutes, not hours.

**Recommendation: CHANGE.** Strongly consider replacing dual-write with a 10-minute maintenance window:
1. Set Telegram webhook to a 503 page, OR keep current bot live but disable writes to commands that mutate state.
2. Run one-shot KV dump → Mongo upsert (local node script, ~5 min for 615KB).
3. Run one-shot D1 dump → Mongo (seconds).
4. Flip `STORAGE_PRIMARY=mongo`, redeploy.
5. Total downtime: 10 min. Telegram retries webhook deliveries on 503 with backoff so most users never notice.

This kills phase-04 entirely (-3h), phase-05's `/__admin/*` routes (-2h of phase-05 + risk surface), and most of phase-06's "dual-write divergence" telemetry. Net: ~5–6h saved AND simpler abort path (just don't flip the flag; KV is still untouched on rollback if you keep KV bindings in `wrangler.toml` for one extra deploy cycle).

**Earns its keep when:** continuous availability is a hard requirement, or dataset is large enough that backfill exceeds an acceptable maintenance window. Neither holds here.

---

### 2. Trading migration is dead weight

**Smell.** Phase-03 (3h) + dual-SQL-store wrapper (part of phase-04) + parity verification (part of phase-05) = ~4–5h to migrate ~100 writes/day.

**Evidence.**
- `phase-03-mongo-sql-store.md:13-15` — "Trading runs exactly 6 distinct SQL statements". Building a SQL-pattern dispatcher to emulate them.
- D1 free tier per CF docs: 5M reads/day + 100k writes/day. Trading uses ~100 writes/day. **Trading is using <0.1% of D1's free quota.**
- The user's stated goal is "validate cold-start UX firsthand". Trading commands aren't on the cold-start latency hot path — they're rare, transactional, and tolerate a 1.5s spike. Migrating them doesn't validate anything new beyond what KV migration already validates.
- `phase-07-cutover-and-decommission.md:81` — wrangler.toml needs D1 binding removed Stage 3. Even if Mongo-side works, you've added MongoSqlStore code that exists for one module.

**Both sides:**
- *For migrating:* operational consistency (one backend), one less binding to manage, eliminates `sql` parameter being conditionally null in module init.
- *Against:* MongoSqlStore is a SQL-shaped wrapper around a document store (see Finding 6); D1 is rock-solid for trading's workload; if Atlas pivot to Upstash happens, trading still needs a SQL backend OR a second migration.

**Recommendation: CHANGE — defer trading migration.** Keep D1 for trading. Migrate KV only. Reasons:
- D1 is doing its job well. Don't fix what isn't broken.
- The cold-start UX validation is fully covered by KV-using modules (`/wordle`, `/loldle` are listed as the soak gate metrics anyway — phase-06-staged-deploy-and-soak.md:23).
- Removes phase-03 (-3h) and SqlStore parts of phase-04 wrapper (-1h) and verifier (-30min).
- Upstash pivot becomes simpler: it's a key-value pivot only; D1 stays put.

**Trade-off:** keeping the dual-backend posture requires `create-sql-store.js` to keep returning `null` when `env.DB` absent. Already true today. Zero cost.

---

### 3. Per-module collections vs single shared collection

**Smell.** 12 collections for 12 modules at ~50KB each. Atlas has no per-collection cost on M0, but ops complexity grows.

**Evidence.**
- `phase-02-mongo-kv-store.md:18-19` — "Per-module collections (12 KV modules → 12 collections), name == module name". Justification: not given beyond convention.
- `phase-02-mongo-kv-store.md:78` — keys still carry the prefix in `_id` ("wordle:games:42"). So both the collection name AND the `_id` carry module identity. Redundant.
- TTL index needs to be created on each of 12 collections (`phase-02-mongo-kv-store.md:124` — `_ensureIndex` per collection).
- Backfill (phase-05) iterates module-by-module — works either way, but verifier counts per-collection.

**Single-collection alternative.**
```js
// One collection: kv
{ _id: "wordle:games:42", value: "...", expiresAt: ... }
```
- One TTL index instead of 12.
- One `createIndex` call instead of 12 lazy-init paths.
- `list()` already filters by `_id` prefix in the per-module wrapper — so cross-module isolation is preserved at the wrapper level, identical semantics.
- Index size is meaningfully smaller per-collection only if collections grow non-uniformly. Here, total = 615KB. Irrelevant.

**Per-module pays off when:** sharding (M10+), per-collection TTL policies differ, or you want per-module backup/restore granularity. None of which apply on M0.

**Recommendation: CHANGE.** Single `kv` collection. Drops phase-02 complexity slightly (single index init), simplifies verifier, simplifies the dump-routes story (which Finding 5 will recommend deleting anyway). Per-module can be added later if scale demands it — pure YAGNI.

**Counter-argument the planner could make:** "Per-module collection makes it visually obvious in Atlas UI which module owns what." Valid for ops debugging. But `_id` prefix achieves the same with `db.kv.find({_id:/^wordle:/}).limit(10)`. Cosmetic, not architectural.

---

### 4. The phantom abort path

**Smell.** The whole plan hinges on a "safe rollback to KV/D1 (or pivot to Upstash)" — but the pivot file doesn't exist.

**Evidence.**
- `plan.md:47` — "if P95 > 3s, **abort to phase-07-alt-pivot.md** TODO".
- `phase-06-staged-deploy-and-soak.md:130` — "Open `phase-07-alt-pivot.md` (TODO; not pre-written here — wait until needed)."
- The 3000ms threshold (`phase-06:109`) is asserted without a citation. Researcher's report on Atlas cold-start gives ~1500ms baseline (`phase-01:50`). 3s is "2× baseline" but **not motivated by a UX research finding** (e.g., "Telegram users abandon at >2.5s").

**Why this matters.** The user's stated motivation is "validate cold-start UX firsthand with safe bail-out". If the bail-out isn't planned, it isn't safe. When the gate trips at 3 AM and the operator is staring at a 3.2s P95, "draft a new plan" is not an executable rollback procedure — it's a panic invitation.

The pivot path described in `phase-06:125-130` is high-level (5 bullet points). Real questions unanswered:
- How long will the bot run on KV/D1 with `DUAL_WRITE=0` while a new Upstash plan is built? Days? Weeks?
- Is `MongoKVStore` reusable for `UpstashKVStore` (similar interface) or starting from scratch?
- Does Atlas data get exported before deletion? When?
- Does the operator pay for Upstash dev work mid-incident, or is there a 7-day "we're on KV only" window?

**Threshold motivation.** Research report 3 (free-db-validation-matrix) reportedly recommends Upstash on cold-start grounds. That implies the researchers expect Atlas to fail this gate. Setting the threshold at 3000ms when expected baseline cold is 1500ms gives ~2× headroom — generous, but means the "validate firsthand" exercise will likely PASS, even if UX is degraded for the slow tail. Should be 2× P95 baseline cold-start measured in phase-01 ping (line 88 todo: "ping latency recorded as baseline number") — i.e., DERIVED from measurement, not asserted at 3000ms before measurement.

**Recommendation: CHANGE.**
- (a) Pre-write `phase-07-alt-pivot.md` as a 1-page skeleton: "leave Atlas writes off, KV is authoritative, draft new Upstash plan within 5 days." Make it real, even if minimal. Otherwise rollback is just hope.
- (b) Replace the 3000ms threshold with a measurement-derived one: `2.5 × P95(cold-start ping from Phase 01)`. If Phase 01 measures 1400ms cold ping, gate is 3500ms. If 1800ms, gate is 4500ms. Honest measurement, not arbitrary number.
- (c) Add a "what does the user feel" research item: 1500ms vs 3000ms vs 5000ms — at what point does a Telegram user retype `/wordle`? Without this, the gate is engineering theater.

---

### 5. Re-introducing admin routes violates project architecture

**Smell.** Phase-05 adds `/__admin/dump-kv` and `/__admin/dump-d1-trades` to the Worker. The architecture doc explicitly rejects this.

**Evidence.**
- `docs/architecture.md:14` (design goals) — "**No admin HTTP surface.** One less attack surface, one less secret. Webhook + menu registration happen out-of-band via a post-deploy node script."
- `docs/architecture.md:358-365` (§ 10 "Why the register step is not in the Worker") — explicitly rejected reasons:
  - "Adds a third secret to manage and rotate."
  - "Adds an attack surface (even a gated one)."
  - "Running locally + idempotently means the exact same script works whether invoked by a human, CI, or a git hook."
- `phase-05-backfill-scripts.md:53-56` — adds `ADMIN_TOKEN` (a third secret), `/__admin/dump-kv` route, and constant-time-compared auth middleware. **Every objection from § 10 applies verbatim.**
- The plan handwaves: routes are "temporary, removed in Phase 07" (`phase-05:99`). But "temporary" admin routes have a way of lingering — and the security review burden lands now, not later.

**Alternative: local-only dump scripts.**
- KV dump: `wrangler kv key list --namespace-id=<id> --remote` paginated, then `wrangler kv key get` per key — slow but works without code changes. OR use the CF KV REST API via `curl` from a local script with an account-token (no Worker change).
- D1 dump: `npx wrangler d1 export miti99bot-db --output=trades.sql --remote`. **Already in phase-07 step 12 as a final-stage backup** — same command works for the migration backfill.
- Mongo write: from local node, no admin route needed.

The "wrangler kv key get per-key is too slow" argument (`phase-05:31`) is real but solvable: CF KV REST API supports bulk read via `keys/bulk` endpoint, or chunked workers from local node parallelize the per-key gets. Either is faster than building, securing, deploying, and later removing a Worker route.

**Recommendation: DELETE.** No `/__admin/*` routes. Use CF KV REST API + `wrangler d1 export` from a local node script. Drops `ADMIN_TOKEN` (a third secret), drops `src/admin/dump-routes.js`, drops the security-considerations debt in phase-05 (lines 165-170), drops the deletion step in phase-07 (lines 13, 128). Net: -1.5h + zero attack surface added.

**Side note:** the planner KNEW this was sketchy — `phase-05:99` flags routes as "temporary" and `phase-07:13` makes deleting them a pre-cutover step. That's the smell. If something must be deleted before cutover, ask why it had to be added.

---

### 6. MongoSqlStore is a SQL-pattern dispatcher emulating SQL on a doc store

**Smell.** Phase-03 builds a regex-driven dispatcher that pattern-matches 6 SQL statements and translates each to a Mongo call. Worst kind of leaky abstraction: pretends to be SQL, isn't, fails open silently if a 7th statement appears.

**Evidence.**
- `phase-03-mongo-sql-store.md:46` — "Statement matching: trim + collapse whitespace + uppercase first 30 chars; switch on prefix".
- `phase-03-mongo-sql-store.md:147` — risk: "7th SQL statement appears (silent breakage)" — likelihood "M", impact "H". The plan's own risk table flags this as MEDIUM-likelihood.
- `phase-03-mongo-sql-store.md:153` — "`OFFSET` with `LIMIT -1` (sqlite-ism) is meaningless in Mongo. Handler ignores LIMIT, applies `.skip(N)` only." Behavior is QUIETLY different from D1 — the wrapper isn't actually SQL-compatible, just SQL-shaped.
- Trading callers (`history.js`, `retention.js`) are listed as "READ FOR CONTEXT" only (`phase-03:104-107`). They're NOT modified. The wrapper exists to AVOID modifying them. Why? They're 2 small files.

**Simpler alternative.** Refactor `trading/history.js` + `trading/retention.js` to call a `MongoTradesStore` directly:
```js
// MongoTradesStore — thin, purpose-built
class MongoTradesStore {
  insert(trade) { /* db.trading_trades.insertOne */ }
  byUser(userId, limit) { /* find().sort().limit() */ }
  distinctUsers() { /* distinct */ }
  oldRowsForUser(userId, keepN) { /* find().skip(keepN) projection */ }
  oldRows(keepN) { /* find().skip(keepN) projection */ }
  deleteByIds(ids) { /* deleteMany({_id:{$in}}) */ }
}
```
Six methods, ~80 LOC. Trading module's two files lose their SQL string literals — gain explicit, typed method calls. Reads better. No regex spaghetti. No "7th statement" silent-breakage risk because you can't accidentally route an unknown query.

**Cost.** Modifying `trading/history.js` and `trading/retention.js`: ~30 LOC of changes total (the SQL strings already isolate the access patterns). That's smaller than the dispatcher + handlers + tests.

**Counter-argument:** "But then the abstraction leaks into the module — the module knows it's talking to Mongo." Yes. That's HONEST. Today the abstraction leaks the other way — the module knows it's talking to SQL even though it might be Mongo. Either way the module knows. Better to know the truth than the lie.

**Recommendation: DELETE phase-03.** Replace with: "Add `src/db/mongo-trades-store.js` (~80 LOC) + refactor `trading/history.js` and `trading/retention.js` to use it directly. Delete `SqlStore` interface in phase-07 alongside D1." Saves ~2h vs phase-03 implementation, deletes the dispatcher risk, gives trading module a cleaner persistence boundary.

**Combined with Finding 2:** if you defer trading migration entirely (recommended), phase-03 deletes outright. ~3h saved.

---

### 7. `last_row_id` returned as ObjectId hex is dead-code-by-design

**Smell.** Plan acknowledges trading doesn't use `last_row_id`, then specifies returning ObjectId hex anyway "for parity".

**Evidence.**
- `phase-03-mongo-sql-store.md:25` — "D1 returns `{ changes, last_row_id }` — last_row_id matters for trading? Grep confirms it's not consumed (insert path discards return). Confirm in step 1."
- `phase-03-mongo-sql-store.md:34-35` — "`run` → `{ changes, last_row_id }`. `last_row_id` returns ObjectId hex (since callers don't use it numerically)."
- `phase-03-mongo-sql-store.md:153` — "`last_row_id` quietly needed by future caller... if caller does math on it, fails loudly". Defensive hand-wave.

The whole field exists because the SqlStore *interface* defines it (see `cf-sql-store.js:43`). The wrapper's purpose is interface parity, so it returns the field. But the value is meaningless (a hex string in a contract that historically was an integer). It's dead code dressed up as parity.

**Recommendation: KEEP if phase-03 stays as-is** (interface parity has weight) — but if the trading-only refactor recommendation (Finding 6) lands, delete `last_row_id` entirely and remove the field from `SqlStore` interface. Two smells solved at once.

---

### 8. Soak duration "24-72h" is a vibe, not a criterion

**Smell.** No exit condition for extending 24h → 72h.

**Evidence.**
- `plan.md:52` — "Phase 06: cold-start P95... > 3000ms over 24h soak window." 24h is the gate.
- `phase-06-staged-deploy-and-soak.md:25` — "Soak window: minimum 24h, ideally 72h to span weekly traffic peaks."
- `phase-06:109-114` (Success Criteria) only references "24h" and "72h" without defining what data 72h gives that 24h didn't.
- Bot has cron-driven traffic (`docs/architecture.md:43` — daily lolschedule, etc.). 24h covers one full daily cycle including all crons. 72h gives 3× sample size but no new patterns unless weekly traffic varies dramatically.

**Recommendation: CHANGE.** Pick one duration with a stated reason:
- 24h with rationale: covers daily cron cycle, sufficient for steady-state.
- 72h with rationale: covers a weekend-vs-weekday traffic differential of >1.5×.
- Or 7 days if weekly cycle matters.

Or define a stop condition: "extend to 72h if any of {error rate >0.5%, traffic <50 req/24h, cold-start P95 between 2500–3000ms (borderline)}". Without this, "24-72h" is operator-discretion masquerading as a plan.

---

### 9. "Cron heartbeat to prevent M0 auto-pause" is worry-driven

**Smell.** Listed in plan-level unresolved questions; plan-08-tests-and-docs.md:161.

**Evidence.**
- `phase-01-atlas-setup.md:18` — "M0 auto-pauses after **30 days of zero ops**."
- The bot has 6+ daily cron triggers (`wrangler.toml` has `[triggers] crons`).
- Trading cron + lolschedule cron + retention crons all hit the DB daily.
- 30 days of zero ops would require the bot to be entirely silent for 30 days — implies all crons fail or are removed.

**Recommendation: DELETE the unresolved Q.** Not a real concern. If ALL crons stop firing for 30 days, you have a bigger problem than auto-pause (the bot is dead). Atlas auto-pause is a non-issue for an active bot. The plan-07 risk table line `phase-07:173` already proposes a "no-op cron heartbeat" — also unnecessary, can be removed.

If paranoia wins: a single line in the existing `misc` cron handler that does `db.runCommand({ping:1})` covers it. Not worth a separate unresolved question.

---

### 10. Tests-and-docs in phase 08 is debt, not closure

**Smell.** Phase 08 (3h, post-cutover) catches doc updates AND a meaningful new e2e test. Ordering is wrong.

**Evidence.**
- `phase-08-tests-and-docs.md:24-26` — e2e test: "Boot fake env with `MongoKVStore` + `MongoSqlStore` against `fake-mongo.js`. Run a representative request..." This validates the FULL stack. Should land BEFORE the irreversible cutover (phase-07 step 18), not after.
- Phases 02–04 each include their own unit tests. Good.
- BUT phase-04 dual-store testing (`phase-04-dual-write-wrappers.md:127-131`) only verifies factory behavior with fake env — does not verify a request actually round-trips through both stores.
- `phase-08:104` proposes a `scripts/check-secret-leaks.js` lint rule. Should have been added in phase-01 alongside the URI introduction, before any commit could leak. Now it's added after 7 phases worth of commits could already contain leaks.

**Recommendation: CHANGE.**
- (a) Move the e2e test from phase-08 to phase-04 (or a new phase-04.5). Run it before phase-05 backfill. Run it before phase-06 deploy. Run it before phase-07 cutover. It's a regression net.
- (b) Move the secret-leak lint to phase-01 step 11 (immediately after `MONGODB_URI` is introduced as a secret). 5-min addition.
- (c) Phase 08 stays as docs-only update + final mark-complete. Trims it to ~1h.

Sequencing rule of thumb: tests for a phase land WITH the phase, not in a stash-everything-into-phase-08 graveyard.

---

## Cross-cutting observations

**Plan budget is misallocated.** 24h budget → 8h on dual-write/backfill/admin routes (Findings 1, 5) + 3h on trading SQL emulation (Findings 2, 6) = 11h on machinery the user might not need. Closer to 13h if Phase 06 telemetry overhead and Phase 08 catch-up are included.

**Plan is over-confident on cold-start.** Phase-06 bakes in the ABORT path (good) but the gate threshold is asserted, not derived; the pivot is a TODO; the user has been told this is "safe to bail out of" but the bail-out is unwritten.

**Plan respects existing project posture EXCEPT in Finding 5.** The architecture doc is clear and this plan re-introduces the exact pattern the doc rejects. Either:
- The plan was drafted without consulting `docs/architecture.md` § 10, OR
- The convenience of admin routes felt worth the violation.

Either way: a planner who reintroduces a rejected pattern owes a one-line justification in the phase file. None present.

---

## Prioritized action list (impact order)

1. **DELETE Phase 05's `/__admin/*` routes.** Use CF KV REST API + `wrangler d1 export` from local. Removes a third secret, removes attack surface, complies with `docs/architecture.md` § 10. (Finding 5)

2. **CHANGE Phase 04 to maintenance-window cutover.** Drop dual-write entirely. ~5–6h saved + removes the latency-amplification problem that Phase 06 is gating on. (Finding 1)

3. **CHANGE Phase 03: defer trading migration OR replace with `MongoTradesStore` direct refactor.** Either drops phase-03 entirely (-3h) or simplifies it from SQL-pattern-dispatcher to 6 explicit methods. (Findings 2 + 6 combined)

4. **PRE-WRITE `phase-07-alt-pivot.md` skeleton + DERIVE the cold-start threshold from Phase-01 baseline measurement.** Make the bail-out real. (Finding 4)

5. **MOVE e2e test from Phase 08 to before Phase 06.** Catch storage-roundtrip regressions BEFORE deploying the dual-write code (or with the maintenance-window approach, before cutover). Move secret-leak lint to Phase 01. (Finding 10)

6. **CHANGE Phase 02 to single shared `kv` collection.** YAGNI — per-module collections add ops complexity without M0-scale benefit. (Finding 3)

7. **DEFINE Phase 06 soak duration with stated criterion.** Pick 24h with reason, OR pick 72h with reason. Stop saying "24-72h". (Finding 8)

8. **DELETE the "M0 auto-pause cron heartbeat" unresolved question.** Bot has 6+ daily crons. Non-issue. (Finding 9)

If recommendations 1+2+3 land: budget drops from 24h → ~12h. Plan is half the size, more reversible, fewer secrets, fewer code files to delete in phase-07, simpler architecture-doc story.

---

## Unresolved questions for the planner

- Is "validate cold-start UX firsthand" a UX research goal (with a defined user-experience threshold) or an engineering curiosity? Answer changes whether the 3000ms gate is right.
- Is the user willing to accept a 10-minute Telegram bot maintenance window? If yes, dual-write is unnecessary.
- Is the user committed to migrating BOTH KV and D1, or is keeping D1 acceptable? Plan assumes both; nobody's challenged it.
- What's the SLO for `/wordle` reply latency that the user actually cares about? The plan uses 3000ms; users might tolerate less or more.

---

**Status:** DONE_WITH_CONCERNS
**Summary:** Plan is structurally sound but over-engineered for dataset size; reintroduces admin-route pattern explicitly rejected by project docs; bail-out path is a TODO. Recommend dropping dual-write, dropping admin routes, deferring trading migration. Cuts plan budget ~50%.
