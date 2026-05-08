# Phase 06 — Tests + docs sync

<!-- Updated: Validation Session 1 - scope narrowed to emoji + quote only -->

## Context

- Existing test patterns: `tests/modules/loldle/`, `tests/modules/wordle/`,
  `tests/modules/trading/`.
- Fakes: `tests/fakes/fake-kv-namespace.js`, `tests/fakes/fake-bot.js`.
- Docs to touch: `README.md`, `docs/adding-a-module.md` (no change needed
  unless the new modules expose a new pattern), potentially a new
  `docs/loldle-modes.md` for the mode roster.
- Blocks: **02 + 03 must be complete.** 04 + 05 are deferred — their tests
  will be added when those phases ship.

## Overview

**Priority:** P1 (closes the plan).
**Status:** pending.

Add focused unit tests for each new module and sync docs. Unit-test only
pure-logic seams (state, lookup, render). Handler tests use fakes — no
workerd, no Telegram fixtures, same convention as `loldle/` tests.

## Key insights

- Each new module mirrors classic's shape closely; tests can be near-
  copies of `tests/modules/loldle/state.test.js` + `lookup.test.js`.
- No integration tests for DDragon (external CDN). Stub `fetch` if any
  unit exercises it; prefer pure functions that take a URL string.
- Skip tests for the scraping scripts — they're network-bound. Manual
  verification (phase 01 success criteria) covers them.

## Requirements

**Functional**
- ≥ 1 test file per new module covering: state round-trip, lookup, render
  / handler happy path.
- `npm test` passes with no regressions.
- `npm run lint` clean.

**Non-functional**
- Coverage not measured explicitly — prioritize meaningful cases over %.
- Don't re-test shared helpers per module — one `normalize-name.test.js`
  is enough.

## Architecture

```
tests/
├── util/
│   └── normalize-name.test.js                 # NEW (this phase)
└── modules/
    ├── loldle-emoji/
    │   ├── state.test.js                      # NEW (this phase)
    │   ├── lookup.test.js                     # NEW (this phase)
    │   └── handlers.test.js                   # NEW (this phase — happy path only)
    ├── loldle-quote/
    │   ├── state.test.js                      # NEW (this phase)
    │   ├── lookup.test.js                     # NEW (this phase)
    │   └── handlers.test.js                   # NEW (this phase)
    ├── loldle-ability/                        # DEFERRED (with phase 04)
    │   ├── state.test.js                      #   slot persistence
    │   └── handlers.test.js                   #   stubs ctx.replyWithPhoto
    └── loldle-splash/                         # DEFERRED (with phase 05)
        ├── state.test.js                      #   skinId persistence
        └── handlers.test.js                   #   stubs ctx.replyWithPhoto
```

## Related code files

**Modify**
- `README.md` — add the four new modes to the architecture snapshot
  (bullet list in `## Architecture snapshot`) and to troubleshooting if
  applicable.
- `docs/architecture.md` — if the project's existing docs list modules,
  mention the loldle family.

**Create**
- Test files listed above.
- `docs/loldle-modes.md` (optional, only if worth it) — one-page
  reference: five modes, what each looks like, command list.

**Delete:** none.

## Implementation steps

1. **`normalize-name.test.js`** — three cases: basic lower+strip, Unicode
   punctuation ("Kai'Sa" → "kaisa"), empty/null input.

2. **Per module `state.test.js`** — use `FakeKvNamespace` +
   `createStore("<module>", { KV: fake })`:
   - Save a game, load it back — deep equal.
   - `clearGame` deletes.
   - `recordResult(true)` increments wins + streak + bestStreak when
     streak exceeds previous best.
   - `recordResult(false)` resets streak to 0.
   - (ability/splash) slot / skinId round-trip.

3. **Per text module `lookup.test.js`** — exact match, case-insensitive,
   punctuation-insensitive, unique-prefix, ambiguous-prefix → null.
   (Could be near-copy of existing loldle lookup test.)

4. **Per module `handlers.test.js`** — use `FakeKvNamespace` + a minimal
   ctx fake: `{ from, chat, message, reply, replyWithPhoto, replyWithSticker }`.
   Walk one happy path: empty state → guess correct → stats incremented.
   For image modes, assert `replyWithPhoto` received a string URL
   starting with `https://ddragon.leagueoflegends.com/`.

5. **Run `npm test`** — confirm all pass. Fix any module code issues
   found.

6. **Update `README.md`**:
   - In `## Architecture snapshot`'s `src/modules/` list, append
     `loldle-emoji/`, `loldle-quote/`, `loldle-ability/`, `loldle-splash/`.
   - No troubleshooting table change needed.

7. **(Optional) `docs/loldle-modes.md`** — single-page mode roster.

8. **Run `npm run lint` + `npm run format`** — clean.

9. **Final smoke-test**: `npm run dev`, test bot, cycle through all five
   loldle commands. Confirm no command conflicts thrown at registry
   build.

## Todo

- [ ] `normalize-name.test.js`
- [ ] Four per-module `state.test.js`
- [ ] Two text-module `lookup.test.js` (emoji, quote — ability/splash
      reuse the same pattern but lookup is trivial, skip if redundant)
- [ ] Four per-module `handlers.test.js`
- [ ] Update README.md architecture snapshot
- [ ] (Optional) docs/loldle-modes.md
- [ ] `npm test` + `npm run lint` + `npm run format` clean
- [ ] Final smoke-test across all 5 loldle commands

## Success criteria

- All new tests pass.
- Classic loldle tests unchanged and still pass.
- `npm run deploy --dry-run` (register:dry) lists all 12 new commands
  (4 modes × 3 commands), with no conflicts.
- README accurately lists new modules.

## Risks

| Risk | Mitigation |
|------|-----------|
| Handler tests drift from actual grammY context shape | Reuse existing loldle handler test scaffolding verbatim |
| Flaky tests due to `Math.random()` in `pickRandomChampion` | Inject `rng` parameter or monkey-patch `Math.random` in tests |

## Security

- Tests use fakes only; no real KV, no real Telegram calls.
- No secrets in test fixtures.

## Open questions

- Cron for periodic DDragon refresh? Out of scope — the scraper runs
  weekly (classic) and we can piggyback ddragon fetch onto the same
  workflow in a follow-up. Not blocking.

## Next steps

After this phase: plan is complete. Run `/ck:plan archive` to close out
and log a journal entry.
