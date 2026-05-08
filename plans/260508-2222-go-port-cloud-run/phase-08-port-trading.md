---
phase: 8
title: "Port trading + Firestore composite indexes"
status: pending
priority: P2
effort: "6h"
dependencies: [4]
---

# Phase 08: Port trading + Firestore composite indexes

## Overview
Port the most complex module: VN-stocks paper trading. Original used D1 (relational SQL) for trades + leaderboards. Translate to Firestore document model with composite indexes for the leaderboard query path.

## Requirements
- Functional: `/trade`, `/buy <ticker> <qty>`, `/sell …`, `/portfolio`, `/leaderboard`, plus the daily price-update cron at `0 17 * * *`.
- Non-functional: leaderboard query stays under 100ms warm. Daily cron fits within 50k-reads/20k-writes per-day cap (≤300 active users, ≤50 unique tickets traded).

## Architecture

Firestore data model (replacing D1's `trading_trades` table):

```
collection: trading_users          ← user state
  doc id: <userID>
  fields:
    balanceVnd: number
    createdAt: timestamp
    lastTradeAt: timestamp
    pnlVnd: number          ← denormalized for leaderboard

  subcollection: trades             ← per-user trade log
    doc id: <auto>
    fields: { ticker, side, qty, priceVnd, ts }

  subcollection: holdings           ← current positions (one per ticker)
    doc id: <ticker>
    fields: { qty, avgCostVnd }

collection: trading_prices          ← current ticker prices
  doc id: <ticker>
  fields: { priceVnd, updatedAt }
```

Composite index: `trading_users` on `(pnlVnd DESC)` for leaderboard. Single-field default indexes cover everything else.

## Related Code Files
- Create: `internal/modules/trading/{module,buy,sell,portfolio,leaderboard,prices,cron_daily_update}.go`
- Create: `internal/modules/trading/store.go` — direct Firestore access (bypassing KVStore for relational queries)
- Create: `firestore.indexes.json` (committed) — composite indexes deployed via `gcloud firestore indexes composite create`
- Modify: `Deps` to include `*firestore.Client` (already present)
- Modify: `MODULES` env var in deploy yaml — add `trading`

## Implementation Steps
1. **Schema**: Define structs `User`, `Trade`, `Holding`, `Price` in `store.go`. Use `firestore` struct tags.
2. **Buy flow**:
   - Read user balance + ticker price.
   - Validate sufficient balance + qty > 0.
   - In a Firestore `RunTransaction`: decrement balance, increment holding (compute new avgCost), append trade, update `lastTradeAt`.
3. **Sell flow**:
   - Symmetric. Realized PnL = (sellPrice - avgCost) * qty. Update `pnlVnd` denorm.
4. **Portfolio**: list holdings + current prices (one read per ticker — typical user holds <10).
5. **Leaderboard**: `Where(pnlVnd > 0).OrderBy(pnlVnd DESC).Limit(10)`. Requires composite index.
6. **Daily price update cron**:
   - Triggered by Cloud Scheduler at `0 17 * * *` (set up in Phase 09).
   - Fetches VN stock prices from existing data source (port URL/parsing from JS module).
   - Writes ~50 ticker docs into `trading_prices`. Stays under 20k writes/day cap easily.
7. **One-time data import** (optional, decided in Phase 12 cutover): script to read D1 dump, transform, write to Firestore. Skip if user opts to start fresh.
8. **Tests**: emulator-based — buy → sell → portfolio → leaderboard parity with JS expectations.
9. **firestore.indexes.json**: capture the composite index definition; `gcloud firestore indexes composite create --collection-group=trading_users --field-config=field-path=pnlVnd,order=descending`.

## Success Criteria
- [ ] Buy/sell round-trips correctly compute balance + avgCost
- [ ] Leaderboard query returns top 10 by pnl in <100ms
- [ ] Daily price cron runs (manual trigger via `/cron/trading-daily-update` for now)
- [ ] Composite index deployed and active
- [ ] Tests pass against emulator

## Risk Assessment
- **Risk**: Firestore transactions have a 500-doc / 5MB / 10s limit. Trading transactions are tiny — fine.
- **Risk**: Leaderboard composite index requires explicit creation (Firestore prompts in console on first failed query). **Mitigation**: capture in `firestore.indexes.json` + deploy via gcloud in CI.
- **Risk**: Denormalized `pnlVnd` can drift if a sell update partially fails. **Mitigation**: always update inside transaction with the trade write.
- **Risk**: Free tier 20k writes/day. Per active user, a buy+sell = 4 writes (user, trade, holding, price-touched). 300 users × 5 trades/day = 6k writes — well within.
- **Risk**: VN stock data source may be unstable. **Mitigation**: port the same source used by JS; if cron fails, retry on next run.

## Rollback
Remove `trading` from `MODULES`. Existing data in `trading_users` collection persists harmlessly; no orphan refs since modules are isolated.
