package log

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// captureLogger swaps the default logger for one writing to buf, returns a
// restore func. Tests must defer the restore to keep the global pristine.
func captureLogger(t *testing.T, level slog.Level) (*bytes.Buffer, func()) {
	t.Helper()
	prev := Default()
	buf := &bytes.Buffer{}
	SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level})))
	return buf, func() { SetDefault(prev) }
}

// decodeOne decodes the most-recent JSON line from buf.
func decodeOne(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	last := lines[len(lines)-1]
	var rec map[string]any
	if err := json.Unmarshal([]byte(last), &rec); err != nil {
		t.Fatalf("not JSON: %q (%v)", last, err)
	}
	return rec
}

func TestParseLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"":         slog.LevelInfo,
		"info":     slog.LevelInfo,
		"INFO":     slog.LevelInfo,
		"debug":    slog.LevelDebug,
		"warn":     slog.LevelWarn,
		"warning":  slog.LevelWarn,
		"error":    slog.LevelError,
		"  Error ": slog.LevelError,
		"bogus":    slog.LevelInfo,
	}
	for in, want := range tests {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestInfo_EmitsJSONShape(t *testing.T) {
	buf, restore := captureLogger(t, slog.LevelInfo)
	defer restore()

	Info("server starting", "port", 8080, "module_count", 5)

	rec := decodeOne(t, buf)
	if rec["msg"] != "server starting" {
		t.Errorf("msg = %v, want 'server starting'", rec["msg"])
	}
	if rec["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", rec["level"])
	}
	if _, ok := rec["time"]; !ok {
		t.Error("missing time field")
	}
	if rec["port"].(float64) != 8080 {
		t.Errorf("port = %v, want 8080", rec["port"])
	}
}

func TestError_AttachesErrField(t *testing.T) {
	buf, restore := captureLogger(t, slog.LevelInfo)
	defer restore()

	Error("kv put failed", "module", "misc", "err", errors.New("boom"))

	rec := decodeOne(t, buf)
	if rec["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", rec["level"])
	}
	if rec["err"] != "boom" {
		t.Errorf("err = %v, want 'boom'", rec["err"])
	}
}

func TestNewlineEscaping_NoLogInjection(t *testing.T) {
	// Closes J3 (log-injection class) — slog must escape \n inside field
	// values so an attacker controlled string can't synthesise a fake log
	// record on the next line.
	buf, restore := captureLogger(t, slog.LevelInfo)
	defer restore()

	Warn("malicious", "user_input", "evil\n{\"level\":\"INFO\",\"msg\":\"forged\"}")

	output := buf.String()
	// Exactly one newline (record terminator) — the embedded \n in the value
	// must be escaped, not raw.
	if got := strings.Count(output, "\n"); got != 1 {
		t.Errorf("output contains %d newlines, want 1 (newlines in values must be escaped): %q", got, output)
	}
	rec := decodeOne(t, buf)
	if !strings.Contains(rec["user_input"].(string), "evil") {
		t.Errorf("user_input lost: %v", rec["user_input"])
	}
}

func TestLevelFiltering_DebugSuppressedAtInfo(t *testing.T) {
	buf, restore := captureLogger(t, slog.LevelInfo)
	defer restore()

	Debug("debug suppressed", "x", 1)
	Info("info kept", "x", 1)

	if strings.Contains(buf.String(), "debug suppressed") {
		t.Errorf("debug record leaked at info level: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "info kept") {
		t.Errorf("info record dropped: %s", buf.String())
	}
}

func TestWith_InlinesAttrs(t *testing.T) {
	buf, restore := captureLogger(t, slog.LevelInfo)
	defer restore()

	scoped := With("trace_id", "abc-123")
	scoped.Info("scoped message", "extra", "x")

	rec := decodeOne(t, buf)
	if rec["trace_id"] != "abc-123" {
		t.Errorf("trace_id = %v, want 'abc-123'", rec["trace_id"])
	}
	if rec["extra"] != "x" {
		t.Errorf("extra = %v, want 'x'", rec["extra"])
	}
}
