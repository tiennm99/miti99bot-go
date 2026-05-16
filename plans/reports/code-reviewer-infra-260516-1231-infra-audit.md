# Infra audit — 260516-1231

## Summary
- 14 findings: 12 safe, 2 I/O-changing
- Overall health: solid. Hot paths (webhook, cron, KV) are well-bounded with timeouts, constant-time secret checks, panic recovery, and strong reads. The biggest themes are (a) log-middleware loses the `req` line on panics and (b) a few error-handling/cleanup gaps in the migration CLI. No data-loss or auth-bypass bugs spotted in the in-scope code.

## Safe findings (behavior-preserving — assistant will apply)

- **[high] `LogRequests` drops `req` line on downstream panic** — `internal/server/log_middleware.go:39-51`
  - What: `next.ServeHTTP` is called inline; if it panics, the trailing `log.Info("req", …)` never runs.
  - Why it matters: 5xx-rate alert filter parses `msg=req status>=500`; panics in cron handlers (no recover at that layer) silently disappear from the dashboard.
  - Fix sketch: move the `log.Info("req", …)` into a `defer` and add a `recover()` that re-panics after logging (or sets status=500). Status downstream tooling parses is preserved.

- **[high] cron handler has no panic recovery** — `internal/server/router.go:56-94`
  - What: `modules.DispatchScheduled` runs without a `defer recover()`. Webhook recovers (`telegram/webhook.go:77-83`); cron does not.
  - Why it matters: a panic in a cron handler tears down the goroutine, `http.Server` writes nothing, the `req` line above is lost, and EventBridge sees a connection reset → SQS DLQ entry without a useful log.
  - Fix sketch: mirror the webhook's `func(){ defer recover() … }()` block around the dispatch call. Log panic+stack, return 500.

- **[high] `kv-import` skips already-imported keys without explicit guard once a partition has any item** — `internal/migration/dynamodb_writer.go:39-61` (verify)
  - What: I initially flagged `attribute_not_exists(pk)` as wrong because pk is shared per module. **Re-verified against AWS docs**: DynamoDB evaluates `ConditionExpression` against the specific item identified by the request's `Key` (pk, sk). The condition is correct as written.
  - Why it matters: no bug; documenting the verification so a future reviewer doesn't "fix" it.
  - Fix sketch: add a one-liner comment near the condition stating "evaluated per (pk,sk) item, not partition-wide".

- **[medium] `runTradingAuditDump` leaks `f.Close` error and silently writes incomplete files** — `cmd/migrate_cf_data/main.go:194-206`
  - What: `defer func(){ _ = f.Close() }()` ignores Close's error; if disk fills mid-write `enc.Encode` returns but Close's flush error never surfaces.
  - Why it matters: audit dumps are evidence; an aborted Close yields a truncated JSONL the operator believes is complete.
  - Fix sketch: shadow `err` in a deferred closer, return `errors.Join(returnedErr, closeErr)`.

- **[medium] `logDispatch` slices mid-UTF-8 codepoint** — `internal/telegram/webhook.go:106-108`
  - What: `text[:dispatchTextPreview]` cuts at byte 64; Vietnamese / emoji messages produce invalid UTF-8 in the `text` log attr.
  - Why it matters: log line still ships (slog escapes), but the preview is unreadable garbage for the most common bot audience.
  - Fix sketch: walk back to the previous rune boundary; `utf8.RuneStart` check or `utf8.DecodeLastRuneInString` until valid.

- **[medium] webhook panic-recover may double-WriteHeader** — `internal/telegram/webhook.go:76-86`
  - What: after `recover()`, handler writes 200 unconditionally. If the panicking handler already called `w.WriteHeader`/`w.Write`, the second `WriteHeader(200)` logs a `superfluous response.WriteHeader` warning.
  - Why it matters: log noise + the actual response code Telegram sees may not be 200.
  - Fix sketch: gate the trailing `w.WriteHeader(http.StatusOK)` behind a `statusRecorder`-style check (set a flag inside recover), OR document that handlers must not touch the http response.

- **[medium] `PerUserLimiter` map grows unbounded per Lambda instance** — `internal/ai/ratelimit.go:19-53`
  - What: `buckets[subject]` is never evicted. Distinct user IDs across the lifetime of a Lambda instance accumulate `*rate.Limiter` entries (~120 B each).
  - Why it matters: Lambda recycles long before this matters in practice (similar logic to `keylock`'s memo), but it's worth a one-line comment matching the keylock rationale so a future reviewer doesn't re-flag it.
  - Fix sketch: copy the eviction-deferred rationale from `keylock/keylock.go` package comment into ratelimit.go.

- **[medium] migration CLI uses `context.Background()` with no SIGTERM handling** — `cmd/migrate_cf_data/main.go:89,131,189` & `cmd/migrate_cf_data/convert_value.go:31`
  - What: every subcommand runs against `context.Background()`. Operator Ctrl-C kills mid-paginate; a partial Scan in `convert-value-to-string` leaves the table in a half-converted state visible to the runtime.
  - Why it matters: idempotent on retry, but the operator has no clean-stop affordance.
  - Fix sketch: `signal.NotifyContext(context.Background(), SIGINT, SIGTERM)` at top of each runner.

- **[low] `subtle.ConstantTimeCompare` leaks length** — `internal/server/router.go:69`, `internal/telegram/webhook.go:49`
  - What: when lengths differ, function returns 0 immediately without scanning bytes. Length itself is observable via timing.
  - Why it matters: secrets are fixed-length (SSM-managed), so length is not actually attacker-controlled. Not exploitable; noting it so the docstring around constant-time checks reflects reality.
  - Fix sketch: no code change; one comment line at the call site clarifies "constant-time within same-length inputs; length is a constant of the deploy".

- **[low] `DynamoDBKVStore.List` returns `nil` (not empty slice) when no items match** — `internal/storage/dynamodb_kv.go:139-177`
  - What: declared `var keys []string`; never initialized to `[]string{}`. Empty result is nil.
  - Why it matters: `FirestoreKVStore.List` (firestore_kv.go:184) has the same nil-return shape, so it's consistent. But callers that do `if keys == nil` get the same as `len == 0`, so semantics are fine; flag only because callers using `keys[:]` on a nil get a nil — usually fine in Go but worth a doc line in `KVStore.List` saying "may return nil; len(keys)==0 is the canonical empty check".
  - Fix sketch: docstring tweak on `KVStore.List` in `kv_store.go:13-20`.

- **[low] `Report.Format` ignores writer errors** — `internal/migration/report.go:41-66`
  - What: every `fmt.Fprintln`/`fmt.Fprintf` return is discarded with `_`.
  - Why it matters: comment at line 39-40 says "callers pass os.Stdout or *bytes.Buffer" — for stdout, a broken-pipe (operator piped to `head`) silently swallows the rest of the report.
  - Fix sketch: short-circuit on first write error; return that error from `Format`. Caller in `main.go` can `os.Exit(1)` on report-write failure.

- **[low] `prefixedStore.List` allocates `make([]string, len(keys))` even when `keys == nil`** — `internal/storage/prefix.go:45-55`
  - What: harmless (`make([]string, 0)` returns non-nil empty slice). Diverges from raw store return shape (nil vs empty). Tests pass; just an inconsistency.
  - Why it matters: cosmetic; flag only if you want backend-vs-prefixed parity.
  - Fix sketch: early-return `nil, nil` when inner returned `nil, nil`.

## I/O-changing suggestions (need user approval — DO NOT apply)

- **[medium] `cmd/server/main.go` writes `WriteTimeout: 6 * time.Minute` while `template.yaml` sets Lambda Timeout=30s** — `cmd/server/main.go:141`
  - What: HTTP server allows 6-minute writes; Lambda kills the container at 30s.
  - Why it matters: under Lambda this never fires; under non-Lambda (local server) it allows 6-min slow-loris writes. Comment says "6 min accommodates /cron/{name}" but cron is already capped at 60s in `internal/server/timeouts.go:9`.
  - Fix sketch: align HTTP `WriteTimeout` with the cron timeout (~75s) + slack.
  - User/operator impact: changes the server's slow-client behavior on the non-Lambda path; doesn't affect prod Lambda. Worth confirming before tightening.

- **[medium] webhook 413 / 400 / 401 responses leak internal error strings to Telegram** — `internal/telegram/webhook.go:45,50,62,65`
  - What: `http.Error(w, "method not allowed", ...)`, `"unauthorized"`, etc. Telegram retries on non-2xx, so these strings are not user-visible. **But** Lambda Function URL is `AuthType: NONE` per `template.yaml:129` — anyone on the internet can probe `/webhook`. Strings reveal "this is a real Telegram bot endpoint."
  - Why it matters: trivial fingerprinting, not a real risk. Mention only because deeper hardening (always return 200 with empty body on auth fail, log the rejection) would reduce noise from random scanners.
  - Fix sketch: collapse all rejection paths to a single bare 401 with empty body.
  - User/operator impact: changes response shape ops dashboards may rely on; the 413 distinction in particular is documented as deliberate ("Telegram + ops dashboards can distinguish body-too-big"). Worth user call.

## Architecture observations (no immediate change)

- Storage layer cross-provider value parity (string vs binary) is deliberate per recent commit and exercised by the `convert-value-to-string` migration tool. Validated, not undone.
- `MemoryProvider` shares one underlying `MemoryKVStore` across modules with `Prefixed` isolation; `DynamoDBProvider` uses partition-per-module. Both bypass the prefix layer's empty-prefix panic by validating module name through `collectionNameRe` before construction. Solid.
- `keylock.Map` and `ai.PerUserLimiter` both rely on Lambda recycle frequency to bound memory. Same rationale, different files — worth a one-paragraph note in `docs/system-architecture.md` so future reviewers find the pattern documented once.
- `firestoreInitTimeout` (10s) lives in main.go but the package comment in `internal/storage/firestore_client.go` is silent on cold-start budget. Cohesion is fine; mention only if you ever move this to a constants file.

## Unresolved questions

- Should the webhook handler's panic-recover path emit a 5xx so Telegram retries? Current behavior is 200 (per code comment, intentional — "do not trigger 24-hour retry storm"). Verify with the team that the trade-off (drop the poisoned update vs retry forever) still holds for AI handlers where a transient panic may be recoverable.
- `Default` package-level `metrics.Registry` is shared across goroutines; tests reset it via `Default.snapshot()`. Is the global ergonomics still desired, or should `cmd/server/main.go` construct and inject a registry? Current state has implicit shared state that's hard to swap in tests other than the test that owns the metrics package.

**Status:** DONE
**Summary:** 14 findings on the in-scope infra layer (12 behavior-preserving, 2 needing operator nod). Highest-leverage fixes: deferred req-log in middleware, panic recovery in cron handler, UTF-8-safe truncation in dispatch logging.
