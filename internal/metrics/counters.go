// Package metrics is a tiny in-memory counter store with periodic flush
// to CloudWatch Logs via the project's structured logger.
//
// Why not Prometheus / OpenTelemetry: the project runs on Lambda free
// tier with scale-to-zero. A pull-based exporter would be scraped from
// outside the instance and routinely hit a cold pod, defeating the point.
// Push-based exporters (StatsD, OTLP) require a paid sink.
//
// CloudWatch Logs is already free up to a generous quota and supports
// log-based metrics (count over `jsonPayload.msg=metrics`) for dashboards
// and alerts. Per-instance counters are reset on flush so the log line
// represents a delta, which CloudWatch Logs's count aggregation can sum
// across instances and time windows.
package metrics

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tiennm99/miti99bot/internal/log"
)

// DefaultFlushInterval is how often Run flushes counters to the log. 60s
// matches the JS source and keeps log volume modest (1 metrics line per
// minute per active instance).
const DefaultFlushInterval = 60 * time.Second

// Registry holds named counters across three categories: command
// invocations, errors, and AI calls. Zero-value Registry is ready to use.
//
// Counters use atomic.Int64 so increments don't lock; the per-name map
// itself is guarded by an RWMutex for the rare add path. Names should be
// short and stable — they become CloudWatch Logs label values.
type Registry struct {
	mu       sync.RWMutex
	commands map[string]*atomic.Int64
	errors   map[string]*atomic.Int64
	ai       map[string]*atomic.Int64
}

// New returns an empty Registry. Most callers use the package-level
// Default instead.
func New() *Registry {
	return &Registry{
		commands: map[string]*atomic.Int64{},
		errors:   map[string]*atomic.Int64{},
		ai:       map[string]*atomic.Int64{},
	}
}

// Default is the package-level registry. Convenience for the common case
// where a process needs exactly one. Tests can construct their own and use
// the methods directly.
var Default = New()

// IncCommand bumps the counter for a command invocation. name is the
// Telegram command without the leading slash.
func (r *Registry) IncCommand(name string) { r.inc(r.commandsMap(), name) }

// IncError bumps the counter for an error category — small, stable kinds
// like "ai-429", "kv-unavailable", "telegram-403".
func (r *Registry) IncError(kind string) { r.inc(r.errorsMap(), kind) }

// IncAI bumps the counter for an AI call by model name. Useful for
// tracking the daily-quota path: sum across instances == requests.
func (r *Registry) IncAI(model string) { r.inc(r.aiMap(), model) }

func (r *Registry) commandsMap() map[string]*atomic.Int64 { return r.commands }
func (r *Registry) errorsMap() map[string]*atomic.Int64   { return r.errors }
func (r *Registry) aiMap() map[string]*atomic.Int64       { return r.ai }

// inc bumps the counter for name in m, allocating on first use. Allocates
// only when the name is new, so steady-state increments are mutex-free.
func (r *Registry) inc(m map[string]*atomic.Int64, name string) {
	r.mu.RLock()
	c, ok := m[name]
	r.mu.RUnlock()
	if ok {
		c.Add(1)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := m[name]; ok {
		c.Add(1)
		return
	}
	c = &atomic.Int64{}
	c.Store(1)
	m[name] = c
}

// snapshot copies and resets the counters atomically per category. The
// returned maps are owned by the caller; the registry's internal state is
// reset to zero for the next interval.
func (r *Registry) snapshot() (cmds, errs, ai map[string]int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmds = drain(r.commands)
	errs = drain(r.errors)
	ai = drain(r.ai)
	return
}

// drain swaps out a counter map's values into a plain int64 map and
// resets each atomic to zero. The map keys are kept so subsequent
// increments don't reallocate the entry — only the count is reset.
func drain(m map[string]*atomic.Int64) map[string]int64 {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]int64, len(m))
	for k, v := range m {
		n := v.Swap(0)
		if n != 0 {
			out[k] = n
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Flush emits one structured log line with the current counters and
// resets them. Safe to call from anywhere; tests use it directly.
//
// The log line shape:
//
//	{"msg":"metrics","commands":{"wordle":3,"loldle":1},"errors":{"ai-429":1},"ai":null}
//
// CloudWatch Logs filters on `jsonPayload.msg=metrics` for dashboards.
// Empty categories appear as null (slog's default for nil maps).
func (r *Registry) Flush() {
	cmds, errs, ai := r.snapshot()
	// Avoid an empty-everything log line — adds noise without signal.
	if cmds == nil && errs == nil && ai == nil {
		return
	}
	// slog renders map[string]int64 as a JSON object; tests assert on
	// per-key substrings rather than full-line equality so non-deterministic
	// hashtable iteration order doesn't make them flaky.
	log.Info("metrics", "commands", cmds, "errors", errs, "ai", ai)
}

// Run starts a goroutine that flushes counters every DefaultFlushInterval
// until ctx is cancelled. It does one final Flush on exit so a SIGTERM
// shutdown captures the trailing window. Returns immediately; the
// goroutine runs in the background.
//
// Idiomatic usage:
//
//	go metrics.Default.Run(rootCtx)
func (r *Registry) Run(ctx context.Context) {
	tick := time.NewTicker(DefaultFlushInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			r.Flush()
			return
		case <-tick.C:
			r.Flush()
		}
	}
}

// IncCommand / IncError / IncAI on the package-level Default — short
// import-path-free spelling for the common case.
func IncCommand(name string) { Default.IncCommand(name) }
func IncError(kind string)   { Default.IncError(kind) }
func IncAI(model string)     { Default.IncAI(model) }

// Flush flushes the default registry. Used in graceful-shutdown paths.
func Flush() { Default.Flush() }

// Run starts the default-registry's flush loop. Cancels on ctx done.
func Run(ctx context.Context) { Default.Run(ctx) }
