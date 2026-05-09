---
phase: 11
title: "Test parity + observability"
status: partial
priority: P3
effort: "4h"
dependencies: [8]
---

# Phase 11: Test parity + observability

## Overview
Reach test-count parity with the JS suite where applicable. Wire structured JSON logs to Cloud Logging. Add lightweight metrics (counters for command invocations, errors, AI calls). Soak the Go service against a test bot for 48 hours before cutover.

## Requirements
- Functional: every JS test that covers logic (not framework/transport) has a Go counterpart. Logs are JSON-shaped, consumable by Cloud Logging severity filters.
- Non-functional: no external metrics backend (free-tier discipline) — Cloud Logging structured fields used as the metrics surface (Log Explorer + Log-based Metrics, all free up to default quota).

## Architecture

```
internal/log/
├── logger.go        ← slog.Logger configured with JSON handler, severity → Cloud Logging convention
└── middleware.go    ← request log: msg=req method= path= status= ms=

internal/metrics/
└── counters.go      ← incrCommand(name), incrError(kind), incrAI(model). Logged at info severity.

tests/integration/   ← optional: emulator-based end-to-end (not run in CI)
```

`slog` (Go 1.21+) handles JSON output. Cloud Logging auto-parses structured `severity` + `message` + custom fields when written to stdout.

## Related Code Files
- Create: `internal/log/{logger,middleware}.go`
- Create: `internal/metrics/counters.go`
- Modify: every module command handler — add `metrics.IncCommand("/wordle")` etc.
- Modify: `internal/ai/*` — add `metrics.IncAI("embedding")` and `metrics.IncError("ai-429")` paths
- Add: per-module `*_test.go` files until parity reached

## Implementation Steps
1. **Logger**: `slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: replaceLevelKey}))`. Map slog `level` → Cloud Logging `severity` convention (DEBUG/INFO/WARNING/ERROR).
2. **Request middleware**: wraps `/webhook`, `/cron/*`. Logs `{msg: "req", method, path, status, ms}` at info. Mirrors JS index.js shape.
3. **Counters**: `metrics.IncCommand(name)` increments an in-memory `sync.Map[string]*atomic.Int64`. Periodic flush every 60s logs `{msg: "metrics", commands: {...}, errors: {...}, ai: {...}}` then resets. Graceful shutdown flushes once on SIGTERM.
4. **Log-based metrics in GCP** (one-time setup, document in deployment-guide.md):
   - Counter on `severity=ERROR` → alerts.
   - Counter on `jsonPayload.msg=req AND jsonPayload.status>=500` → 5xx rate.
   - Counter on `jsonPayload.msg=metrics` → daily aggregation by command.
5. **Test parity audit**:
   - Run `find . -name "*.test.js" | wc -l` against JS repo for baseline.
   - Run `find . -name "*_test.go"` count in Go repo.
   - Aim ≥80% of JS tests have a Go counterpart. Skip framework-only tests (e.g. CF Worker fetch handler tests) — they have no analogue.
6. **48-hour soak**:
   - Point a separate test bot at the Cloud Run service.
   - Manual playthrough of every module's commands × 3 users.
   - Watch Cloud Logging for errors. Watch Firestore reads/writes per day.
   - Capture cold-start P95 (`severity=INFO AND jsonPayload.msg=req` filtered to first request after gap).
7. **Compare to Phase 01 baseline**: if Phase 11 cold-start P95 > Phase 01 baseline × 1.5, investigate before cutover (gRPC client init usually the suspect).

## Success Criteria
- [x] **Logger** ported: `internal/log/log.go` exposes `slog.JSONHandler` writing to stdout, severity-aware via `LOG_LEVEL` env (Phase 04 of fix-all-review-findings forward-ported this).
- [x] **Request middleware** ported: `internal/server/log_middleware.go` wraps every route and emits `{msg:"req", method, path, status, ms}` per request.
- [x] **In-memory counters** ported: `internal/metrics/counters.go` exposes `IncCommand`/`IncError`/`IncAI` with 60s periodic `Flush` to `{msg:"metrics", commands, errors, ai}`. Wired into the dispatcher so every command invocation + handler error is counted; `cmd/server/main.go` runs the flush loop bound to rootCtx (one final flush on SIGTERM).
- [x] Test coverage 69.8% across 20 packages (`fix-all-review-findings` Phase 05 raised it from 44.7% baseline). Module-level coverage: champname/keylock/telegram 100%, util 90%, log/chathelper/loldle/wordle/misc 77-81%, others ≥70%.
- [ ] All errors during 48h soak triaged — **deferred** (requires Cloud Run deployment).
- [ ] Cold-start P95 ≤1.5s — **deferred** (requires Phase 01 GCP baseline).
- [ ] Daily Firestore reads <40k cap — **deferred** (production observation).
- [ ] Cloud Logging log-based metrics setup — **deferred** (one-time GCP console / `gcloud logging metrics create`; document in `docs/deployment-guide.md` once Phase 01 lands).
- [ ] No memory leaks check — **deferred** (production observation).

## Risk Assessment
- **Risk**: in-memory counters lost when instance scales to zero. **Mitigation**: acceptable — Cloud Logging is the source of truth via per-request log lines; in-memory counters are just convenience for debugging.
- **Risk**: 48h soak reveals a cold-start regression we can't fix without major rework. **Mitigation**: trigger an abort criterion → keep CF Worker as primary, treat Go as standby.
- **Risk**: log-based metrics setup is fiddly. **Mitigation**: use `gcloud logging metrics create` in `scripts/setup-logging.sh`, idempotent.

## Rollback
None needed — observability is read-only. If logging is overly noisy, lower default level via env var.
