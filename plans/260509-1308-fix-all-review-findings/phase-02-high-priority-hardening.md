---
phase: 2
title: "High-priority hardening"
status: completed
priority: P1
effort: "2-3h"
dependencies: [1]
---

# Phase 2: High-priority hardening

## Overview
Pre-public-launch security/reliability fixes: env allowlist (H1), panic recovery (M9), visibility enforcement (M2), cron timeout reduction (M4), 413/400 header bug (M3), emoji HTML escape (M7). These four items are the gate before exposing the bot publicly.

## Requirements
- Functional: protected commands gated by admin check; panicking handler does not trigger Telegram retry storm; future API keys do not auto-leak to all modules.
- Non-functional: defense-in-depth at trust boundaries.

## Architecture

### Env allowlist (H1)
Replace `secretEnvKeys` denylist with explicit allowlist, opt-in per module via `MODULE_<NAME>_*` convention. Module declares required keys; `Build` filters env to only declared keys.

### Visibility enforcement (M2)
Two-tier dispatcher gate:
- `VisibilityProtected` → require `update.Message.From.ID` ∈ `ADMIN_USER_IDS` env (comma-separated).
- `VisibilityPrivate` → bot-owner-only (single ID).
- `VisibilityPublic` → unchanged.

Cheaper than per-chat admin lookup; defer Telegram `getChatMember` call to a future iteration if needed.

### Panic recovery (M9)
Wrap `b.ProcessUpdate` in `defer recover()` inside `webhook.go` handler. Log panic, return 200, prevent retry storm.

### Cron timeout (M4)
Lower `defaultCronTimeout` from 5m to 60s. Document long-running cron pattern (publish to PubSub, exit fast).

### Header-shadow fix (M3)
Detect `MaxBytesError` separately from generic decode errors; do not call `http.Error` after MaxBytesReader has already written 413.

### Emoji HTML escape (M7)
`html.EscapeString(emojis)` at `loldleemoji/render.go:20`.

## Related Code Files
- Modify: `cmd/server/main.go` — replace `secretEnvKeys` with allowlist resolver
- Modify: `internal/modules/module.go` — add `RequiredEnv []string` field on `Module`
- Modify: `internal/modules/registry.go` — filter env per module
- Modify: `internal/modules/dispatcher.go` — visibility gate
- Modify: `internal/telegram/webhook.go` — panic recovery + MaxBytesError handling
- Modify: `internal/server/timeouts.go` — cron timeout 5m → 60s
- Modify: `internal/modules/loldleemoji/render.go` — html.EscapeString
- Modify: `internal/modules/loldle/loldle.go`, `internal/modules/loldleemoji/loldleemoji.go` — declare protected commands need admin
- Test: `webhook_test.go`, `dispatcher_test.go`, `registry_test.go`

## Implementation Steps

### 1. Env allowlist
1. Add `RequiredEnv []string` to `Module`.
2. In `registry.Build`, build `Deps.Env` from `intersect(os env, mod.RequiredEnv)`.
3. Delete `secretEnvKeys` (no longer needed; nothing leaks by default).
4. Update misc/util/loldle/loldleemoji modules — none currently need env, declare empty.
5. Tests: assert unrelated env var does not appear in `Deps.Env`.

### 2. Visibility enforcement
1. Add `ADMIN_USER_IDS` env parsing in `loadConfig` → `[]int64`.
2. Add `BOT_OWNER_ID` env parsing → `int64`.
3. In `dispatcher.Install`, before invoking handler, check `cmd.Visibility`:
   - `Private`: require `update.Message.From.ID == BOT_OWNER_ID`
   - `Protected`: require `update.Message.From.ID ∈ ADMIN_USER_IDS`
   - Else: proceed
4. Reject denied calls silently (no reply — avoid leak that protected command exists).
5. Tests: protected command from non-admin returns no-op; from admin proceeds.

### 3. Panic recovery in webhook
At `internal/telegram/webhook.go:58`:
```go
func() {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("webhook handler panic: %v", r)
        }
    }()
    b.ProcessUpdate(ctx, &update)
}()
```
Test: register a handler that panics; assert webhook returns 200 and no goroutine leaks.

### 4. Lower cron timeout
`internal/server/timeouts.go:8`: `defaultCronTimeout = 60 * time.Second`. Update doc comment.

### 5. Header-shadow fix
At `internal/telegram/webhook.go:49-54`, check `errors.As(err, &maxBytesErr)`:
```go
var maxBytesErr *http.MaxBytesError
if errors.As(err, &maxBytesErr) {
    // 413 already written by MaxBytesReader
    return
}
http.Error(w, "bad request", http.StatusBadRequest)
```
Update `TestWebhookHandler_RejectsOversizedBody` to assert exact 413 status.

### 6. Emoji escape
`render.go:20`: `clue := "🎭 " + html.EscapeString(emojis)`.

## Success Criteria
- [x] Env allowlist: `Deps.Env` is empty by default; denylist + `envForModules` deleted. Phase 07 will add per-module allowlist plumbing.
- [x] Future `GEMINI_API_KEY` cannot auto-leak (no env flows by default)
- [x] Non-admin caller of Protected/Private commands silently denied via `Auth.Permits` in dispatcher
- [x] Handler that panics → 200 to Telegram, stack logged via `runtime/debug.Stack()` (test: `TestWebhookHandler_RecoversPanicAndReturns200`)
- [x] Cron handler timeout = 60s (`internal/server/timeouts.go:8`)
- [x] Oversized webhook body returns clean 413 (`*http.MaxBytesError` branch, test rewritten with valid-prefixed JSON)
- [x] Emoji string `html.EscapeString` in `loldleemoji/render.go:28`
- [x] All existing tests pass; `Auth.Permits` table-driven test added; panic-recovery test added

## Risk Assessment
- **Risk:** Visibility gate breaks dev workflow if `ADMIN_USER_IDS` unset → default to "deny all protected/private when env unset" with a startup warning. Bot owner must set env explicitly.
- **Risk:** Panic recovery hides bugs → still log full stack trace via `runtime/debug.Stack()` so Cloud Logging captures it.
- **Risk:** 60s cron timeout is too aggressive for a future heavy cron → document escape hatch via `Cron.Timeout` override field.

## Security Considerations
- Visibility gate uses constant-time comparison? Not needed — IDs are small ints, equality check is fine.
- Panic recovery must NOT echo internal error to user — only log server-side.
- Env allowlist prevents future leak class entirely.

## Next Steps
Phase 03 (helper extraction) can run in parallel after Phase 02 lands; they touch different files.
