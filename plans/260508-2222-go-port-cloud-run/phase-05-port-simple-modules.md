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
- [ ] `/wordle`, `/wguess apple`, `/wgiveup`, `/wstats` work end-to-end (deferred to follow-up cook 5b)
- [ ] `/loldle`, `/lguess <champion>`, `/lgiveup` work (deferred to follow-up cook 5c)
- [x] `/help` lists all loaded modules' public + protected commands (covers util + misc; will pick up wordle/loldle automatically once 5b/5c land)
- [ ] All ported tests pass (count parity with JS suite where applicable) — partial: util/misc tests added; wordle/loldle pending
- [ ] Image size stays ≤25 MiB after embedding word + champion data (deferred — current binary 17 MB without embeds)

## Cook scope split
This phase ships in three sub-cooks:
- **5a (this cook):** util + misc — small, validates the module-loading pipeline end-to-end. ✅ done.
- **5b (next):** wordle — 14k-word dict, scoring, sessions. ~500 LoC + data file.
- **5c (next):** loldle classic — champion JSON, daily reset, attribute comparison. ~700 LoC + data file.

## Implementation deviations (5a)
- `modules.Deps` gained a `Registry *Registry` pointer so `/help` can introspect at runtime. Pointer is captured at factory time and stable thereafter; Registry is documented read-only after Build returns.
- Static factory catalog (`modules.Factories`) moved to `cmd/server/main.go::factories()` to avoid an import cycle (`modules → util → modules`). The empty `internal/modules/modules.go` file remains as a doc anchor.
- `misc.lastPing.At` stored as int64 ms-epoch (matches JS `Date.now()`) — preserves byte-for-byte KV parity for the future export-import migration.
- Telegram-side handler tests intentionally skipped — would require a fake bot HTTP server for negligible coverage gain. Renderer + KV behaviour ARE tested.

## Code review (5a)
[Phase 5a review](reports/code-reviewer-260509-0813-phase5a-util-misc.md) — 1 critical (`/info` nil-deref), 2 high (1 informational + 1 perf-deferred), 4 mediums/lows. C1 and L2 (KV wire-format parity) fixed in same session; M1 doc, L3 escape-test, H1 thread-id comment also applied.

## Risk Assessment
- **Risk**: 14k-word file embedded → ~120 KiB. `go:embed` puts it in the binary; no runtime IO. Acceptable.
- **Risk**: Wordle scoring has a known JS-side edge case (double-letter); ensure ported logic matches. **Mitigation**: bring the failing-cases test verbatim.
- **Risk**: Loldle daily reset uses UTC in JS; confirm Go uses same. **Mitigation**: explicit `time.Now().UTC()` in date key.

## Rollback
Remove modules from `Factories` slice or `MODULES` env. Each module is independent.
