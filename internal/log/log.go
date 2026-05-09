// Package log is a thin facade over stdlib log/slog with a JSON handler
// preconfigured for Cloud Logging. Cloud Run reads stdout line-by-line; with
// a JSON line, Cloud Logging picks up `severity`, `message`, and `time` and
// surfaces remaining fields as structured labels for filtering.
//
// Why a facade instead of importing slog directly: (1) callers stay
// log-package-agnostic (we can swap to logrus/zap later by editing one file);
// (2) the Fatal helper preserves stdlib's "log + exit 1" ergonomic; (3)
// LOG_LEVEL env is honoured at process start without every caller wiring it.
//
// Usage:
//
//	log.Info("server starting", "port", 8080)
//	log.Error("kv write failed", "module", "misc", "command", "ping", "err", err)
//	log.Fatal("missing required env", "key", "TELEGRAM_BOT_TOKEN")
//
// slog escapes newlines and quotes in field values, which closes the
// log-injection class (J3 in the 2026-05-09 review).
package log

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// defaultLogger is constructed at init from LOG_LEVEL. Tests can swap it via
// SetDefault — but the public Info/Warn/Error/Fatal helpers always read the
// current default so test substitutions take effect immediately.
var defaultLogger *slog.Logger

func init() {
	defaultLogger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(os.Getenv("LOG_LEVEL")),
	}))
}

// parseLevel maps LOG_LEVEL env to a slog.Level. Unknown / empty → Info.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// SetDefault swaps the package-level logger. Used by tests to capture output;
// production code never calls this.
func SetDefault(l *slog.Logger) { defaultLogger = l }

// Default returns the current logger. Useful when a caller needs the *slog.Logger
// directly (e.g. to pass into a third-party API that wants slog).
func Default() *slog.Logger { return defaultLogger }

// Debug, Info, Warn, Error route to the default logger. args is alternating
// key/value pairs (slog convention) or pre-built slog.Attr values.
func Debug(msg string, args ...any) { defaultLogger.Debug(msg, args...) }
func Info(msg string, args ...any)  { defaultLogger.Info(msg, args...) }
func Warn(msg string, args ...any)  { defaultLogger.Warn(msg, args...) }
func Error(msg string, args ...any) { defaultLogger.Error(msg, args...) }

// Fatal logs at Error level then exits with status 1, mirroring stdlib's
// log.Fatal ergonomic. Use only at startup boundaries — handlers should
// return errors, not exit.
func Fatal(msg string, args ...any) {
	defaultLogger.Error(msg, args...)
	os.Exit(1)
}

// With returns a child logger that inlines the given attrs into every record.
// Useful for per-request scopes (e.g. attach a trace id once).
func With(args ...any) *slog.Logger { return defaultLogger.With(args...) }

// LogAttrs is a small re-export so callers can use slog.LogAttrs ergonomics
// (typed attrs, no allocation) without importing slog themselves.
func LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	defaultLogger.LogAttrs(ctx, level, msg, attrs...)
}
