# Code review — lolschedule cron + trading module + framework changes

Date: 2026-05-10
Reviewer: code-reviewer (staff)
Scope: 1× framework change, 1× new cron, 1× new module (~30 files)
Verification: build clean, `go vet` clean, `go test -race` 24/24 green; **but `gofmt -l` reports 1 file dirty** (see B1).

---

## Critical (blocks merge)

### C1. `gofmt -l` fails on `internal/modules/trading/handlers.go`
Line 22-28 of `state` struct has misaligned field tags. `gofmt -d` proposes:
```
-	kv      storage.KVStore
-	prices  *PriceClient
-	locks   keylock.Map
-	nowFn   func() time.Time
+	kv                 storage.KVStore
+	prices             *PriceClient
+	locks              keylock.Map
+	nowFn              func() time.Time
 	commingSoonMessage string
```
golangci-lint v2.12 enforces `gofmt`; CI will fail. Run `gofmt -w internal/modules/trading/handlers.go`.

### C2. Sell-rollback can silently lose user shares
`internal/modules/trading/handlers.go:188-189`:
```go
p.AddAsset(symbol, qty)
_ = SavePortfolio(ctx, s.kv, userID, p)
```
The deduction-rollback Save's error is dropped with `_ =`. If KBS is down (already true at this codepath) AND the rollback write also fails (transient DynamoDB throttle, ctx deadline near expiry), the user's prior `DeductAsset` is in-memory only, never reverted, and on the next Load they will see the previous (post-deduct) state. Net result: shares deleted, no VND credited.

Fix: log + replace user-facing message when rollback fails so an op is alerted, e.g.:
```go
if err := SavePortfolio(ctx, s.kv, userID, p); err != nil {
    log.Error("trading_sell_rollback_failed", "user", userID, "symbol", symbol, "qty", qty, "err", err)
    return chathelper.Reply(ctx, b, chatID, "Sell failed and rollback errored — contact support before retrying.")
}
```
Or — better — fetch the price BEFORE acquiring the lock (parallel to handleBuy's structure) so no rollback path is needed. handleSell currently fetches *under* the lock too, which also blocks the user's mutex on a 10-second HTTP call (see H1).

---

## High

### H1. handleSell holds the per-user lock across a 10s HTTP call
`handlers.go:174-185`: lock acquired → Load → DeductAsset → **FetchPrice (10s timeout)** → AddCurrency → Save. Concurrent operations on the same user serialise behind a transient HTTP call to KBS. handleBuy correctly fetches the price *before* the lock. Rewrite handleSell to mirror handleBuy's order: validate, FetchPrice, then acquire lock, then Load → check holdings → Deduct + AddCurrency → Save. Eliminates the lock-held HTTP call AND removes the rollback hazard from C2.

### H2. lolschedule cron will exceed the 60s server timeout above ~1100 subscribers
`internal/server/timeouts.go:9`: `defaultCronTimeout = 60 * time.Second`.
`internal/modules/lolschedule/cron.go:31`: `telegramRateLimitDelay = 50 * time.Millisecond` when subs > 30.
At N subscribers, throttled inter-send delay alone is `(N-1) * 50ms`. SendMessage HTTP latency adds ~50–200ms each. Effective ceiling ≈ 600–800 subscribers before the cron context cancels mid-batch. The handler does check `ctx.Done()` (good — returns ctx.Err) but the run is then logged as a failure with no resume state; the next day's run starts from chat[0] again so early subscribers are over-served and tail subscribers are starved (not fair).

Mitigations (pick one for v1; defer the rest):
- a. Cap N: refuse new subscribers above e.g. 800 (warn user).
- b. Shard schedule: emit one cron per 500-sub group with offset times. Requires Phase 05 EventBridge work.
- c. Async fan-out: cron enqueues N SQS messages, each consumer SendMessages → done in parallel. Best long-term but needs new IaC.

For v1 with realistic JS-source subscriber counts (<100), this is probably fine; flag for monitoring after deploy.

### H3. handleSell: silent rollback save error swallows user data loss
Already covered in C2 — also high-severity from the data-integrity angle.

### H4. No `From.ID == 0` defense
The user's task description says "we explicitly refuse but verify" — code does NOT verify. `senderInfo` (handlers.go:50) only refuses `nil` From, not `From.ID == 0`. If Telegram (or a malicious local-dev fixture) ever produces a User with ID=0, all such users would key into `user:0` and share a portfolio. Telegram's spec says IDs are positive, so this is defense-in-depth, but trivial to add:
```go
if msg == nil || msg.From == nil || msg.From.ID == 0 {
    return 0, 0, false
}
```

---

## Medium

### M1. `Currency` is `map[string]float64` for VND — should be int64
VND has no sub-unit; the smallest legal denomination is 1 VND. `float64` arithmetic on `cost := float64(qty) * price` and `Meta.Invested += amount` accumulates IEEE-754 drift. After a few hundred buys at non-round prices (24,500 × 137 = 3,356,500 — exact, ok; but 18,750 × 31 = 581,250 — also exact, but compounded sums of non-power-of-two integers eventually drift). At Vietnamese stock-trade volumes this is unlikely to materialise as user-visible cents, but flagging because:
- (a) JSON decode `float64` of a saved 24,500,000 then `× 137` could round-trip-shift if KV ever stores e.g. "1.5e7";
- (b) `FormatVND` uses `math.Round` which masks drift in the UI but not in the stored ledger.

Severity is medium (not high) because the upstream JS likely had the same issue and no incident has been reported. Recommend documenting the trade-off in `portfolio.go` or migrating to int64 in v2.

### M2. No ticker length / alphabet validation
`symbols.go:30-35` accepts any non-empty `args[1]` after upper+trim. There's no length cap, no `[A-Z0-9]` enforcement, no defence against unicode lookalikes. While `url.PathEscape` makes the HTTP call safe and `ErrNoPrice` paths skip cache writes (so no KV pollution from invalid lookups), a user could still spam:
```
/trade_buy 1 АAA       (Cyrillic А, looks like ASCII A)
```
which generates KBS HTTP calls + a partial DoS amplification through your Lambda. Add a regex check, e.g. `^[A-Z0-9]{1,16}$` after upper+trim, return `ErrUnknownTicker` for misses. Cheap, principled.

### M3. Field typo: `commingSoonMessage`
`handlers.go:27, 118, 212` — should be `comingSoonMessage` (one m). User-invisible (it's a private field), but CI linters with spell-check rules flag this. Style grep'd consistently (3 occurrences); a single rename works.

### M4. `chatIDString` in cron_test.go is dead/wrong code
`cron_test.go:35-37`:
```go
func chatIDString(id int64) string {
    return time.Unix(id, 0).Format("00") // arbitrary stringification
}
```
`Format("00")` returns the literal string `"00"` because "00" contains no Go time-format directives. So every chat error message is identical: `"fakeSender: induced failure for chat 00"`. Replace with `strconv.FormatInt(id, 10)` or just inline `fmt.Errorf("fakeSender: induced failure for chat %d", id)`.

### M5. lolschedule daily-push has no retry / dead-chat unsubscribe
A subscriber who blocks the bot returns 403 from SendMessage. The cron logs `failed++` and never removes them. Over weeks, the failure count grows. Not a correctness issue but an operational drag. Consider, in a future PR, trimming subscribers whose SendMessage returns specific 403/400 error codes.

### M6. `Phase 05 EventBridge schedule` deferred — daily-push is dead-on-deploy
Per `plan.md:35`, Phase 05 is deferred. Without the `AWS::Scheduler::Schedule`, the registered `lolschedule_daily_push` cron will never fire in production. This is intentional per the plan, but I'm flagging because the README / changelog should not advertise the daily-push feature until Phase 05 ships. Verify the README copy doesn't promise active push.

---

## Low

### L1. `runDailyPush`: throttle decision is binary on `len(subs) > 30`
At N=31, the cron suddenly serialises with 50ms delays. Telegram's 30/sec global limit is a target rate not a hard ceiling — 30 contiguous sends is fine. The threshold is conservative; not wrong, just unnecessarily slow at N=31..100.

### L2. `handleStats` allocates a per-call `heldList` slice — tiny GC churn at scale, fine for v1.

### L3. `prices.go:103` shadows builtin `close`
```go
close := body.DataDay[0].C
if close <= 0 { ... }
```
`close` is a Go builtin (channel close). Shadowing is legal but lint-noisy. Rename to `c` or `lastClose`.

### L4. Test `TestRunDailyPush_SendsToAllSubscribers` asserts ordering of `sender.calls`
Subscribers come back from `listSubscribers` in JSON-array order, which is the order they were added — *currently*. If the persistence layer ever switches to a set-like backend, the test breaks. Either lock the contract in `listSubscribers`'s godoc or sort before asserting in the test.

### L5. README / template.yaml — `trading` enabled by default
`template.yaml:17` adds `trading` to ModulesCSV. Trading is a financial-looking command surface (paper or not). For a personal bot this is fine, but consider whether it should be opt-in via a `--with-trading` deploy flag in case future operators want to disable it without editing the template. v1: leave as-is.

---

## Edge cases / scout findings

- **handleStats** reads portfolio without keylock; safe because LoadPortfolio JSON-decodes a fresh struct each call (no shared map memory with concurrent buy/sell). **No race.**
- **`defer s.locks.Acquire(key)()` semantics** — verified correct: outer call evaluates immediately (acquires), Unlock is deferred. Both buy and sell hold the lock over the right region.
- **ResolveSymbol cache-write fallback** — `_ = kv.PutJSON(...)` on cache miss + successful KBS lookup is intentional and safe (next call will reresolve). Acceptable.
- **`from.ID` collision** — see H4.
- **KBS HTTP error semantics** — 4xx/5xx → ErrNoPrice (verified by test). Network errors → wrapped. JSON decode errors → wrapped. Negative close → ErrNoPrice. Empty data_day → ErrNoPrice. **All paths covered.**
- **Cron auth** — `subtle.ConstantTimeCompare` used (router.go:68). No constant-time bypass via header probing. Good.
- **Cron name regex** — `^[a-z0-9_]{1,32}$`, blocks log injection. `lolschedule_daily_push` matches. Good.
- **No PII / secret leak** — error messages to users are generic ("Could not load portfolio. Try again later."); KBS upstream URLs not echoed; SendMessage params not logged with chat content; no stack traces propagated.
- **Stats fan-out latency** — sequential per-ticker FetchPrice; for a portfolio of 50 tickers at 100ms KBS latency that's 5s of dead time before the user sees anything. Below the 60s ceiling but bad UX. Probably fine for v1 (typical user holds <10).
- **Integer overflow** — `int64` for `qty` and `Assets` map values; max 9.2e18, never reachable for stock counts. `float64` for VND has 53-bit mantissa (~9e15 = 9 quadrillion VND ≈ $360 billion); not reachable.

---

## Positive observations

- Lock granularity (per-user) is correct, not over-broad. Distinct users never block each other.
- handleBuy correctly fetches price *before* acquiring lock — minimises lock duration.
- Tests use `httptest.NewServer` everywhere; **no real KBS calls in `go test`**. Hermetic.
- Dependency injection via `messageSender` interface in cron.go is exemplary: enables real-bot test without mocking the full `*bot.Bot` API.
- `BuildOptions` extension pattern: future deps (Bot, Embedder, Chatter) are added without breaking the `Build` signature. Good API stability hygiene.
- Cache write failure on `ResolveSymbol` is correctly non-fatal (one-line comment explains why).
- `senderInfo` correctly refuses channel posts / inline queries to avoid `user:0` collision.
- Defensive nil-map repair in `LoadPortfolio` is correct defence-in-depth.
- Throttle implementation in cron is select-based on `ctx.Done` — cooperative cancellation is wired.
- 24/24 packages green with `-race`; CI integration looks healthy.

---

## Recommended action order

1. **Fix C1** (gofmt) — 10s, unblocks CI.
2. **Fix C2 + H1 together** by reordering handleSell to fetch price before lock (mirrors handleBuy). One change, two issues resolved.
3. **Add H4** (From.ID == 0 check) — 3 lines.
4. **Add M2** (ticker regex) — 5 lines + 1 test.
5. **Rename M3** (`commingSoonMessage` → `comingSoonMessage`) — global replace.
6. **Fix M4** (chatIDString dead code) — 2-line fix.
7. **Defer rest** (M1 float→int64, M5 dead-chat unsub, L-series) to a follow-up PR.

After (1)–(6), the change is mergeable. (1)–(3) are mandatory before deploy.

---

## Unresolved questions

1. Is the upstream JS `trading` module also using float64 for VND? If yes, M1 is parity (acceptable v1) — if no, this is a regression worth fixing now.
2. What's the realistic peak `lolschedule` subscriber count? If <300 ever, H2 is non-blocking; if growth is plausible, the decision in H2 (a/b/c) needs choosing before Phase 05 EventBridge ships.
3. Should the handleSell rollback path also restore `Meta.Invested` symmetry? Currently Buy doesn't touch Invested and Sell doesn't either — Invested only moves on `trade_topup`. This makes "Invested" mean "total deposits", not "cost basis", which deviates from typical brokerage semantics. Confirm intent matches JS source.
4. Is `Phase 05 EventBridge` going to land before public release? If yes, the daily-push code is exercised on first deploy. If no, it's dead-but-tested code. Either is fine — just confirm.

---

**Status:** DONE_WITH_CONCERNS
**Summary:** Code is well-structured, hermetic-tested, race-clean. Two real correctness issues (C1 gofmt blocker, C2 silent rollback save) and one architectural smell (H1 lock-held HTTP call) need fixing before deploy. Trading module is a credible peer of wordle/loldle in shape and discipline; lolschedule cron is testable and correctly authenticated.
**Concerns:** C1 will fail CI. C2 + H1 are data-integrity (low probability, but not negligible at production scale). H4 is defense-in-depth. M-tier are quality-of-life. Phase 05 EventBridge schedule is deferred-by-design — verify README doesn't over-promise active push.
