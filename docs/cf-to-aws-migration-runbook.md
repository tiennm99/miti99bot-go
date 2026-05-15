# Runbook: Cloudflare data → AWS migration

This doc is the operator runbook for moving durable Cloudflare KV / D1 data into the live AWS DynamoDB shape used by `miti99bot`.

> Scope here is only durable state that the current Go runtime still reads. Do not bulk-copy legacy Cloudflare data.

## Live AWS target shape

DynamoDB runtime contract:
- partition key: `pk = moduleName`
- sort key: `sk = caller key`
- payload attr: `value`

Examples:
- `wordle` + `stats:<subject>`
- `loldle` + `config:<subject>`
- `lolschedule` + `subscribers`
- `misc` + `last_ping`
- `trading` + `user:<telegram_id>`

## Migration matrix (locked from live code)

| Source dataset / prefix | Current consumer | Action | AWS target | Notes |
|---|---|---|---|---|
| `wordle:stats:*` | `internal/modules/wordle/state.go` | migrate | `pk=wordle`, `sk=stats:<subject>` | durable player stats |
| `wordle:game:*` | `internal/modules/wordle/state.go` | skip | none | ephemeral in-flight game state |
| `loldle:stats:*` | `internal/modules/loldle/state.go` | migrate | `pk=loldle`, `sk=stats:<subject>` | durable player stats |
| `loldle:config:*` | `internal/modules/loldle/state.go` | migrate | `pk=loldle`, `sk=config:<subject>` | durable per-subject config |
| `loldle:game:*` | `internal/modules/loldle/state.go` | skip | none | ephemeral in-flight game state |
| `twentyq:stats:*` | `internal/modules/twentyq/state.go` | migrate | `pk=twentyq`, `sk=stats:<subject>` | durable player stats |
| `twentyq:game:*` | `internal/modules/twentyq/state.go` | skip | none | ephemeral in-flight game state |
| `lolschedule:subscribers` | `internal/modules/lolschedule/subscribers.go` | migrate | `pk=lolschedule`, `sk=subscribers` | durable subscriber list |
| `lolschedule:matches:*` | `internal/modules/lolschedule/api_client.go` | skip | none | cache only |
| `misc:last_ping` | `internal/modules/misc/misc.go` | migrate | `pk=misc`, `sk=last_ping` | `/mstats` still reads it |
| `trading:user:*` or equivalent legacy portfolio state | `internal/modules/trading/portfolio.go` | migrate (transform) | `pk=trading`, `sk=user:<telegram_id>` | must become current `Portfolio` JSON |
| `trading:sym:*` | `internal/modules/trading/symbols.go` | skip | none | cache only |
| retired module namespaces (`loldle_emoji`, `loldle_quote`, `loldle_ability`, `loldle_splash`, `semantle`, `doantu`, etc.) | no live Go consumer | archive | none | export only if operator wants cold backup |

## Trading source inventory

What is confirmed in-repo:
- legacy D1 table `trading_trades` exists
- known columns from prior work: `id`, `user_id`, `symbol`, `side`, `qty`, `price_vnd`, `ts`
- current AWS runtime target is `internal/modules/trading/portfolio.go`:
  - `currency map[string]float64`
  - `assets map[string]int64`
  - `meta.invested float64`
  - `meta.createdAt int64`

What is **not** confirmed in-repo:
- authoritative legacy source table/column for `meta.invested`
- authoritative legacy source table/column for `meta.createdAt`
- whether old Cloudflare runtime stored a user portfolio snapshot separately from `trading_trades`
- whether legacy `users` / `holdings` tables referenced in older plans still exist in production D1

## Phase 01 operator procedure

### 1) Inventory D1 tables

List all tables:

```sh
wrangler d1 execute <database> --remote \
  --command "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name" \
  --json
```

Dump table definitions for all discovered tables, then inspect every name that looks trading-, portfolio-, user-, or holding-related:

```sh
wrangler d1 execute <database> --remote \
  --command "SELECT name, sql FROM sqlite_master WHERE type='table' ORDER BY name" \
  --json
```

Inspect columns for every candidate table that could hold portfolio snapshots or metadata:

```sh
wrangler d1 execute <database> --remote \
  --command "PRAGMA table_info(<table_name>)" \
  --json
```

Do not stop at historical names like `trading_trades`, `users`, or `holdings`; the goal is to inspect whatever production D1 actually contains today.

### 2) Lock the trading transform inputs

Before any import code is written, prove one of these is true:
- a portfolio snapshot table already exists and contains the exact source for `currency`, `assets`, `meta.invested`, and `meta.createdAt`, or
- those fields must be derived, and the derivation is written down explicitly and accepted.

Minimum decision record to capture:
- source table(s)
- source column(s)
- mapping rule into `Portfolio`
- whether historical `trading_trades` rows are import inputs only or audit-only exports

### 3) Inventory Cloudflare KV prefixes

For each durable and skip candidate prefix above, list keys and capture representative values.
Use Wrangler KV listing for at least:
- `wordle:stats:`
- `wordle:game:`
- `loldle:stats:`
- `loldle:config:`
- `loldle:game:`
- `twentyq:stats:`
- `twentyq:game:`
- `lolschedule:subscribers`
- `lolschedule:matches:`
- `misc:last_ping`
- `trading:user:` (candidate only; inspect if a legacy portfolio snapshot prefix exists)
- `trading:sym:`

### 4) Freeze the matrix

Do not proceed to import tooling until each discovered dataset is tagged as one of:
- `migrate`
- `skip`
- `archive`

Anything not in the matrix is out of scope by default.

## Phase 01 done checklist

- [ ] Every live Cloudflare KV prefix is classified as `migrate`, `skip`, or `archive`
- [ ] Every migrated KV dataset has an exact DynamoDB `(pk, sk)` target
- [ ] Trading source table(s) and column(s) are locked
- [ ] `meta.invested` source is explicit
- [ ] `meta.createdAt` source is explicit
- [ ] Retired module namespaces are explicitly excluded from runtime import
- [ ] AWS cutover remains gated on a green parity report from the migration plan

## Current blocker

The repo proves the live AWS target shape and most KV policy, but it does **not** prove the authoritative legacy source for trading `meta.invested` and `meta.createdAt`. Phase 03 should stay blocked until the operator finishes the D1 inventory above.

## Unresolved questions

1. Does production D1 still have `users` / `holdings` tables, or only `trading_trades`?
2. If only `trading_trades` exists, what exact derivation should define `meta.createdAt`?
3. If only `trading_trades` exists, should `meta.invested` be reconstructed from surviving positions only or from full historical cost basis?
4. Are any retired Cloudflare KV namespaces still carrying data the operator wants archived before teardown?
