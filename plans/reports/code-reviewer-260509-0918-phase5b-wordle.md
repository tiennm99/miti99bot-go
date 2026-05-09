# Phase 5b — Wordle Module Port — Code Review

**Reviewer**: code-reviewer | **Date**: 2026-05-09 09:26
**Scope**: `internal/modules/wordle/**`, `cmd/server/main.go` factories registration
**Verdict**: DONE_WITH_CONCERNS — port is JS-faithful and tests are green, but two real concurrency races exist on production paths.

---

## Summary

| Area | Findings |
|------|----------|
| Build / test | `go vet`, `go test -race -count=1 ./...` clean. 14855-word dict embeds cleanly; binary 17 MB. |
| JS-parity | Compare/lookup/state/render/handlers are byte-for-byte faithful to the JS source. Wire format (Stats / GameState JSON) verified. |
| Critical | C1: `defaultRNG` data race in `pickRandom` (reproducible under `-race`). |
| High | H1: same-subject Get→mutate→Put race in handlers (lost guesses possible). H2: dead `debugPickerError`. |
| Medium | M1: `handleGiveup` from a never-played user creates+forfeits a fresh game (JS-faithful, but undocumented surprise). M2: docstring on `subjectFor` ChatType ordering is JS-faithful but should be locked with a tiny test. |
| Low | L1: 1 missing `compareWords` shape worth adding. L2: minor docstring fix. |

Critical (C1) and High (H1) are real prod regressions over the JS source, since Cloudflare Workers serializes per-request and Go does not. Both are minor changes to fix.

---

## A. JS-parity correctness — `compare.go`

**Verdict**: faithful. Two-pass algorithm and consumption order match the JS source line-for-line. The five JS-suite cases are ported verbatim and all pass. I cross-checked one additional shape that the existing tests do not explicitly cover:

- **All-same-letter target, all-same-letter guess (different letters)**: `target="aaaaa", guess="bbbbb"` → 5×wrong. (Trivial; covered implicitly by AllWrong.)
- **All-same-letter target, mixed guess containing one match**: `target="aaaaa", guess="aabbb"` → correct,correct,wrong,wrong,wrong. (Pool consumed, no partial possible. Worth a test — see L1.)
- **Pool-exhaustion mid-pass**: `target="abide", guess="aahed"` (already covered).

I see no edge case where the Go and JS algorithms disagree. The string-byte iteration (`guess[i] == target[i]`) is safe because both inputs are pre-validated to ASCII a-z by `validateGuess`/`loadWords`.

**Recommendation L1**: Add one extra test for "duplicate target, single matching guess letter" to lock pool-exhaustion behavior before any future refactor. Cheap; closes the only conceptual gap I spotted.

---

## B. Wire-format JSON shape

**Verdict**: faithful, locked by `state_test.go`.

Verified directly:
- Field order matches JS `startFreshGame` literal order (`target, guesses, solved, giveup, startedAt`). Go `encoding/json` emits in struct-declaration order.
- `Stats.LastResultAt *int64` marshals as `null` when nil — matches JS shape.
- Empty `Guesses` slice (initialized `[]GuessRecord{}` in `startFresh`) marshals as `[]`, not `null`. Round-trip through `MemoryKVStore` returns a non-nil empty slice (Go decodes `[]` → empty slice with len==cap==0 and `nil==false`).
- One subtle gotcha: a hypothetical zero-value `GameState{}` marshals `Guesses` as `null`, since the slice is nil. Code never relies on the zero value (every read path goes through `loadGame` which returns `nil, nil` on missing, and `startFresh` sets `[]`). Safe today; would be unsafe only if someone added a `New()` constructor that returned a zero value to a caller that marshalled it.

No regressions vs JS.

---

## C. Subject resolution

**Verdict**: matches JS. Order of cases is private → group/supergroup → default(channel/unknown), which is the JS exact behavior. Channel posts often have no `From` so subject ends up `""` and the handler replies "Cannot identify chat." — same null-result as JS.

---

## D. Concurrency — read-modify-write race on `GameState`

**C1 — Critical (data race)**: `defaultRNG` race in `daily.go`.

The `go-telegram/bot` library's `ProcessUpdate` defaults to `go r(ctx, b, upd)` — every webhook update is dispatched in a fresh goroutine (`process_update.go:31`). When two `/wordle` or `/wordle_new` calls land concurrently and both need a fresh game, they both call `pickRandom(s.words, nil)` which dereferences the package-level `defaultRNG`. `*rand.Rand` methods are NOT goroutine-safe.

Reproduced under `-race` against an isolated harness:
```
WARNING: DATA RACE
Read at math/rand.(*rngSource).Uint64()
  pickRandom() ... main.func1()
Previous write at math/rand.(*rngSource).Uint64()
```

**Why prior reviews missed it**: existing tests inject their own `rand.Rand` (via the `rng` parameter), so the package default is never exercised concurrently in the test suite. Production traffic does the opposite: every handler call uses `defaultRNG`.

**Fix**: replace the `*rand.Rand` singleton with concurrent-safe top-level functions, or wrap with a mutex. The simplest patch is to use the top-level `rand.Intn` from `math/rand` (which IS goroutine-safe via internal mutex) when `rng == nil`:
```go
if rng == nil {
    return words[rand.Intn(len(words))], nil
}
```
That removes the singleton and the race in one line. Tests that pass an explicit `*rand.Rand` continue to work unchanged.

(Note for context-engineering: the user's framing in F said "real handler runs are single-flight per goroutine via the bot dispatcher" — this is incorrect for `go-telegram/bot v1.20.0` whose default mode is async. Confirm before merging.)

---

**H1 — High (logical race, no data race)**: Get → mutate → Put on `GameState` is not atomic.

Two concurrent `/wordle apple` calls for the same subject can:
1. T1 loadGame → guesses=[a]
2. T2 loadGame → guesses=[a]
3. T1 append "apple" → save guesses=[a,apple]
4. T2 append "berry" → save guesses=[a,berry]

T1's guess is silently lost. The MemoryKV mutex protects each individual op, not the compound. Same applies to Firestore without transactions.

For a single-user-per-DM use case this is rare (humans don't fire two `/wordle` in <50 ms). For group chats sharing a subject ID, it's plausible. JS is not vulnerable here only because Cloudflare Workers per-isolate serialize against the same KV — but that's an environmental property, not a JS-source guarantee. Once the bot is on Go + Firestore, the race window opens.

**Fix options**:
- (cheapest, JS-faithful) document the race and accept it; flag as known issue in Phase 11.
- Add a per-subject sync.Mutex map in `state` (16 lines, no Firestore work).
- Use Firestore transactions for `getOrInit + saveGame` and accept the latency hit.

I recommend option B (per-subject mutex map) — it eliminates the race for both backends without a Firestore round-trip increase, and the map can be sharded later if contention shows up. Document option A explicitly if you defer; "linger silently" is the worst outcome.

---

## E. RNG init — covered above (C1)

The user's note in E correctly flagged the question. Confirmed: it IS a real prod race, not just a test issue. Fix per C1.

---

## F. Test gaps

Two suggestions:

1. **`compare`: pool-exhaustion with single matching letter and duplicate guess.**
   ```go
   func TestCompareWords_DuplicateGuessSingleTargetExhaustsAfterCorrect(t *testing.T) {
       // target "aaaaa", guess "aabbb" → c,c,w,w,w
       r := CompareWords("aabbb", "aaaaa")
       want := "correct,correct,wrong,wrong,wrong"
       if got := resultsLetters(r); got != want { t.Errorf("got %s want %s", got, want) }
   }
   ```
   Exercises the "no partial possible because pool is empty" branch from a different angle than `TestCompareWords_DuplicateGuessSingleTarget`.

2. **Concurrency invariant for the pickrandom path.**
   ```go
   func TestPickRandom_NilRNGIsRaceFree(t *testing.T) {
       words := []string{"a","b","c","d","e"}
       var wg sync.WaitGroup
       for i := 0; i < 100; i++ {
           wg.Add(1)
           go func() { defer wg.Done(); _, _ = pickRandom(words, nil) }()
       }
       wg.Wait()
   }
   ```
   Combined with `go test -race`, this would have caught C1 in CI.

If only one test is added, prefer #2 — it locks the prod-path safety property that the existing test suite cannot demonstrate.

---

## H2 — High: dead code

`debugPickerError` (`daily.go:71`) is unexported, has no callers anywhere in the tree (`grep -rn debugPickerError`), and exists only as a doc comment. YAGNI. Drop the function and the comment.

---

## M1 — Medium: `handleGiveup` from a never-played user

`getOrInit` is called by `handleGiveup`. If a user types `/wordle_giveup` without ever having issued `/wordle` or `/wordle_new`, the handler:
1. creates a fresh game (recording `target`),
2. immediately marks it `Giveup=true`,
3. records a loss (Played=1, Streak=0),
4. reveals the random target.

This is byte-faithful to JS, so don't change behavior — but the JS code has the same gotcha and it's mildly user-hostile (now the user has a `lastResultAt` and a `Played` of 1 without ever playing). If you want to deviate from JS for this one, add an early guard:
```go
prior, err := loadGame(...)
if prior == nil { return reply(ctx, b, msg, "No active round. /wordle_new to start.") }
```
Worth a one-line decision in the plan deviations list whether to keep JS-parity or fix.

---

## M2 — Medium: lock subject ordering with a test

`subjectFor` has 4 explicit branches and an empty-string fallback. There's no test. Three asserts on:
- private + From → user.ID
- group + From → chat.ID
- channel + From → user.ID
- nil msg → ""

would lock semantics against future refactor and demonstrate the JS-parity contract. ~15 LOC.

---

## L1 — Low: extra compare test

See A above. One additional case (target="aaaaa") would exercise the rarely-tested "pool exhausted before pass 2 starts" branch.

## L2 — Low: doc nit

`daily.go:60` — comment "initialized lazily in init()" is a contradiction in terms; init() is eager. Replace with "initialized once at package load".

## L3 — Low: `gameTTLSeconds` constant

Constant is unused (only documented). Either:
- Add a `// nolint:unused` style comment plus a TODO referencing Phase 11 GC,
- Delete it and put the comment in a Markdown file.

Currently Go's vet doesn't complain about unused package-level constants, so nothing's broken — but a future reader will wonder why it's there. The block comment IS clear about intent, so this is just polish.

---

## Positive observations

- Two-pass compare is the textbook-correct algorithm and ports exactly.
- The decision to use `*int64` for `Stats.LastResultAt` to preserve `null` parity is exactly right.
- `validateGuess`'s priority order (empty > length > unknown) matches JS, including the early-empty short-circuit.
- The `loadWords` panic on bad embedded data is the right choice — failing the build/cold-start is much better than serving from a half-loaded dict.
- `argAfterCommand` correctly handles `/wordle@bot apple` (idx is the space, not the `@`).
- The `state.go` design (closure-captured KV) cleanly avoids the JS module-level `let db = null` pattern, and the Factory signature in `wordle.go` aligns with the existing Phase 03 deps contract.
- 14855-word dict is byte-identical to JS source (head + tail + count match `grep -oE`).

---

## Recommended actions (priority order)

1. **C1 fix (must)**: replace `defaultRNG` with `rand.Intn`-from-package or wrap with a mutex. ~3 line change in `daily.go`.
2. **H1 decide (should)**: add per-subject mutex in `state` OR explicitly document the lost-guess window in Phase 11 followups. Don't leave silent.
3. **H2 fix (should)**: delete `debugPickerError`.
4. **F#2 add (should)**: race test for `pickRandom(words, nil)` so CI catches future regressions.
5. **F#1, M2 add (nice)**: extra compare test + subjectFor test.
6. **M1, L1-L3 (optional)**: judgment calls; document or fix at your discretion.

---

## Metrics

- Type / nil safety: clean. No `any`/interface{}, no nullable struct fields beyond the one explicit `*int64`.
- Test coverage (logic): compare 5 cases, lookup 7 cases, daily 5 cases, state 6 cases, words 1 case = 24 cases for ~250 LoC of business logic. Roughly proportional. Handler tests intentionally skipped (documented).
- Linter: `go vet ./...` clean.
- Build: 17 MB stripped, dict 88 KB embedded. Within budget.

---

## Unresolved questions

1. **Concurrency policy**: do you want JS-equivalent best-effort semantics (accept H1 lost guesses), or per-subject serialization? JS got this for free from Workers' isolate model; Go does not. A one-line deviations note in `phase-05-port-simple-modules.md` would settle it.
2. **`/wordle_giveup` from never-played user**: keep JS-faithful (creates+forfeits), or add the early-out guard? (M1 above.)
3. **`/wordle_new` double-counting**: if a user spams `/wordle_new` six times before guessing once, the JS source records 5 abandons → 5 losses → streak hammered to 0. Is that the intended behavior in production? Both runtimes behave identically; flagging only because it's surprising and there's no test locking it.

---

**Status**: DONE_WITH_CONCERNS
**Summary**: Wordle port is byte-faithful to JS and tests are green, but a real `defaultRNG` data race (C1) and a Get→mutate→Put logical race (H1) exist on prod paths since Go runs handlers concurrently while CF Workers do not.
