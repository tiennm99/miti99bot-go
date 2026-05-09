---
phase: 5
title: "Port simple modules (util, misc, wordle, loldle classic)"
status: done
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
- [x] `/loldle`, `/loldle <champion>`, `/loldle_giveup`, `/loldle_stats`, `/loldle_setmax` (private) ported
- [x] `/help` lists all loaded modules' public + protected commands (util + misc + wordle + loldle)
- [x] All ported tests pass — wordle and loldle JS vitest suites ported verbatim, plus Go-only coverage for race-free pickers, pool exhaustion, render alignment, keylock fan-out
- [x] Image size stays ≤25 MiB after embedding word + champion data (binary 17 MB; 88 KB words.txt + 65 KB champions.json are noise vs the 10 MB Firestore SDK)

## Cook scope split
This phase shipped in three sub-cooks:
- **5a (done):** util + misc — small, validates the module-loading pipeline end-to-end. ✅
- **5b (done):** wordle — 14855-word dict, scoring, sessions. ✅
- **5c (this cook):** loldle classic — 172-champion JSON, attribute comparison, sticker pools. ✅

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

## Implementation deviations (5c — loldle)
- Per-subject lock extracted from wordle into `internal/keylock` (shared package). Both wordle and loldle now import it. Naming chosen as a peer to `internal/storage` and `internal/telegram` rather than nesting under `internal/modules/`.
- KV TTL deferred — Cloudflare KV's `expirationTtl` has no Firestore equivalent. Phase 11 GC if old games become a cost concern.
- Sticker pools (win/lose/giveup) preserved verbatim from `stickers.js`; file_ids are bot-scoped to `@miti99bot` and were already valid against the new bot per the test-bot policy.
- `lastResultAt` deliberately omitted from loldle stats (parity with JS source — different from wordle's stats which DOES include it; that asymmetry exists in the JS source).
- `pickRandomChampion` and `pickSticker` use `math/rand.Intn` (package-level mutex-protected globals) so concurrent /loldle handlers don't race on RNG state. Same pattern as wordle 5b.
- `winRate` uses `math.Round` not `int(...)` truncation, after Phase 5c review caught the JS-parity bug. The same fix was retroactively applied to wordle's `/wordle_stats`.

## Code reviews
- [Phase 5a review](reports/code-reviewer-260509-0813-phase5a-util-misc.md) — 1 critical (`/info` nil-deref), 2 high (1 informational + 1 perf-deferred), 4 mediums/lows. C1, L2, M1, L3, H1 doc applied.
- [Phase 5b review](reports/code-reviewer-260509-0918-phase5b-wordle.md) — 1 critical (`defaultRNG` data race) + 2 high (Get-mutate-Put logical race; dead `debugPickerError`) + extra compare test + race test for `pickRandom`. All addressed in same session. Mediums (M1 giveup-on-never-played JS-faithful gotcha; M2 `subjectFor` test) deferred — JS-parity intentional.
- [Phase 5c review](reports/code-reviewer-260509-0940-phase5c-loldle.md) — 1 high (`winRate` truncation across both wordle + loldle) + 4 mediums (test gaps). H1 fixed in both modules in same session; M1 (render alignment golden test) and M2 (keylock fan-out + serialisation tests) added; M3/M4 deferred — covered transitively elsewhere.

## Risk Assessment
- **Risk**: 14k-word file embedded → ~120 KiB. `go:embed` puts it in the binary; no runtime IO. Acceptable.
- **Risk**: Wordle scoring has a known JS-side edge case (double-letter); ensure ported logic matches. **Mitigation**: bring the failing-cases test verbatim.
- **Risk**: Loldle daily reset uses UTC in JS; confirm Go uses same. **Mitigation**: explicit `time.Now().UTC()` in date key.

## Rollback
Remove modules from `Factories` slice or `MODULES` env. Each module is independent.
