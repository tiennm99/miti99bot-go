---
phase: 4
title: "Structured logging"
status: pending
priority: P2
effort: "2-3h"
dependencies: []
---

# Phase 4: Structured logging

## Overview
Forward-port Phase 11's "Cloud Logging structured JSON" from the port plan. Cloud Run treats `stdout` lines as records but only parses JSON for severity/labels/trace correlation. Every `log.Printf` site added before this lands is a future migration. Also closes log-injection class (J3) by making newlines safe-by-construction.

## Requirements
- Functional: same log content emitted, JSON-encoded.
- Non-functional: severity levels, structured fields, trace ID propagation hooks.

## Architecture

New package `internal/log` (or `internal/obs`):
```go
package log

import "log/slog"

var defaultLogger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))

func Info(msg string, args ...any)    { defaultLogger.Info(msg, args...) }
func Warn(msg string, args ...any)    { defaultLogger.Warn(msg, args...) }
func Error(msg string, args ...any)   { defaultLogger.Error(msg, args...) }
func Fatal(msg string, args ...any)   { defaultLogger.Error(msg, args...); os.Exit(1) }

func With(args ...any) *slog.Logger   { return defaultLogger.With(args...) }
```

Slog's JSONHandler is stdlib (Go 1.21+), zero deps. Cloud Logging auto-recognizes `severity`, `time`, `message` keys.

### 18 call sites to rewire
- `cmd/server/main.go` (×9) — startup messages
- `internal/server/router.go:77, 86` — cron logging
- `internal/modules/dispatcher.go:24` — handler error
- `internal/modules/misc/misc.go:51` — KV write failure

Mechanical translation:
```go
log.Printf("misc /ping: putJSON failed: %v", err)
// becomes
log.Error("misc ping putJSON failed", "module", "misc", "command", "ping", "err", err)
```

## Related Code Files
- Create: `internal/log/log.go` (~40 LOC)
- Create: `internal/log/log_test.go`
- Modify: every file with `log.Printf` (18 sites)
- Modify: `internal/telegram/webhook.go` — panic recovery (Phase 02) uses new logger

## Implementation Steps

1. **Create `internal/log` package** with stdlib slog.JSONHandler.
2. **Add log level env** — `LOG_LEVEL=info|debug|warn|error` (default info).
3. **Write tests** — capture output, assert JSON shape with `severity`, `time`, custom fields.
4. **Migrate `cmd/server/main.go`** — 9 sites. `log.Fatalf` → `log.Fatal`.
5. **Migrate `internal/server/router.go`** — 2 sites. Add structured fields (`route=/cron`, `name=$name`).
6. **Migrate `internal/modules/dispatcher.go`** — 1 site.
7. **Migrate `internal/modules/misc/misc.go`** — 1 site.
8. **Migrate `internal/telegram/webhook.go`** — panic recovery from Phase 02.
9. **Search-grep `log.Printf` and `log.Fatalf`** — confirm zero remaining.
10. **Smoke test locally** — run server, hit endpoint, verify Cloud-Logging-friendly JSON in stdout.

## Success Criteria
- [ ] Zero `log.Printf` / `log.Fatalf` calls outside `internal/log`
- [ ] All log lines are valid JSON
- [ ] Each line has `severity`, `time`, `message`, plus structured fields
- [ ] Cron error log no longer has CRLF-injection risk (J3)
- [ ] LOG_LEVEL env respected
- [ ] All existing tests pass

## Risk Assessment
- **Risk:** Newline handling — slog escapes newlines in field values, so error wrapping `%v` of a newline-bearing error becomes safe automatically.
- **Risk:** Test output noise — tests can use `slog.NewTextHandler(io.Discard, ...)` injected via init or env flag.
- **Risk:** Performance regression — slog is ~2× slower than `log.Printf` per call but well under 1µs; negligible for webhook latency.

## Next Steps
- Phase 6b/7+ modules use `log` package from day one — no migration debt.
- Once Cloud Logging structured queries work, build error-rate dashboard (Phase 11 telemetry concern).
