# Whole-project architecture & code-quality review

**Date:** 2026-05-09
**Scope:** every Go file in `cmd/` + `internal/`, plus `Dockerfile`, `Makefile`, `.github/workflows/ci.yml`, `go.mod`. Phases 02–06a landed; per-phase reports already cover their sub-cooks. This pass focuses on **cross-cutting** issues those sub-cooks could not see.
**Build/test status at review time:** `go vet ./...` clean; `go test -race -count=1 ./...` clean (10 pkgs). Local toolchain `go1.26.2`.
**Skipped (already in prior reports):** winRate truncation (5c), defaultRNG race (5b), info nil-deref (5a), %q-vs-JS (6a), per-module renderBoard tests (5c-M1), loldle helper extraction (6a-Medium), unbounded keylock map size (keylock package doc), all C/H from phase 02-03 review (every fix landed).

---

## TL;DR

Two real shipping blockers no phase report could catch because they cross phase boundaries:

1. **Dockerfile build will fail** — `golang:1.23-alpine` cannot satisfy `go.mod`'s `go 1.25.0` directive without a `toolchain` line. Phase-02's build was logged as green when the local toolchain was 1.23-compatible; the project bumped go.mod since.
2. **Three `update.Message` nil-derefs** in shipped modules (`misc.go:54,79,94`, `util/help.go:100`) — same shape as the /info bug Phase 5a fixed, just in commands that 5a's review didn't touch. JS source has the same latency; Go panics on nil deref where JS just throws and the framework swallows.

Plus a **drift cluster** of three near-identical helpers across four modules (subjectFor, argAfterCommand, nowMillis, normalize, reply, replyHTML) — the 6a report flagged this for 6b prep, but the **subjectFor variants are not byte-equivalent** (wordle vs loldle differ on the channel-with-no-From edge). Either drift will produce a real divergence the next time someone "fixes" only one copy, or 6b extracts now and the drift goes away.

The rest is hygiene.

---

## Critical

### C1 — Dockerfile builder image is older than go.mod's `go` directive

**Files:** `Dockerfile:1`, `go.mod:3`

```
Dockerfile: FROM golang:1.23-alpine AS builder
go.mod:     go 1.25.0
```

Go's `go.mod` `go N` directive is a hard floor: the toolchain refuses to build with `go.mod requires go >= 1.25.0`. Without a `toolchain` line in go.mod, the 1.23 image cannot auto-download 1.25 (auto-toolchain only fires when `toolchain go1.X.Y` is declared). This means **every `docker build`** today fails — and the CI image (`actions/setup-go` with `go-version: '1.23'` in `.github/workflows/ci.yml:19`) will fail too the next time it runs.

Why no prior review caught it: Phase 02 review pinned the Dockerfile contents at a time when go.mod said `go 1.23`; whoever bumped to 1.25 did not also bump the Dockerfile / CI matrix.

**Fixes (pick one):**

a. Bump `Dockerfile` builder to `golang:1.25-alpine` and `.github/workflows/ci.yml` `go-version` to `'1.25'`. Cleanest.
b. Lower `go.mod` to `go 1.23` (or whatever version is actually required by the dependencies — `cloud.google.com/go/firestore v1.22.0` only needs 1.22+).
c. Add `toolchain go1.25.0` to go.mod and rely on auto-download. Slowest cold builds in CI; not recommended.

Recommend (a). Verify nothing in the codebase actually needs 1.25 features (no `min`/`max`/`clear`/etc. usage I could spot, so (b) is also safe).

### C2 — Three `update.Message` nil-derefs in shipped modules

**Files:**
- `internal/modules/misc/misc.go:54` (/ping)
- `internal/modules/misc/misc.go:79` (/mstats)
- `internal/modules/misc/misc.go:94` (/fortytwo)
- `internal/modules/util/help.go:100` (/help)

Every one writes `update.Message.Chat.ID` without first checking `update.Message != nil`. Phase 5a's review caught the same shape in `info.go:36` and the fix landed there (`if msg == nil { return nil }`). The pattern was not propagated. With `bot.HandlerTypeMessageText` + `bot.MatchTypeCommand` the dispatcher only fires on text-message updates today, so `update.Message` is non-nil in practice — but:

- The infosec-style "untrusted external input" boundary lives at the webhook decoder. A malformed Telegram payload that decodes into a partially-populated `models.Update` still satisfies `MatchType` matching at the library level but can leave `Message == nil`. Library-level guarantees here are thin.
- Future visibility-aware dispatch (callbacks, edited-messages) will route through different handler types, and the same factory-supplied `Command.Handler` may be reused. The /info fix already captured this in a comment; misc and util/help did not get the same hardening.
- Defensive cost is one line per handler.

**Fix:** add `if update.Message == nil { return nil }` (or equivalent guard) at the top of each handler. Cheaper than a test, and matches the pattern Phase 5a established.

---

## Major

### J1 — Helper-function drift across four modules; subjectFor variants are NOT byte-equivalent

**Files:**
- `internal/modules/wordle/handlers.go:30-47` — `subjectFor`
- `internal/modules/loldle/handlers.go:33-46` — `subjectFor`
- `internal/modules/loldleemoji/handlers.go:35-48` — `subjectFor`

Phase 6a flagged "extract `normalize`, `subjectFor`, `argAfterCommand`, `findChampion` for 6b prep". The flag was right but undersold the urgency:

```go
// wordle (handlers.go:30-47)
switch msg.Chat.Type {
case models.ChatTypePrivate:
    if msg.From != nil { return strconv.FormatInt(msg.From.ID, 10) }
case models.ChatTypeGroup, models.ChatTypeSupergroup:
    return strconv.FormatInt(msg.Chat.ID, 10)
default:
    if msg.From != nil { return strconv.FormatInt(msg.From.ID, 10) }
}
return ""

// loldle and loldleemoji (handlers.go:33-46 / 35-48)
switch msg.Chat.Type {
case models.ChatTypeGroup, models.ChatTypeSupergroup:
    return strconv.FormatInt(msg.Chat.ID, 10)
default:
    if msg.From != nil { return strconv.FormatInt(msg.From.ID, 10) }
}
return ""
```

Functionally equivalent **today** because Telegram populates `From` on every private DM. But:

- For `ChatTypeChannel` with no `From` (anonymous channel post), wordle returns `""` and loldle/emoji also return `""` — same. ✓
- For `ChatTypePrivate` with `From == nil` (which Telegram never does, but the type system allows), **wordle** returns `""` and **loldle/emoji** also return `""` via the default branch. Identical.

The risk isn't current behavior; it's that **someone editing one copy to fix a bug will forget to edit the other two**. Phase 5c found exactly this with `winRate` (truncation bug existed in both wordle and loldle; the 5b review fixed wordle and missed loldle, then 5c had to clean up).

Other drift in the same files:
- `argAfterCommand` is byte-identical across wordle / loldle / loldleemoji (3 copies).
- `nowMillis` is byte-identical across wordle / loldle / loldleemoji (3 copies).
- `reply(ctx, b, chatID, text)` (loldle/loldleemoji) vs `reply(ctx, b, msg, text)` (wordle) — slightly different signatures; not interchangeable but the implementation body is duplicated.
- `replyHTML` is byte-identical across loldle / loldleemoji.
- `normalize` is byte-identical across loldle / loldleemoji (and a related `normalizeWord` in wordle that drops digit support — different alphabet, intentional).
- `findChampion` shape is identical across loldle / loldleemoji modulo type names.

**Recommendation:** extract `internal/modules/util/chathelper` (or `internal/champname` for the loldle-specific normalize+findChampion pair). Do it as the **first** commit of phase 6b before any new variant lands; then 6b's quote/ability/splash variants pick up the helpers from a single source and the drift problem disappears. Cost: ~80 LOC of helper + 4 import line changes per module.

A **shared `WinRate(wins, played int) int`** helper should be part of the same extraction — currently 3 copies (wordle/loldle/loldleemoji) all using `math.Round` correctly today, but one drift opportunity per port.

### J2 — Logging is `log.Printf` everywhere; Cloud Logging will not parse it

**Files:** `cmd/server/main.go` (×9), `internal/server/router.go:77,86`, `internal/modules/dispatcher.go:24`, `internal/modules/misc/misc.go:51`. 18 call sites total in non-test code.

Cloud Run forwards `stdout` to Cloud Logging line-by-line. Cloud Logging treats each line as a record but only **parses structured JSON** for severity, trace correlation, and label-based filtering. `log.Printf("cron %s failed: %v", ...)` becomes a single text payload with severity DEFAULT — every alert filter / dashboard / SLO query will need a regex.

Phase 11 plans "Cloud Logging structured JSON" so this is on the roadmap. The concern is: **every call site added until Phase 11 is debt** that has to be migrated. With Phase 11 bumping into Phase 6b/7/8 worth of new modules, the migration target is moving.

**Recommendation:** introduce a tiny `internal/log` (or `internal/obs`) package now with a `WithFields(...)` API that emits JSON lines (Go 1.21+ `slog.JSONHandler` is stdlib, zero deps), and route all current call sites through it. Future modules pick up the structured form for free. ~30 LOC + 18 mechanical edits. Could also be Phase 11's first commit — but the longer it waits, the more sites to mechanically rewrite.

Phase 5a's `misc.go:51` `log.Printf("misc /ping: putJSON failed: %v", err)` is a good motivating example: `module=misc command=ping op=putJSON err=...` as JSON fields makes the eventual error rate dashboard a one-line query; the current shape needs a regex.

Bonus: the `log.Printf("cron name=%s", name)` at `router.go:77` is **PII-adjacent** — `name` is operator-controlled (cron names are validated `^[a-z0-9_]{1,32}$`), so no real injection risk, but Phase 11 plan says "Cloud Logging" and the only thing standing between us and a `log_entry_payload_size_too_large` is hand-discipline.

### J3 — Cron handler chain has no log-injection guard for the **error** branch

**Files:** `internal/server/router.go:86`

`log.Printf("cron %s failed: %v", name, err)` — `name` is regex-validated so it's safe. But `err` is whatever the module returned, which is **module-controlled and may include user input**. A loldle module today does `fmt.Errorf("loldle saveGame: %w", err)` then chains downward; if a future module ever does `fmt.Errorf("user input was %q", argAfterCommand(msg.Text))` and that error bubbles up, the log line gets a newline-bearing user string. CWE-117 (log injection) class.

This is theoretical today (no current handler error-wraps user input). But the bot will only get more user-input-touching modules from here. Phase 11's structured-JSON conversion (J2) makes this naturally safe (JSON encodes newlines as `\n`).

Stopgap until then: `log.Printf("cron %s failed: %s", name, strings.ReplaceAll(err.Error(), "\n", "  "))` — ugly, but bounds the damage. Or **defer to J2** and fix structurally.

### J4 — `update.Message.Chat.ID` is a soft trust boundary that the codebase doesn't enforce

Same shape as C2 but a finer point: `models.Update` is decoded directly from the webhook body via `json.NewDecoder(r.Body).Decode(...)`. We trust Telegram's TLS-authenticated webhook (X-Telegram-Bot-Api-Secret-Token validates the *delivery*, not the *payload* — anyone with the secret can send any payload). With the secret leaked, a forged request with `Message.Chat.ID = -<your-target-chat>` would route a guess into another user's stats / send a sticker into someone else's group. Practical risk is low (secret is only-on-Telegram-server today), but defense-in-depth is cheap:

- Validate `update.UpdateID > 0` (Telegram always positive).
- Validate `update.Message.Chat.ID` non-zero before trusting it.
- Reject `Message.Date` more than ~24h old (replay window).

None of these are urgent. Track in Phase 11 alongside structured logging.

---

## Medium

### M1 — `bot.New` may return error for transient network reasons; main.go treats it as fatal

**File:** `cmd/server/main.go:69-72`

```go
b, err := telegram.NewBot(cfg.TelegramBotToken)
if err != nil { log.Fatalf("telegram bot init: %v", err) }
```

`telegram.NewBot` passes `WithSkipGetMe()` so the only thing left to fail is option-application — which is in-process and deterministic. Today `err` is always nil after argument validation. Slight surprise that `bot.New` can return `error` at all in this path. Not actionable; flagging because future contributors might switch back to the GetMe-blocking variant and assume the fail-fast path handles transients gracefully (it doesn't; Cloud Run will hot-loop restart the container).

Fix: add a comment explaining "with WithSkipGetMe + WithNotAsyncHandlers, bot.New does not perform I/O; this error is unreachable in practice" — or leave alone.

### M2 — Synchronous webhook dispatch holds Cloud Run instance for full handler duration

**Files:** `internal/telegram/webhook.go:56-58`, `client.go:19`

`bot.WithNotAsyncHandlers()` makes handler dispatch synchronous (good — solves H2 from the Phase 02 review). But it means a slow handler (e.g. a `/loldle` first response that does 3 Firestore reads + 1 sticker send) holds the webhook open for hundreds of ms — and Cloud Run min-instance=0 default + 1-concurrent-request-per-instance budget means a queue forms during traffic spikes.

Numbers from real handlers I reviewed:
- `/loldle` first guess: 3 Firestore reads (game, stats, config), 2 Firestore writes (game saveGame, stats put), 1 sticker send, 1 message send. ~250-500ms P95 on warm instance.
- `/wordle` similar.

The `handlerTimeout = 10 * time.Second` cap (`webhook.go:26`) is the right shape but Telegram retries after 60s of no 2xx, so a 10s cap means **on a 10s-stuck handler the user gets a duplicate update (Telegram retry) AND the original eventually completes** — a duplicate-write window during the retry / completion overlap. The keylock per-subject mutex protects same-subject writes, but the **second invocation enters the handler** first and sees state from the abandoned first invocation, possibly skipping the user's intended action.

**Recommendation:** lower handlerTimeout to 8s (gives 2s margin before Telegram retry), and document the retry / duplicate-update pattern as a known limitation. Or: switch to async-with-detached-context per Phase 02 review's H2 alternative ("`context.WithoutCancel(r.Context())` + own goroutine"). Not v1; phase 11 work.

### M3 — `Cron` handler error mapping conflates "handler failed" with "cron not found"

**File:** `internal/server/router.go:81-89`

```go
if err := modules.DispatchScheduled(...); err != nil {
    if errors.Is(err, modules.ErrCronNotFound) { http.NotFound(...); return }
    log.Printf("cron %s failed: %v", name, err)
    http.Error(w, "cron failed", http.StatusInternalServerError)
}
```

500 on handler error is correct behavior (Cloud Scheduler retries 5xx, doesn't retry 4xx). But Cloud Scheduler also has a "max retry attempts" cap that, when exceeded, sends to a dead-letter; with bare 500 there's no way for a handler to signal "do not retry, I poisoned this". Today no handler has this need, but a "the user's quota is exhausted" handler would benefit from returning 4xx-class.

YAGNI today. Worth a `Cron.RetryPolicy` field (or sentinel error like `modules.ErrCronDoNotRetry`) when the first such handler lands. Document in Phase 09's plan.

### M4 — `Module.Name` can be silently overwritten without warning

**File:** `internal/modules/registry.go:119`

```go
mod := factory(moduleDeps)
mod.Name = name  // enforce: module name is its registry key
```

Phase 02-03 review flagged this as L1. Still here. Today: harmless, factories happen to either set `Name: name` themselves or leave it blank. But **if a module factory sets `Name: "different"` for legitimate reasons** (refactor, copy-paste, dynamic name override), that intent is **silently discarded** with no log line. Suggest:

```go
if mod.Name != "" && mod.Name != name {
    return nil, fmt.Errorf("module factory for %q returned mismatched Name=%q", name, mod.Name)
}
mod.Name = name
```

Or drop `Module.Name` entirely (registry already keys by module name; the field is redundant). Either is more honest than "silently overwrite".

### M5 — `firestore_kv.go` → 216 LOC; `loldle/handlers.go` → 334 LOC; `wordle/handlers.go` → 284 LOC; `loldleemoji/handlers.go` → 269 LOC; `loldle/compare.go` → 253 LOC; `cmd/server/main.go` → 211 LOC

Project rule (CLAUDE.md "Consider Modularization"): files >200 LOC should be considered for splitting. Six files exceed.

| File | LOC | Suggested split |
|------|-----|-----------------|
| `loldle/handlers.go` | 334 | Each `handle*` to its own file (handle_loldle.go, handle_giveup.go, handle_stats.go, handle_setmax.go) — most of the 334 is one handler each; helpers like `subjectFor` / `argAfterCommand` move to a sibling file or to the shared package per J1. |
| `wordle/handlers.go` | 284 | Same — handle_wordle, handle_new, handle_giveup, handle_stats. |
| `loldleemoji/handlers.go` | 269 | Same. |
| `loldle/compare.go` | 253 | Year/multi/exact compare functions split into `compare_year.go`, `compare_multi.go` — already cleanly separated by attr type within the file. |
| `firestore_kv.go` | 216 | Validate / prefixSuccessor → `firestore_keys.go`. List / Get / Put / Delete stay together. ~60 LOC migration. |
| `cmd/server/main.go` | 211 | `loadConfig` + `splitCSV` + `envForModules` → `config.go`. `buildProvider` → already a candidate for `provider.go`. main() shrinks to ~80 LOC of orchestration. |

This is a guideline, not a hard rule, and J1's helper-extraction will incidentally pull ~50 LOC out of three handlers.go files — making M5 mostly self-resolving. Recommend doing M5 **after** J1, since J1 mechanically dictates which lines move out first.

### M6 — `loldle/state.go` `getOrInitGame` is identical in shape to `loldleemoji/state.go` `getOrInitGame`; almost identical to `wordle/state.go` `getOrInit`

Three copies of "load existing or start fresh" with tiny variations in:
- whether maxGuesses is dynamic per subject (loldle/emoji yes, wordle no).
- whether StartedAt initialises to nil (loldle/emoji) or now-millis (wordle).

Each module's gameState shape differs enough that a shared interface is awkward, but the **pattern** is so repetitive it screams for extraction. Phase 11 problem; tracking only.

### M7 — `MemoryProvider.Base()` is production code surface area

**File:** `internal/storage/kv_provider.go:35`

Phase 04 review M4 flagged this. Still here. The method is documented "test-only" but is on the public production type. A future module can `provider.(*storage.MemoryProvider).Base()` and bypass module isolation — silently. The phase-04 review suggested moving to a `_test.go` build-tagged file or a `storagetest` helper package. Still recommended; cheap.

---

## Minor

### N1 — `Registry` struct has 5 unexported fields used only internally; `AllCommands` is the lone exported map

`AllCommands` exposed because `dispatcher.Install` needs to iterate it. `publicCmds`/`protected`/`private` are accessed only via getter methods (`PublicCommands()` etc.) which sort+copy. Inconsistency: the dispatcher could equally use a getter. Cosmetic only; small refactor would close out the "callers can mutate AllCommands post-build" vector that Phase 02 review M7 flagged.

### N2 — `FirestoreProvider.For` accepts ANY moduleName without validation

**File:** `internal/storage/firestore_provider.go:22`

The comment says "Module names are validated by modules.Build before reaching here, so we don't sanitize again." True — but the `KVProvider` interface advertises that anyone may construct one. A test using `provider.For("__reserved__")` (Firestore-banned collection name) gets an error from gRPC, not a clean `validateCollection` rejection. Cheap to add a one-line check in `For`. Not blocking.

### N3 — `gameTTLSeconds` (wordle) is unused; flag from Phase 5b L3 still present

**File:** `internal/modules/wordle/state.go:18`

Constant + comment have stale-doc smell. Phase 11 was supposed to add a TTL cron; until then, delete the constant or move the TTL note to a Markdown doc. Compiler doesn't complain (Go ignores unused package-level constants), so it just sits there.

### N4 — `loldle/loldle.go:14` `MaxGuesses = 8` is exported but only used internally; same in `loldleemoji/state.go:15` `MaxGuesses = 5`. Same for `MaxGuessesCap`.

Capitalised constants in domain-private packages with no out-of-package callers. Either lowercase them or move to docs. Style nit.

### N5 — `loldle/state.go:30` and `loldleemoji/state.go:23-26` define `gameState` (lowercase) — unexported. But `loldle/loldle.go:14` defines `MaxGuesses` exported. Mixed casing within the same package suggests no hard convention. Pick one. Style nit.

### N6 — `pickDaily` (wordle/daily.go:34) is unused by handlers but kept "for parity"

Effectively dead code with a passing test. Either wire into a /wordle_daily handler (Phase 06+ work) or delete + delete the test. Same shape as N3.

### N7 — `internal/modules/modules.go` is a 7-line empty-package comment file

Vestigial. Could be folded into `module.go`'s package doc. Or kept. Truly cosmetic.

### N8 — `cmd/server/main.go:184` has misaligned struct-init padding

```go
ModuleEnv:              envForModules(envMap),
```
vs
```go
TelegramBotToken:      envMap["TELEGRAM_BOT_TOKEN"],
```

`gofmt` should rewrite this on save. Probably fine but failing-rule-scope nit.

---

## Architectural observations

### Package boundaries: clean

- `internal/keylock` is a generic primitive, correctly placed at top-level peer to `storage` / `telegram` / `server`.
- `internal/storage` is cleanly factored: `KVStore` interface + two impls + a prefix wrapper. `KVProvider` abstraction is the right shape (modules see `KVStore`, not the provider).
- `internal/modules` framework is clean: registry holds maps, validate gates inputs, dispatchers tie to bot/HTTP. Modules are leaves.
- `cmd/server` is the composition root and owns the catalog (`factories()`). Comment in `internal/modules/modules.go` correctly explains why the catalog cannot live inside `internal/modules`.

### Trust boundaries: mostly enforced

| Boundary | Validation | Gap |
|----------|------------|-----|
| `MODULES` env → registry | `moduleNameRe` regex, dedupe, factory lookup | None |
| Webhook body → handler | `MaxBytesReader(1MiB)`, `json.Decoder` | C2 (nil-deref past decode) |
| Webhook auth | constant-time secret compare | None |
| Cron route → dispatch | `cronNameRe` regex + constant-time secret | None |
| Cron error → log | direct `%v` of internal error | J3 (potential CRLF injection theoretical) |
| Module name → KV provider | `moduleNameRe` regex (no `:`) | N2 (Firestore provider doesn't double-check) |
| KV key → Firestore | `validateKey` thorough | None (Phase 04 review confirmed) |
| User input → reply text | `html.EscapeString` everywhere I checked | None |

### Concurrency: clean across the board

- All mutating handlers use `defer s.locks.Acquire(subject)()`.
- `math/rand` package-level functions are mutex-protected (used everywhere).
- `keylock.Map` uses `sync.Map` correctly.
- `Registry` is read-only post-Build by convention (documented).
- `srv.Shutdown` waits for in-flight handlers; provider closes after shutdown.
- Bot dispatcher is synchronous (`WithNotAsyncHandlers`), so `r.Context()` lives across handler.

`go test -race -count=1` clean on every package.

### Error propagation: mostly clean, two patterns

1. **Module handlers** return `error`; the dispatcher logs and discards. Phase 02-03 review M6 flagged that this is decorative. Still true. Neither metrics nor retry nor user-visible "internal error" reply hooks into the return value. For an early-stage codebase this is fine; mark for Phase 11 observability.

2. **Storage layer** wraps errors with `fmt.Errorf("firestore put %s/%s: %w", ...)` consistently. `errors.Is` checks against `ErrNotFound` and `ErrCronNotFound` — these are the only sentinels. Good.

### Configuration: clean

- All env vars read once in `loadConfig`. Per-module env via `Deps.Env` with explicit deny-list (`secretEnvKeys`). Deny-list is correct shape but requires manual upkeep; Phase 02 review H5's allow-list alternative is still preferable but not blocking.
- No env var read after startup. Good.

### Dead/vestigial code

| Symbol | File | Justification |
|--------|------|---------------|
| `gameTTLSeconds` | `wordle/state.go:18` | Documented in Phase 5b review (L3) |
| `pickDaily` | `wordle/daily.go:34` | Has a test; unused by handlers. (N6) |
| `internal/modules/modules.go` | (entire file) | 7-line empty package doc. (N7) |
| `MemoryProvider.Base()` | `kv_provider.go:35` | Test-only on production type. (M7) |
| `Module.Name` | `module.go:55` | Overwritten by registry; never read by factory. (M4) |

None blocking.

---

## Tests gap (cross-cutting)

Per-phase reports already enumerated module-specific gaps. Cross-cutting gaps:

1. **Webhook handler integration test** — phase-02 review test plan #1 was filed; a `webhook_test.go` file exists. Verified it covers method/secret/decode paths. Confirmed adequate.
2. **Cron handler integration test** — `router_test.go` exists. Verified it covers method/secret/cronNameRe/dispatch paths.
3. **No CI matrix entry for `make build`** — `.github/workflows/ci.yml` only does `go vet`, `go test`, `go build`. Doesn't `docker build` from CI. Combined with C1, that's why no one noticed the Dockerfile/go.mod mismatch. **Add `- run: docker build -t miti99bot-go .` to CI**. ~3 lines.
4. **No emulator-gated tests in CI** — Phase 04 review L1/L3 flagged this. Makefile `test-emulator` exists; CI only runs the no-emulator subset. Acceptable for now (emulator setup adds 30-60s to CI run); track for Phase 10/11.

---

## Recommended action order

| # | Severity | Action | Effort |
|---|----------|--------|--------|
| 1 | C1 | Bump Dockerfile + CI to Go 1.25 (or lower go.mod to 1.23) | 5min |
| 2 | C1 | Add `docker build` step to ci.yml so this never recurs silently | 5min |
| 3 | C2 | Add `if update.Message == nil { return nil }` to misc + util/help handlers | 10min |
| 4 | J1 | Extract shared helpers to `internal/champname` (or similar) as the FIRST commit of Phase 6b | 1-2h |
| 5 | J2 | Introduce `internal/log` with slog.JSONHandler; rewire 18 call sites | 2-3h, or defer to Phase 11 |
| 6 | M5 | Split files >200 LOC into per-handler / per-concern files (after J1) | 1-2h |
| 7 | M4/M7/N* | Hygiene pass — Module.Name guard, Base() to test-tag, dead-code cleanup | 1h |

Items 1-3 are blockers for the next deploy / merge. The rest can ride along Phase 6b/11 as natural cleanup.

---

## Positive observations

- **Trust boundaries enumerated and individually fixed** with constant-time compares + regex-validated routes + strict per-key Firestore validation. Each fix has a comment explaining the threat model. Operationally friendly.
- **`KVProvider` interface is exactly one method** — easy to mock, hard to misuse. Phase 04's review called this out; six modules later, it's still aging well.
- **Per-subject keylock + `WithNotAsyncHandlers` together** turn the goroutine-per-update model into a sequenced-per-subject model. Equivalent guarantee to JS Workers' isolate-per-request. Documented at `keylock/keylock.go:5-12`.
- **Wire-format tests** lock JS-parity for every persisted JSON shape (gameState, stats, roundConfig). `*int64` for nullable timestamps. Defends migration goal.
- **Embed strategy + panic-on-bad-data** consistently applied across wordle/loldle/loldleemoji. Build-time bug surfaces at startup, not on first user.
- **Phase reports themselves**: clear, opinionated, action-ordered. The 6a report's "extract helpers as 6b prep" foresight is exactly the kind of forward-pointing review note that makes the next reviewer's job cheaper.
- **CI does `-race -count=1`** from day one. The single most-valuable lint a Go service can have.
- **Defensive stripping of secrets from `Deps.Env`** via deny-list. Allow-list would be tighter but the deny-list is honest about its limits and tested.
- **`srv.Shutdown(15s) → defer closeProvider()`** ordering is correct and the comment in Phase 04 review M5 is now in code (mostly). Graceful shutdown story is solid.

---

## Unresolved questions

1. **Q1**: Phase 11's "Cloud Logging structured JSON" is the natural home for J2/J3. Bringing it forward to Phase 6b (~2-3h) versus letting it pile up to Phase 11 (~2-4h migration cost) — **which side has the better expected-value tradeoff**? Recommend forward-port: every module added in Phase 6b/7/8 is a J2 caller-site, so the marginal cost of structured-log-from-the-start is lower.
2. **Q2**: J1's helper extraction — into `internal/modules/util/` (already exists, but it's the /info /help /stickerid module) or a fresh `internal/modules/util/chathelper/` subpackage? Or a top-level `internal/champname` for the loldle-specific normalize+findChampion pair? Naming bikeshed; but the choice constrains how Phase 7+ AI modules will reuse the same helpers.
3. **Q3**: C1 — bump go.mod down to 1.23 (zero feature loss) versus bump Dockerfile + CI to 1.25 (more typical, but adds Go-version churn). The codebase doesn't use any 1.24/1.25 features I found. Cheapest is to lower go.mod.
4. **Q4**: M2 — keep `WithNotAsyncHandlers` (synchronous) or switch back to async-with-detached-context per the Phase 02 review's H2 alternative? Synchronous is simpler and matches JS-Worker semantics; async would buy back webhook return latency at the cost of a small goroutine pool. Defer until cold-start / latency telemetry from Phase 11 says one way or the other.

---

**Status:** DONE_WITH_CONCERNS
**Summary:** Architecture and concurrency are solid; two cross-phase blockers (Dockerfile/go.mod version mismatch + nil-deref pattern not propagated to misc/help) need fixing before the next merge, plus a J1 helper-drift cluster that should be extracted as Phase 6b's first commit before five more modules compound the problem.
