---
phase: 5
title: "Port simple modules (util, misc, wordle, loldle classic)"
status: partial
priority: P2
effort: "6h"
dependencies: [4]
---

# Phase 05: Port simple modules

## Overview
Port the four KV-only, AI-free modules: `util` (info/help renderer), `misc` (stub easter eggs), `wordle` (5-letter game with 14k-word dict), `loldle` classic (LoL champion guesser). Validates the framework end-to-end before the more complex modules.

## Requirements
- Functional: command parity with JS — same names, same behaviors, same KV state shape (so a future export-import migration is feasible).
- Non-functional: each module file ≤200 lines per code-standards.md. Static word/champion datasets embedded via `go:embed`.

## Architecture

```
internal/modules/util/
├── util.go            ← Module factory, registers /info /help
├── info.go            ← /info handler
└── help.go            ← /help renderer (groups by visibility)

internal/modules/misc/
└── misc.go            ← stub commands

internal/modules/wordle/
├── wordle.go          ← factory, registers /wordle /wguess /wgiveup /wstats
├── game.go            ← session state struct, Get/Save via KV
├── guess.go           ← scoring (green/yellow/gray)
├── data/words.txt     ← 14k word dict (embedded)
└── words.go           ← go:embed loader

internal/modules/loldle/
├── loldle.go          ← factory
├── game.go            ← session state
├── champions.go       ← go:embed champion JSON
├── data/champions.json
└── compare.go         ← attribute comparison logic
```

## Related Code Files
- Create: above tree under `internal/modules/{util,misc,wordle,loldle}`
- Modify: `internal/modules/modules.go` Factories slice — append `util.New, misc.New, wordle.New, loldle.New`
- Copy: word list + champion JSON from JS repo (verbatim)

## Implementation Steps
1. Copy `src/modules/util/*` JS source as reference. Implement `/info` (returns env-derived bot info) + `/help` (groups commands public+protected, omits private).
2. `/help` queries the registry — already accessible via `Deps`. Format as Telegram MarkdownV2.
3. Misc module: port commands as-is (mostly text replies).
4. Wordle:
   - Copy `src/modules/wordle/words.txt` to `internal/modules/wordle/data/words.txt`.
   - `go:embed data/words.txt` into a `string`, split lines, build a `map[string]struct{}` for O(1) validity checks.
   - Game state: `{ word string; guesses []string; status string }` saved per user.
   - KV key: `game:<userID>`.
5. Loldle classic:
   - Copy champion JSON dataset into `data/champions.json`.
   - State: `{ targetID string; guesses []string }` per user per UTC day. Key: `game:<userID>:<yyyy-mm-dd>`.
   - Comparison: gender, position, species, resource, range, region, release year. Yields green/yellow/red per attribute.
6. Port unit tests from JS:
   - `wordle/format_test.go` — score formatting
   - `wordle/guess_test.go` — green/yellow/gray correctness, double-letter edge case
   - `loldle/compare_test.go` — each attribute comparison
   - `loldle/game_test.go` — daily reset, max-guesses gate
7. Wire into `Factories` slice. `MODULES=util,misc,wordle,loldle` env var enables them.
8. Smoke test on Cloud Run with dev bot.

## Success Criteria
- [x] `/wordle`, `/wordle <word>`, `/wordle_new`, `/wordle_giveup`, `/wordle_stats` ported (commands renamed from spec's `/wguess` etc. to match JS source)
- [ ] `/loldle`, `/lguess <champion>`, `/lgiveup` work (deferred to follow-up cook 5c)
- [x] `/help` lists all loaded modules' public + protected commands (covers util + misc + wordle; picks up loldle automatically once 5c lands)
- [x] All ported tests pass — wordle compare suite is verbatim port of JS vitest, plus extra coverage for pool exhaustion and race-free `pickRandom`
- [x] Image size stays ≤25 MiB after embedding word data (binary still 17 MB; 88 KB dict embed is rounding error against the 10 MB Firestore SDK)

## Cook scope split
This phase ships in three sub-cooks:
- **5a (done):** util + misc — small, validates the module-loading pipeline end-to-end. ✅
- **5b (this cook):** wordle — 14855-word dict, scoring, sessions. ✅
- **5c (next):** loldle classic — champion JSON, daily reset, attribute comparison. ~700 LoC + data file.

## Implementation deviations (5a)
- `modules.Deps` gained a `Registry *Registry` pointer so `/help` can introspect at runtime. Pointer is captured at factory time and stable thereafter; Registry is documented read-only after Build returns.
- Static factory catalog (`modules.Factories`) moved to `cmd/server/main.go::factories()` to avoid an import cycle (`modules → util → modules`). The empty `internal/modules/modules.go` file remains as a doc anchor.
- `misc.lastPing.At` stored as int64 ms-epoch (matches JS `Date.now()`) — preserves byte-for-byte KV parity for the future export-import migration.
- Telegram-side handler tests intentionally skipped — would require a fake bot HTTP server for negligible coverage gain. Renderer + KV behaviour ARE tested.

## Implementation deviations (5b — wordle)
- KV TTL: JS uses Cloudflare KV's `expirationTtl: 60*60*24*7`. Firestore has no equivalent per-doc TTL; `gameTTLSeconds` constant is informational. Old games linger — Phase 11 GC if needed.
- `pickDaily` ported but unused (handlers call `pickRandom`). Kept for parity so future "daily wordle" mode is a one-line swap.
- Added `subjectLocks` (per-subject `sync.Mutex` map) to serialise `Get → mutate → Put` in handlers. Cloudflare Workers' isolate model gave the JS source this for free; Go + Firestore needs explicit locking or two concurrent guesses to the same group chat silently lose one.
- `pickRandom(words, nil)` falls through to `math/rand.Intn` (package-level, mutex-protected globals) instead of a singleton `*rand.Rand` so the bot dispatcher's per-update goroutines don't race on RNG state.
- KV wire-format parity: `GameState.Giveup` always emitted (no omitempty); `Stats.LastResultAt` is `*int64` so unplayed accounts marshal as `null` matching JS shape; `StartedAt` is ms-epoch int64.
- Subject IDs converted to strings for KV keys (`game:<subject>`); JS uses numbers but Cloudflare KV stringifies on the wire so Firestore round-trips identically.
- Word-list loader panics on malformed embedded data — corrupt regen of `words.txt` is a build-time bug, not a runtime concern worth recovering from.

## Code reviews
- [Phase 5a review](reports/code-reviewer-260509-0813-phase5a-util-misc.md) — 1 critical (`/info` nil-deref), 2 high (1 informational + 1 perf-deferred), 4 mediums/lows. C1, L2, M1, L3, H1 doc applied.
- [Phase 5b review](reports/code-reviewer-260509-0918-phase5b-wordle.md) — 1 critical (`defaultRNG` data race) + 2 high (Get-mutate-Put logical race; dead `debugPickerError`) + extra compare test + race test for `pickRandom`. All addressed in same session. Mediums (M1 giveup-on-never-played JS-faithful gotcha; M2 `subjectFor` test) deferred — JS-parity intentional.

## Risk Assessment
- **Risk**: 14k-word file embedded → ~120 KiB. `go:embed` puts it in the binary; no runtime IO. Acceptable.
- **Risk**: Wordle scoring has a known JS-side edge case (double-letter); ensure ported logic matches. **Mitigation**: bring the failing-cases test verbatim.
- **Risk**: Loldle daily reset uses UTC in JS; confirm Go uses same. **Mitigation**: explicit `time.Now().UTC()` in date key.

## Rollback
Remove modules from `Factories` slice or `MODULES` env. Each module is independent.
