---
phase: 3
title: "Shared helper extraction"
status: completed
priority: P2
effort: "1-2h"
dependencies: []
---

# Phase 3: Shared helper extraction

## Overview
Eliminate helper drift across `wordle`, `loldle`, `loldleemoji`, `misc`. The 6a review flagged this for 6b prep; the architecture review confirmed `subjectFor` variants already differ in shape, and `winRate` truncation drift bit Phase 5b/5c. Extract before the next module port lands and compounds the problem.

## Requirements
- Functional: zero behavior change; helpers must be byte-equivalent at call sites.
- Non-functional: single source for chat-helper + champion-name primitives; future modules import rather than copy.

## Architecture

Two new packages:

### `internal/modules/util/chathelper`
Generic helpers usable by any module:
- `SubjectFor(msg *models.Message) string` — single canonical impl (private/group fallback)
- `ArgAfterCommand(text string) string`
- `NowMillis() int64`
- `Reply(ctx, b, msg, text) error`
- `ReplyHTML(ctx, b, msg, text) error`
- `WinRate(wins, played int) int` — `math.Round` correctly

### `internal/champname`
Loldle-specific:
- `Normalize(s string) string`
- `FindChampion[T any](needle string, all []T, name func(T) string) (T, bool)` — generic over champion type

(Or keep as helpers in `internal/modules/util/chathelper` — see unresolved Q2 from arch report.)

## Related Code Files
- Create: `internal/modules/util/chathelper/chathelper.go`
- Create: `internal/modules/util/chathelper/chathelper_test.go`
- Create: `internal/champname/champname.go` (or fold into chathelper)
- Create: `internal/champname/champname_test.go`
- Modify: `internal/modules/wordle/handlers.go` — delete local helpers, import
- Modify: `internal/modules/loldle/handlers.go` — same
- Modify: `internal/modules/loldleemoji/handlers.go` — same
- Modify: `internal/modules/misc/misc.go` — same (uses `nowMillis`)
- Modify: `internal/modules/loldle/lookup.go` — delete local `findChampion`/`normalize`
- Modify: `internal/modules/loldleemoji/lookup.go` — same

## Implementation Steps

1. **Decide canonical `SubjectFor`** — pick the loldle/emoji shape (no `ChatTypePrivate` special-case — `default` branch already handles it). Document in a comment.
2. **Write chathelper package** with all 6 helpers + table-driven tests.
3. **Migrate wordle** — replace 4 local helpers with imports; run tests; assert no behavior change.
4. **Migrate loldle** — same.
5. **Migrate loldleemoji** — same.
6. **Migrate misc** — only `nowMillis`.
7. **Write champname package** with `Normalize` + generic `FindChampion`. Tests cover prefix-match, ambiguous-prefix, exact-match, accent-insensitive.
8. **Migrate loldle/loldleemoji** lookup paths.
9. **Run full test suite + race detector.**

## Success Criteria
- [x] Single `SubjectFor` impl; zero copies in modules (`internal/modules/util/chathelper`)
- [x] Single `Normalize` + `Find` (generic) impl (`internal/champname`)
- [x] All wire-format tests still pass (no behavior drift)
- [x] `go test -race -count=1 ./...` clean
- [x] Net LOC reduction across handler files: ~290 net lines removed (589 deletions vs 299 insertions across all files; loldle/loldleemoji/wordle handlers each ~50–60 lines slimmer)

## Risk Assessment
- **Risk:** Generic `FindChampion[T]` may not compile cleanly with current Go version → fallback to interface + type assertion or per-module thin wrapper.
- **Risk:** Subtle `SubjectFor` divergence (private channel with no From) → covered by table-driven tests with all chat types.
- **Mitigation:** Migrate one module at a time, run tests between each.

## Next Steps
- Phase 06 file-size splits become mechanical after this lands.
- Phase 07+ AI modules import these helpers instead of copying.
