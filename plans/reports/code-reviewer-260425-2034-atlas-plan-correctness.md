# Atlas Migration Plan — Correctness Review

## TL;DR

Plan is **mostly implementable but has 5 blockers and ~10 high-severity gaps that will burn the implementer**. Worst gap: `MongoSqlStore` SQL-pattern dispatch is internally contradictory (5 handlers for 6 statements, retention.js queries 3 & 4 differ in shape and bind layout in ways the plan handwaves) AND `last_row_id` is required by the existing test contract (`tests/db/create-sql-store.test.js:48-52`) which directly contradicts plan-03's claim it's unused. Cold-start measurement methodology is hand-waved, instrumentation snippet has a `marks` reference-error bug, and the plan's "stubMongo" approach is effectively a dead-letter URI that risks stalling register if any code path tries to construct a MongoClient with it (which the matrix permits if anyone misconfigures the flags).

The plan is "rollback-correct" but not "round-trip-correct" — Stage-2 reverse-backfill is explicitly NOT pre-built, so a Stage-2 abort with new Mongo-only writes loses data on rollback to KV.

---

## Findings

| # | Severity | File:Section | Issue | Recommendation |
|---|----------|--------------|-------|----------------|
| 1 | BLOCKER | phase-03 §Key Insights, §Functional | Plan claims `last_row_id` is unused; `tests/db/create-sql-store.test.js:48-52` explicitly asserts `result.last_row_id` is present. Returning ObjectId hex (string) where test expects a number breaks build. | Either return `last_row_id: 0` for INSERT (numeric, matches D1 "no rowid" path) and don't pretend it's the inserted ID; OR refactor the existing test to accept hex. Document choice in phase-03. |
| 2 | BLOCKER | phase-04 §Stub for register, §Implementation step 6 | `stubMongo = "mongodb://stub-not-used"` is a string, not a duck-typed `MongoClient`. If any code path constructs `new MongoClient(stubMongo)` (e.g. someone forgets to wire the `DUAL_WRITE=0` short-circuit, or `STORAGE_PRIMARY=mongo` env leaks into register), `register.js` hangs ≥10s on `serverSelectionTimeoutMS`. The matrix correctness depends on factory branching that doesn't exist yet. | Make `stubMongo` a duck-typed object exposing `db()`, `connect()` no-ops (pattern matches `stubKv`). Or: have factories check `env.MONGODB_URI === stubMongo` literal sentinel and short-circuit unconditionally. Add a unit test that constructs the factory with stubMongo + every flag combo and asserts no network call. |
| 3 | BLOCKER | phase-03 §Key Insights bullet "Mongo equivalents", §Architecture handlers | Statement 4 (`SELECT id ... WHERE user_id = ? ORDER BY ts DESC LIMIT -1 OFFSET ?`) and statement 5 (`SELECT id ... ORDER BY ts DESC LIMIT -1 OFFSET ?`) are described as one handler "handleRetentionOffset" but plan never specifies how the dispatcher distinguishes per-user vs global. The 3 LIMIT-token resolution rules in `tests/fakes/fake-d1.js:155-170` (negative limit = "all rows") are also missing from plan. Without a spec, implementer cannot write the dispatcher. | Add explicit dispatcher rules: detect `WHERE user_id` substring after normalization; route to per-user vs global. Document that LIMIT -1 is sqlite-ism for "no limit"; Mongo handler ignores it entirely and applies only `.skip(N)`. List the 6 normalized prefixes as constants in the plan. |
| 4 | BLOCKER | phase-07 §Rollback Stage 2 | "Reverse-backfill from Mongo to KV. Build script on demand." During Stage-2 7-day soak, Mongo-only writes accumulate. If Stage-2 abort happens on day 7, KV is missing 7 days of writes. Operator must build a backfill script under outage pressure with no test coverage. | Pre-build `scripts/backfill-mongo-to-kv.js` and `backfill-mongo-to-d1.js` BEFORE Stage 2 begins. Test against fake-mongo + fake-kv. List in phase-07 step 8 prerequisites. |
| 5 | BLOCKER | phase-01 §Risk + phase-02 §Implementation | `mongodb` v6.7+ npm bundle is **~4-5 MB** per the researcher report. CF Workers Free plan limit is **3 MiB compressed** (10 MiB unbottled paid). Plan's mitigation "measure with `wrangler deploy --dry-run`" is reactive, not preventive. If bundle exceeds limit, Phase 02 is blocked AFTER Phase 01 already committed `mongodb` install + wrangler config changes. | Move bundle-size check to phase-01 step 8 as a HARD gate. Budget ≥1MB headroom. Document fallback: switch to `mongodb-driver-core` (no DNS/SRV resolver) or abandon Atlas before any further phase work. |
| 6 | HIGH | phase-02 §Architecture "Decision: keep prefix inside `_id`" + phase-04 §Functional | `create-store.js:65` strips the prefix from results. If MongoKVStore stores `_id="wordle:games:42"` and `list({prefix:"games:"})` is called from inside the wrapper (which prepends `"wordle:"` first → `fullPrefix="wordle:games:"`), MongoKVStore must filter `_id` starting with that AND return KEYS WITH PREFIX so `create-store.js` can strip. Plan inconsistently says "prefix-strip mirrors `create-store.js:65`" (key 22) AND "the store doesn't strip prefixes" (key 76). Ambiguity will yield wrong list results. | Match CFKVStore behavior exactly: MongoKVStore returns `keys` AS-IS (prefixed). The wrapper strips. Make this explicit in phase-02 §Functional and write a regression test using a 2-level prefix (`wordle:games:`) to lock semantics. |
| 7 | HIGH | phase-02 §Functional, §Implementation step 3 | TTL stale-read window: Mongo TTL sweeper runs every ~60s. A key with `expirationTtl=10s` is still readable for up to 60s after expiration. Plan acknowledges this in Risk row 2 but `getJSON` does NOT check `expiresAt` field on read. Consumers (e.g. game state with TTL) will see "expired" data. CF KV does NOT have this behavior. | Add explicit `expiresAt` filter on read in MongoKVStore: `findOne({_id, $or: [{expiresAt: {$exists: false}}, {expiresAt: {$gt: new Date()}}]})`. OR document the divergence loudly in the contract. Test must cover: put with 1s TTL, sleep 2s, getJSON returns null. |
| 8 | HIGH | phase-04 §Functional, §Architecture sequence | Dual-write order is documented as `Promise.all` parallel. But plan says "Throw only on primary failure" and "secondary failures get reconciled in Phase 05 backfill". The reconciliation path is broken: Phase 05 backfill uses `$setOnInsert` (skip-if-exists). If primary write succeeded but secondary failed mid-dual-write, the doc IS missing in Mongo, so backfill would fill it from KV. **But Phase 05 is documented as a one-shot, not a recurring sweeper.** Divergences accumulating after Phase 05 are silent. | Add a recurring "drift verifier" cron (1/hr) that samples N keys cross-store and logs/alerts mismatches. Or: change secondary-failure handling to push the failed key onto a retry queue (KV list) that Phase 05 verifier drains. |
| 9 | HIGH | phase-04 §Architecture flag matrix row 4 | `STORAGE_PRIMARY=mongo` + `DUAL_WRITE=0` post-cutover means writes go ONLY to Mongo. M0 is single-region (aws-ap-southeast-1) with NO BACKUPS (acknowledged in phase-01 key insight 5). For 7-day Stage-2 soak there is no fallback if Mongo loses data. | Add an explicit Stage-2 safeguard: enable mongodump-equivalent (or `npm run backfill:d1` reversed for trading) on day-3 of Stage 2 to checkpoint to local disk. Or: keep `DUAL_WRITE=1` permanently (leave KV writes on for one extra cycle as zero-cost insurance) and only flip after Phase 07 binding deletion is irreversible. |
| 10 | HIGH | phase-06 §Architecture "Telemetry helper" | Code snippet refers to `marks` outside its definition: `console.log(JSON.stringify({event:"cmd_timing", cmd, total, ...extra, marks}))` — `marks` is in the outer closure but never declared in the snippet. Implementer copy-pastes → ReferenceError at runtime. | Fix the snippet: declare `const marks = []` at top; have `mark(label)` push to it. Or remove `marks` from output. Snippet quality matters because Phase 06 is the decision gate. |
| 11 | HIGH | phase-06 §Functional, §Implementation step 3 | Cold-start detection via `Date.now() - ISOLATE_BORN < 200ms` is unreliable. CF isolate boot can be 50ms but the Mongo connect + first read is 1500ms. By the time the FIRST handler logs, age may already be >200ms. Cold requests get bucketed as warm. | Use a different signal: track a `let isFirstRequest = true; if (isFirstRequest) { isFirstRequest = false; logCold(); }` flag at module scope. Combined with `isolate_age_ms` for the histogram, but use the boolean for cold/warm bucketing. |
| 12 | HIGH | phase-08 §Functional doc updates | Plan does not mention removing `npm run db:migrate` from `package.json:13` chain inside `npm run deploy` (line 12). Phase 07 step 19 deletes `scripts/migrate.js` but `npm run deploy` still calls `npm run db:migrate` — deploys break. | Add to phase-07 step 17: edit `package.json` to remove `db:migrate` from the `deploy` chain and remove the `db:migrate` script entry. |
| 13 | HIGH | phase-05 §Functional, §Implementation step 4 | Backfill of D1 → Mongo for trading: D1 uses `INTEGER PRIMARY KEY AUTOINCREMENT`; Mongo plan uses `ObjectId`. Plan says "fields mapped 1:1" but the `id` field is the table's primary key and (per phase-03) is exposed as `_id.toHexString()`. Existing trading rows lose their original integer IDs entirely. Any external consumer (logs, exports) referencing trade IDs is broken. Retention's DELETE-by-id pass works (uses string-or-int either way) but historical trade IDs in logs become un-joinable. | Either (a) preserve original integer ID in a `legacy_id` field during backfill so support queries still work; or (b) document that pre-cutover trade IDs are abandoned and add a migration note to changelog. Currently, plan does neither. |
| 14 | HIGH | phase-01 §Functional, §Implementation step 7 | `compatibility_flags = ["nodejs_compat_v2"]`: per researcher report this is correct. BUT `nodejs_compat` (v1) and `nodejs_compat_v2` are NOT additive — picking v2 changes process/buffer/streams globals. Existing modules using `crypto.timingSafeEqual` (Phase 05 step 2) or other Node APIs may behave differently. Plan's mitigation "run full test suite after edit" only covers unit tests; suite uses fakes, not workerd. | Add: deploy a `wrangler dev` smoke that exercises every module's first command. Add a section in `docs/using-mongodb.md` listing every `node:` import the codebase uses and which surface they need. Otherwise a silent prod regression is plausible. |
| 15 | HIGH | phase-05 §Implementation step 2, §Security | `/__admin/dump-kv` returns raw values including TTL records. Plan correctly uses `crypto.timingSafeEqual` but does NOT specify what happens on token mismatch — bare `return 401` leaks timing via early-return before timing-safe compare runs. Also: the plan does not say where the route mount lives in `src/index.js` request flow vs the existing `/webhook` handler (route-ordering matters; admin routes must NOT be reached without auth). | Specify: (a) compare token first, before any branching by header presence; (b) return 401 with no body; (c) place admin routes BEFORE `/webhook` in the dispatcher with explicit order documented. (d) Set `X-Robots-Tag: noindex` to prevent caching/indexing. (e) Rate-limit (CF native or cheap counter in KV). |
| 16 | MEDIUM | phase-02 §Architecture connection memoization | `client = new MongoClient(...)` then `connectPromise = client.connect()` — if `connect()` REJECTS, both `client` and `connectPromise` stay populated. Next call: `if (client) return client.db(...)` — returns a Db on a dead client. All subsequent reads hang or fail with cryptic "no connection" errors. | On reject, null both. Pattern: `connectPromise = client.connect().catch(err => { client = null; connectPromise = null; throw err; })`. |
| 17 | MEDIUM | phase-02 §Implementation step 1 fake-mongo surface | Listed methods omit `find().sort().skip()` chaining (used by retention pattern from phase-03). Phase-08 §Unresolved Q1 says TTL semantics deferred. Surface inventory is incomplete. | Re-derive the surface from phase-03 handlers: insertOne, find/sort/skip/limit/project/toArray, distinct, deleteMany, countDocuments, createIndex. Audit phase-03 handlers against fake-mongo capabilities before phase-02 starts (ordering: phase-03 design completes the surface, then fake-mongo). |
| 18 | MEDIUM | phase-04 §Architecture flag matrix | Rollback case: "primary=mongo, dual=on" → revert to "primary=kv, dual=on". During the time between cutover (mongo-primary) and rollback decision, Mongo accepted writes that KV may have missed (if dual-write secondary failed silently). When primary flips back to KV, those writes are invisible. The flag matrix doesn't address this; phase-07 only handles the "Stage 2 with KV stale" version. | Document explicitly in phase-04: rollback to KV-primary AFTER any Mongo-primary period requires reverse-backfill of any new Mongo doc. Cross-link to finding #4. |
| 19 | MEDIUM | phase-01 §Functional + phase-08 §Lint check | `MONGODB_URI` rotation cadence: phase-08 unresolved Q5 leaves "quarterly proposed". Phase-01 says "rotate quarterly" without owner. Telegram secrets pattern says nothing about cadence either — there is no project-level secret rotation runbook. | Phase-08 Step in `using-mongodb.md`: add a runbook section "Rotation: every 90 days, owner = repo maintainer; calendar entry to be created in `docs/cost-tracking.md` review cycle". Or accept "rotate-on-suspicion-only" and document. |
| 20 | MEDIUM | phase-04 §Implementation step 7 | `wrangler.toml` already has `[vars] MODULES = "..."`. Adding `STORAGE_PRIMARY` and `DUAL_WRITE` to same `[vars]` block is correct. But phase-07 step 17 deletes them — and the phase-04 dual-store factory hard-reads them. After phase-07 deletion, factories must default sanely (no flags = mongo-only). Plan does not specify the post-deletion factory behavior beyond "simplify to Mongo-only (no flags)". | Pre-design the post-cutover `create-store.js` shape in phase-04 (so the simplification step is mechanical in phase-07). Specifically: factories default to `MongoKVStore`-only when flags absent; KV/D1 branches removed. |
| 21 | MEDIUM | phase-05 §Functional, sample size | Verifier samples 100 keys per module. With ≥1000 keys/module possible (loldle-emoji game state), 100/1000 = 10% sample. At 0.1% real corruption rate, expected mismatches = 0.1 — verifier reports PASS even with 1 in 1000 keys silently corrupted. | Increase sample to N=√(total) capped at 500. Or: full-scan compare on collections <10K docs (cheap on M0 with small data). |
| 22 | MEDIUM | phase-06 §Connection saturation criterion | "M0 connection peak ≤ 400 of 500". Plan says observed via Atlas dashboard but does not specify alerting or capture. Operator must check manually each day during 24-72h soak — easy to miss. | Add: enable Atlas free-tier alert email at "current connections > 400". Or: programmatic poll every 5min via Atlas Admin API (free tier supports). Pin in `using-mongodb.md`. |
| 23 | MEDIUM | phase-02 §Architecture connection options | `maxPoolSize: 1, minPoolSize: 0`. Single connection per isolate is correct for free tier, but the driver default `maxPoolSize=100` means in dev / locally, ops parallel to 100. `serverSelectionTimeoutMS: 5000` means a paused M0 cluster gives a 5s hang per cold start (acknowledged in Phase 06 abort but not as a normal-ops cost). | Document the latency budget: cold = 1500ms (TLS+SCRAM) + ≤5000ms (server selection) = up to 6.5s p99 absolute worst case. Make sure the abort gate at phase-06 (>3000ms = abort) accounts for this. The 3000ms threshold may be unreachable even in the green case. |
| 24 | MEDIUM | phase-03 §Implementation step 1 | "Grep `src/modules/trading/` for every `.run(`, `.all(`, `.first(`, `.prepare(`, `.batch(` call. Confirm only 6 SQL strings exist." Verified in this review: 6 found, 0 prepare/batch. But: the grep is on TODAY's code. If trading evolves before Phase 03 ships (developer adds a 7th query during dual-write deployment window), dispatcher silently fails to match → `Error("MongoSqlStore: pattern not matched")` thrown to user mid-handler. | Add to phase-04 dispatcher: log unmatched queries to telemetry, fall back to D1 read during dual-write (since D1 is still authoritative until Stage 2 starts). After Stage 2, unmatched queries are a hard failure that surfaces in Phase 06 monitor. |
| 25 | MEDIUM | phase-08 §Unresolved Q4 | "Cron heartbeat to prevent M0 auto-pause" — `wrangler.toml:43` has crons `["0 17 * * *", "0 1 * * *"]`. Misc module's `last_ping` write is on every ad-hoc command, not on cron. If bot has zero command activity for 30 days, M0 auto-pauses. The user has stated this is unlikely but plan should be deterministic, not probabilistic. | Add a cron schedule that explicitly writes `misc:last_ping` (or any Mongo doc) every 7 days. Verify it executes against Mongo (post-cutover, all writes go to Mongo, so the existing daily cron via `misc` module is sufficient — ONLY if any of those crons writes Mongo, which the misc module crons need to be confirmed to do). |
| 26 | LOW | phase-01 §Architecture cold path | "Cold path: ~1500ms (TLS + SCRAM + server selection)." Server-side region is `aws-ap-southeast-1`; CF PoP for VN traffic is also Singapore. Latency floor is ~10ms, so 1500ms is dominated by TLS+SCRAM. Reasonable estimate. No action; calling out for transparency. | None. |
| 27 | LOW | phase-04 §Test plan integration test | "Verify via `instanceof` check after exposing `_implementations` array on dual stores for testability." This is a code-smell — exposing internals for tests. | Use a sentinel string like `store._kind === "dual"` (one line, not arrays). Or test behavior end-to-end (write → read from one of two seams). |
| 28 | LOW | phase-08 §Functional doc list | `docs/development-roadmap.md` mentioned twice in plan (phase-08 §Functional + Todo). User feedback memory says "roadmap = future only" — completed migration must NOT be added; it should be REMOVED if currently listed. Phase-08 says "remove migration item per future-only convention" — correct. | None — already correct. Just flagging consistency. |
| 29 | LOW | phase-02 §Architecture document shape | `value` stored as string. CFKVStore stores strings via `kv.put(key, value)` where value is already serialized. Plan correctly mirrors this. But Mongo allows native typed values — slight space waste. Performance: `JSON.parse(doc.value)` per read. Acceptable; matches contract. | None — matches CFKVStore contract. |
| 30 | LOW | phase-07 §Implementation step 18 destructive ops | `npx wrangler kv namespace delete --namespace-id <id>` and `npx wrangler d1 delete miti99bot-db` are interactive on wrangler 4.x (require typed confirmation). Plan flags this in Risk row 4 ("Operator runs the destructive commands manually") — correct. | None — already addressed. |

---

## Per-section deep dives — BLOCKER + HIGH only

### Finding #1: `last_row_id` is consumed by an existing test

**File:** `tests/db/create-sql-store.test.js:48-52`
```js
it("returns changes and last_row_id on INSERT", async () => {
  ...
  expect(result).toHaveProperty("last_row_id");
});
```

Plan-03 §Key Insights says "D1 returns `{ changes, last_row_id }` — last_row_id matters for trading? Grep confirms it's not consumed (insert path discards return). Confirm in step 1." This is technically true for `src/modules/trading/`, but the SqlStore CONTRACT (`sql-store-interface.js:19`) declares `last_row_id` as `number`, and the test enforces shape. Returning a hex string violates the existing contract. Decisions:
- (a) Keep contract as `number`. New `MongoSqlStore` returns `last_row_id: 0` for inserts (matches "no rowid" semantics). Existing test passes.
- (b) Loosen contract to `number | string`. Update test. Risky for any future caller doing arithmetic.

**Recommendation:** Pick (a). Document in phase-03 that `last_row_id` is non-meaningful in MongoSqlStore; if future caller needs the inserted ID, they must read `_id` from a separate read.

### Finding #2: `stubMongo` is a string, not a duck-typed binding

**File:** `phase-04-dual-write-wrappers.md:88-93`

Plan says: `export const stubMongo = "mongodb://stub-not-used";`

But `stubKv` is a duck-typed object with `.get`, `.put`, `.list` methods. The plan's matrix relies on every register-time path having `DUAL_WRITE=0` so `MongoClient` is never constructed with `stubMongo`. This is a tight coupling: any future change to factory branching (or accidental flag mis-set) will result in `new MongoClient("mongodb://stub-not-used")` which:
- Triggers DNS resolution attempt
- Hangs on `serverSelectionTimeoutMS=5000ms`
- Fails register, blocking deploy

Plan gives no test that asserts "stubMongo never reaches MongoClient". Build break risk is real.

**Recommendation:** Convert to duck-typed object stub:
```js
export const stubMongo = {
  db() { return stubMongoDb; },
  connect: async () => undefined,
  close: async () => undefined,
};
```
Then `mongo-client.js getDb(env)` checks if `env.MONGODB_URI === STUB_SENTINEL` (a defined constant) and short-circuits. Add unit test in phase-04 that constructs the factory with stubMongo + each flag combination and uses `vi.spyOn(MongoClient.prototype, 'connect')` to assert ZERO connect calls.

### Finding #3: Retention dispatcher has 2 statements but plan describes 1 handler

**File:** `phase-03-mongo-sql-store.md:115` ("handles both 4 & 5 — one user-scoped, one global — distinguished by presence of WHERE")

The plan's normalized-prefix matching scheme breaks here:
- Statement 4: `SELECT id FROM trading_trades WHERE user_id = ? ORDER BY ts DESC LIMIT -1 OFFSET ?`  (binds: `[userId, offset]`)
- Statement 5: `SELECT id FROM trading_trades ORDER BY ts DESC LIMIT -1 OFFSET ?`  (binds: `[offset]`)

After normalization (collapse whitespace, trim, uppercase first 30 chars), prefix is `SELECT ID FROM TRADING_TRADES`. Identical. Dispatcher cannot distinguish on first 30 chars. Plan's "presence of WHERE user_id" is the actual signal — but plan doesn't say to inspect beyond 30 chars.

Bind layout differs:
- Stmt 4: `binds[0]=userId, binds[1]=offset`
- Stmt 5: `binds[0]=offset`

Handler logic must inspect WHERE presence to know how to read binds.

**Recommendation:** Phase-03 dispatcher must do:
1. Normalize full query (not just first 30 chars).
2. Match via regex: `WHERE\s+user_id\s*=\s*\?` → user-scoped, binds[0]=userId, binds[1]=offset.
3. Else if matches global pattern → binds[0]=offset.
4. Document the regex set as constants alongside the 6 statements. List explicitly:

```js
const SQL_PATTERNS = {
  INSERT_TRADE: /^INSERT INTO TRADING_TRADES \(USER_ID, SYMBOL, SIDE, QTY, PRICE_VND, TS\) VALUES/,
  HISTORY_QUERY: /^SELECT ID, USER_ID, SYMBOL, SIDE, QTY, PRICE_VND, TS FROM TRADING_TRADES WHERE USER_ID = \? ORDER BY TS DESC LIMIT \?$/,
  DISTINCT_USERS: /^SELECT DISTINCT USER_ID FROM TRADING_TRADES$/,
  RETENTION_USER_OFFSET: /^SELECT ID FROM TRADING_TRADES WHERE USER_ID = \? ORDER BY TS DESC LIMIT -1 OFFSET \?$/,
  RETENTION_GLOBAL_OFFSET: /^SELECT ID FROM TRADING_TRADES ORDER BY TS DESC LIMIT -1 OFFSET \?$/,
  DELETE_BY_IDS: /^DELETE FROM TRADING_TRADES WHERE ID IN \(/,
};
```

Add a test that mismatches one statement (e.g. extra space after `LIMIT`) and asserts the dispatcher errors loudly with "MongoSqlStore: pattern not matched" — before falling back to D1 (during dual-write) or failing (post-cutover).

### Finding #4: Stage-2 reverse-backfill is undocumented work

**File:** `phase-07-cutover-and-decommission.md` Stage 2 step 11 + Rollback table

Stage 2 deploys with `DUAL_WRITE=0`, so for 7 days, KV becomes stale. Rollback path: "Re-enable `DUAL_WRITE=1` + flip `STORAGE_PRIMARY=kv`. KV is stale by N days but recoverable via reverse-backfill (script not pre-written; build only if rollback needed)."

Building a reverse-backfill UNDER outage pressure is a known anti-pattern: untested code, no time to verify, possible data loss. The script can be written as straightforward inverse of `backfill-mongo.js`:
- For each Mongo collection, list all docs with `_id.startsWith("modulePrefix:")`
- For each, write to KV via `/__admin/inject-kv` route (auth-gated)
- Plus: tracking which keys were updated AFTER the cutover decision so the operator knows the pre-Stage-2 state vs post-Stage-2 deltas

**Recommendation:** Build `scripts/backfill-mongo-to-kv.js` and `scripts/backfill-mongo-to-d1.js` as part of phase-07 step 11 prerequisites (BEFORE Stage 2 deploys). Test against fakes with full coverage. List in plan as a non-optional artifact.

### Finding #5: mongodb driver bundle size vs CF Workers limit

**File:** `phase-01-atlas-setup.md` Risk row 5

CF Workers Free plan limit: **3 MiB compressed** worker bundle (10 MiB uncompressed for paid). The `mongodb` v6.7+ npm package includes:
- Core driver (~600KB minified)
- BSON (~300KB)
- SRV/SCRAM auth (~150KB)
- TLS/socket compatibility shims for nodejs_compat_v2 (~variable)

Researcher report says "~4-5 MB compressed" — over the free-plan limit.

If after `wrangler deploy --dry-run` the bundle exceeds 3 MiB, phase-01 has already committed:
- `wrangler.toml` change (compatibility flag)
- `package.json` change (mongodb dep)
- secrets created
- atlas cluster running

…and phase-02 cannot deploy. Operator must rollback all of phase-01.

**Recommendation:** Phase-01 step 8 (after `npm install mongodb`) MUST check: `npx wrangler deploy --dry-run --outdir=./.tmp-deploy && du -sh ./.tmp-deploy`. Hard gate at 2.7 MiB (10% headroom). If exceeds, abort phase entirely; pivot to Upstash plan (smaller HTTP-only client). Document in phase-01 §Success Criteria.

### Finding #6: list() prefix-strip ambiguity

**File:** `phase-02-mongo-kv-store.md` line 22 + line 76 contradict

Currently `create-store.js:65` does the strip:
```js
keys: result.keys.map((k) => (k.startsWith(prefix) ? k.slice(prefix.length) : k)),
```

CFKVStore.list() (cf-kv-store.js:71-72) returns keys WITH PREFIX. The wrapper strips. Plan-02 says "list() returns ... module namespace already stripped" (from interface JSDoc) but ALSO "keep prefix inside `_id`... the store doesn't strip prefixes."

The interface contract (`kv-store-interface.js:27`) literally says "module namespace already stripped" — but that's only true at the wrapper layer (createStore), not at the underlying CFKVStore. CFKVStore returns prefixed keys.

**Recommendation:** MongoKVStore returns prefixed keys (matches CFKVStore). The wrapper strips. Tests for MongoKVStore directly (without wrapper) assert prefixed keys returned. Tests for `createStore("wordle", env)` assert stripped keys. Make this explicit: phase-02 §Functional updated to "list() returns keys with module namespace **preserved**; the namespace wrapper in `create-store.js:65` strips the prefix."

### Finding #7: TTL stale-read window

**File:** `phase-02-mongo-kv-store.md` Risk row 2 + missing read-time check

The Mongo TTL background task runs every ~60 seconds. Document with `expirationTtl: 10` is still readable for up to 60s after expiration. CFKVStore + CF KV does NOT have this stale-read window — CF KV is consistent on read.

For game state (`expirationTtl: 7 days`), 60s of slack is invisible.
For short-lived caches (e.g. price feeds with `expirationTtl: 60s`), users could see "expired" cache entries.

**Recommendation:** MongoKVStore.get and getJSON filter at read time:
```js
async get(key) {
  const doc = await coll.findOne({
    _id: key,
    $or: [
      { expiresAt: { $exists: false } },
      { expiresAt: { $gt: new Date() } }
    ]
  });
  return doc?.value ?? null;
}
```
Cost: minor query complexity; covered by `_id` index. Add explicit test: put with 1s TTL, sleep 2s, get returns null even before TTL sweeper fires.

### Finding #8: Dual-write divergence detection is one-shot

**File:** `phase-04-dual-write-wrappers.md` §Functional + phase-05 verifier is a manual run

Plan: secondary write fails → log + continue. Reconciliation = phase-05 backfill (skip-if-exists).

Issue: Phase-05 runs ONCE (or daily during soak). Between phase-05 runs, divergences accumulate. Backfill `$setOnInsert` only fills missing docs — it cannot catch a CF KV write that succeeded with newer state where a previous secondary write to Mongo failed (Mongo has stale OR no doc).

**Recommendation:** Add a recurring drift-verifier cron: every hour, sample N keys per module, compare hashes, log mismatches. OR: change failed-secondary-write handling to push the failed key + value onto a retry queue (small KV list at `__retry:mongo-failed`). A separate worker or cron drains the queue with retry. This decouples user-facing latency from secondary durability.

### Finding #9: Stage 2 single-region, no backups

**File:** `phase-07-cutover-and-decommission.md` Stage 2 + phase-01 Key Insight 5

Stage 2 keeps `DUAL_WRITE=0` for 7 days. M0 has NO backups + single-region. Atlas free-tier has ~99% SLA, not 99.9%. A 7-day window with M0 outage = data loss.

**Recommendation:** Phase-07 Stage 2 step 9.5 (insert): "Run `wrangler d1 export miti99bot-db` to local file at start of Stage 2 (snapshot 1) and end of Stage 2 (snapshot 2). Document in cutover-log." Plus: leave `DUAL_WRITE=1` for the first 24h of Stage 2 (overlap window), then flip on day 1 → soak 6 more days. Risk row in phase-07 explicitly addresses single-region M0.

### Finding #10: Telemetry helper has reference error

**File:** `phase-06-staged-deploy-and-soak.md:38-49`

```js
export function startTiming(env, cmd) {
  const t0 = Date.now();
  return {
    mark(label) { /* push {label, dt} into local array */ },
    end(extra = {}) {
      const total = Date.now() - t0;
      console.log(JSON.stringify({ event: "cmd_timing", cmd, total, ...extra, marks }));
    }
  };
}
```

`marks` is referenced but never declared. `mark(label)` is a no-op stub.

**Recommendation:** Fix snippet — `const marks = []` declared at top, `mark(label) { marks.push({label, dt: Date.now() - t0}); }`. This is a copy-paste-into-prod risk.

### Finding #11: Cold-start detection threshold

**File:** `phase-06-staged-deploy-and-soak.md:53` "Cold == age < 200ms"

Cold isolate boot = ~50ms.
First Mongo connect = ~1500ms.
By the time the FIRST request handler logs `isolate_age_ms`, age is already >200ms.

So `isolate_age_ms < 200ms` matches NEVER. All cold requests get bucketed as warm. Phase-06 measurement is broken.

**Recommendation:**
```js
let isFirstRequestInIsolate = true;
// inside handler:
const isCold = isFirstRequestInIsolate;
isFirstRequestInIsolate = false;
console.log({ event: "request", cmd, cold: isCold, total_ms });
```

`isolate_age_ms` is still useful for histogram ("how warm is warm?") but cold/warm bucketing must use the boolean.

### Finding #12: package.json deploy chain references migrate.js after deletion

**File:** `package.json:12` and `phase-07-cutover-and-decommission.md:19`

```json
"deploy": "npm run build && wrangler deploy && npm run db:migrate && npm run register",
"db:migrate": "node scripts/migrate.js",
```

Phase-07 step 19 deletes `scripts/migrate.js`. After deletion, `npm run deploy` fails: `node scripts/migrate.js` → ENOENT.

**Recommendation:** Phase-07 step 17 update: edit `package.json` to remove `&& npm run db:migrate` from `deploy` chain, and remove the `db:migrate` script entry. This is a 2-line edit but easily forgotten.

### Finding #13: Trading ID type drift on backfill

**File:** `phase-05-backfill-scripts.md` step 5 + `phase-03-mongo-sql-store.md:24`

D1 trade rows have `id: 1, 2, 3, ...`. After backfill, Mongo has `_id: ObjectId, id: <hex>`. The original integer 1, 2, 3 are gone. Any historical telemetry, logs, or external dashboards referencing trade IDs are orphaned.

**Recommendation:** During backfill, set both:
```js
{ _id: ObjectId(), legacy_id: row.id, ... }
```
And in `MongoSqlStore.adapt()`, expose `id` as the new ObjectId hex BUT keep `legacy_id` so support queries can still find historical trades. Document in phase-08 changelog: pre-cutover trade IDs preserved as `legacy_id`.

### Finding #14: nodejs_compat_v2 surface change

**File:** `phase-01-atlas-setup.md` Risk row 3

`nodejs_compat` (v1) and `nodejs_compat_v2` are NOT additive — they're alternatives. Switching changes the runtime's process/buffer/streams globals.

Codebase uses (search needed): `crypto.timingSafeEqual` (Phase 05), maybe `Buffer`, maybe `process.env`. The CLAUDE.md says register script uses `--env-file-if-exists` (Node 20.6+), but that's the deploy-time Node, not the worker. Worker doesn't use `process.env` (good).

**Recommendation:** Phase-01 step 7 add: `grep -rn "import.*node:\|process\.env\|Buffer\." src/` and document every Node API touched. Test each on `wrangler dev` before phase-02 begins. Add a section in `using-mongodb.md` listing surface dependencies.

### Finding #15: Admin route timing and ordering

**File:** `phase-05-backfill-scripts.md` step 2 + step 3

`crypto.timingSafeEqual` is called only after the route matches. The route matching itself (string compare path, header presence check) leaks timing. Plan does NOT specify:
- Where in the request flow `/__admin/*` mounts (before or after `/webhook`?)
- What the unauth response body is (could leak existence)
- Rate limiting

**Recommendation:**
- Mount admin router BEFORE `/webhook` to ensure no Telegram secret check is short-circuited.
- 401 response: empty body, generic `WWW-Authenticate: Bearer` header.
- Rate limit via KV counter (5 req/min/IP) — minor, but cheap.
- `crypto.timingSafeEqual` requires fixed-length input — pad short tokens or reject early with constant-time check on length.
- Add `X-Robots-Tag: noindex, nofollow` to prevent search crawlers.

---

## Unresolved questions

1. Bundle-size measurement: has anyone (the user) actually deployed `mongodb` v6.7 to a CF Worker on Free plan and confirmed it fits under 3 MiB compressed? Researcher report says "~4-5 MB compressed". This contradiction must be resolved before phase-01 starts. Recommend: do a smoke deploy of a minimal worker with `import { MongoClient } from "mongodb"` and capture `wrangler deploy --dry-run` size output. If >3 MiB on Free plan, abort the entire plan.

2. Existing `tests/db/create-sql-store.test.js:48-52` requires `last_row_id` to be present and equality-checkable. Decision: phase-03 returns `0` (number) for inserts, OR refactors the test? Plan does not say.

3. Misc module's existing daily cron: does it currently write KV? If yes, post-cutover it writes Mongo and prevents auto-pause. If no, M0 auto-pauses after 30 days idle. Phase-08 §Unresolved Q4 leaves this open. Action item: confirm misc cron handler writes any data on each invocation.

4. `wrangler dev` Mongo support: phase-01 step 9 implies `wrangler dev` works against Atlas via Workers Sockets API. This was added in 2025 and the researcher confirms — but real-world reliability matters. Recommend: a Phase 02 sub-step "test full CRUD against real Atlas from `wrangler dev` for ≥10 min". If it stalls / drops connections, dev experience is broken even if prod works.

5. M0 pause vs cron heartbeat: phase-08 §Unresolved Q4. Action: edit `misc/index.js` (or add a new `keep-alive` cron if misc doesn't write). Effort: 5 min. Should not be unresolved at this stage.

6. Stage-1 vs Stage-2 cutover decision criteria: Phase-07 lists soak duration but not abort criteria for Stage 1. What metric triggers Stage-1 rollback? "Daily verifier PASS" but `verify-mongo-parity.js` runs against a 7-day stable Mongo state where new writes happen — is there a definition of "PASS" that distinguishes "drift due to live traffic" from "drift due to bug"?

---

**Status:** DONE_WITH_CONCERNS
**Summary:** Plan is broadly sound and rollback-safe up to Phase 06, but has 5 blockers (last_row_id contract, stubMongo type, retention dispatcher ambiguity, missing reverse-backfill script, untested bundle size) and 10 high-severity gaps (TTL stale reads, divergence-detection one-shot, telemetry bug, cold-start detection threshold, package.json drift, ID type drift, etc.) that will cause implementation pain or silent prod regressions if not addressed before phase-01 starts. Recommend planner pass before code lands.
