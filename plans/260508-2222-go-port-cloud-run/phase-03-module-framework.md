---
phase: 3
title: "Module framework + storage interfaces"
status: done
priority: P1
effort: "4h"
dependencies: [2]
---

# Phase 03: Module framework + storage interfaces

## Overview
Replicate the JS plug-n-play module system in Go. Define `Module`, `Command`, `Cron` types. Build static module registry with conflict detection. Define `KVStore` interface (SQL pattern dropped — trading uses Firestore directly). Wire dispatcher to `bot.RegisterHandler` calls.

## Requirements
- Functional: at runtime, `MODULES` env var (CSV) selects which modules load. Each module exposes `Commands []Command` and optional `Crons []Cron`. Registry detects name conflicts across all visibility levels and aborts on conflict (fail-fast at startup).
- Non-functional: zero reflection, no plugins. Static slice of constructors registered in `internal/modules/registry.go`. Idiomatic Go (interfaces small, structs concrete).

## Architecture

```
internal/modules/
├── module.go           ← Module, Command, Cron types + Visibility enum
├── registry.go         ← static map + Build() + name-conflict detection
├── dispatcher.go       ← installCommands(b *bot.Bot, reg *Registry)
├── cron_dispatcher.go  ← DispatchScheduled(name, deps)
├── validate.go         ← validateCommand / validateCron
└── modules.go          ← static import map (slice of factories)

internal/storage/
├── kv_store.go         ← KVStore interface
├── memory_kv.go        ← in-memory fake (for tests + smoke)
└── prefix.go           ← per-module key prefixing wrapper
```

Module type:

```go
type Visibility int
const (
    VisibilityPublic Visibility = iota
    VisibilityProtected
    VisibilityPrivate
)

type Command struct {
    Name        string                                  // ^[a-z0-9_]{1,32}$
    Visibility  Visibility
    Description string                                  // required
    Handler     func(ctx context.Context, b *bot.Bot, u *models.Update) error
}

type Cron struct {
    Schedule string                                     // documentation only
    Name     string                                     // unique within module
    Handler  func(ctx context.Context, deps Deps) error
}

type Module struct {
    Name     string
    Commands []Command
    Crons    []Cron
    Init     func(ctx context.Context, deps Deps) error // optional
}

type Deps struct {
    KV       KVStore   // already prefixed per-module
    Firestore *firestore.Client
    Gemini   *genai.Client
    Env      map[string]string
}

type Factory func() Module
```

`KVStore` interface mirrors the JS contract:

```go
type KVStore interface {
    Get(ctx context.Context, key string) ([]byte, error)
    GetJSON(ctx context.Context, key string, dst any) error // returns ErrNotFound if missing
    Put(ctx context.Context, key string, val []byte) error
    PutJSON(ctx context.Context, key string, val any) error
    Delete(ctx context.Context, key string) error
    List(ctx context.Context, prefix string) ([]string, error)
}
```

## Related Code Files
- Create: `internal/modules/{module,registry,dispatcher,cron_dispatcher,validate,modules}.go`
- Create: `internal/storage/{kv_store,memory_kv,prefix}.go`
- Modify: `cmd/server/main.go` to construct registry, pass to dispatcher

## Implementation Steps
1. Define types in `internal/modules/module.go`. Visibility enum + Command/Cron/Module/Deps structs.
2. `internal/modules/validate.go`:
   - `validateCommand(c Command) error` — name regex, visibility known, description nonempty, handler nonnil.
   - `validateCron(c Cron) error` — name nonempty, handler nonnil.
3. `internal/modules/modules.go`: empty `var Factories = []Factory{}` for now. Each module registers itself in subsequent phases.
4. `internal/modules/registry.go`:
   - `Build(env []string, factories []Factory) (*Registry, error)`.
   - For each name in `env` ∩ factory map: call factory, validate every command/cron, accumulate into `publicCmds`, `protectedCmds`, `privateCmds`, `allCmds`.
   - Detect duplicate command names across all 3 maps → error `command conflict: /foo defined in <a> and <b>`.
5. `internal/modules/dispatcher.go`:
   - `Install(b *bot.Bot, reg *Registry)`: iterate `reg.AllCommands`, call `b.RegisterHandler(bot.HandlerTypeMessageText, "/"+name, bot.MatchTypeCommand, handler)`.
6. `internal/modules/cron_dispatcher.go`:
   - `DispatchScheduled(ctx, cronName string, reg *Registry, deps Deps)`: look up cron by name across all modules, run all matching handlers concurrently (errgroup).
7. `internal/storage/memory_kv.go`: `sync.Map`-backed KVStore for tests + smoke runs.
8. `internal/storage/prefix.go`: `Prefixed(s KVStore, prefix string) KVStore` wrapper that prepends `<prefix>:` to all keys.
9. Wire in `cmd/server/main.go`: build registry, install commands, pass to webhook + cron handlers.
10. Unit tests: `registry_test.go` (conflict detection, validation errors), `prefix_test.go` (round-trip).

## Success Criteria
- [x] Empty `MODULES=""` boots cleanly (no fallback handler today; grammY's `/start` parity deferred to a future phase)
- [x] Two modules with same command name → startup fails with clear error (`TestBuild_DetectsCommandConflict`)
- [x] Per-module KVStore prefix isolation verified by test (`TestBuild_PerModulePrefixedKV`, `TestPrefixed_RoundTrip`, `TestDispatchScheduled_PassesPrefixedDeps`)
- [x] `go vet ./...` + `go test -race -count=1 ./...` green

## Implementation deviations
- `Factory func() Module` → `Factory func(deps Deps) Module`: handler closures capture deps directly. Eliminates a separate `Module.Init` lifecycle step.
- `Factories []Factory` → `Factories map[string]Factory`: required for `MODULES`-env name lookup; prevents duplicate names at compile-load.
- `Deps` ships only `KV` + `Env` today. `Firestore` + `Gemini` fields land in Phases 04 / 07 (YAGNI).
- Cron uniqueness enforced across modules (registry-level), instead of "concurrent errgroup of all matches" — simpler and matches the one-cron-per-name reality.
- `cmd/server` strips `TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEBHOOK_SECRET`, `CRON_SHARED_SECRET` from `Deps.Env` to prevent accidental leakage.
- Module names validated against `^[a-z0-9_]{1,32}$` (same regex as commands) so KV prefix isolation cannot be subverted by a `:` in the name.

## Risk Assessment
- **Risk**: `go-telegram/bot` `RegisterHandler` is more general than grammY's `bot.command`. Need to confirm behavior on `/cmd@botname` (group chats). **Mitigation**: library docs say `MatchTypeCommand` strips `@botname`; verify with a group chat test before Phase 05.
- **Risk**: Static factory slice means new modules require code change — same constraint as JS `index.js` static map. Acceptable.

## Rollback
Revert to Phase 02 main.go. Module framework is purely additive.
