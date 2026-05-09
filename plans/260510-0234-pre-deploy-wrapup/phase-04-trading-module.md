---
phase: 4
title: "Trading module port (VN stocks paper trading)"
status: pending
priority: P2
effort: "6h"
dependencies: []
---

# Phase 04: Trading module port (VN stocks paper trading)

## Overview
Port the `trading` module from the original miti99bot to Go. Paper-trading on Vietnam-listed stocks: per-user portfolio + buy/sell commands + daily price refresh cron. Largest remaining cloud-agnostic chunk; carries from `plans/260508-2222-go-port-cloud-run/` Phase 08 unchanged in scope.

## Requirements
- **Functional:**
  - Commands (parity with original): `/buy <ticker> <qty>`, `/sell <ticker> <qty>`, `/portfolio`, `/price <ticker>`, `/leaderboard`
  - Daily price refresh cron at market-close (Vietnam: 15:00 ICT = UTC 08:00) — fetch latest closes for tracked tickers, store snapshot, recompute portfolio P&L
  - Per-user paper-money starting balance, persistent ledger of trades
  - Leaderboard: top-N users by total portfolio value
- **Non-functional:**
  - Stays inside Firestore + DynamoDB free tiers (per-user state in 1-3 KV keys, leaderboard a single derived doc)
  - Daily price API call counts: <50/day (well inside any reasonable free tier on the data source)

## Architecture
**Module shape** mirrors existing modules (e.g. `wordle`):
```
internal/modules/trading/
  trading.go        Module struct + factory + Commands() + Crons()
  api_client.go     HTTP client to VN-stocks data source
  api_client_test.go
  portfolio.go      core domain: Portfolio struct, buy/sell, mark-to-market
  portfolio_test.go
  handlers.go       command handlers (/buy, /sell, /portfolio, /price, /leaderboard)
  handlers_test.go
  cron.go           daily price refresh handler
  cron_test.go
  format.go         message formatting helpers
  format_test.go
```

**KV layout** (per-module partition, no cross-keys):
- `user:<id>:portfolio` → JSON Portfolio (cash, positions, trade history)
- `prices:<ticker>` → JSON {price, timestamp}
- `leaderboard` → JSON sorted list (recomputed by cron)
- `tickers` → JSON array (set of tracked tickers across all users; cron iterates this)

**Concurrency:** Buy/sell mutate the same user portfolio; reuse existing `internal/keylock` (already in repo from wordle work) keyed by `user:<id>:portfolio`.

**Cron:** registered with name `daily_refresh`, schedule `0 8 * * *` (UTC = 15:00 ICT, market close). Iterates `tickers`, fetches each price, updates `prices:*`, recomputes leaderboard.

## Related Code Files
- Create: all files under `internal/modules/trading/` (per architecture above)
- Modify: `cmd/server/main.go` — add `"trading": trading.New` to factories map
- Reference: `internal/modules/wordle/` as the closest existing template (commands + state + per-user mutex)
- Reference: `internal/modules/lolschedule/api_client.go` as the closest HTTP-fetching template

## Implementation Steps
1. **Locate original miti99bot trading source** — review the JS implementation in https://github.com/tiennm99/miti99bot to nail down exact command shape, message formats, leaderboard rules, and the data source URL.
2. **Verify data source** — confirm the API used by original miti99bot is still free + accessible. If not, evaluate alternatives (TCBS public API, VPS public API, etc.). Document the choice in `api_client.go` header.
3. **Stub `api_client.go`** with the chosen endpoint + request shape; unit-test with golden HTTP fixtures (no network calls in tests).
4. **Domain (`portfolio.go`):** pure Go, no I/O — Portfolio struct, Buy/Sell methods returning new state + delta, mark-to-market against a price map. Heavily unit-tested (this is the easiest part to get wrong silently).
5. **Handlers (`handlers.go`):** parse args, load portfolio from KV under `keylock`, call domain method, persist, format reply. Mirror error paths from original (insufficient funds, unknown ticker, etc.).
6. **Cron (`cron.go`):** fetch tickers list, iterate, fetch each price, update KV, recompute leaderboard. Returns aggregate counts in log.
7. **Wire in `cmd/server/main.go`:** add factory line; bump `MODULES` env default in `template.yaml` to include `trading`.
8. **Tests:** ≥80% coverage on `portfolio.go` (domain), happy paths on handlers + cron with mock `api_client`. Match the bar set by `wordle`.
9. **Smoke locally:** `MODULES=trading go run ./cmd/server`; exercise `/buy`, `/sell`, `/portfolio`; manually trigger `/cron/trading_daily_refresh`.

## Success Criteria
- [ ] All five commands implemented at parity with original miti99bot
- [ ] Daily refresh cron registered (Phase 05 wires it to AWS Scheduler)
- [ ] Portfolio domain has ≥80% test coverage
- [ ] No flaky tests; no network calls in unit tests (HTTP fixtures only)
- [ ] `go vet`, `go test ./internal/modules/trading/...`, full `go build` green
- [ ] Local smoke against in-memory KV exercises buy/sell/portfolio/leaderboard end-to-end
- [ ] `template.yaml` MODULES default updated to include `trading`

## Risk Assessment
- **Original API source no longer free** — Mitigation: pre-flight check in step 2; if blocked, document fallback (web scrape with caching, or paid tier acceptance, or feature gate the module) and re-scope this phase.
- **Time zone bugs in market close** — Mitigation: store all timestamps as UTC, format for display only; unit-test the cron's UTC→ICT translation explicitly.
- **Leaderboard recomputation cost** at scale — Mitigation: full recompute is O(users); under 1k users this is single-digit ms. Past that, switch to incremental updates triggered on each trade.
- **Schema drift between paper-money currency and real ticker prices** (VND vs USD vs cents) — Mitigation: portfolio stores integer minor units (VND đồng, no fractional); document this loudly in `portfolio.go` header.
- **Concurrent buy/sell on same user** — Mitigation: `keylock` per `user:<id>:portfolio` already proven in wordle.

## Open questions
1. Starting balance default — match original (likely 100M VND)? Confirm in step 1.
2. Allow short selling? Original likely doesn't; default to "long only, can't sell what you don't own."
3. Price ticks during market hours: refresh on `/price <ticker>` command, or only on cron? Cron-only is simpler and free-tier-friendlier; confirm against original behavior.
4. Should the cron also run on weekends (Vietnam market closed)? Skip Sat/Sun in handler; emit a no-op log line.
5. Multi-user leaderboard privacy — show user IDs or display names? Match original behavior; default to display name with fallback to "user-<id>".
