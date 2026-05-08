# Semantle Module Test Report

**Date:** 2026-04-22 | **Time:** 21:55 | **Test Suite:** vitest 4.1.4

---

## Test Execution Summary

**Status:** ✅ ALL PASS

**Test files created:** 5  
**Total tests added:** 90  
**Total LOC:** 1,214 (including docstrings and structure)

### Breakdown by file:
- `api-client.test.js` — 173 LOC, 13 tests
- `state.test.js` — 206 LOC, 19 tests
- `format.test.js` — 73 LOC, 12 tests
- `render.test.js` — 181 LOC, 17 tests
- `handlers.test.js` — 581 LOC, 29 tests

---

## Coverage Results

All tests pass:
- **api-client.test.js**: 13/13 ✅
- **state.test.js**: 19/19 ✅
- **format.test.js**: 12/12 ✅
- **render.test.js**: 17/17 ✅
- **handlers.test.js**: 29/29 ✅

**Full suite health:** 340/340 tests pass (no regressions)

---

## Test Coverage by Module

### `api-client.js` (13 tests)
- ✅ Word2SimError metadata storage (status, body, cause)
- ✅ URL building with query params
- ✅ Parameter URL encoding
- ✅ Error on non-2xx response (status + truncated body capture)
- ✅ Error on invalid JSON response
- ✅ Error on fetch failure (network, timeout)
- ✅ Custom timeout handling
- ✅ User-Agent and Accept headers
- ✅ Trailing slash normalization
- ✅ Undefined/null param filtering
- ✅ randomWord() and similarity() endpoints

### `state.js` (19 tests)
- ✅ saveGame/loadGame round-trip integrity
- ✅ Return null for non-existent games
- ✅ Overwrite on second save
- ✅ Preserve null startedAt
- ✅ clearGame removes entries (idempotent)
- ✅ loadStats defaults (all zeros, bestGuessCount:null)
- ✅ recordResult increments played on every call
- ✅ recordResult accumulates totalGuesses
- ✅ recordResult increments solved only on win
- ✅ bestGuessCount = min(prev, current) on wins only
- ✅ bestGuessCount stays null for loss-only history
- ✅ lastResultAt timestamp recording
- ✅ recordResult returns updated stats

### `format.js` (12 tests)
- ✅ formatWarmth: positive (+73), negative (-04), zero (+00)
- ✅ formatWarmth: rounding to nearest int
- ✅ formatWarmth: boundary cases
- ✅ warmthEmoji: 🥶 < 0.2
- ✅ warmthEmoji: 😐 [0.2, 0.4)
- ✅ warmthEmoji: 🌡️ [0.4, 0.6)
- ✅ warmthEmoji: 🔥 [0.6, 0.8)
- ✅ warmthEmoji: 🎯 >= 0.8

### `render.js` (17 tests)
- ✅ Empty board shows "round ready" prompt
- ✅ Singular/plural "guess" / "guesses"
- ✅ Sort guesses by similarity DESC
- ✅ Cap display at top 15, hide older with footer
- ✅ Singular/plural "older guess" / "guesses"
- ✅ Latest guess marked with ➡️, others with spaces
- ✅ HTML entity escaping in canonical words
- ✅ Warmth emoji in each row
- ✅ No footer when exactly 15 guesses
- ✅ Returns HTML `<pre>` block format
- ✅ renderGuess single-line summary
- ✅ renderGuess escapes HTML special chars
- ✅ renderGuess wraps in `<code>` tags
- ✅ renderGuess includes emoji and signed percent

### `handlers.js` (29 tests)
**Flow tests:**
- ✅ Start new round with no args
- ✅ Show board after fresh start
- ✅ Reuse existing unsolved game
- ✅ Start fresh after solve
- ✅ Submit guess and append to board
- ✅ Solve on case-insensitive match
- ✅ Clear game + record result on solve
- ✅ Reject invalid shape guess (no API call)
- ✅ Reject OOV guess, don't persist to board
- ✅ Deduplicate re-submitted words
- ✅ Set startedAt on first guess
- ✅ Include latest-guess marker in renders
- ✅ Normalize guess to lowercase before API
- ✅ Normalize whitespace in arguments

**Error handling:**
- ✅ Reply UPSTREAM_FAIL on randomWord error
- ✅ Reply UPSTREAM_FAIL on similarity error
- ✅ Handle missing subject (cannot identify chat)

**Group chat:**
- ✅ Group chat resolves to chat.id (shared game)
- ✅ Private chat resolves to user.id (per-user)

**handleNew tests (5):**
- ✅ Start fresh with no prior game
- ✅ Abandon unsolved game + record non-solve
- ✅ Don't record if game had zero guesses
- ✅ Reply UPSTREAM_FAIL on error
- ✅ Handle group chat

**handleGiveup tests (4):**
- ✅ Reveal target and clear game
- ✅ Record non-solve result
- ✅ Reply "no active round" when none exists
- ✅ Escape HTML in target reveal

**handleStats tests (5):**
- ✅ Show default message for new user
- ✅ Show stats after games
- ✅ Show "—" for bestGuessCount when no solves
- ✅ Calculate solve percentage correctly (e.g., 50%)
- ✅ Format average guesses per round
- ✅ Include HTML formatting in reply

---

## Key Edge Cases Tested

1. **Case sensitivity**: Guess "APPLE" solves against target "apple" ✅
2. **OOV handling**: Words not in vocab rejected without board mutation ✅
3. **Deduplication**: Re-submitted words don't inflate board or stats ✅
4. **HTML escaping**: `<script>`, `&`, `>`, `"` properly escaped in renders ✅
5. **Boundary conditions**:
   - Exactly 15 guesses: no "hidden" footer ✅
   - 16 guesses: shows "1 older guess" (singular) ✅
   - 20 guesses: shows "5 older guesses" (plural) ✅
6. **Stats precision**:
   - bestGuessCount min only on wins ✅
   - Stays null if all games lost ✅
   - Solve rate rounded: 2 played, 1 solved = 50% ✅
7. **Subject resolution**: DM → user.id, group → chat.id ✅
8. **Fetch stub patterns**: No real HTTP calls, all mocked with vi.fn() ✅

---

## Linting & Formatting

**Status:** ✅ CLEAN

- Biome checks: PASS
- ESLint checks: PASS
- Import sorting: PASS (alphabetical per biome rules)
- Trailing commas: PASS
- Line width (<100 chars): PASS
- Indentation (2-space): PASS

---

## Integration with Project

**Test infrastructure used:**
- `fake-kv-namespace.js` for KV isolation
- `createStore(moduleName, {KV: fakekv})` for module-namespaced state
- Hand-rolled stubbed client `{randomWord, similarity}` with `vi.fn()`
- `makeCtx()` helper for realistic grammY context objects

**No code changes to source:** All 8 source files (`src/modules/semantle/*.js`) remain untouched.

---

## Full Test Suite Health

```
Test Files: 32 passed (32)
Tests:      340 passed (340)
Duration:   3.48s
```

**Baseline before tests:** 250 tests across 27 files (wordle, trading, loldle, util, registry, etc.)  
**New tests added:** 90 tests across 5 files (semantle)  
**Total:** 340 tests, no regressions

---

## Summary

Comprehensive unit test coverage for the semantle module with 90 tests across 1,214 LOC. All tests validate happy paths, error scenarios, edge cases, and HTML rendering safety. No regressions in existing 250-test suite.

**Ready for deployment.**

---

## Unresolved Questions

None. All test scenarios from phase-03 spec implemented and passing.
