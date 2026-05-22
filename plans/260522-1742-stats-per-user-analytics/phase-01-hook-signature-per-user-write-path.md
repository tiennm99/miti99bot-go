---
phase: 1
title: Hook signature + per-user write path
status: completed
priority: P1
effort: 1h
dependencies: []
---

# Phase 1: Hook signature + per-user write path

## Overview

Pass the Telegram update into the `CommandHook` so the stats module can attribute invocations to users, and extend `stats.counter.Inc` to write the new per-user and per-pair keys.

## Requirements

- Functional: every authorized command invocation increments `count:<cmd>`, `user:<userID>`, and `pair:<cmd>:<userID>`. Username field on `user:<userID>` is refreshed each call.
- Functional: when `From.Username` is empty, skip the `user:`/`pair:` writes; `count:<cmd>` still increments.
- Non-functional: hook still runs detached from request context (2s timeout, see `internal/modules/dispatcher.go:73`). Existing race on read-modify-write is accepted.
- Non-functional: write fan-out stays within the hook's 2s budget. Three writes execute concurrently.

## Architecture

`CommandHook` is the only cross-module hook that needs to evolve. Today's signature `func(ctx context.Context, name string)` drops sender info before stats sees it. New signature `func(ctx context.Context, name string, update *models.Update)` is minimal, idiomatic, and only one current implementation (stats) needs to adapt.

Write fan-out lives inside `counter.Inc` and uses a `sync.WaitGroup` of three goroutines mirroring the existing render-side pattern at `internal/modules/stats/stats.go:94-109`. Errors are logged and swallowed (best-effort, same as today).

## Related Code Files

- Modify: `internal/modules/module.go` — update `CommandHook` type.
- Modify: `internal/modules/dispatcher.go` — pass `update` to `RunCommandHooks`.
- Modify: `internal/modules/registry.go` — adjust `RunCommandHooks` signature and storage.
- Modify: `internal/modules/stats/stats.go` — update `Inc` signature, add per-user write fan-out, change `countEntry` and add `userEntry`.

## Implementation Steps

1. In `module.go`, change `CommandHook` type to `func(ctx context.Context, name string, update *models.Update)`. Update the field comment.
2. In `registry.go`, locate `RunCommandHooks`. Add `update *models.Update` parameter and forward to each registered hook.
3. In `dispatcher.go:73-76`, pass `update` into `reg.RunCommandHooks(hookCtx, cmdCopy.Name, update)`. The goroutine already captures `update` in scope.
4. In `stats.go`:
   - Define `userEntry struct { Username string \`json:"username"\`; N int64 \`json:"n"\` }`.
   - Add helpers `userKey(id int64) string` returning `"user:" + strconv.FormatInt(id, 10)` and `pairKey(cmd string, id int64) string` returning `"pair:" + cmd + ":" + strconv.FormatInt(id, 10)`.
   - Change `Inc` signature to `func (c *counter) Inc(ctx context.Context, name string, update *models.Update)`.
   - Inside `Inc`: keep the existing `count:<cmd>` increment. If `update != nil && update.Message != nil && update.Message.From != nil && update.Message.From.Username != ""`, fan out two more increments under a single `sync.WaitGroup`:
     - `user:<id>` — GetJSON, set `Username` to current value, `++N`, PutJSON.
     - `pair:<cmd>:<id>` — GetJSON, `++N`, PutJSON.
   - Wrap the `count:` increment in the same fan-out so all three writes run in parallel.
5. Run `make vet test` from repo root. No new test deps in this phase (tests in Phase 3).

## Success Criteria

- [ ] `make vet` clean.
- [ ] `make test` passes (existing stats tests will need a minor signature update — fix as part of this phase or defer to Phase 3 if straightforward).
- [ ] `go build ./...` produces no errors.
- [ ] Manual trace: handler invocation in dispatcher passes update through to `stats.Inc`; debug log line in `Inc` confirms user attribution path.

## Risk Assessment

- **Risk:** Existing `internal/modules/stats/stats_test.go` constructs the counter directly and may call `Inc(ctx, "name")`. Fix call sites in the same phase.
- **Risk:** Other modules might register a `CommandHook` in the future and break. Mitigation: search the codebase for `CommandHook:` assignments before merging — currently stats is the only user.
- **Risk:** Concurrent invocations of the same command by the same user lose updates (read-modify-write race). Accepted, documented at `stats.go:35-37`.
