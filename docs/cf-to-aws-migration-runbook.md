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

## Migration matrix (locked from live code AND live CF inventory)

Live CF inventory taken from production via wrangler on 2026-05-16:
- D1 `miti99bot-db`: only `trading_trades` exists (plus internal `_cf_KV`, `_migrations`, `sqlite_sequence`). No `users` or `holdings` tables. 11 rows, 1 distinct user.
- KV namespace `f7f190fcb2fa42eb84a05542911334b0`: 21 keys total (see breakdown below).

| Source dataset / prefix | Live keys | Current consumer | Action | AWS target | Notes |
|---|---|---|---|---|---|
| `wordle:stats:*` | 1 | `internal/modules/wordle/state.go` | migrate | `pk=wordle`, `sk=stats:<subject>` | durable player stats |
| `wordle:game:*` | 0 | `internal/modules/wordle/state.go` | skip | none | ephemeral; not present |
| `loldle:stats:*` | 4 | `internal/modules/loldle/state.go` | migrate | `pk=loldle`, `sk=stats:<subject>` | durable player stats |
| `loldle:config:*` | 1 | `internal/modules/loldle/state.go` | migrate | `pk=loldle`, `sk=config:<subject>` | durable per-subject config |
| `loldle:game:*` | 0 | `internal/modules/loldle/state.go` | skip | none | ephemeral; not present |
| `twentyq:stats:*` | 1 | `internal/modules/twentyq/state.go` | migrate | `pk=twentyq`, `sk=stats:<subject>` | durable player stats |
| `twentyq:game:*` | 0 | `internal/modules/twentyq/state.go` | skip | none | ephemeral; not present |
| `lolschedule:subscribers` | 1 | `internal/modules/lolschedule/subscribers.go` | migrate | `pk=lolschedule`, `sk=subscribers` | durable subscriber list |
| `lolschedule:matches:*` | 0 | `internal/modules/lolschedule/api_client.go` | skip | none | cache only; not present |
| `misc:last_ping` | 0 | `internal/modules/misc/misc.go` | skip | none | JS Worker never wrote it (KV 404). `/mstats` will start fresh on AWS. |
| `trading:user:*` | 1 | `internal/modules/trading/portfolio.go` | **migrate (flat KV copy)** | `pk=trading`, `sk=user:<telegram_id>` | CF KV already holds the exact `Portfolio` JSON shape (`currency`/`assets`/`meta`). No D1 transform required. |
| `trading:sym:*` | 7 | `internal/modules/trading/symbols.go` | skip | none | symbol price cache |
| `trading_trades` (D1) | 11 rows | n/a (no AWS consumer) | archive-only | none | optional cold JSONL export for audit; not import input |
| `doantu:stats:*` | 2 | retired Go module | skip | none | retired; operator chose not to archive |
| `loldle-ability:stats:*` | 1 | retired Go module | skip | none | retired; operator chose not to archive |
| `loldle-emoji:stats:*` | 1 | retired Go module | skip | none | retired; operator chose not to archive |
| `semantle:stats:*` | 1 | retired Go module | skip | none | retired; operator chose not to archive |

## Trading source — LOCKED 2026-05-16

The legacy CF JS Worker already snapshots the user portfolio into KV at `trading:user:<telegram_id>`. The stored value is byte-for-byte the JSON shape the Go AWS runtime expects:

```json
{
  "currency": {"VND": 170850000},
  "assets": {"TCB": 1000, "TCX": 1000, "FPT": 10000, "VCB": 1000},
  "meta": {"invested": 1000000000, "createdAt": 1776743792792}
}
```

Decision record:
- **Source** for trading migration = CF KV key `trading:user:<telegram_id>`. Not D1.
- **Mapping rule** = identity copy. Read KV value, write to DynamoDB at `(pk=trading, sk=user:<telegram_id>)` with the same `value` attribute.
- **`meta.invested`** = read directly from KV; no derivation.
- **`meta.createdAt`** = read directly from KV; no derivation.
- **D1 `trading_trades`** = archive-only audit export (JSONL). Not used by the import path. Only 11 rows / 1 user as of 2026-05-16 — operator may export or skip without runtime impact.

This invalidates the earlier "Phase 03 D1 transform" framing. Phase 03 is now a plain KV copy plus optional D1 audit dump.

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

### 2) Lock the trading source

Resolved 2026-05-16. CF KV `trading:user:<telegram_id>` already holds the exact `Portfolio` JSON shape; no transform path is needed. See "Trading source — LOCKED 2026-05-16" above for the decision record. The historical D1 `trading_trades` table is archive-only audit data.

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

## Phase 01 done checklist — CLOSED 2026-05-16

- [x] Every live Cloudflare KV prefix is classified as `migrate`, `skip`, or `archive`
- [x] Every migrated KV dataset has an exact DynamoDB `(pk, sk)` target
- [x] Trading source table(s) and column(s) are locked (resolved as KV-only — see "Trading source — LOCKED")
- [x] `meta.invested` source is explicit (KV `trading:user:*`)
- [x] `meta.createdAt` source is explicit (KV `trading:user:*`)
- [x] Retired module namespaces are explicitly excluded from runtime import
- [x] AWS cutover remains gated on a green parity report from the migration plan (`plans/260510-0114-aws-port/phase-07-cutover.md` lines 13, 20, 42, 72)

## Phase 03 impact

The D1-transform branch of Phase 03 is dropped. Phase 03 is now:
- flat KV → DynamoDB copy across the 9 durable keys above
- optional one-shot `trading_trades` JSONL audit dump (operator-elective)

Effort revised from 4-6h to ~2h.

## Phase 02 toolchain (built 2026-05-16)

Single Go binary `cmd/migrate_cf_data` with 3 subcommands. No admin HTTP routes. No dependency on the running AWS bot process. Shared helpers live under `internal/migration/`.

### Required environment

| Var | Purpose | Used by |
|---|---|---|
| `CLOUDFLARE_API_TOKEN` | read-scope token | inventory, kv-import, trading-audit-dump |
| `CLOUDFLARE_ACCOUNT_ID` | production CF account | all |
| `CF_KV_NAMESPACE_ID` | production KV namespace | inventory, kv-import |
| `CF_D1_DATABASE_ID` | production D1 database | trading-audit-dump |
| `AWS_REGION` (+ standard AWS SDK creds) | DynamoDB writes | kv-import (non-dry-run) |

### Commands

Inventory (read-only classification, prints counts; no writes anywhere):

```sh
go run ./cmd/migrate_cf_data inventory
```

KV import dry-run against staging — proves wiring and key-shape mapping without any DynamoDB writes:

```sh
go run ./cmd/migrate_cf_data kv-import --table=miti99bot-staging --dry-run
```

KV import (default idempotent — rejects writes where `(pk, sk)` already exists; rerun is safe):

```sh
go run ./cmd/migrate_cf_data kv-import --table=miti99bot-staging
```

KV import with explicit overwrite (drops the `attribute_not_exists` guard; only use when intentionally re-importing a known-good source after rollback):

```sh
go run ./cmd/migrate_cf_data kv-import --table=miti99bot-staging --overwrite
```

Optional `trading_trades` audit dump (D1 → JSONL, audit-only — not an import input):

```sh
go run ./cmd/migrate_cf_data trading-audit-dump --out=plans/reports/migration-260515-2250-trading-audit.jsonl
```

### Report layout

Every `kv-import` run prints a Migration report block with four buckets: `Imported`, `Skipped (already present)`, `Skipped (policy)`, `Failed`. Each bucket is keyed by source prefix (or by skip-reason for the policy bucket). Phase 04 parity verification compares Phase 01 inventory counts against this report.

### Verified end-to-end against production CF on 2026-05-16

`inventory` and `kv-import --dry-run` both run cleanly against prod CF. 9 durable keys map to the runtime `(pk, sk)` shape exactly as the policy table predicts. No DynamoDB writes were issued during verification (`--dry-run`).
