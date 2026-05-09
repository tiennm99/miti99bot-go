# code-reviewer · phase 6a · loldle-emoji port

Date: 2026-05-09
Scope: first sub-cook of Phase 6 — port `loldle-emoji` from JS to Go.
Verdict: **DONE_WITH_CONCERNS** — all flagged items are non-blocking. JS-parity solid; build/test green.

---

## Scope reviewed

- `internal/modules/loldleemoji/{loldleemoji,champions,normalize,lookup,state,render,handlers}.go` (+ tests)
- `internal/modules/loldleemoji/data/emojis.json` (172 records, verbatim from JS)
- `internal/modules/registry.go` — moduleNameRe relaxation
- `internal/modules/registry_test.go` — updated rejection cases
- `cmd/server/main.go` — factory wiring
- Cross-checked against JS source `src/modules/loldle-emoji/{index,handlers,state,render,lookup}.js`
  and `src/util/normalize-name.js`

go vet ✓ · go test -race -count=1 ./... ✓ on the loldleemoji package.

---

## A. JS-parity correctness

Walked every player-visible flow side-by-side with `handlers.js`:

| Flow | JS | Go | Match |
|---|---|---|---|
| `/loldle_emoji` no arg, no game | render empty board | same | ✓ |
| `/loldle_emoji` no arg, mid-round | render board with N guesses | same | ✓ |
| `/loldle_emoji` no arg, target gone from refreshed pool | clearGame + "data was updated" reply (HTML) | same | ✓ |
| `/loldle_emoji <bad>` champion not found | plain-text reply, no guess counted | same (note Go uses `%q` → ASCII-only diff for non-ASCII args; matches classic loldle) | ✓ |
| `/loldle_emoji <dup>` already guessed | HTML reply, **guess NOT counted, NOT saved** | same — duplicate-check returns before `game.Guesses = append(...)` and before any save | ✓ |
| `/loldle_emoji <correct>` win | recordResult(true) + clearGame + sticker — wait no, **emoji has no stickers** (correctly omitted) | same | ✓ |
| Last-guess loss | recordResult(false) + clearGame, render full board | same | ✓ |
| Mid-round wrong guess | save game with appended guess | same | ✓ |
| `startedAt` stamping | first **actual** guess (not empty-arg view) starts the clock | same — `if game.StartedAt == nil { now := nowMillis(); game.StartedAt = &now }` is **after** the no-arg early-return and **after** the duplicate-guess early-return | ✓ |
| `/loldle_emoji_giveup` no game | "No active round" hint | same | ✓ |
| `/loldle_emoji_giveup` active | recordResult(false) + clearGame + reveal | same | ✓ |
| `/loldle_emoji_stats` win-rate rounding | `Math.round(wins/played * 100)` | `math.Round(...)` (lesson from Phase 5c) | ✓ |
| `/loldle_emoji_setmax` private | strconv.Atoi range-check 1..10, persisted via configKey | same | ✓ |

**One JS-Go behavioural divergence found** — non-blocking, present in classic loldle too:

> `Champion not found: %q.` — Go's `%q` escape format differs from JS's `${arg}` interpolation for non-ASCII inputs (e.g. `"中文"` → `"中文"` in Go vs literal in JS). Affects only the user-visible error text reflecting their own typo. Already shipped in classic loldle (Phase 5c). Not worth fixing here unless paired with a same-PR fix in classic loldle.

`getOrInitGame` correctly handles the mid-round-cap-reduction edge case: if a previous round saved with N guesses and the operator then `/loldle_emoji_setmax`'d below N, the existing-with-overflow check `len(existing.Guesses) < maxGuesses` is **false** so a fresh round is started. Matches JS.

---

## B. moduleNameRe relaxation safety

Tracked `loldle-emoji` end-to-end through the storage layer:

1. `Build()` validates against `^[a-z0-9_-]{1,32}$` — accepts.
2. **Memory backend**: `MemoryProvider.For(name)` calls `Prefixed(base, name)` → prefix becomes `"loldle-emoji:"`. `Prefixed.k()` does plain string concat. Hyphen passes through cleanly.
3. **Firestore backend**: `FirestoreProvider.For(name)` creates `NewFirestoreKVStore(client, name)` → collection name `loldle-emoji`. Firestore collection name rules: not `__.*__`, no `/`, valid UTF-8, ≤1500 bytes. `loldle-emoji` is fine.
4. **`:` rejected**: confirmed by `TestBuild_RejectsInvalidModuleName` adding `"a:b"` to the rejection set. The storage prefix delimiter integrity is preserved.
5. **Cron name regex** (`internal/server/router.go`) is independent of moduleNameRe. Module-with-hyphen names are not used in URL paths; cron names live in `Cron.Name`, validated separately.
6. **Telegram BotFather command names** — irrelevant; module names are not commands. Commands still use the strict `commandNameRe = ^[a-z0-9_]{1,32}$` (validate.go), so command names with hyphens would still be rejected. Verified handler command names: `loldle_emoji`, `loldle_emoji_giveup`, etc. — underscore-only, no hyphen leak. Good.

**Verdict**: relaxation is safe and well-scoped. The added test
`TestBuild_AcceptsHyphenatedModuleName` plus the expanded
rejection list locks the contract.

One minor nit: `moduleNameRe` would also accept reserved Firestore patterns like `__abc__` (starts/ends with `__`). Not introduced by this change — the prior regex `^[a-z0-9_]{1,32}$` allowed it too. Out of scope.

---

## C. Wire-format JSON shape parity

Round-tripped each persisted shape against the JS-written form:

```
gameState (StartedAt nil):  {"target":"Aatrox","guesses":[],"startedAt":null}     ✓
gameState (StartedAt set):  {"target":"Aatrox","guesses":["Ahri"],"startedAt":N}   ✓
stats:                       {"played":N,"wins":N,"streak":N,"bestStreak":N}        ✓
roundConfig:                 {"maxGuesses":N}                                       ✓
```

Critical detail re-verified: `startFreshGame` initialises `Guesses: []string{}` (not `nil`), so the marshaled output has `"guesses":[]` not `"guesses":null`. JS-side `existing.guesses.length` survives the round-trip. Confirmed via empirical test.

**Gap**: there's no test that asserts a JS-written record (i.e. raw JSON bytes
from a hypothetical KV migration source) decodes into the Go struct correctly.
The existing tests only round-trip Go → KV → Go. See section F for a suggested
test.

---

## D. Concurrency

| Surface | Verdict |
|---|---|
| `keylock.Map` per subject | Standard pattern, identical to classic loldle. Get→mutate→Put critical sections are wrapped at the top of every mutating handler. ✓ |
| `handleStats` not locked | Same as classic loldle. Single read; no lost-update risk. ✓ |
| `math/rand.Intn` global | Mutex-protected globals (lesson from Phase 5b). ✓ |
| `s.pool` slice | Built once in `loadPool()` (factory time), never mutated. `findByExactName`/`findChampion` return `&s.pool[i]` — safe because backing array is immutable for the life of the registry. Handlers only read `.ChampionName` and `.Emojis` (both strings, immutable). ✓ |
| Pool pointer outliving handler | Handler returns drop the pointer; pool persists. No GC concern. ✓ |
| `s.locks` zero-value | `keylock.Map` documents zero-value usability. ✓ |
| `defer s.locks.Acquire(subject)()` idiom | Inner call evaluated immediately (lock acquired); outer call deferred (unlock on return). Idiomatic. ✓ |

No race detected by `-race` test runs. No mutable shared state outside KV.

---

## E. Accepted-duplication tradeoff

`normalize`, `subjectFor`, `argAfterCommand` are duplicated across loldle and
loldleemoji. Three more variants planned (loldle-quote, loldle-ability,
loldle-splash). At 5 callers the case for extraction is overwhelming.

**Recommendation**: extract NOW, before phase 6b adds the 3rd duplicate. The
deferred-extraction note in `normalize.go` says "once a third variant lands"
— phase 6b is exactly that, so the natural extraction point is **phase 6b's
prep step**, not later. Suggested package: `internal/loldlecommon` or
`internal/champname` (for `normalize`+`findChampion` together; the lookup
function itself is also duplicated and is the most useful extraction target).

This is a non-blocking observation; the current code is fine. Just a
flag-it-for-the-next-cook so drift doesn't compound (e.g. someone fixes a bug
in one copy and not the others — the `winRate` truncation lesson from 5c is
exactly this kind of bug).

---

## F. Test gaps (highest-leverage additions)

The existing 25-ish cases cover the in-Go logic well. Two additional tests
would meaningfully tighten the migration story:

1. **JS-wire-format decode test** — put raw bytes
   `{"target":"Ahri","guesses":["Aatrox"],"startedAt":1700000000000}` into the
   KV via `kv.Put`, then call `loadGame`, then assert all three fields. This
   locks the migration contract. Equivalent for `stats` and `roundConfig`. One
   `t.Run`-table per shape, ~30 lines total.

2. **`getOrInitGame` cap-reduction case** — pre-populate a game with
   `len(Guesses) == 5`, set MaxGuesses to 3 via setMaxGuesses, then call
   `getOrInitGame` and assert a fresh round was started (target may differ,
   but `len(Guesses) == 0`). Defends the defensive branch on
   `state.go:saveGame` overwriting a stale round.

Optional third: **`getMaxGuesses` with out-of-range stored value** falls back
to default. Currently relied on but not directly tested. ~5 lines.

(Telegram-side handler tests skipped per intentional choice — agreed; the
return-path of SendMessage isn't worth a fake HTTP server.)

---

## Issues by priority

### Critical
- None.

### High
- None.

### Medium
- **Extract `normalize`, `subjectFor`, `argAfterCommand`, `findChampion` into a shared package as part of phase 6b prep** (E). Three more variants are imminent; deferring extraction past 6b means three more copies must be edited every time the helper changes. Not blocking 6a; flagging for 6b's plan.

### Low
- **`%q` escape divergence for non-ASCII args** in "Champion not found: %q." (A). Already shipped in classic loldle. If addressed, do both modules in one PR.
- **README does not mention `loldle-emoji` in MODULES example** (cmd/server/main.go review). Trivial — `/ck:docs` material.
- **`moduleNameRe` would accept `__abc__`** (Firestore-reserved). Not introduced here; pre-existing latent issue. Out of scope.

### Nice-to-have tests
- JS-wire-format decode test for `gameState` / `stats` / `roundConfig` (F).
- `getOrInitGame` cap-reduction case (F).

---

## Positive observations

- `winRate = math.Round(...)` correctly applied without prompting (5c lesson stuck).
- `startFreshGame` writes `Guesses: []string{}` (not nil) — wire-format-correct without explicit comment, but the `state_test.go` `TestGameState_StartedAtNullByDefault` locks it.
- `loadPool` panics on bad data (build-time bug) but filters empty-emoji records in normal flow — matches JS exactly.
- `defer s.locks.Acquire(subject)()` is in every mutating handler. Easy to forget; consistent here.
- Registry test additions (`TestBuild_RejectsInvalidModuleName` with explicit
  `"a:b"` entry, `TestBuild_AcceptsHyphenatedModuleName`) clearly document
  the regex-relaxation contract — exactly the kind of test that catches a
  later "tighten the regex" regression.
- Module-name-vs-package-name mismatch (`loldle-emoji` registered, `loldleemoji` package) is documented in code comments. Good.
- All handler command names use underscores (`loldle_emoji`, etc.) — correctly stricter than module names, matching Telegram BotFather command rules.

---

## Unresolved questions

1. Phase 6b will add `loldle-quote`. Should the helper-extraction work be
   counted as part of 6a (touch the just-shipped code) or 6b (carve out
   helpers as the very first commit of 6b)? My recommendation: 6b. The
   current 6a code is correct and shipping.
2. Should the `%q`-vs-JS-interpolation divergence be tracked as a follow-up
   ticket against both loldle modules, or accepted indefinitely? Trivial
   either way.

---

**Status:** DONE_WITH_CONCERNS
**Summary:** loldle-emoji port is JS-parity-correct, race-clean, and wire-format-stable; concerns are non-blocking — extract shared helpers in 6b prep, add a JS-wire-format decode test, and the pre-existing `%q` divergence remains (consistent with classic loldle).
