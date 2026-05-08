# Code Review: Phase 02 (partial) + Phase 03 — Repo Bootstrap & Module Framework

**Date:** 2026-05-08
**Scope:** `cmd/server/`, `internal/{server,telegram,modules,storage}/`, Dockerfile, CI, README
**Spec:** `plans/260508-2222-go-port-cloud-run/phase-{02,03}.md`
**Build status:** `go vet ./...` green, `go test -race -count=1 ./...` green

---

## TL;DR

Solid scaffolding. Test coverage on registry + prefix wrapper is good. **Three real bugs** that will bite Phase 04+:

1. `bot.Start(ctx)` is wrong for webhook mode (will spam Telegram with 409s).
2. Cron handler receives **unprefixed** Deps, breaking per-module KV isolation for crons.
3. Webhook secret-token compare is not constant-time.

Plus several things to harden before any module ships in Phase 05.

---

## Critical

### C1 — `bot.Start(ctx)` runs long-polling against an active webhook
**File:** `cmd/server/main.go:64` and comment at lines 61–63.

The comment claims `Start` "enables long-polling-style internals the library uses for handler matching cleanup." That is wrong. Reading `vendor/github.com/go-telegram/bot/bot.go:137` and `get_updates.go`:

- `Start` spawns `getUpdates` (a long-poll loop calling Telegram's `getUpdates` API) and `waitUpdates` workers.
- When a webhook is registered, Telegram returns **HTTP 409 Conflict** for every `getUpdates` call. The library will log the error via `defaultErrorsHandler` and retry with exponential backoff capped at 5s. **Forever.** Visible noise in Cloud Run logs and wasted egress.
- `waitUpdates` only services the `b.updates` channel. Our webhook calls `b.ProcessUpdate(...)` directly, bypassing the channel entirely, so the workers idle.

`Start` is doing nothing useful in webhook mode. Remove it. Either:
- Drop the goroutine altogether (handler dispatch via `ProcessUpdate` does not require workers).
- Or use `bot.WithWebhookSecretToken(...)` + `b.StartWebhook(ctx)` + `b.WebhookHandler()` and feed via the library's channel (more idiomatic, but requires reworking our handler).

Smallest fix: delete line 64.

### C2 — Cron dispatcher hands handlers the **base** Deps, not per-module prefixed Deps
**Files:** `internal/modules/cron_dispatcher.go:11–17`, `internal/server/router.go:56`, `cmd/server/main.go:35,48`.

The flow:
1. `main.go` builds `deps := modules.Deps{KV: kv, ...}` with the **unprefixed** in-memory KV.
2. `Build()` constructs each module with a *prefixed* `moduleDeps` (registry.go:76–80). Good.
3. The cron handler (a closure inside the factory) **may** capture the prefixed `moduleDeps.KV` — fine for closures.
4. But `DispatchScheduled(ctx, name, reg, deps)` is called with the **base, unprefixed** `deps` (router.go:56), and it passes this to `cron.Handler(ctx, deps)`.

So a Phase 05 module that writes the idiomatic way:

```go
Cron{Handler: func(ctx context.Context, deps modules.Deps) error {
    return deps.KV.Put(ctx, "last_run", []byte(time.Now().String()))
}}
```

…writes to the **bare** key `last_run`, colliding with every other module's cron. Two modules running daily crons will silently overwrite each other.

The signature *implies* `deps` is the channel by which the handler gets its dependencies. Either:
- `DispatchScheduled` reconstructs prefixed deps from the cron owner (use `cronOwners` map, currently thrown away after Build).
- Or remove `Deps` from `CronHandler` signature and document "capture deps via factory closure". Risky footgun.

Recommend the first option: add `cronOwners` to `Registry` and have `DispatchScheduled` re-prefix.

### C3 — Webhook secret compare is not constant-time
**File:** `internal/telegram/webhook.go:35`.

```go
if r.Header.Get(secretTokenHeader) != secret || secret == "" {
```

Go's `!=` on strings short-circuits on first byte mismatch. An attacker observing response timing across many requests can recover the secret byte-by-byte (classic CWE-208).

Fix:
```go
got := r.Header.Get(secretTokenHeader)
if secret == "" || subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
    http.Error(w, "unauthorized", http.StatusUnauthorized)
    return
}
```

Telegram secrets are also rate-limited at the network edge, so the practical risk is moderate, but the fix is one line and `crypto/subtle` is stdlib.

---

## High

### H1 — `/cron/{name}` is fully unauthenticated
**File:** `internal/server/router.go:36–66`.

Phase 09 plans OIDC auth, but until then this endpoint is **public on the internet**. Anyone who knows a cron name (and they're guessable: `daily`, `wordle_reset`, etc.) can:
- Trigger billable side effects (Firestore writes, Gemini API calls in later phases).
- DoS the instance: each request locks an instance for up to 5 minutes (`defaultCronTimeout`). Cloud Run free tier scales to ≤1 concurrent instance per the plan; a handful of attacker requests pin the bot.

Until Phase 09 lands, at minimum:
- Require a shared secret header (e.g. `X-Cron-Token` env var) and reject if absent. Cheap, removes the public-discovery risk.
- Or gate behind an `ENABLE_CRON_ENDPOINT` env flag (default false) so dev environments don't expose it.

### H2 — Webhook `r.Context()` cancels mid-handler (async dispatch + immediate 200)
**Files:** `internal/telegram/webhook.go:49–50`; library `process_update.go:31` confirms async-by-default.

`b.ProcessUpdate(r.Context(), &update)` runs the handler in a fresh goroutine (`go r(ctx, b, upd)`). The webhook then writes 200 and returns, after which the HTTP server cancels `r.Context()`. Any handler that does network I/O (Telegram API calls, Firestore reads) will get `context.Canceled` from its `ctx`.

This is invisible today (no commands shipped), but the moment Phase 05 lands a real handler doing `b.SendMessage(ctx, ...)`, calls will fail intermittently — and worse, only when the webhook returns *fast* (the race is tighter for cheap handlers).

Fix: pass a detached context to `ProcessUpdate`. Go 1.21+ has `context.WithoutCancel`:
```go
ctx := context.WithoutCancel(r.Context())
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel() // will not be hit before goroutine — see note below
b.ProcessUpdate(ctx, &update)
```

Note: because `ProcessUpdate` spawns a goroutine, you cannot `defer cancel()` here — the goroutine outlives the request. Better: `bot.WithNotAsyncHandlers()` so the handler runs synchronously *and* the webhook returns 200 only after dispatch completes (acceptable since Telegram allows up to 60s before retry). Or wrap in our own goroutine with a timeout that lives in main's context tree.

### H3 — Webhook body read is unbounded
**File:** `internal/telegram/webhook.go:41`.

`json.NewDecoder(r.Body).Decode(...)` reads until the body closes. A malicious client (we already 401'd unauthorized requests, but the body is read… wait, no — body is read *after* the auth check, so this is mostly an authorized-Telegram concern). Still, defensive:

```go
r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
```

Telegram updates with media are bounded; 1 MiB is plenty.

### H4 — Webhook secret WARN should be a fatal at startup
**File:** `internal/telegram/webhook.go:27–28`.

If `TELEGRAM_WEBHOOK_SECRET` is empty, the handler logs once and rejects all requests. The bot is broken but the process keeps running, returning 401 to every Telegram update. Telegram retries fail. Better: `log.Fatal` at startup if secret is empty (force-fail-fast) — `cmd/server/main.go:22-24` already has the same shape for the bot token.

### H5 — All env vars (including secrets) leak to every module via `Deps.Env`
**File:** `cmd/server/main.go:91–106`, `internal/modules/module.go:62`.

`loadConfig` builds `envMap` from the entire process environment, then stuffs it into `deps.Env`. `TELEGRAM_BOT_TOKEN` and `TELEGRAM_WEBHOOK_SECRET` are in there. Every module factory receives the full bag. A future module that logs `deps.Env` (for debugging) — or worse, reflects it into a Telegram message — leaks the bot token. With Firestore creds and Gemini keys arriving in Phase 04/07, this surface only grows.

Fix: redact known-sensitive keys before passing to modules, or restructure `Deps.Env` to be per-module-allowlisted (`MODULES` env names a module; that module gets only its own `MODULE_<NAME>_*` vars).

Minimum acceptable today: hardcode a deny-list:
```go
for _, k := range []string{"TELEGRAM_BOT_TOKEN", "TELEGRAM_WEBHOOK_SECRET"} {
    delete(envMap, k)
}
```

### H6 — Module names from `MODULES` env are not validated; `:` in a name breaks KV isolation
**Files:** `cmd/server/main.go:110–122`, `internal/storage/prefix.go:11–16`, `internal/modules/registry.go:69`.

`splitCSV` only trims whitespace. `Prefixed(s, "a:b")` produces prefix `"a:b:"`. A separate module `"a"` writes to key `"b:foo"` → underlying key `"a:b:foo"` — the same as module `"a:b"` writing to `"foo"`. Two modules sharing storage breaks the isolation invariant the framework otherwise enforces.

Either:
- Validate module names at `Build` start (same regex as commands: `^[a-z0-9_]{1,32}$`).
- Or assert factory map keys conform at package init (every key in `Factories` is checked).

### H7 — `bot.New` blocks 5s on Telegram API at startup
**File:** `internal/telegram/client.go:10`.

`bot.New(token)` calls `GetMe` synchronously (library `bot.go:91–99`, 5s default timeout) unless `WithSkipGetMe()` is passed. On a Cloud Run cold start with transient network blip, the bot **fails to start**, the container exits, and Cloud Run retries — a self-DoS during Telegram API outages. Cold-start budget is 500ms (per phase 02 spec). 5s eats that 10x over.

Pass `bot.WithSkipGetMe()` in production. Token validity is independently asserted on the first outgoing call.

---

## Medium

### M1 — Duplicate names in `MODULES` env produce confusing "command conflict" error
**File:** `internal/modules/registry.go:69–113`.

`MODULES="alpha,alpha"` calls `factories["alpha"]` twice. Second iteration's commands collide with first → `command conflict: /a1 defined in "alpha" and "alpha"`. The user sees the same name twice and has to deduce the cause.

Fix: dedupe `enabled` first, or fail fast with `fmt.Errorf("duplicate module %q in MODULES", name)`.

### M2 — Spec deviation in `DispatchScheduled` (concurrent errgroup) not documented
**Files:** spec phase-03 step 6 (errgroup; multiple modules can claim same name); `cron_dispatcher.go:11`.

Phase 03 spec says crons run "across all modules concurrently (errgroup)". Current implementation enforces cron-name uniqueness across modules (`cronOwners` in registry.go:106), then runs the single matched handler. This is a stronger invariant — fine — but worth a comment in `cron_dispatcher.go` so the next reader doesn't reintroduce errgroup. (The user's deviation list called out factory shape and module map; cron uniqueness is undocumented.)

### M3 — Single-handler `WriteTimeout` straddles webhook (fast) and cron (slow) routes
**File:** `cmd/server/main.go:57`.

`WriteTimeout: 6 * time.Minute` accommodates the cron path. The webhook path inherits this 6-minute leash; if a handler hangs (nil deref + library async dispatch), the request stays open 6 minutes, holding a Cloud Run instance against its concurrency cap (free tier: 1 inflight request per instance per the plan).

Use per-handler `http.TimeoutHandler` wrapping just `/webhook` at, say, 10s. Cron stays at the server-level 6-minute cap.

### M4 — Log injection via cron name (and update fields)
**Files:** `internal/server/router.go:52,61`.

`name` comes from `r.URL.Path` (already %-decoded by net/http). Validation only blocks `/`. Newlines, ANSI escapes, etc. would let an attacker forge log lines. Log shippers like Cloud Logging treat each line as a JSON record; a smuggled `\n{"severity":"CRITICAL",...}` line would forge a fake event.

Fix: validate `name` against `^[a-z0-9_]{1,32}$` (same regex as commands). 404 anything else. Bonus: fail-fast on attempts to dispatch unregistered crons before any logging.

### M5 — Dead code: `errors.Is(err, http.ErrBodyReadAfterClose)`
**File:** `internal/telegram/webhook.go:42–44`.

`http.ErrBodyReadAfterClose` is documented as "returned by Read after the body has been closed", which only happens if the handler reads `r.Body` post-return. `json.Decoder` reads synchronously inside the handler; this branch is unreachable. Remove it (or replace with a generic decode-error log if observability matters).

### M6 — `CommandHandler` returning `error` is decorative
**Files:** `internal/modules/module.go:27`, `internal/modules/dispatcher.go:22–26`.

The library's `bot.HandlerFunc` is `func(ctx, *Bot, *Update)` — no error return. Our wrapper logs `cmdCopy.Handler` errors and that's it. The `error` return offers no flow control: no retry, no metrics, no Telegram-side signal. It's a comfortable pattern for handler authors but creates the illusion of error handling.

Either:
- Document explicitly in `Command.Handler` godoc: "returned errors are logged and otherwise ignored; do not rely on them to retry or alert".
- Or hook in a metrics counter / error reporter at the central log site so the field has teeth.

### M7 — `Registry.AllCommands` is a public mutable map
**File:** `internal/modules/registry.go:14`.

Build returns a `*Registry` whose `AllCommands` field is a `map[string]Command`. Callers can mutate it post-construction. The dispatcher relies on Build-time validation; a downstream `reg.AllCommands["evil"] = Command{...}` after `Install(b, reg)` is silent because handlers are already registered with `bot`, but anyone reading `reg.AllCommands` for a `/help` listing would see ghost commands. Either lock fields private + expose accessor, or document the invariant.

---

## Low

### L1 — `mod.Name` is overwritten after factory returns
**File:** `internal/modules/registry.go:81`.

`mod.Name = name` clobbers whatever the factory set. Today factories set `Name: name` themselves (`registry_test.go:34`); the assignment is defensive but undocumented. The factory's `Module.Name` field becomes effectively meaningless. Either drop the field from `Module` (it's the registry key, not a module property) or document the override.

### L2 — `MemoryKVStore.List` on empty prefix returns *all* keys
**File:** `internal/storage/memory_kv.go:68–79`.

`strings.HasPrefix(k, "")` is always true. Through `Prefixed(_, "modA").List(ctx, "")` this reduces to `inner.List("modA:")` which is correct, but raw `MemoryKVStore.List(ctx, "")` returns the whole keyspace. If a test or smoke runner uses the bare store, it will see everything. Document or refuse empty prefix.

### L3 — `Registry.Cron` returns `Cron` by value but also stores by value
**File:** `internal/modules/registry.go:31–34`.

Fine functionally. If `Cron` ever grows to hold mutable state (rate limiter, last-run timestamp), value-copy semantics will surprise the next author. Worth a comment that `Cron` is value-typed deliberately.

### L4 — `splitCSV` empty-vs-empty edge: `MODULES=,` returns `nil`, `MODULES=` returns `nil`, `MODULES=alpha,,beta` returns `["alpha","beta"]`
**File:** `cmd/server/main.go:110–122`.

Behaviorally fine (skip blanks), but no test exercises any of these paths. One table test would lock it down.

### L5 — `ErrCronNotFound` declared with `fmt.Errorf` instead of `errors.New`
**File:** `internal/modules/cron_dispatcher.go:20`.

`fmt.Errorf("cron not found")` works but doesn't use a format directive. `errors.New` is the more idiomatic choice and signals "this is a sentinel".

---

## Nit

### N1 — `Module.Init` field referenced in spec but absent from code
The spec (phase-03.md:64) lists `Init` as an optional field on `Module`. Implementation drops it (factory-takes-deps pattern eliminates the need). Already covered in user's deviation list (#1) — no action needed, but cross-reference in `module.go` godoc would help future readers.

### N2 — `ErrCronNotFound` could include the registry's known cron names in the error
For diagnostics, errors at the dispatcher layer could list available names: `cron %q not found; available: [a, b, c]`. Phase 09 will add proper observability; minor today.

### N3 — `cmd/server/main.go:42–43` log line reads cleanly but accesses `reg.Crons()` which sorts on every call
Currently called once at startup, no perf concern. Worth a comment that `Crons()` allocates a sorted copy if downstream code starts calling it per-request.

### N4 — Dockerfile has no healthcheck
`distroless/static:nonroot` has no shell, so `HEALTHCHECK CMD wget …` won't work without adding a busybox layer. Cloud Run uses HTTP startup probes anyway, so this is fine; no action.

---

## Test Gaps (would catch a regression)

Existing tests cover registry conflict + validation, prefix round-trip, prefix-list-strip. Missing:

1. **`TestWebhookHandler_*`** (no test file for `internal/telegram/`):
   - Rejects non-POST → 405.
   - Rejects missing/wrong secret → 401.
   - Rejects malformed JSON → 400.
   - Accepts valid update → 200, `b.ProcessUpdate` invoked exactly once.
   - Constant-time compare verified by parametric byte-flip table (won't *prove* timing-safe but locks the API choice).

2. **`TestCronHandler_*`** (no test for `internal/server/router.go`):
   - 405 on GET, 404 on `/cron/`, 404 on `/cron/foo/bar`, 404 on unknown name (`ErrCronNotFound` → 404 mapping).
   - Path-traversal smoke: `/cron/../etc` cleaned by net/http to `/etc`, never reaches handler. Worth a test that confirms.
   - Log-injection guard: `/cron/foo%0Abar` → 404 (after M4 fix).

3. **`TestMemoryKVStore_Concurrent`** — race-free when many goroutines Put/Get simultaneously. Trivial; pairs with `-race` in CI.

4. **`TestPrefixedStore_PrefixCollision`** — module `"foo"` writing key `"o:bar"` does *not* show up in `Prefixed(_, "foo:o").List("bar")`. Locks the H6 invariant once names are validated.

5. **`TestBuild_DedupesModules`** — once H6/M1 fixed, assert the chosen behavior (error vs silent dedupe).

6. **`TestDispatchScheduled_PrefixedDeps`** — once C2 fixed, write a cron handler that puts to `deps.KV` and assert the underlying base store contains the prefixed key.

7. **`TestInstall_RegistersAllCommands`** — assert `b.RegisterHandler` is called for each command. Library's `Bot` exposes `handlers` only privately; can wrap in a small test helper that registers a deterministic-match handler and feeds an update through `ProcessUpdate`.

---

## Positive Observations

- Clean package layout, idiomatic small interfaces (`KVStore` is the right shape).
- `Prefixed` wrapper is a tidy 50-line solution; tests prove round-trip + isolation.
- Validation error messages mention the offending module — saves debugging time.
- `splitCSV` handles whitespace + empty entries gracefully without external libs.
- Dockerfile is correct: distroless + nonroot + `-s -w` + `CGO_ENABLED=0`. 6.4 MB binary is well under the 15 MiB target.
- CI runs `-race -count=1` from day one.
- Spec deviations are documented in code comments (factory shape, factory map). Easy for next reviewer.

---

## Recommended Action Order

| # | Severity | Action |
|---|----------|--------|
| 1 | C1 | Delete `go b.Start(rootCtx)` in main.go:64 |
| 2 | C2 | Fix cron dispatcher to pass per-module-prefixed Deps |
| 3 | C3 | Replace webhook secret compare with `subtle.ConstantTimeCompare` |
| 4 | H1 | Gate `/cron/{name}` behind shared-secret env check until Phase 09 |
| 5 | H2 | Detach context for `ProcessUpdate` (or switch to `WithNotAsyncHandlers`) |
| 6 | H5 | Strip secrets from `Deps.Env` |
| 7 | H6 | Validate module names with the command regex |
| 8 | H7 | `bot.New(token, bot.WithSkipGetMe())` |
| 9 | H3 / H4 | `MaxBytesReader` + `log.Fatal` on empty secret |
| 10 | M-class | Address as time allows; M2/M5 are doc-only |

C1, C2, C3 are blocking — they will manifest as production incidents the moment Phase 05 ships any real module.

---

## Unresolved Questions

1. **OIDC vs shared-secret for cron:** Phase 09 plans OIDC. Is a temporary shared-secret bridge acceptable, or should `/cron/*` be entirely disabled (404'd at the router) until Phase 09 lands? The latter is safer; the former lets Phase 05 modules write crons that can be smoke-tested locally.
2. **Async vs sync handler dispatch:** the library defaults async, we want bounded handler execution. Is `bot.WithNotAsyncHandlers()` the agreed pattern, or do we accept the goroutine-per-update model and need a separate concurrency cap? Affects H2's resolution.
3. **`Module.Name` field:** keep for symmetry with JS port, or drop now that it's unused? Affects L1 + future module authoring docs.
4. **`Deps.Env` policy:** redact known-sensitive keys (cheap), or restructure to per-module allowlist (correct)? H5's resolution depends on this.

---

**Status:** DONE_WITH_CONCERNS
**Summary:** Scaffolding is sound; three real bugs (long-poll-vs-webhook, unprefixed cron Deps, non-constant-time secret compare) must land before Phase 05 ships any module.
