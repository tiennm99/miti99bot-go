---
phase: 1
title: Implement deploynotify package + main.go hook
status: completed
priority: P3
effort: 1h
dependencies: []
---

# Phase 1: Implement deploynotify package + main.go hook

## Overview

Create `internal/deploynotify/` — a small package that compares the
baked-in `gitSHA` against a `last_notified_sha` value in KV and DMs the
bot owner if (and only if) it changed. Wire it from `cmd/server/main.go`
right after `modules.Install`.

## Requirements

**Functional**
- Send exactly one Telegram DM to `BOT_OWNER_ID` per *new* gitSHA observed.
- On subsequent cold starts with the same SHA, send nothing.
- Skip silently when: `gitSHA` empty (local build), `BOT_OWNER_ID == 0`,
  or KV operation fails.

**Non-functional**
- Never panic; never return an error that aborts startup.
- ≤3s wall time on the happy path (network DM + 1 KV read + 1 KV write).
- No new env vars, no new IAM permissions (DynamoDB read/write already
  granted to the partition).

## Architecture

```
cmd/server/main.go
    │
    │  after modules.Install(b, reg, auth):
    ├─→ deploynotify.Run(ctx, deploynotify.Config{
    │       Bot:       b,
    │       KV:        provider.For("deploynotify"),
    │       OwnerID:   cfg.BotOwnerID,
    │       GitSHA:    gitSHA,          // package-level var, ldflags-injected
    │       Timeout:   3 * time.Second,
    │   })
    │       │
    │       ├─ skipReason() short-circuit  (no SHA / no owner)
    │       ├─ kv.GetJSON("last_notified_sha", &prev)
    │       ├─ if prev.SHA == gitSHA → return (silent)
    │       ├─ bot.SendMessage(owner, "🚀 miti99bot deployed: <code>SHA</code>")
    │       └─ kv.PutJSON("last_notified_sha", {SHA: gitSHA, At: now})
```

KV namespace: `deploynotify` (new partition, isolated from module data).
Key: `last_notified_sha`.
Value shape:
```go
type notifyRecord struct {
    SHA string `json:"sha"`
    At  int64  `json:"at"` // ms-since-epoch, for debug only
}
```

Telegram message (plain text — no parse_mode dependency on formatting
edge cases):
```
🚀 miti99bot deployed: <SHORT_SHA>
```

## Related Code Files

- Create: `internal/deploynotify/deploy_notify.go` (snake_case per Go conventions)
- Create: `internal/deploynotify/deploy_notify_test.go`
- Modify: `cmd/server/main.go` — declare `var gitSHA string`, add
  `deploynotify.Run(...)` call after `modules.Install(b, reg, auth)` and
  before the `go func() { srv.ListenAndServe() }()` block.

## Implementation Steps

1. **Create `internal/deploynotify/deploy_notify.go`** with:
   - `type Config struct { Bot *bot.Bot; KV storage.KVStore; OwnerID int64; GitSHA, Timeout }`.
   - `func Run(ctx context.Context, cfg Config)` — fire-and-forget, no
     error return; all failures logged via `internal/log`.
   - Internal helper `shouldNotify(ctx, kv, sha) (bool, error)` so dedup
     is unit-testable without a real Telegram bot.
   - Internal helper `markNotified(ctx, kv, sha) error`.
   - Internal `renderMessage(sha string) string` — single line, easy to test.

2. **Wire into `cmd/server/main.go`**:
   - Add `var gitSHA string` at package level (alongside `factories()`).
   - After `modules.Install(b, reg, auth)` and the existing `log.Info("modules loaded", ...)`:
     ```go
     deploynotify.Run(rootCtx, deploynotify.Config{
         Bot:     b,
         KV:      provider.For("deploynotify"),
         OwnerID: cfg.BotOwnerID,
         GitSHA:  gitSHA,
         Timeout: 3 * time.Second,
     })
     ```
   - Import: `"github.com/tiennm99/miti99bot/internal/deploynotify"`.

3. **Tests** (`deploy_notify_test.go`):
   - `TestShouldNotify_FirstRun` — empty KV → returns true.
   - `TestShouldNotify_SameSHA` — KV holds current SHA → returns false.
   - `TestShouldNotify_DifferentSHA` — KV holds old SHA → returns true.
   - `TestRun_SkipsWhenSHAEmpty` — gitSHA="" → no KV access, no send.
   - `TestRun_SkipsWhenNoOwner` — OwnerID=0 → no KV access, no send.
   - `TestRenderMessage_ContainsSHA` — output includes the SHA.
   - Use `storage.NewMemoryKVStore()` for KV.
   - For the Telegram send path: skip end-to-end Telegram tests — the
     existing `testutil.RecordingBot` pattern is heavier than needed.
     Cover send via an indirection: `Config.sender` field of type
     `func(ctx, chatID, text) error` defaulting to `b.SendMessage`
     wrapper. Tests inject a recorder.

## Success Criteria

- [ ] `go build ./...` succeeds.
- [ ] `go test ./internal/deploynotify/...` passes.
- [ ] `go test ./...` passes (no regressions).
- [ ] `cmd/server/main.go` still under reasonable size; deploynotify call
      adds ≤6 LOC.
- [ ] Manual review: a `Run` invocation with empty SHA touches neither KV
      nor Telegram (traceable in code, not just behaviour).

## Risk Assessment

| Risk | Mitigation |
|---|---|
| Cold-start storm sends duplicate DMs (2+ instances boot concurrently after deploy) | Documented in plan Out-of-Scope; race window narrow; failure mode is annoyance not corruption. |
| KV write succeeds, Telegram send fails | Order is reversed: send first, write only on send success. So a failed send doesn't permanently silence retries. |
| KV read fails (DynamoDB throttle) | Treat as "not notified yet" → fall through to send. Worst case: extra DM. |
| Test for Run-with-bot leaks a goroutine | Run is synchronous (uses Timeout via context.WithTimeout). No goroutine spawned. |

## Security Considerations

- Owner ID is already in env; no new secret material.
- Telegram message body contains only the short git SHA — public
  information (the repo is public). No env/secrets leak risk.
- KV partition `deploynotify` is read/write under existing IAM scope.
