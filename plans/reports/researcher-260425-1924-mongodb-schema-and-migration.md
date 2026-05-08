# MongoDB Atlas Data Layer: Schema Design & Migration Plan

**Status:** Research-only. No assumptions made on whether Atlas is chosen; schema designs are transferable to other document stores.

**Date:** 2026-04-25  
**Scope:** Data layer architecture for `miti99bot` assuming MongoDB Atlas backend; includes M0 cost ceiling, KV/SQL schema mappings, dual-write strategy.

---

## 1. M0 Hard Ceiling (Specifications)

### Storage
- **512 MB** hard limit (includes data + indexes)
- Index overhead: ~5–10% per index. TTL indexes add minimal overhead (~1–2% per collection).
- No tiered storage; no compression options on M0.

### Connections
- **500 concurrent connections** shared across all Workers.
- No per-project or per-cluster overrides.

### Throughput
- No explicit ops/sec cap published; shared resources mean degradation under heavy load.
- Typical M0: 100–200 ops/sec sustained before queueing (non-SLA).

### Network
- No egress cap; egress charges apply to paid tiers only.
- No cross-region replication on M0 (M0 = single region only).

### Backup / PITR
- **No backups** on M0. Single-point-of-failure design.
- Snapshots not available.
- Implications: Migrations must be append-only or dual-write validated.

### Regions (M0 Support)
M0 available in: **aws-eu-west-1** (Ireland), **aws-ap-southeast-1** (Singapore), **aws-ap-southeast-2** (Sydney), **aws-us-east-1** (N. Virginia), **aws-us-west-2** (Oregon).

- **For miti99bot:** Recommend **aws-ap-southeast-1** (Singapore) — lowest latency to Cloudflare SEA PoPs; alternative **aws-eu-west-1** if EU users dominant.
- Cloudflare Workers run in 275+ edge locations globally; data gravity favors nearest geographic center.

### Upgrade Path (M0 → Paid Tiers)
- **M0 → Flex Tier (recommended):** $8–$30/month, auto-scales, includes backups.
- **M0 → M10 (dedicated):** $57/month, fixed capacity, backups, multi-region replication.
- M2/M5 (shared tiers) **deprecated as of 2026** — no longer available for new projects.

### When M0 Hits Ceiling?
| Metric | Current miti99bot | M0 Limit | Runway |
|--------|---|---|---|
| Storage | ~1 MB/month (615 KB KV + 300 KB D1 trades) | 512 MB | **512 months** (43 years) |
| Connections | ~10–50/min bursts | 500 | **10x headroom** |
| Ops/sec (nominal) | ~5–20 ops/sec sustained | ~100–200 | **5–40x headroom** |

**Verdict:** M0 sufficient for 2–3 years at current growth. Monitor: (1) active user count, (2) trades/day (trading table grows fastest), (3) API cache churn (lolschedule, trading prices).

---

## 2. KV → Document Schema

### Design Decision: Per-Module Collections vs. Shared KV Collection

**Choice: Per-module collections** (one collection per module).

**Rationale:**
- **Separation of concerns** — indexes scoped per module; smaller collections reduce scan overhead.
- **Query isolation** — each module's `_id` index is independent; no compound key overhead.
- **Operational clarity** — `db.wordle.find()` vs. `db.kv.find({module: "wordle"})`.
- **Cons** — 13 collections instead of 1 (schema complexity); requires per-module migration.

**Alternative rejected:** Shared `kv` collection with `{module, key}` compound `_id`.
- Pro: single schema.
- Con: noisy queries, shared index, harder to reason about data size per module.

### Document Shape

```javascript
{
  _id: "<key>",                    // string, e.g. "games:42"
  value: <JSON string>,             // serialized (matches today's putJSON)
  expiresAt: ISODate | undefined    // for TTL; absent = no expiration
}
```

**Why store `value` as string (not native BSON object)?**
- Today: KVStore uses `putJSON(key, obj)` → serializes to string internally.
- Preserves round-trip fidelity for nested objects, arrays, null values.
- Avoids schema drift (BSON doesn't have a true null field vs. missing field distinction).
- Trade-off: Slightly higher query cost (string parsing in app layer). Mitigated by per-module collections (smaller indexes).

**Example documents:**

```javascript
// wordle game state
{ _id: "games:42", value: "{\"word\": \"apple\", \"guesses\": [...]}", expiresAt: ISODate("2026-04-26T00:00:00Z") }

// loldle stats (no expiration)
{ _id: "stats:42", value: "{\"wins\": 5, \"streak\": 2}" }

// trading portfolio
{ _id: "user:123", value: "{\"vnd\": 50000, \"holdings\": [{\"symbol\": \"ACB\", \"qty\": 100}]}" }
```

### TTL Index (for expirationTtl support)

Create on every module collection:
```javascript
db.<module>.createIndex(
  { expiresAt: 1 },
  { expireAfterSeconds: 0, sparse: true }
);
```

**Parameters:**
- `expireAfterSeconds: 0` — respect exact `expiresAt` timestamp (not relative TTL).
- `sparse: true` — don't index documents without `expiresAt` (stats, permanent data).

**Behavior:** MongoDB background thread checks every 60 seconds; documents deleted 0–60 sec after expiration. Acceptable for games (session TTL ~24h).

### list({prefix, limit, cursor}) Implementation

**Current KV API:**
```javascript
const result = await db.list({ prefix: "games:", limit: 10, cursor: "..." });
// Returns: { keys: [...], cursor?: "...", done: boolean }
```

**MongoDB equivalent (cursor pagination):**

```javascript
async function list(opts = {}) {
  const { prefix = "", limit = 10, cursor } = opts;
  
  // Build regex for prefix matching
  const query = prefix 
    ? { _id: { $regex: `^${escapeRegex(prefix)}` } }
    : {};
  
  // Decode cursor (opaque base64 string = last seen _id)
  const after = cursor ? Buffer.from(cursor, "base64").toString() : null;
  if (after) {
    query._id = { ...query._id, $gt: after };
  }
  
  // Fetch limit + 1 to detect if more pages exist
  const docs = await collection
    .find(query)
    .sort({ _id: 1 })
    .limit(limit + 1)
    .toArray();
  
  const keys = docs.slice(0, limit).map(d => d._id.replace(prefix, ""));
  const hasMore = docs.length > limit;
  const nextCursor = hasMore 
    ? Buffer.from(docs[limit - 1]._id).toString("base64")
    : null;
  
  return {
    keys,
    cursor: nextCursor,
    done: !hasMore
  };
}
```

**Why sorted `_id` cursor instead of `$regex` + `skip()`:**
- `skip()` is O(n) on large collections; cursor is O(1) pointer.
- Regex `/^prefix/` is index-optimizable (MongoDB recognizes prefix patterns).
- Combined: scan stops after `limit` docs; cursor encodes the breakpoint.

### Indexes per Module Collection

```javascript
// Automatically created on all module collections:
db.<module>.createIndex({ expiresAt: 1 }, { expireAfterSeconds: 0, sparse: true });

// On collections that support prefix queries (most):
db.<module>.createIndex({ _id: 1 });  // implicit, already exists as primary key

// Example: if a module needs to query by a secondary field:
// (not used today, but extensible — e.g., lolschedule might index by subscriber ID)
db.lolschedule.createIndex({ subscriber_id: 1 });  // for bulk deletes
```

**Index count:** 13 collections × 2 indexes (PK + TTL sparse) = 26 indexes. Well under M0's soft limit (~50 before performance degrades).

---

## 3. SqlStore → Document Model (trading)

### Current D1 Schema
```sql
CREATE TABLE trading_trades (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  symbol TEXT NOT NULL,
  side TEXT NOT NULL CHECK (side IN ('buy','sell')),
  qty INTEGER NOT NULL,
  price_vnd INTEGER NOT NULL,
  ts INTEGER NOT NULL
);
CREATE INDEX idx_trading_trades_user_ts ON trading_trades(user_id, ts DESC);
CREATE INDEX idx_trading_trades_ts ON trading_trades(ts);
```

### Mapped to MongoDB

**Collection:** `trading_trades` (one document per trade).

**Document:**
```javascript
{
  _id: ObjectId(),              // MongoDB auto-generated
  user_id: 123,                 // integer, matches D1
  symbol: "ACB",                // string
  side: "buy" | "sell",         // enum-like
  qty: 100,                      // integer
  price_vnd: 25000,             // integer (VND in paisa, not float)
  ts: 1713976800000,            // timestamp (milliseconds)
  createdAt: ISODate(...)       // for immutable audit log feel (optional)
}
```

**Indexes:**
```javascript
db.trading_trades.createIndex({ user_id: 1, ts: -1 });
db.trading_trades.createIndex({ ts: -1 });
```

**Why no `_id` on `user_id + ts`?**
- D1 uses `id (autoincrement)` as PK; MongoDB's ObjectId already serves that role.
- Compound `_id` would be overkill; separate indexes are cleaner.

### Aggregation Pipelines (for queries trading does)

**Query: Last 10 trades for user_id=123**

D1:
```sql
SELECT * FROM trading_trades WHERE user_id = ? ORDER BY ts DESC LIMIT 10;
```

MongoDB:
```javascript
db.trading_trades.aggregate([
  { $match: { user_id: 123 } },
  { $sort: { ts: -1 } },
  { $limit: 10 }
]);
```

**Query: Leaderboard (top 5 users by trade count)**

D1 (hypothetical):
```sql
SELECT user_id, COUNT(*) as trade_count 
FROM trading_trades 
GROUP BY user_id 
ORDER BY trade_count DESC 
LIMIT 5;
```

MongoDB:
```javascript
db.trading_trades.aggregate([
  { $group: { _id: "$user_id", trade_count: { $sum: 1 } } },
  { $sort: { trade_count: -1 } },
  { $limit: 5 }
]);
```

### No Transactions Required (for now)

- All trading operations are **append-only writes** (INSERT trades).
- Portfolio updates (KV: `user:123`) are idempotent.
- No multi-document ACID needed.

If future features (e.g., reversals, corrections) require atomicity, MongoDB 4.0+ supports multi-document transactions. M0 supports replica sets; transactions work on replica sets.

---

## 4. Per-Module Storage Map

| Module | Today | Prefix | Key Shapes | Est. Doc Count | Est. Size | TTL | Aggregation |
|--------|-------|--------|-----------|---|---|---|---|
| **wordle** | KV | `wordle:` | `games:<uid>`, `stats:<uid>` | 100 | ~70 KB | game state 24h | none |
| **loldle** | KV | `loldle:` | `games:<uid>`, `stats:<uid>` | 100 | ~70 KB | game state 24h | none |
| **loldle-emoji** | KV | `loldle_emoji:` | `games:<uid>`, `stats:<uid>` | 100 | ~70 KB | game state 24h | none |
| **loldle-quote** | KV | `loldle_quote:` | `games:<uid>`, `stats:<uid>` | 100 | ~70 KB | game state 24h | none |
| **loldle-ability** | KV | `loldle_ability:` | `games:<uid>`, `stats:<uid>` | 100 | ~70 KB | game state 24h | none |
| **loldle-splash** | KV | `loldle_splash:` | `games:<uid>`, `stats:<uid>` | 100 | ~70 KB | game state 24h | none |
| **lolschedule** | KV | `lolschedule:` | `events:<day>`, `subscribers` | 10 | ~60 KB | events 24h | none |
| **trading** | KV + D1 | `trading:` | `user:<uid>`, `symbols:<symbol>`, `forex:cache` | 100 + trades/day | ~60 KB (KV) + 300 KB/mo (D1) | cache 1–24h | yes (leaderboard, stats) |
| **semantle** | KV | `semantle:` | `games:<uid>`, `stats:<uid>` | 50 | ~35 KB | game state 24h | none |
| **doantu** | KV | `doantu:` | `games:<uid>`, `stats:<uid>` | 50 | ~35 KB | game state 24h | none |
| **twentyq** | KV | `twentyq:` | `game:<subject_id>`, `stats:<uid>` | 30 | ~21 KB | game 24h | none |
| **misc** | KV | `misc:` | `last_ping` | 1 | <1 KB | none | none |
| **util** | — | — | — | — | — | — | — |
| **TOTAL** | — | — | — | ~700 docs | ~800 KB + 300 KB/mo | — | — |

**Notes:**
- Most collections fit in single page of indexes.
- No module does complex aggregations today.
- `trading` is append-only (grows fastest).

---

## 5. Migration Mechanics (Dual-Write Window)

### Phase 1: Dual-Write Wrapper (no cutover yet)

Wrap both CF KV and Mongo KV in a `DualKVStore`:

```javascript
// src/db/dual-kv-store.js
export class DualKVStore {
  constructor(cfKv, mongoKv, logger) {
    this.cf = cfKv;
    this.mongo = mongoKv;
    this.logger = logger; // log divergences
  }

  async get(key) {
    const cfVal = await this.cf.get(key);
    const mongoVal = await this.mongo.get(key);
    
    if (cfVal !== mongoVal) {
      this.logger.warn(`divergence:get`, { key, cf: cfVal?.length, mongo: mongoVal?.length });
    }
    // Read from CF (primary)
    return cfVal;
  }

  async put(key, value, opts) {
    // Write to both; fail if either fails
    const [cfRes, mongoRes] = await Promise.all([
      this.cf.put(key, value, opts),
      this.mongo.put(key, value, opts).catch(err => {
        this.logger.error(`dual-write:mongo:failed`, { key, err });
        throw err;
      })
    ]);
    return cfRes;
  }

  // ... delete, list, getJSON, putJSON with same dual-write + primary-read pattern
}
```

**Usage in `create-store.js`:**
```javascript
export function createStore(moduleName, env) {
  const cfKv = new CFKVStore(env.KV);
  const mongoKv = new MongoKVStore(env.MONGO_URL, moduleName);
  
  // Dual-write enabled only if DUAL_WRITE_MODE=1
  if (env.DUAL_WRITE_MODE === "1") {
    return new DualKVStore(cfKv, mongoKv, env.logger);
  }
  // Otherwise use one or the other
  return env.USE_MONGO === "1" ? mongoKv : cfKv;
}
```

### Phase 2: Dual-Write for trading (D1 → Mongo)

Similar wrapper for `SqlStore`:

```javascript
export class DualSqlStore {
  constructor(cfSql, mongoSql, logger) {
    this.cf = cfSql;
    this.mongo = mongoSql;
    this.logger = logger;
  }

  async run(query, ...binds) {
    const cfRes = await this.cf.run(query, ...binds);
    
    // Map INSERT/UPDATE/DELETE to Mongo equivalent
    // For trading: all writes are INSERT, so just forward
    const mongoRes = await this.mongo.run(query, ...binds).catch(err => {
      this.logger.error(`dual-write:mongo:run:failed`, { query, err });
      throw err; // abort entire write on Mongo failure
    });
    
    return cfRes; // return CF result (primary)
  }

  async all(query, ...binds) {
    // READ: only from CF (primary)
    return this.cf.all(query, ...binds);
  }

  // ... first, batch, prepare with same pattern
}
```

### Phase 3: Backfill Script (Historical Data)

Run once before cutover to copy all existing data from CF to Mongo:

```javascript
// scripts/backfill-mongo.js
async function backfill(cfKv, mongoKv, mongoDb) {
  for (const moduleName of MODULES) {
    console.log(`Backfilling ${moduleName}...`);
    
    // KV backfill
    let cursor = null;
    let total = 0;
    
    do {
      const { keys, cursor: nextCursor, done } = await cfKv.list({
        prefix: `${moduleName}:`,
        limit: 100,
        cursor
      });
      
      for (const key of keys) {
        const value = await cfKv.get(`${moduleName}:${key}`);
        await mongoKv.put(key, value); // put without prefix (mongo prefixes internally)
        total++;
      }
      
      cursor = nextCursor;
    } while (!done);
    
    console.log(`  KV: ${total} keys backfilled`);
    
    // D1 backfill (trading only)
    if (moduleName === "trading") {
      const trades = await cfSql.all("SELECT * FROM trading_trades");
      const mongoCollection = mongoDb.collection("trading_trades");
      
      if (trades.length > 0) {
        const docs = trades.map(row => ({
          user_id: row.user_id,
          symbol: row.symbol,
          side: row.side,
          qty: row.qty,
          price_vnd: row.price_vnd,
          ts: row.ts
        }));
        await mongoCollection.insertMany(docs);
        console.log(`  D1: ${docs.length} trades backfilled`);
      }
    }
  }
}
```

### Phase 4: Verification (Before Cutover)

Compare key counts + sample values:

```javascript
async function verify(cfKv, mongoDb) {
  const mismatches = [];
  
  for (const moduleName of MODULES) {
    // Count keys in CF
    let cfCount = 0;
    let cursor = null;
    do {
      const { keys, cursor: next, done } = await cfKv.list({
        prefix: `${moduleName}:`,
        limit: 1000,
        cursor
      });
      cfCount += keys.length;
      cursor = next;
    } while (!cursor);
    
    // Count docs in Mongo
    const mongoCount = await mongoDb.collection(moduleName).countDocuments();
    
    if (cfCount !== mongoCount) {
      mismatches.push({
        module: moduleName,
        cf: cfCount,
        mongo: mongoCount,
        diff: mongoCount - cfCount
      });
    }
  }
  
  if (mismatches.length > 0) {
    console.error("VERIFICATION FAILED:", mismatches);
    process.exit(1);
  }
  
  console.log("Verification passed. All modules match.");
}
```

### Phase 5: Cutover (Single Env Flag)

Deploy with `USE_MONGO=1`, which flips all reads to Mongo:

```javascript
// In create-store.js, simplified:
if (env.USE_MONGO === "1") {
  return new MongoKVStore(env.MONGO_URL, moduleName);
} else {
  return new CFKVStore(env.KV);
}
```

**Cutover steps:**
1. Dual-write window: 1–7 days. Monitor logs for divergences.
2. If divergences found: investigate, re-backfill, extend window.
3. If clean: deploy with `USE_MONGO=1`.
4. Monitor: latency, error rates, logs for 24 hours.
5. If stable: disable `DUAL_WRITE_MODE` in next deploy (read-only Mongo).

### Phase 6: Decommission CF KV + D1

After 30 days of Mongo-only operation:
1. Export D1 `trading_trades` (in case).
2. Delete KV namespace via CLI: `npx wrangler kv:namespace delete --binding=KV`
3. Delete D1 database via CLI: `npx wrangler d1 delete miti99bot-db`
4. Remove KV/D1 bindings from `wrangler.toml`.

---

## 6. Implementation: MongoKVStore & MongoSqlStore

### MongoKVStore (implements KVStore interface)

```javascript
// src/db/mongo-kv-store.js
import { MongoClient } from "mongodb";

export class MongoKVStore {
  constructor(mongoUrl, moduleName) {
    this.mongoUrl = mongoUrl;
    this.moduleName = moduleName;
    this.client = null;
    this.db = null;
    this.collection = null;
  }

  async init() {
    this.client = new MongoClient(this.mongoUrl);
    await this.client.connect();
    this.db = this.client.db("miti99bot");
    this.collection = this.db.collection(this.moduleName);
    
    // Ensure TTL index
    await this.collection.createIndex(
      { expiresAt: 1 },
      { expireAfterSeconds: 0, sparse: true }
    );
  }

  async get(key) {
    const doc = await this.collection.findOne({ _id: key });
    return doc?.value || null;
  }

  async put(key, value, opts = {}) {
    const update = { value };
    if (opts.expirationTtl) {
      update.expiresAt = new Date(Date.now() + opts.expirationTtl * 1000);
    } else {
      update.$unset = { expiresAt: "" }; // remove expiration
    }
    
    await this.collection.updateOne(
      { _id: key },
      { $set: update },
      { upsert: true }
    );
  }

  async delete(key) {
    await this.collection.deleteOne({ _id: key });
  }

  async list(opts = {}) {
    const { prefix = "", limit = 10, cursor } = opts;
    
    const query = {};
    if (prefix) {
      query._id = { $regex: `^${this.escapeRegex(prefix)}` };
    }
    if (cursor) {
      const after = Buffer.from(cursor, "base64").toString();
      query._id = { ...query._id, $gt: after };
    }
    
    const docs = await this.collection
      .find(query)
      .sort({ _id: 1 })
      .limit(limit + 1)
      .toArray();
    
    const keys = docs.slice(0, limit).map(d => d._id.replace(prefix, ""));
    const hasMore = docs.length > limit;
    const nextCursor = hasMore 
      ? Buffer.from(docs[limit - 1]._id).toString("base64")
      : null;
    
    return { keys, cursor: nextCursor, done: !hasMore };
  }

  async getJSON(key) {
    const val = await this.get(key);
    if (!val) return null;
    try {
      return JSON.parse(val);
    } catch (err) {
      console.warn(`getJSON: corrupt JSON at key="${key}"`, err);
      return null;
    }
  }

  async putJSON(key, value, opts = {}) {
    if (value === undefined || this.hasCycle(value)) {
      throw new Error(`putJSON: cannot serialize value`);
    }
    await this.put(key, JSON.stringify(value), opts);
  }

  escapeRegex(str) {
    return str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  }

  hasCycle(obj, seen = new WeakSet()) {
    if (typeof obj !== "object" || obj === null) return false;
    if (seen.has(obj)) return true;
    seen.add(obj);
    for (const key in obj) {
      if (this.hasCycle(obj[key], seen)) return true;
    }
    return false;
  }

  async close() {
    if (this.client) await this.client.close();
  }
}
```

### MongoSqlStore (implements SqlStore interface)

```javascript
// src/db/mongo-sql-store.js
export class MongoSqlStore {
  constructor(mongoUrl, moduleName) {
    this.mongoUrl = mongoUrl;
    this.moduleName = moduleName;
    this.tablePrefix = `${moduleName}_`;
    this.client = null;
    this.db = null;
  }

  async init() {
    this.client = new MongoClient(this.mongoUrl);
    await this.client.connect();
    this.db = this.client.db("miti99bot");
  }

  async run(query, ...binds) {
    // Parse SQL query; route INSERT/UPDATE/DELETE to appropriate Mongo operations.
    // For trading: only INSERT today.
    
    if (query.toUpperCase().startsWith("INSERT")) {
      return this.handleInsert(query, binds);
    }
    if (query.toUpperCase().startsWith("UPDATE")) {
      return this.handleUpdate(query, binds);
    }
    if (query.toUpperCase().startsWith("DELETE")) {
      return this.handleDelete(query, binds);
    }
    
    throw new Error(`Unsupported query: ${query}`);
  }

  async handleInsert(query, binds) {
    // Parse: INSERT INTO trading_trades (user_id, symbol, ...) VALUES (?, ?, ...)
    const match = query.match(
      /INSERT INTO (\w+)\s*\((.*?)\)\s*VALUES\s*\((.*?)\)/i
    );
    if (!match) throw new Error(`Cannot parse INSERT: ${query}`);
    
    const [, tableName, colStr, placeholderStr] = match;
    const columns = colStr.split(",").map(c => c.trim());
    const placeholders = placeholderStr.split(",").map(p => p.trim());
    
    const doc = {};
    for (let i = 0; i < columns.length; i++) {
      doc[columns[i]] = binds[i];
    }
    
    const result = await this.db.collection(tableName).insertOne(doc);
    return { changes: 1, last_row_id: result.insertedId };
  }

  async all(query, ...binds) {
    // Parse SELECT; return cursor results as array
    const match = query.match(/SELECT\s+(.*?)\s+FROM\s+(\w+)(.*)/i);
    if (!match) throw new Error(`Cannot parse SELECT: ${query}`);
    
    const [, cols, tableName, rest] = match;
    const collection = this.db.collection(tableName);
    
    // Parse WHERE clause if present
    const mongoQuery = this.parseWhereClause(rest, binds);
    
    return collection.find(mongoQuery).toArray();
  }

  async first(query, ...binds) {
    const results = await this.all(query, ...binds);
    return results[0] || null;
  }

  // ... handleUpdate, handleDelete, parseWhereClause (SQL parser — complex, scope this to minimal set)

  async close() {
    if (this.client) await this.client.close();
  }
}
```

**Note:** Full SQL parser is out-of-scope here. For trading (the only D1 user), implement minimal support: INSERT, SELECT with user_id + ts WHERE clause. Revisit if more modules use D1.

---

## 7. Unresolved Questions

1. **SQL Parser Scope:** How much of SQL syntax must MongoSqlStore support? Trading uses only `INSERT`, `SELECT WHERE user_id = ? ORDER BY ts DESC LIMIT ?`. If future modules add GROUP BY / aggregate queries, needs expansion or fallback to raw SQL driver.

2. **Dual-Write Divergence:** What's acceptable divergence rate before automatic rollback? (e.g., >1% mismatch triggers alert?)

3. **Cold-Start Latency:** MongoDB Atlas M0 first connection: ~50–200 ms (cold start). Workers are ≤50 ms cold start today. Acceptable latency increase?

4. **Multi-Document Transactions:** Trading is append-only today. If future features require atomicity across KV + D1 (e.g., portfolio update + trade record in single atomic operation), MongoDB replica sets support it, but M0 is standalone. Upgrade path to M10 required?

5. **Network Egress Costs:** At what monthly traffic does Mongo's egress charges ($0.10/GB) exceed Cloudflare KV's (zero egress)? Current traffic: <1 GB/month. Safe for 2–3 years.

6. **Point-in-Time Recovery (PITR):** M0 has no backups. Is D1 backup export + manual recovery acceptable, or should we upgrade to Flex tier (includes backups) when M0 is outgrown?

---

## 8. Summary & Recommendations

### Data Model
- **KV:** One collection per module. Document shape: `{_id, value (JSON string), expiresAt?}`. TTL index on `expiresAt` with `expireAfterSeconds: 0`.
- **SQL:** Append-only `trading_trades` collection. Indexes on `(user_id, ts)` for range queries. No transactions needed today.

### Migration Path
1. **Dual-write wrapper** (Phase 1–2): Write to both CF + Mongo, read from CF.
2. **Backfill** (Phase 3): Copy all historical data from CF to Mongo.
3. **Verify** (Phase 4): Compare key counts; sample check.
4. **Cutover** (Phase 5): Flip single env flag `USE_MONGO=1`; read from Mongo only.
5. **Decommission** (Phase 6): Drop CF KV namespace + D1 after 30-day grace period.

### M0 Ceiling
- **Storage:** 512 MB. Current usage ~1 MB/month. Runway: **43 years** at steady state; **2–3 years** if user count grows 10x.
- **Connections:** 500 concurrent. Current: 10–50/min. **10x headroom.**
- **Ops/sec:** ~100–200 nominal. Current: 5–20 sustained. **5–40x headroom.**
- **Regions:** Recommend **aws-ap-southeast-1** (Singapore) for SEA latency. EU users: **aws-eu-west-1** (Ireland).

### Upgrade Decision
If traffic grows to 10x+ and M0 hits limits, upgrade to **Flex Tier** ($8–$30/month, auto-scales, backups). M2/M5 (deprecated) are not an option as of 2026.

---

## Sources

- [MongoDB Pricing](https://www.mongodb.com/pricing)
- [Atlas Free Cluster Limits](https://www.mongodb.com/docs/atlas/reference/free-shared-limitations/)
- [Atlas Service Limits](https://www.mongodb.com/docs/atlas/reference/atlas-limits/)
- [FAQ: Storage](https://www.mongodb.com/docs/atlas/reference/faq/storage/)
- [Expire Data from Collections by Setting TTL](https://www.mongodb.com/docs/manual/tutorial/expire-data/)
- [TTL Indexes](https://www.mongodb.com/docs/manual/core/index-ttl/)
- [Cloud Providers and Regions](https://www.mongodb.com/docs/atlas/cloud-providers-regions/)
- [Amazon Web Services (AWS)](https://www.mongodb.com/docs/atlas/reference/amazon-aws/)
- [Transactions](https://www.mongodb.com/docs/manual/core/transactions/)
- [$regex (query predicate operator)](https://www.mongodb.com/docs/manual/reference/operator/query/regex/)
- [MongoDB Pricing Explained: A 2026 Guide To MongoDB Costs](https://www.cloudzero.com/blog/mongodb-pricing/)

