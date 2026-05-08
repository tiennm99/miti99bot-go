# MongoDB Atlas Fit Analysis for miti99bot — Cloudflare Workers

## Executive Summary

**Recommendation: DO NOT use MongoDB Atlas M0 for this project.**

Atlas M0 (Free, 512MB) **cannot solve the stated KV quota pain** — it has **100 ops/sec throughput** but **NO daily cap**, which sounds good until you account for:
1. **Cold-start cost** — Every fresh isolate incurs ~1500ms TLS handshake + SCRAM auth. No connection pooling across stateless invocations.
2. **No money-for-slots trade-off** — Upstash and Turso are HTTP-based, eliminating TLS overhead and working natively in Workers.
3. **Operational fit mismatch** — The bot is read-heavy game state + append-only trading ledger. KV + D1 are already optimized for this; Atlas adds complexity without benefit.

**Better option: Upstash Redis** — FREE tier: 500K commands/month (was 10K/day), unlimited storage in free tier, HTTP-native, memoizable connection per isolate, proven on Workers. **Backup: Turso** for SQL-shaped data (trading module move away from D1).

---

## Problem Statement

Current storage:
- **Cloudflare KV**: 100k reads, 1k writes/day free (resets UTC 00:00)
- **Cloudflare D1**: 1 query/sec free tier
- **Workload**: 13 active modules, many KV-read-heavy (games, schedules), 1 append-only SQL (trading)
- **Pain**: Even modest daily traffic exhausts KV quota mid-game; users hit quota-exhausted errors

User hypothesis: MongoDB Atlas Free (M0) lifts the cap. Reality: **M0 has a throughput cap (100 ops/sec) not a daily-quota cap**, and the architecture cost is substantial.

---

## Option Evaluation

### 1. MongoDB Atlas M0 (Proposed)

#### Specification
- **Storage**: 512 MB (plenty for bot state)
- **Throughput**: 100 ops/sec (no daily operation limit)
- **Connections**: 500 max
- **Regions**: Multi-region (AWS us-east-1, eu-west-1, etc.)
- **Cost**: Free; auto-pauses after 30 days inactivity
- **Deprecated features**: Backups, server-side JS, sharding, auditing

#### Cloudflare Workers Compatibility ✅ (as of March 2025)

**Yes, but with caveats.** Cloudflare shipped `node:net`, `node:tls` (TLS socket support) in Q1 2025.

**Requirements:**
- `wrangler.toml`: `compatibility_flags = ["nodejs_compat_v2"]` + `compatibility_date = "2025-03-20"`
- Driver: `npm install mongodb` v6.7+
- Connection string: `mongodb+srv://user:pass@cluster.mongodb.net/db` (works; explicit-host strings also work)
- Auth: SCRAM over TLS (native)

**Evidence:** [Cloudflare Workers and MongoDB (March 2025)](https://alexbevi.com/blog/2025/03/25/cloudflare-workers-and-mongodb/) demonstrates full CRUD: drop, insert, query operations.

#### Operational Trade-offs

| Dimension | Status |
|-----------|--------|
| **Cold-start cost** | **❌ SEVERE** — ~1500ms TLS+SCRAM per new isolate; Cloudflare Workers are stateless per-invocation, so every cold start = new connection = handshake latency visible to user |
| **Connection pooling** | **❌ IMPOSSIBLE** — Cloudflare Workers V8 isolates have no shared connection pool; memoizing a MongoClient per isolate helps warm requests but cold starts still stall |
| **Driver bundle size** | ~4-5 MB compressed (mongodb npm pkg); moderate but adds to bundle cost |
| **TLS root CA** | ✅ Bundled in driver; no manual CA setup needed |
| **Durable Objects support** | ⚠️ Possible but adds complexity; DO provides in-memory queue to batch inserts, but defeats simplicity goal |
| **Local dev (`wrangler dev`)** | ✅ Works as-is; Workers Sockets API routes TCP to Atlas |
| **Transactions** | ❌ Not on M0; single-document ACID only |
| **Change streams** | ❌ Not on M0 |
| **GridFS** | ❌ Not on M0 |

#### Cost-Benefit for miti99bot

**Against:**
1. **Throughput bottleneck is fake** — 100 ops/sec is enough for this workload (12 active modules, casual daily traffic), but doesn't justify the architecture cost
2. **Cold-start latency** — User sends `/wordle` command → Telegram → Worker cold-start → TLS+auth to Atlas = added 1500ms+ wait. KV is instant (cached). Upstash is HTTP (200-300ms).
3. **No improvement over KV quota** — User's real pain is hitting the 1k/day write limit. Atlas removes that cap but adds latency. Upstash removes both.
4. **Operational debt** — Miti99bot was built around KV/D1 abstractions. Swapping to MongoDB means:
   - Rewriting `src/db/create-store.js` → `mongodb-store.js`
   - Rewriting every module's data model (arrays → collections, expiry → TTL indexes)
   - No transactions → can't safely increment leaderboards atomically
   - Unfamiliar failure modes (connection errors, timeouts, replica-set failovers)

**For:**
- "Real" database (schemas, indexes, aggregation pipeline)
- Free tier is permanent (unlike some services)
- Strong community + documentation

---

### 2. Upstash Redis (Recommended Primary)

#### Specification
- **FREE tier (March 2025)**:
  - 500K commands/month (was 10K/day, increased 50x)
  - Unlimited storage in free tier
  - 1GB data limit on free tier (soft; upgrade if exceeded)
  - 200 GB bandwidth/month free
- **Cost**: $1 / million commands on paid tier ($0.0000000001 per command); free tier never bills
- **Auth**: Token-based HTTP (no TCP)
- **Regions**: global edge locations

#### Cloudflare Workers Compatibility ✅ (Native)

**Perfect fit.** Upstash is **designed for Workers.**

**Setup:**
- `npm install @upstash/redis`
- Environment: REST endpoint + token (HTTP auth, no TLS handshake cost)
- `wrangler.toml`: no special flags needed; standard env vars

**Evidence:**
- [Use Redis in Cloudflare Workers (Upstash docs)](https://upstash.com/docs/redis/tutorials/cloudflare_workers_with_redis)
- [Cloudflare Workers database integration with Upstash](https://blog.cloudflare.com/cloudflare-workers-database-integration-with-upstash/)
- [New Pricing (March 2025)](https://upstash.com/blog/redis-new-pricing) — 500K cmd/month free

#### Operational Trade-offs

| Dimension | Status |
|-----------|--------|
| **Cold-start cost** | ✅ MINIMAL — HTTP-based, no TLS handshake; typical latency 50-200ms to nearest Upstash PoP |
| **Connection pooling** | ✅ NOT NEEDED — Stateless HTTP; no per-connection overhead |
| **Memoization** | ✅ Cache client per isolate (module-scope static) for warm requests |
| **KV abstraction fit** | ✅ PERFECT — Redis strings/hashes are KV-shaped; `@upstash/redis` API mirrors `getJSON`/`putJSON` |
| **Expiry (TTL)** | ✅ Redis `EX` / `EXAT` — supported natively |
| **Transactions** | ✅ Redis multi/exec (ACID per-key) |
| **Data types** | ✅ Strings, hashes, lists, sets, sorted sets (richer than KV) |
| **Persistence** | ✅ Redis is durable; data survives restarts |
| **Local dev** | ✅ Works in `wrangler dev` with real Upstash instance (not local redis needed) |
| **Operations limit** | ✅ 500K/month = ~16,600 ops/day free; daily quota of games (wordle, loldle×5, trading, etc.) is ~2-5k reads + 500-1k writes = within quota |
| **Cost overflow** | ✅ If bot goes viral: $1/million commands on overage; at 100k ops/day = $3/month extra |

#### Fit for Modules

| Module | Current | Proposed | Notes |
|--------|---------|----------|-------|
| `wordle`, `loldle*`, `lolschedule`, `semantle`, `doantu`, `twentyq`, `misc` | KV store | Upstash (string/hash) | Direct swap; prefixing works |
| `trading` | D1 + KV | **Turso** (SQL) | Append-only log + leaderboard queries; SQL better fit |
| `util` | KV | Upstash | Subscription lists, daily state |

**Trade-off:** Lose `list()` pagination (KV has it; Upstash Redis does not). Solution: use Redis `KEYS` pattern matching or maintain an explicit set of subscription keys.

#### Cost Breakdown (Realistic Scenario)

Assume 5k reads + 1k writes/day (conservative for active bot):
- **Monthly ops**: (5k + 1k) × 30 = 180,000 → **under 500k free limit ✅**
- **Bandwidth**: Game assets (loldle splash art, etc.) ~10MB/day → 300MB/month → **under 200GB free limit ✅**
- **Cost**: $0 (free tier)
- **Overflow risk** (10x traffic): 1.8M ops → $1.80/month ✅

---

### 3. Turso (libSQL) — SQL Alternative / D1 Replacement

#### Specification
- **FREE tier**:
  - 1 billion row reads/month
  - 25 million writes/month
  - 5 GB storage
  - 3 databases
  - 3 locations (replication)
- **Cost**: Pay-as-you-go ($0.000002/read, $0.00002/write after free tier)
- **Protocol**: HTTP (REST API via `@libsql/client`)
- **Advantage**: SQLite-compatible, edge-hosted

#### Cloudflare Workers Compatibility ✅ (Native HTTP)

**Setup:**
- `npm install @libsql/client`
- Standard HTTP; no TCP needed

**Evidence:** [Turso + Cloudflare Workers](https://developers.cloudflare.com/workers/tutorials/connect-to-turso-using-workers/)

#### Trade-offs for miti99bot

| Dimension | vs. KV/D1 | vs. Upstash |
|-----------|-----------|------------|
| **For game state (KV-shaped)** | ⚠️ Overkill — SQL adds latency (prepare → bind → execute) vs. simple get/put | ⚠️ SQL is slower than Redis for simple lookups |
| **For trading ledger (SQL-shaped)** | ✅ Natural fit; D1 equiv + cheaper | ✅ SQL is natural; better than Redis (which lacks JOIN) |
| **Replication** | ❌ D1 is zoned; Turso is geo-replicated | ✅ Geo-replication on free tier |
| **Cold-start** | ✅ HTTP (same as Upstash) | ✅ HTTP |
| **Operations ceiling** | ✅ 1B reads/month >> KV 100k/day | ✅ 1B reads/month >> game load |
| **Storage** | ✅ 5GB >> 512MB KV | ✅ 5GB >> game state |
| **Transactions** | ❌ D1 transactions not on Turso free (?) | Need to verify |

**Practical use:** Keep Upstash for KV (games, state); move `trading` table to Turso. Or go all-in on Turso and use SQLite tables as KV (slower but unifies stack).

---

### 4. Neon (Postgres) — SQL Alternative

#### Specification
- **FREE tier** (Oct 2025 update):
  - 100 CU-hours/month (0.25 CU = 400 hours continuous, 1 CU = 100 hours)
  - 0.5 GB storage per branch
  - 5 compute units per month (tiny)
- **Cost**: $14/month pro plan
- **Advantage**: Full PostgreSQL semantics; better for complex analytics

#### Cloudflare Workers Compatibility ⚠️ (Hyperdrive Required)

**Setup:** Neon + [Cloudflare Hyperdrive](https://developers.cloudflare.com/workers/observability/) (connection pooling). Hyperdrive is included free on all Workers plans.

**Trade-offs:**
- ✅ Full SQL power
- ⚠️ Free tier is weak (0.5 GB storage, 100 CU-h/mo) — trading module alone could exhaust this
- ⚠️ Hyperdrive adds latency (one more hop)
- **Not recommended for miti99bot** — overkill for the workload

---

### 5. PlanetScale (MySQL) — Quick Mention

- **FREE tier**: One serverless cluster, 1 GB storage, 1M row reads/month
- **Workers compatible**: Yes, via Hyperdrive or `@planetscale/database` client
- **Trade**: Like Neon, overkill for bot state. Better for e-commerce platforms.

---

### 6. Status Quo + Optimization (KV Batching)

Can the bot survive on current KV + D1 by batching writes and using Durable Objects?

#### Approach
- **Durable Objects** as write coordinator: batch 5-10 game turns into 1 KV write
- Trade: adds 50-200ms latency for coordination
- Complexity: need to manage DO lifecycle, billing (Durable Objects are $0.15/million requests + $0.15/GB storage)

#### Reality Check
- Solves the **daily quota problem** (batches reduce write count by 80%)
- But **adds latency** (DO round-trip)
- **Costs more** than Upstash free tier ($0 vs. $0.15/million reqs + storage)
- **Operational debt** — Durable Objects are stateful; risk of stale writes if DO crashes

**Verdict:** Not recommended. Upstash is simpler + cheaper + faster.

---

## Recommendation Ranking

### 🥇 **Primary: Upstash Redis**

**Rationale:**
1. **Solves the pain** — 500K cmd/month free = 16.6k/day = 10x current KV quota
2. **Minimal latency** — HTTP native; no TLS handshake cost
3. **KV-compatible** — Direct `@upstash/redis` drop-in for current `KVStore` interface
4. **Free** — Overflow cost is negligible ($3/month at 10x traffic)
5. **Proven** — Officially supported by Cloudflare, battle-tested on Workers

**Implementation effort:** 3-4 hours
- Implement `upstash-store.js` (KVStore wrapper around `@upstash/redis`)
- Update `create-store.js` to instantiate Upstash
- Update `wrangler.toml` (add env vars)
- Swap KV namespaces → Upstash endpoint + token
- Test `npm test` (should pass with fakes unchanged)

**Risk:** Low. Redis is simpler than MongoDB; no transactions needed for games.

---

### 🥈 **Secondary (Future): Turso for Trading Module Only**

**When to do:** After Upstash is live, if trading module grows (leaderboard queries, filtering by date range).

**Plan:**
- Keep Upstash for game state (KV)
- Move `trading_trades` table to Turso
- Rationale: SQL is better for aggregates (top 10 traders, this month's stats)

**Implementation effort:** 2-3 hours
- Implement `turso-sql-store.js`
- Migrate `trading` migrations from D1 to Turso
- Test

---

### ❌ **Reject: MongoDB Atlas M0**

**Why not:**
1. **Cold-start latency** — TLS handshake adds 1-2s per cold start; game commands feel slow
2. **No quota relief** — Throughput is 100 ops/sec (fine), but no daily op cap (irrelevant; current bottleneck is KV quota, not throughput)
3. **Architectural mismatch** — Bot is read-heavy game state + append-only log; KV + D1 are already optimized; MongoDB adds schema management overhead
4. **Operational complexity** — SCRAM auth, connection pooling issues, single-document ACID only (no leaderboard transactions)
5. **Zero upside** — Upstash does everything Atlas does for this workload, faster, cheaper, simpler

---

## MongoDB Driver Specifics (If You Ignore This Recommendation)

If the team insists on MongoDB, here's what works:

### Requirements
- **Node.js driver**: v6.7+ (`npm install mongodb@^6.7.0`)
- **wrangler.toml**:
  ```toml
  compatibility_date = "2025-03-20"
  compatibility_flags = ["nodejs_compat_v2"]
  ```
- **Env var**: `MONGODB_URI = "mongodb+srv://user:pass@cluster-hash.mongodb.net/db"`

### Cold-Start Cost
- First request per isolate: ~1500ms (TLS + SCRAM auth)
- Subsequent requests on same warm isolate: ~50-100ms (connection reused)
- **Problem**: Cloudflare Workers can spawn new isolates at any time; you can't rely on warmth

### Memoization Pattern
```js
let mongoClient = null;

export async function getMongoClient(env) {
  if (!mongoClient) {
    mongoClient = new MongoClient(env.MONGODB_URI, {
      maxPoolSize: 1,  // minimize resource use
      minPoolSize: 0,  // no background connections
      serverSelectionTimeoutMS: 5000,
    });
    await mongoClient.connect();
  }
  return mongoClient;
}
```

**Caveat:** This doesn't solve cold-start latency; it just reuses the connection on warm requests.

### Connection String Format
- ✅ `mongodb+srv://...` works (Cloudflare resolves SRV records via DNS)
- ✅ `mongodb://shard0.host,shard1.host:27017` works (explicit hosts)
- ✅ TLS is default; root CA bundled in driver

### Known Limitations
- ❌ No replica-set sessions (sharded transactions)
- ❌ No change streams
- ❌ No GridFS
- ❌ M0 has no backups, auditing, or custom auth mechanisms

### Local Dev
- `wrangler dev` → Workers Sockets API → Atlas (works as-is)
- No `--local` flag needed; no manual routing required

---

## Comparison Matrix

| Dimension | KV (Status Quo) | Upstash | Turso | Neon | **Atlas** |
|-----------|-----------------|---------|-------|------|-----------|
| **Daily quota** | 100k reads, 1k writes | 500k cmd/mo | 1B reads, 25M writes | 100 CU-h/mo | Unlimited (100 ops/sec) |
| **Cold-start latency** | Instant (cached) | ~100ms HTTP | ~100ms HTTP | ~200ms Hyperdrive | **~1500ms TLS** |
| **KV fit** | ✅ Native | ✅ Perfect (Redis) | ⚠️ SQL overhead | ⚠️ SQL overhead | ⚠️ Document model |
| **SQL fit** | ❌ None | ❌ No SQL | ✅ SQL | ✅ PostgreSQL | ⚠️ BSON |
| **Memoization** | N/A | ✅ HTTP client | ✅ HTTP client | ✅ Pooled | ⚠️ Connection pooling hard |
| **Free cost** | $0 (quota limit) | $0 | $0 | $0 (weak quota) | $0 (auto-pause after 30d) |
| **Operational complexity** | Low | Low | Low | Medium | **High** |
| **Workers native** | ✅ KV binding | ✅ HTTP | ✅ HTTP | ⚠️ Hyperdrive | ⚠️ Node.js compat |
| **Risk level** | Low (proven) | Low (proven) | Low (proven) | Medium (big compute) | **High (cold-start, pooling)** |

---

## Adoption Risk Assessment

### Upstash
- **Maturity**: Production-ready (2018+, YC-backed)
- **Breaking changes**: Rare; API stable since 2021
- **Abandonment risk**: Very low (funded, profitable, enterprise customers)
- **Community**: 10k+ GitHub stars; active Discord

### Turso / libSQL
- **Maturity**: 2023+; newer but growing fast
- **Breaking changes**: Some API churn early on, stabilizing
- **Abandonment risk**: Low (Chiselstrike backing, open-source)
- **Community**: 5k+ GitHub stars; smaller but active

### MongoDB
- **Maturity**: Highly stable (20+ years)
- **Breaking changes**: Rare
- **Abandonment risk**: None (massive company)
- **Community**: Massive
- **Problem**: Not designed for serverless; Cloudflare compat is **new and fragile** (Q1 2025)

---

## Unresolved Questions

1. **Turso transaction support on free tier** — Need to confirm if free tier allows `BEGIN`/`COMMIT` or only single-statement atomicity.
2. **Upstash key namespace collision across modules** — If two modules both use a key named `state`, does Upstash handle prefixing? (Likely yes, but need to test)
3. **Redis KEYS pattern on large free tier storage** — If bot accumulates 1GB in Upstash free tier, will `KEYS wordle:*` scans become slow? (Probably fine; Upstash is optimized for this)
4. **MongoDB cold-start in production** — The blog post tests locally; real-world Cloudflare production cold-start latency is unknown. Need real-world telemetry.
5. **Durable Objects write coalescing** — Can DO batch the writes below into a single KV transaction? (Answer: yes, automatic; but do we need it for games?)

---

## Next Steps (If Upstash Recommended)

1. **Create `src/db/upstash-store.js`** — Implement `KVStore` interface using `@upstash/redis`
2. **Update `src/db/create-store.js`** — Swap `new CFKVStore(env.KV)` → `new UpstashStore(env)`
3. **Update `wrangler.toml`** — Remove KV namespace bindings; add `UPSTASH_REDIS_REST_URL` and `UPSTASH_REDIS_REST_TOKEN`
4. **Test locally** — `npm test` (fakes unchanged; tests should pass)
5. **Deploy** — `npm run deploy`
6. **Monitor** — Track Redis command usage via Upstash console; should stay under 500k/mo

**Estimated effort**: ~4-6 hours (including testing + docs update).

---

## Sources Cited

1. [Cloudflare Workers and MongoDB (March 2025)](https://alexbevi.com/blog/2025/03/25/cloudflare-workers-and-mongodb/)
2. [A Year of Improving Node.js Compatibility in Cloudflare Workers](https://blog.cloudflare.com/nodejs-workers-2025/)
3. [MongoDB Atlas Free Cluster Limits (Official Docs)](https://www.mongodb.com/docs/atlas/reference/free-shared-limitations/)
4. [Cloudflare Workers KV Pricing & Limits](https://developers.cloudflare.com/kv/platform/pricing/)
5. [New Pricing and Increased Limits for Upstash Redis (March 2025)](https://upstash.com/blog/redis-new-pricing)
6. [Use Redis in Cloudflare Workers (Upstash Docs)](https://upstash.com/docs/redis/tutorials/cloudflare_workers_with_redis)
7. [Cloudflare Workers Database Integration with Upstash](https://blog.cloudflare.com/cloudflare-workers-database-integration-with-upstash/)
8. [Turso + Cloudflare Workers Integration](https://developers.cloudflare.com/workers/tutorials/connect-to-turso-using-workers/)
9. [Neon with Cloudflare Workers (Docs)](https://developers.cloudflare.com/workers/databases/third-party-integrations/neon/)
10. [Connect to Databases (Cloudflare Workers Docs)](https://developers.cloudflare.com/workers/databases/connecting-to-databases/)
11. [Cloudflare Durable Objects — Write Coalescing](https://developers.cloudflare.com/durable-objects/concepts/what-are-durable-objects/)
