# Code Review — Phase 5c: loldle module port

**Date:** 2026-05-09
**Plan:** plans/260508-2222-go-port-cloud-run/phase-05-port-simple-modules.md (Phase 5 final sub-cook)
**Scope:** internal/keylock + internal/modules/loldle (15 new files, 1 modified, 1 deleted)
**Build:** `go vet`, `go test -race`, `go build` all clean. Loldle coverage 43.9% (handler/render layer not exercised — explicitly out of scope per intentional choice 6).
**Compared against:** /config/workspace/tiennm99/miti99bot/src/modules/loldle/* (JS source).

---

## Summary

Solid byte-for-parity port. The 7-case JS vitest suite is faithfully ported, edge cases for `parseYear` / `compareYear` / `compareMultiValue` covered, and the keylock extraction is well-scoped. JS-vs-Go semantic divergence is mostly avoided through careful `toLowerSet` / `asString` / nil-tolerant value plumbing.

**One real correctness divergence found** (winRate rounding) and a small handful of hardening recommendations. No security or concurrency defects.

---

## Critical Issues

None.

---

## High Priority

### H1. winRate uses truncation instead of `Math.round` — diverges from JS

**File:** `internal/modules/loldle/handlers.go:299`
**JS:** `const winRate = s.played ? Math.round((s.wins / s.played) * 100) : 0;` (handlers.js:223)
**Go:** `winRate = int(float64(st.Wins) / float64(st.Played) * 100)`

`int(...)` on a positive float in Go is truncation toward zero, equivalent to `Math.floor`, **not** `Math.round`.

**Concrete divergence:**
- 2 wins / 3 played → JS shows `67%`, Go shows `66%`.
- 3 wins / 7 played → JS shows `43%`, Go shows `42%`.
- 5 wins / 6 played → JS shows `83%`, Go shows `83%` (lucky — exact .333).

**Fix:**
```go
winRate = int(math.Round(float64(st.Wins) / float64(st.Played) * 100))
```
…and add `"math"` import.

**Note:** the same bug exists in `internal/modules/wordle/handlers.go:270`; the phase 5b review missed it. Apply the same fix there for consistency. Suggest an `internal/modules/util/`-shared helper to deduplicate, but that's optional.

---

## Medium Priority

### M1. Render layer has 0% test coverage

**Files:** `render.go` (`renderGuess`, `renderBoard`, `formatRowGroups`, `padRight`)

These are pure HTML-producing functions. The most likely regression — the **column-alignment math** in `formatRowGroups` — would produce subtly mis-aligned `<pre>` blocks that are hard to spot in human review but trivial to detect with one golden-string test.

**Suggested test (locks the JS-parity output exactly):**
```go
func TestRenderGuess_ColumnAlignment(t *testing.T) {
    rows := []AttributeRow{
        {Key: "gender", Label: "Gender", Type: attrExact,
            GuessValue: "Male", Result: ResultCorrect},
        {Key: "release_date", Label: "Release year", Type: attrYear,
            GuessValue: "2013", Result: ResultWrong, Direction: "up"},
    }
    got := renderGuess("Aatrox", rows)
    // Label column is padded to len("Release year") = 12.
    // Wrong year row appends ⬆️.
    want := "<pre>🎯 Champion     AATROX\n" +
        "✅ Gender       Male\n" +
        "❌ Release year 2013 ⬆️</pre>"
    if got != want {
        t.Errorf("\ngot  %q\nwant %q", got, want)
    }
}
```

A second test with `renderBoard([])` confirming the empty-board hint path would also be cheap insurance.

### M2. `internal/keylock` package has no tests

**File:** `internal/keylock/keylock.go` (no `keylock_test.go`)

The package is small (one method) but the contract — *distinct keys are independent, same-key callers serialize* — is exactly the kind of thing tests should pin so a future "let's swap to a `chan` semaphore" doesn't silently break callers. Two tests, ~20 lines:

```go
func TestMap_DistinctKeysDoNotBlock(t *testing.T) {
    var m Map
    aDone := make(chan struct{})
    bStart := make(chan struct{})
    unlockA := m.Acquire("a")
    go func() {
        unlock := m.Acquire("b")
        close(bStart)
        unlock()
    }()
    select {
    case <-bStart: // ok — "b" did not block on "a"
    case <-time.After(time.Second):
        t.Fatal("Acquire(\"b\") blocked on Acquire(\"a\")")
    }
    unlockA()
    _ = aDone
}

func TestMap_SameKeySerializes(t *testing.T) {
    var m Map
    var seq []int
    var mu sync.Mutex
    var wg sync.WaitGroup
    for i := 1; i <= 10; i++ {
        i := i
        wg.Add(1)
        go func() {
            defer wg.Done()
            unlock := m.Acquire("k")
            defer unlock()
            mu.Lock()
            seq = append(seq, i)
            mu.Unlock()
        }()
    }
    wg.Wait()
    if len(seq) != 10 {
        t.Errorf("expected 10 entries, got %d", len(seq))
    }
}
```

### M3. `compareMultiValue` empty-set / nil-input edge cases not exercised

The current `compare_test.go` covers full-match, partial-overlap, case-insensitivity, and ambiguity, but doesn't lock JS parity for these shapes that production data could reach:

- Both sides nil/empty (e.g. a hypothetical no-positions champion). JS returns `"correct"`. Go does too — but no test asserts it.
- One side empty, other non-empty. JS returns `"wrong"`. Go matches — untested.
- Mixed-case duplicates within a single side: `["Top","top"]` vs `["Top"]`. Should be `correct` (set collapse). Untested.

```go
func TestCompareMulti_EdgeShapes(t *testing.T) {
    cases := []struct{ name string; a, b []string; want string }{
        {"both empty",        nil, nil,                                ResultCorrect},
        {"one empty",         []string{"X"}, nil,                       ResultWrong},
        {"dup collapses",     []string{"Top", "top"}, []string{"Top"},  ResultCorrect},
        {"whitespace folds",  []string{"  Top  "}, []string{"Top"},     ResultCorrect},
        {"empty strings filter", []string{"Top", ""}, []string{"Top"},  ResultCorrect},
    }
    for _, tc := range cases {
        if got := compareMultiValue(tc.a, tc.b); got != tc.want {
            t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
        }
    }
}
```

### M4. `subjectFor` not unit-tested

It's a small switch but a behaviour-bearing trust boundary (wrong subject → wrong subject's stats get incremented). Three table cases (private/group/channel) would lock the JS-parity contract:

```go
func TestSubjectFor(t *testing.T) {
    cases := []struct{ chatType models.ChatType; chatID, fromID int64; from bool; want string }{
        {models.ChatTypePrivate,    100, 200, true,  "200"},
        {models.ChatTypeGroup,      100, 200, true,  "100"},
        {models.ChatTypeSupergroup, 100, 200, true,  "100"},
        {models.ChatTypeChannel,    100, 200, true,  "200"}, // channel falls through to From
        {models.ChatTypeChannel,    100,   0, false, ""},    // anon channel post → no subject
    }
    for _, c := range cases {
        msg := &models.Message{Chat: models.Chat{ID: c.chatID, Type: c.chatType}}
        if c.from { msg.From = &models.User{ID: c.fromID} }
        if got := subjectFor(msg); got != c.want {
            t.Errorf("%v: %q, want %q", c.chatType, got, c.want)
        }
    }
}
```

---

## Low Priority

### L1. Sticker pool send happens inside the per-subject lock

**File:** `handlers.go:222, 235, 274`

`trySendSticker` is called before `replyHTML` *within* the locked critical section. A slow Telegram API response holds the per-subject lock and serialises subsequent /loldle commands behind it.

Mitigation: not v1. JS source has the equivalent serialisation on Workers. Phase 11 if it ever shows up in oncall.

### L2. `getOrInitGame` silently drops in-progress rounds when admin lowers maxGuesses

**File:** `handlers.go:111`

If a round has 6 guesses and admin runs `/loldle_setmax 5`, the next `/loldle` call hits `existing.Guesses < maxGuesses` (6 < 5 = false) and starts a fresh round, scoring no loss. Matches JS exactly (handlers.js:88) — flagged for awareness only, not a deviation.

### L3. `argAfterCommand` duplicated across wordle and loldle

Both packages contain identical 9-line `argAfterCommand`. The handlers.go comment acknowledges this:
> "duplicated to keep package-local; promoting to a shared helper buys very little until a 4th module needs it."

Reasonable YAGNI. If a third game module materialises in Phase 6+, lift to `internal/modules/util` then.

### L4. `formatValue` and `formatRowGroups` interact subtly with `html.EscapeString`

`renderGuess`/`renderBoard` pass `r.value` (which may already be `formatValue`'s output, e.g. `"Runeterra, Shurima"`) through `html.EscapeString`. JS does the same. The comma-space format is HTML-safe by construction but the chain is worth a render-test (covered by M1's suggestion above).

---

## Edge Cases Found by Scout

Verified the following don't disagree between Go and JS:

- **Unicode normalize:** "Kaïsa" → "kasa" in both (Go iterates bytes; UTF-8 multi-byte sequences fall outside `'a'..'z'` and get stripped, matching JS regex). ✓
- **Leading whitespace in input:** `findChampion(cs, "  Aatrox  ")` → "aatrox" → match. ✓
- **`/loldle@miti99bot Aatrox`:** `argAfterCommand` finds the first space after `@miti99bot`. ✓
- **Negative `formatDuration` input** (-500, -1500, -2500): JS `Math.max(0, Math.round(…))` and Go's `if total < 0 { total = 0 }` yield identical outputs because the only negative-rounding edge (`Math.round(-2.5) === -2` vs Go's truncate-then-clamp) is masked by the `max(0, …)` clamp on both sides. ✓
- **`pickSticker(nil)` and `pickSticker([]string{})`:** both return `""`, and `trySendSticker` short-circuits before `rand.Intn(0)`. ✓
- **`gameState` JSON shape:** field order `{target, guesses, startedAt}` matches JS `JSON.stringify({target, guesses, startedAt})`. `*int64` for nullable `startedAt` is the right Go type — confirmed by `state_test.go:17`'s golden assertion `…"startedAt":null}`.
- **Ambiguous prefix returns `nil`:** JS `prefixMatches.length === 1 ? … : null` matches Go's bail-on-second-hit logic.
- **`setsEqual` with different-length sets** short-circuits in both. ✓

No new cross-runtime divergences discovered beyond H1.

---

## Architectural

### Keylock placement: correct

`internal/keylock/` (top-level, peer to `storage`/`telegram`/`server`) is the right home. It's a generic synchronisation primitive with no module-specific awareness, so placing it under `internal/modules/` would imply ownership it doesn't have. Name "keylock" is short and accurate. Alternatives (`internal/locks`, `internal/sync`, `internal/concur`) are not improvements; `sync` collides with stdlib mentally and `concur` is an unfamiliar abbreviation. **Keep as-is.**

### Champions slice lifetime

`pickRandomChampion` returns `&s.champions[i]`. Validated: the slice is constructed once in `New()` from `loadChampions()` (which returns a freshly-allocated slice via `json.Unmarshal`) and never reassigned or appended-to. The pointer is safe for the lifetime of `*state`, which is the lifetime of the bot process. ✓

### `state` ownership

`*state` is created once per `New()` call and shared across all 4 handler closures via method receiver. `keylock.Map`'s "do not copy after first use" contract is honoured because `s.locks` is only ever accessed via the pointer receiver. ✓

---

## Concurrency Audit

- **`pickRandomChampion` / `pickSticker`:** use `math/rand.Intn` (package-level, mutex-protected globals) — same pattern wordle 5b adopted after the previous review. ✓
- **`s.champions` reads:** read-only after construction, no synchronisation needed. ✓
- **`keylock.Map.Acquire`:** `sync.Map.LoadOrStore` is documented as atomic; multiple goroutines racing on first-use of the same key all receive the identical `*sync.Mutex`. ✓
- **`Get→mutate→Put` in handlers:** wrapped by `defer s.locks.Acquire(subject)()`. ✓
- **`recordResult`:** load-modify-save inside the lock. ✓
- **`saveGame`:** inside the lock. ✓
- **Telegram send calls:** inside the lock (cosmetic concern flagged as L1, not a race).

`go test -race -count=1 ./internal/modules/loldle/...` passes clean.

---

## Wire-format JSON Audit

| Type           | Go marshal output                                           | JS `JSON.stringify` equivalent                              | Match |
|----------------|-------------------------------------------------------------|-------------------------------------------------------------|-------|
| `gameState{}`  | `{"target":"…","guesses":[],"startedAt":null}`              | `{"target":"…","guesses":[],"startedAt":null}`              | ✓     |
| `gameState`    | `{"target":"…","guesses":["Ahri"],"startedAt":1700000000000}`| same                                                         | ✓     |
| `stats{}`      | `{"played":0,"wins":0,"streak":0,"bestStreak":0}`           | `{"played":0,"wins":0,"streak":0,"bestStreak":0}`           | ✓     |
| `roundConfig`  | `{"maxGuesses":5}`                                           | `{"maxGuesses":5}`                                           | ✓     |

`*int64` for `StartedAt` is correctly chosen — `time.Time` would have marshaled as `"0001-01-01T00:00:00Z"` and broken parity. Verified by `state_test.go:17`.

The intentional omission of `LastResultAt` from `stats` (vs wordle's stats which has it) matches the JS source asymmetry — confirmed by `state.js:96-105` (no lastResultAt in the JS schema).

---

## Positive Observations

- **Thoughtful `parseYear` micro-implementation** — manual base-10 to avoid pulling `strconv`. Tiny, defensible.
- **`buildRows` / `formatRowGroups` separation** mirrors the JS shape exactly, makes column-width calc a single source of truth.
- **`fmt.Errorf("loldle <fn>: %w", err)`** error wrapping pattern is consistent across state.go.
- **Comments explain JS-parity choices** at every non-obvious branch (e.g. compare.go:128 `JS toSet falls back to splitting on ","`, state.go:23 `using time.Time would marshal as "0001-01-01T00:00:00Z"`).
- **Embed strategy** with build-time panic on bad data — the right choice for a static dataset.
- **JS test fixtures lifted verbatim** as Go fixture vars in compare_test.go — drift detector across runtimes.
- **`html.EscapeString` applied at render boundary**, not at compare-time — values stay round-trip-clean for storage.

---

## Recommended Actions

1. **H1:** fix `winRate` rounding in `loldle/handlers.go:299` and **also in `wordle/handlers.go:270`** (same bug). Use `math.Round`.
2. **M1:** add 1–2 render tests (golden-string for `renderGuess`, empty-board for `renderBoard`).
3. **M2:** add 2 keylock tests (distinct-key independence + same-key serialisation).
4. **M3, M4:** add `compareMultiValue` edge-shapes table test and `subjectFor` table test.
5. None of these block landing — H1 is a minor user-visible discrepancy, not a security/correctness issue.

---

## Metrics

- Files reviewed: 14 new + 1 modified (loldle + keylock + main.go wiring).
- LOC: ~880 production + ~440 test.
- Type coverage: 100% (Go is statically typed).
- Test coverage: 43.9% statements (handler/render layer untested by design).
- Lint: `go vet ./...` clean.
- Race: `go test -race -count=1` clean.
- Build: 17 MB stripped binary, +65 KB champions.json embedded — within budget.

---

## Unresolved Questions

1. Should `winRate` be a shared helper in `internal/modules/util/` (e.g. `WinRate(wins, played int) int`) given both wordle and loldle compute it identically? YAGNI says wait until 3 modules; but H1's twin-fix burden suggests it's already 2.
2. Should an integration smoke test for `New(...)` exist that asserts loadChampions doesn't panic and returns ≥150 records? Currently `TestLoadChampions_EmbedIsValid` covers it indirectly. Probably sufficient.

---

**Status:** DONE_WITH_CONCERNS
**Summary:** One real JS-parity bug found (winRate uses truncation instead of Math.round, present in both loldle and wordle); architecture/concurrency/wire-format are clean; recommend small render+keylock test additions before next phase.
