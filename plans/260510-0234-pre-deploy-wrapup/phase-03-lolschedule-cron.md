---
phase: 3
title: "lolschedule daily-push cron handler"
status: pending
priority: P2
effort: "3h"
dependencies: []
---

# Phase 03: lolschedule daily-push cron handler

## Overview
Implement the deferred `lolschedule_daily_push` cron — fans out today's match schedule to subscribers at 08:00 ICT. Requires extending `modules.Deps` to expose `*bot.Bot` (current blocker noted in `internal/modules/lolschedule/lolschedule.go:8-12`).

## Requirements
- **Functional:**
  - `lolschedule.Module.Crons()` returns one entry: name `daily_push`, schedule `0 1 * * *` (UTC = 08:00 ICT).
  - Handler reads subscribers, fetches today's matches via existing `api_client.go`, sends formatted message to each chat via `*bot.Bot`.
  - Failed sends per-chat are logged, do not abort the batch (one bad chat doesn't take down the whole push).
- **Non-functional:**
  - Handler completes within Lambda's 30s default timeout for typical subscriber counts (<100). Past that, paginate or move to async.
  - Rate-limit-aware: respect Telegram's 30 messages/sec global cap. For low subscriber counts, no batching needed.

## Architecture
**Deps extension (the real work):**
```go
// internal/modules/module.go
type Deps struct {
    KV       storage.KVStore
    Embedder ai.Embedder
    Chatter  ai.Chatter
    Env      map[string]string
    Bot      *bot.Bot   // NEW — nil-safe; modules check before use
}
```

`*bot.Bot` is already constructed in `cmd/server/main.go` before `modules.Build`. Wire it into `BuildOptions` (typed, like `Embedder`/`Chatter`) and have `modules.Build` thread it into each module's `Deps`. Modules that don't need it ignore it — same pattern as Gemini.

**Cron handler:**
```go
// internal/modules/lolschedule/cron.go (new file)
func (m *Module) dailyPush(ctx context.Context) error {
    if m.deps.Bot == nil {
        return errors.New("lolschedule: daily push requires bot reference")
    }
    subs, err := listSubscribers(ctx, m.kv)
    if err != nil { return err }
    matches, err := m.api.TodayMatches(ctx)  // existing api_client method
    if err != nil { return err }
    msg := formatMatches(matches)            // existing format.go helper

    var sent, failed int
    for _, chatID := range subs {
        if _, err := m.deps.Bot.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: msg}); err != nil {
            log.Warn("lolschedule push failed", "chat", chatID, "err", err)
            failed++
            continue
        }
        sent++
    }
    log.Info("lolschedule daily push complete", "sent", sent, "failed", failed)
    return nil
}
```

## Related Code Files
- Modify: `internal/modules/module.go` — add `Bot *bot.Bot` to `Deps` and `BuildOptions`
- Modify: `internal/modules/registry.go` (or wherever `modules.Build` constructs Deps) — thread `Bot` through
- Modify: `cmd/server/main.go` — pass `b` (already constructed) into `modules.BuildOptions{Bot: b}`
- Create: `internal/modules/lolschedule/cron.go` — `dailyPush` handler + helper
- Modify: `internal/modules/lolschedule/lolschedule.go` — implement `Crons()` returning the registration; remove the deferred-cron comment block at line 8-12
- Create: `internal/modules/lolschedule/cron_test.go` — table tests for handler with mock bot + KV

## Implementation Steps
1. **Deps extension:**
   - Add `Bot *bot.Bot` field to `Deps` and `BuildOptions` (in `internal/modules/module.go`)
   - Update `modules.Build` to copy `BuildOptions.Bot` into each constructed `Deps`
   - Update `cmd/server/main.go` to pass `Bot: b` in the options literal
   - Run `go vet ./...` + `go build ./...` — should be clean (additive change)
2. **lolschedule cron registration:**
   - Add `Crons() []modules.Cron` to `Module`, returning `{{Name: "daily_push", Schedule: "0 1 * * *", Handler: m.dailyPush}}` (verify the exact struct shape from `modules.Cron` definition)
   - Remove the deferred-cron comment block in `lolschedule.go`
3. **Handler implementation:**
   - Write `cron.go` per architecture above
   - Reuse existing `api_client.TodayMatches` and `format.go` helpers (read these to confirm signatures; adjust if needed)
4. **Tests:**
   - Mock `*bot.Bot` via interface or wrapper; assert SendMessage called once per subscriber
   - Cover: happy path (3 subs, all succeed); partial failure (1 of 3 fails, batch continues); empty subscribers (no-op, no error); API failure (returns error)
5. **Wire dispatch:** confirm `internal/modules/cron_dispatcher.go` (or wherever crons are surfaced to `internal/server/router.go`) picks up the new registration without further wiring. Check by hitting `/cron/lolschedule_daily_push` locally with the right secret token; should call the handler.
6. **Local smoke:** `go test ./internal/modules/lolschedule/...` green; manual `curl` against running server with at least one subscriber.

## Success Criteria
- [ ] `modules.Deps.Bot` exposed; nil-safe (modules without it work unchanged)
- [ ] `lolschedule.Module.Crons()` returns one entry
- [ ] `dailyPush` handler implemented per architecture
- [ ] Cron-handler unit tests pass (happy path + partial failure + empty subs)
- [ ] `go vet`, `go build`, full `go test` green
- [ ] Manual `/cron/lolschedule_daily_push` invocation works locally and triggers fan-out

## Risk Assessment
- **Deps extension breaks every module** if not nil-safe — Mitigation: zero-value `*bot.Bot` is `nil`, all current modules ignore the field, additive change. Add a registry test that builds a module without Bot to confirm.
- **Telegram global rate limit** (30 msg/sec) on large subscriber counts — Mitigation: add a 50ms sleep between sends if subs > 30; below that, send hot. Document threshold in cron handler.
- **Handler exceeds Lambda 30s timeout** at very large sub counts — Mitigation: estimate at 100 subs × 100ms each = 10s, comfortably under. If breached, raise function timeout to 60s in `template.yaml` (still free).
- **Long-poll bot client used elsewhere** vs cron-time short-lived calls — current `bot.Bot` instance is shared; SendMessage is goroutine-safe per the lib's design. Confirm in upstream go-telegram/bot docs if uncertain.

## Open questions
1. Do we want per-subscriber timezone awareness, or push to all at the global 08:00 ICT? Original miti99bot pushes globally — match for parity.
2. Failure handling: store failed-chat IDs for retry on next push, or just log? Default: log only; transient Telegram failures resolve naturally.
3. Should daily push respect a "no matches today" outcome with a quiet skip vs. sending an empty message? Quiet skip; matches parity with original.
