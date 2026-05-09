# Test Audit: miti99bot-go Coverage & Quality Report

**Date:** 2026-05-09  
**Auditor:** QA Lead  
**Scope:** Full project test suite (72 Go files, 22 test files, 6360 LOC)  
**Status:** DONE_WITH_CONCERNS

---

## Executive Summary

- **Test Execution:** ✅ All tests pass (count=1 to avoid flakes)
- **Race Detector:** ✅ No data races detected across concurrent access patterns
- **Overall Coverage:** 44.7% (below industry 60-80% target)
- **Build Status:** ✅ go vet passes, no linting errors
- **Critical Gap:** Handler functions in wordle, loldleemoji, misc, util have 0% coverage — handlers never tested via integration tests

**High-Risk Packages:** wordle (37.1%), loldleemoji (36.3%), misc (21.1%)

---

## Per-Package Coverage Table

| Package | Coverage | Status | Primary Gap |
|---------|----------|--------|-------------|
| internal/keylock | 100.0% ✅ | Excellent | None — concurrent access well-tested |
| internal/telegram | 100.0% ✅ | Excellent | None — webhook auth tested end-to-end |
| internal/modules | 71.6% | Good | Registry/Build; public accessors untested (0%) |
| internal/server | 71.4% | Good | Router integration; cron timeout edge cases |
| internal/modules/loldle | 53.0% | Below target | Handler functions (handleLoldle, handleGiveup, etc.) untested |
| internal/storage | 42.9% | Poor | Firestore ops skip on CI (emulator-only); GetJSON/PutJSON unused |
| internal/modules/loldleemoji | 36.3% | Poor | Handler layer untested; state functions incomplete |
| internal/modules/wordle | 37.1% | Poor | **All 5 handler functions have 0% coverage** |
| internal/modules/util | 39.2% | Poor | Handler layer untested (infoCommand, helpCommand, stickerIDCommand all 0%) |
| internal/modules/misc | 21.1% | Poor | Handlers only tested via KV contract, never end-to-end with bot |
| cmd/server | 0.0% ❌ | No tests | main() entry point untestable; buildProvider() and config loading untested |

**Total: 44.7%** (below 60% threshold)

---

## Top 5 Highest-Risk Coverage Gaps

### 1. **Wordle Handler Layer (0% coverage)**
**Files:** `internal/modules/wordle/handlers.go`  
**Functions untested:**
- `handleWordle` (121–189): Main /wordle command — guess submission, board display, win/loss logic
- `handleNew` (193–221): /wordle_new — round abandonment, auto-giveup stats recording
- `handleGiveup` (225–253): /wordle_giveup — reveal answer, idempotency on finished rounds
- `handleStats` (256–284): /wordle_stats — win rate calculation (math.Round call), streak display
- `subjectFor`, `argAfterCommand`, `rejectMessage`, `reply`: All wrapper helpers untested

**Why it matters:** Handlers encapsulate game flow logic, context cancellation, KV error propagation, Telegram API replies. A broken `subjectFor` or missing nil-check on `msg.From` would only surface in production.

**Edge cases not tested:**
- `msg == nil` paths (lines 123–124, 194–195, etc.) — guard clauses exist but never executed
- Context timeout during KV operations (saveGame, loadGame failures)
- Nil map/slice operations (e.g., `msg.Chat.Type` when msg is non-nil but Chat is nil)
- Empty chat ID or user ID edge cases
- Concurrent access: two simultaneous /wordle guesses on same subject race on keylock (tested in isolation, not in handler context)

---

### 2. **Misc Module Handler Handlers (11% coverage in handler functions)**
**Files:** `internal/modules/misc/misc.go`  
**Functions untested as handlers:**
- `pingCommand` (lines 42–60): Handler closure — KV write best-effort path, bot.SendMessage error propagation
- `mstatsCommand` (lines 62–85): Handler closure — GetJSON missing key, error handling, time formatting
- `fortytwoCommand` (lines 87–100): Handler closure — easter egg reply

**Why it matters:** Misc is the "framework-validating" module; if its handlers fail, the whole bot's command routing is in question.

**Coverage detail:** Tests verify KV contract (Put/Get round-trip) but **never invoke the actual handler closures** via bot.SendMessage or with real Telegram Update objects.

---

### 3. **Util Module Handlers (0% coverage)**
**Files:** `internal/modules/util/util.go` + `internal/modules/util/help.go`, `info.go`, `stickerid.go`  
**Functions untested:**
- `infoCommand` (info.go:15): /info handler — never tested
- `helpCommand` (help.go:92): /help handler — RenderHelp (100% tested) but handler closure untested
- `stickerIDCommand` (stickerid.go:21): /stickerid handler and its `stickerFrom` helper (0% coverage)

**Why it matters:** /help is critical for user onboarding. A nil registry or missing module would break silently.

---

### 4. **Firestore Integration Tests Skipped on CI**
**Files:** `internal/storage/firestore_kv_test.go`  
**Status:** 5 out of 11 Firestore tests skip when `FIRESTORE_EMULATOR_HOST` unset (standard CI environment)

**Tests skipped:**
- `TestFirestoreKV_PutGetRoundTrip`: Basic round-trip (skipped)
- `TestFirestoreKV_GetMissingReturnsErrNotFound`: ErrNotFound mapping (skipped)
- `TestFirestoreKV_PutGetJSON`: JSON marshal/unmarshal (skipped)
- `TestFirestoreKV_DeleteIdempotent`: Delete semantics (skipped)
- `TestFirestoreKV_ListByPrefix`: Prefix iteration (skipped)

**What IS tested on CI:** Only validation (key format, reserved names) — happy path ops untested.

**Risk:** Any breakage in firestore.Client.Get, Put, Delete, List surfaces only in production. Module-level KV operations (recordResult, loadGame, etc.) invoke these untested paths.

---

### 5. **Loldleemoji Handlers (0% coverage on handler layer)**
**Files:** `internal/modules/loldleemoji/` (New + handlers for loldleemoji_* commands)  
**No handlers_test.go exists.** State and render tested in isolation; command dispatch untested.

**Untestable seams:**
- Handler returns `error` but no tests verify error propagation to bot.SendMessage
- Concurrent state mutations via keylock not tested in handler context

---

## Quality Analysis: Test Patterns & Brittle Areas

### ✅ Strengths

1. **Keylock mutex tests are excellent** (keylock_test.go:16–73)
   - Distinct keys don't block (timing test, 40ms timeout)
   - Same key serializes correctly (32 goroutines, 100 iterations each, atomic counter)
   - No flakes observed in race detector runs

2. **Table-driven tests properly structured** (e.g., validate_test.go, modules/registry_test.go)
   - Consistent naming: TestXxx_CaseName/CaseName
   - Subtests enable per-case failure isolation

3. **Mock-light design:** Most tests use real in-memory KVStore (storage.NewMemoryKVStore())
   - Avoids mock divergence from prod Firestore
   - Catches JSON serialization bugs

4. **Telegram webhook tests** (telegram/webhook_test.go) test auth + parsing
   - Secret constant-time comparison verified
   - Oversized body rejected (5 MB limit)
   - Malformed JSON rejected

### ⚠️ Concerns

1. **Handler functions never called in tests**
   - Handlers take `context.Context`, `*bot.Bot`, `*models.Update` but tests only exercise KV layer
   - `reply()` helper never invoked; any bot.SendMessage error would be silent in tests
   - Telegram API reply format never validated in tests (unlike JS version which tests reply text)

2. **Missing nil checks on traversals**
   - `msg.Chat.Type` assumes msg.Chat exists but only msg == nil is guarded
   - `msg.From` checked in subjectFor but could be nil in other paths (argAfterCommand doesn't check)
   - No test for `msg == nil` in update dispatch

3. **Storage KV contract untested for operations actually used**
   - `GetJSON`, `PutJSON`, `Delete` have 0% or near-0% coverage in Firestore impl
   - MemoryKVStore covers them but divergence possible if JSON marshal logic differs
   - No concurrent Put+Get race test on same key (only keylock, not KV semantics)

4. **Context cancellation edge cases**
   - Handlers have `ctx` but no tests cancel mid-operation
   - Firestore ops check context but integration tests don't verify timeout handling
   - /cron/{name} has 6-minute timeout; no tests stress it

5. **No end-to-end integration test**
   - No test that spins up full bot + registry + storage + Telegram client
   - Config loading (splitCSV, envForModules, secretEnvKeys stripping) never tested
   - main() is untestable as written (flag parsing, signal handling, blocking ListenAndServe)

6. **Firestore emulator-only on local dev**
   - CI doesn't run `make test-emulator` (if it exists)
   - List() operation (storage/firestore_kv.go:170) only tested with prefix validation; actual iteration untested
   - Delete semantics untested in CI

---

## Error Handling & Edge Cases Audit

### Well-Tested ✅
- Module name validation (kebab-case, hyphens, reserved names)
- Command name validation (lowercase, 1–32 chars)
- Firestore key validation (no `/`, `.`, `..`, `__x__`)
- Prefix validation in List
- HTTP cron auth (constant-time secret check)
- Cron name validation (regex-enforced)

### Untested or Partial ⚠️
- **Empty/nil inputs:** msg == nil tested in guard clauses but not invoked
- **JSON errors:** decode failures in GetJSON not tested; Firestore variant untested
- **Concurrent mutations:** keylock tested in isolation; concurrent handler invocations on same subject not tested
- **Context timeout:** handlers accept ctx but no tests cancel it during KV ops
- **KV errors mid-transaction:** startFresh writes game state; if saveGame fails, state is inconsistent—not tested
- **Oversized payloads:** JSON encode limit not tested (if target word is huge)
- **HTTP chunked encoding:** webhook handler reads body size; chunked/streaming untested
- **Signal handling:** graceful shutdown in main.go untestable

---

## Race Detector Results

**Command:** `go test -race ./...` (10 seconds per pkg with race instrumentation)

**Result:** ✅ **PASS** — No data races detected.

**Tested concurrent patterns:**
- Keylock per-key mutual exclusion (keylock_test.go)
- Nil RNG usage in wordle/daily_test.go:TestPickRandom_NilRNGIsRaceFree — safe to use shared rand.Rand without lock

**NOT stress-tested by race detector:**
- Concurrent handler invocations (handlers not tested)
- Firestore client concurrent Get/Put (emulator skipped on CI)
- In-memory KV concurrent access under module handlers (only in isolation)

---

## Performance Observations

- **Test execution time:** ~0.08s total for all test suites (fast ✅)
- **Slowest package:** wordle (0.083s) — mostly from LoadWords embedding validation
- **No slow tests:** All tests complete in <0.1s individually

---

## Untestable Seams (By Design or Complexity)

| Seam | Reason | Impact |
|------|--------|--------|
| `main()` in cmd/server | Entry point; signal handling, HTTP server startup | Cannot test startup sequence, config loading, provider selection |
| `telegram.Client` | External Telegram API | Mocked/stubbed; prod connectivity untested |
| `firestore.Client` | Requires emulator or GCP creds | Skipped on CI; only validation tested |
| `http.Server.ListenAndServe` | Blocking; requires real port | Tested via httptest (router_test.go) instead |
| Command/Cron handler closures | Dispatch layer tested but handler bodies not | Handlers never invoked with real Update objects |

---

## Test Organization Quality

**Good:**
- Separate *_test.go files per module (loldle_test.go split into compare_test, render_test, state_test)
- Helper functions (noopCmd, noopCron, buildRegistry) reduce duplication
- Unique collection names in Firestore tests prevent cross-test pollution

**Could improve:**
- No golden files or snapshot tests for render output (render_test.go uses string comparison)
- No helpers for common handler test patterns (would reduce untested handler gap)
- No test utilities for building Update objects (telegram/webhook_test.go builds them manually)

---

## Coverage Gaps: Specific File:Line References

### `internal/modules/wordle/handlers.go`
- **30–47** `subjectFor`: Guard clauses on msg == nil, msg.From == nil never executed
- **51–60** `argAfterCommand`: Empty string, no space, space handling — only indirectly tested via state layer
- **64–73** `rejectMessage`: Case coverage complete in lookup tests but not in handler context
- **77–83** `reply`: Never invoked; if bot.SendMessage returns error, handler would fail silently
- **121–189** `handleWordle`: Main flow untested — 0% coverage
- **193–221** `handleNew`: New round initiation untested
- **225–253** `handleGiveup`: Idempotency, giveup stat recording untested
- **256–284** `handleStats`: Win rate calculation (math.Round) never exercised with actual wins/losses

### `internal/modules/misc/misc.go`
- **42–60** `pingCommand` handler closure: Best-effort KV write, bot.SendMessage never tested
- **62–85** `mstatsCommand` handler closure: GetJSON error path, time formatting untested
- **87–100** `fortytwoCommand` handler closure: Simple but never invoked

### `internal/modules/util/util.go` & helpers
- **12–19** `New`: Factory returns module but handlers never invoked
- **help.go:92** `helpCommand` handler: RenderHelp 100% but handler dispatch untested
- **info.go:15** `infoCommand` handler: 0% coverage
- **stickerid.go:21** `stickerIDCommand` handler: 0% coverage
- **stickerid.go:70** `stickerFrom` helper: 0% coverage

### `internal/modules/loldle/handlers.go` (not listed in reports but inferred)
- `handleLoldle`, `handleGiveup`, `handleStats`, `handleSetMax`: Handlers untested
- Same pattern as wordle — state/render tested in isolation, handler dispatch missing

### `internal/modules/loldleemoji/` (similar pattern)
- Handlers exist but never tested

### `internal/storage/firestore_kv.go`
- **87–114** `Get`: Only validation tested; actual Get + snap.DataAt untested on CI
- **115–126** `GetJSON`: 0% coverage in Firestore; MemoryKVStore covers JSON but divergence possible
- **127–150** `Put`: 33% (validation only); actual Put untested
- **142–151** `PutJSON`: 0% coverage
- **152–169** `Delete`: 0% coverage in Firestore
- **170–204** `List`: 11% (validation + prefixSuccessor); actual iteration untested

### `internal/storage/kv_provider.go`
- **24–39** Provider constructors: 0% coverage (factories in main.go select them, untestable)

### `cmd/server/main.go`
- **51–118** `main`: Entry point untestable (signal handling, server startup)
- **125–152** `buildProvider`: Config → storage backend selection untested; Firestore vs memory fallback untested
- **165–186** `loadConfig`: Environment parsing untested

### `internal/server/router.go`
- **45–51** `New`: Router construction untested directly (tested via handlers but not New itself)

---

## Recommendations by ROI

### Critical (Do First)

1. **Add wordle handler integration tests** (3–4 hours)
   - Create wordle/handlers_test.go with bot.Bot mock
   - Test all 5 handlers: handleWordle, handleNew, handleGiveup, handleStats + subjectFor edge cases
   - Mock bot.SendMessage to verify reply text (win/loss messages, error cases)
   - **Impact:** 20–25% coverage gain in wordle; blocks production safety gate

2. **Add misc handler integration tests** (1–2 hours)
   - Create misc/handlers_test.go exercising pingCommand, mstatsCommand, fortytwoCommand
   - Verify KV write side effects + bot reply
   - **Impact:** 10% coverage gain in misc; validates framework end-to-end

3. **Add firestore emulator to CI** (2–3 hours)
   - Docker Compose setup or Cloud Emulator in GitHub Actions
   - Run full Firestore test suite on every push
   - **Impact:** 10–15% coverage gain in storage; catches Firestore-specific bugs

### High (Do Next)

4. **Add util handler tests** (1–2 hours)
   - Test infoCommand, helpCommand, stickerIDCommand with mock bot
   - Verify /help output with various registries
   - **Impact:** 15% coverage gain in util

5. **Add loldle/loldleemoji handler tests** (2–3 hours)
   - Same pattern as wordle handlers
   - **Impact:** 20% coverage gain in loldleemoji, 10–15% in loldle

6. **Add nil-safety tests for all handler guards** (1 hour)
   - Test Update.Message == nil path
   - Test Message.Chat == nil path
   - Test Message.From == nil path
   - **Impact:** Covers edge cases, prevents silent failures

### Medium (Nice to Have)

7. **Add context cancellation tests** (2–3 hours)
   - Handlers accept ctx; test timeout during KV ops
   - Verify error propagation (no silent drops)
   - **Impact:** Resilience; currently untested

8. **Add main() integration test** (2–3 hours)
   - Separate testable config loading from entry point
   - Test buildProvider logic, config parsing
   - **Impact:** Catches startup bugs; currently 0% coverage

9. **Add performance benchmarks** (1–2 hours)
   - Benchmark CompareChampions, CompareWords (game-critical paths)
   - Benchmark keylock contention under high concurrency
   - **Impact:** Prevent performance regression

### Refactoring (Enables Testing)

10. **Extract handler helpers into testable functions** (1 hour)
    - Handlers are closures over `state`; extract reply/error logic into package functions
    - Allows testing reply paths without mocking bot
    - **Impact:** Simplifies handler tests; current pattern requires bot mock

11. **Create test utilities for Update builders** (1 hour)
    - Helpers for newPrivateMessage, newGroupMessage, newChannelMessage
    - Reduces boilerplate in handler tests
    - **Impact:** Enables test proliferation

---

## Unresolved Questions

1. **Is cmd/server/main.go intentionally untestable?** Should it be refactored to extract testable config/provider logic, or is it acceptable as-is since deployment validates startup?

2. **Are Firestore emulator tests run in CI?** The skip message says "CI does not run emulator today" — is there a `make test-emulator` target or separate CI job?

3. **Should handlers be tested via bot.Bot mock or integration test with fake Telegram?** Current approach tests KV contract; mocking bot is simpler but less realistic.

4. **Are there performance requirements for handler latency?** No benchmarks present; cloud function cold start may be critical.

5. **Is context cancellation during KV ops handled gracefully?** Handlers don't check ctx.Done(); is this intentional fire-and-forget, or a gap?

6. **Should private emoji/loldle handler (/loldle_setmax, easter eggs) be tested?** Currently 0% coverage on private commands.

---

## Summary: Coverage by Category

| Category | Tested | Untested | Gap |
|----------|--------|----------|-----|
| Unit logic (compare, lookup, state) | ✅ | — | 0% |
| KV contract (round-trip, not found) | ✅ (memory) | Firestore ops | 40% |
| Validation (names, keys, formats) | ✅ | — | 0% |
| **Handler dispatch** | ⚠️ (registry OK) | **Handler bodies** | **100%** |
| **Bot replies** | ❌ | **All handler text responses** | **100%** |
| HTTP routing | ✅ | — | 0% |
| Concurrent access | ✅ (keylock, RNG) | **Handler concurrency** | **50%** |
| Error propagation | ✅ (isolation) | **Composite errors** | **50%** |
| Firestore integration | ✅ (emulator) | **CI coverage** | **100%** |
| Main/bootstrap | ❌ | **Entry point, config** | **100%** |

---

## Final Assessment

**Overall Quality:** Good unit test foundation; weak integration coverage.

**Biggest Risk:** Handler layer has 0% coverage — a broken game flow (e.g., msg.From nil, savegame failure) would only surface in production. This is the #1 blocker for confidence.

**Second Risk:** Firestore ops untested on CI; any change to Get/Put logic or connection handling is unvalidated until production.

**Actionable Path:** Implement wordle, misc, util handler tests (4–5 hours total) → coverage jumps to 55–60%. Add Firestore emulator CI (2–3 hours) → 65–70%. Current test architecture is solid; just needs handler-layer extension.

**Status:** ✅ DONE_WITH_CONCERNS — All tests pass, no races, but coverage is below acceptable threshold and handler layer is entirely untested.
