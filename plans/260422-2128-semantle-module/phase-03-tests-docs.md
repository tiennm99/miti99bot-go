# Phase 3 — Tests & Documentation

Ship-ready polish: unit-test coverage with fake KV + stubbed fetch, module README,
`/help` integration verification.

## Context links

- Overview: `./plan.md`
- Prior phases: `./phase-01-foundation.md`, `./phase-02-gameplay.md`
- Test pattern: `tests/modules/wordle/` and `tests/modules/trading/` (fetch-stubbing examples)
- Fakes: `tests/fakes/{fake-kv-namespace,fake-bot,fake-modules}.js`

## Files to create

### `tests/modules/semantle/api-client.test.js` (~50 LOC)
Stub `global.fetch` with `vi.fn()`:
- asserts query-string building (rank filters, URL encoding)
- asserts `Word2SimError` on non-2xx
- asserts AbortController timeout path (simulate slow upstream)

### `tests/modules/semantle/state.test.js` (~60 LOC)
Using `fake-kv-namespace`:
- load/save round-trip preserves shape
- clearGame removes entry
- recordResult increments played/solved/totalGuesses correctly
- bestGuessCount is `min(prev, current)` only when solved
- loadStats returns defaults when empty

### `tests/modules/semantle/format.test.js` (~25 LOC)
Pure-function coverage:
- formatWarmth signed/rounded
- warmthEmoji buckets boundary cases

### `tests/modules/semantle/render.test.js` (~40 LOC)
- renderBoard empty state renders "round ready" prompt
- renderBoard sorts by similarity desc (verify first row = highest score)
- renderBoard caps to top 15 when >15 guesses
- renderGuess escapes HTML-unsafe chars in the word

### `tests/modules/semantle/handlers.test.js` (~130 LOC)
Integration-ish: fake KV + stubbed client (not real fetch).
- happy path: round start → guess → guess → solve (case-insensitive) → stats updated
- OOV guess: not appended, no state mutation
- _giveup reveals target and clears game
- _new abandons prior round + clears, records non-solve
- error from client surfaces a user-friendly message; state unchanged
- case sensitivity: guess "APPLE" against target "apple" solves the round

## Files to create (docs)

### `src/modules/semantle/README.md`
Mirror `src/modules/loldle/README.md` shape. Sections:
- **Commands** table (from `plan.md`)
- **Data source** — point at `tiennm99/word2sim` hosted instance
- **Architecture** — list of files and what each does
- **Storage** — KV layout table (`game:<subject>`, `stats:<subject>`)
- **Config** — `WORD2SIM_API_URL` env var
- **Credits** — word2vec / GoogleNews pretrained vectors

## Files to edit

### `docs/adding-a-module.md`
No change expected unless the word2sim env-var pattern is the first of its kind.
If so, add a short note about `[vars]` config for external API bases.

### `scripts/register.js`
Verify `setMyCommands` picks up the new public commands automatically (no code
change expected — registry is the source of truth).

## Todo

- [ ] `api-client.test.js`
- [ ] `state.test.js`
- [ ] `format.test.js`
- [ ] `render.test.js`
- [ ] `handlers.test.js`
- [ ] `src/modules/semantle/README.md`
- [ ] Run `npm test` — all pass
- [ ] Run `npm run lint && npm run format` — clean
- [ ] `npm run register:dry` — confirms `/semantle`, `/semantle_giveup`, `/semantle_new`,
      `/semantle_stats` appear in the command payload
- [ ] Optional: `docs/adding-a-module.md` one-paragraph addition if env-var
      pattern is novel

## Success criteria

- Test coverage for all new files (>80% line coverage, pragmatic thresholds).
- No biome/eslint warnings (project enforces 100-char line width, sorted imports,
  trailing commas).
- `npm run register:dry` output includes all public `/semantle*` commands.
- `src/modules/semantle/README.md` parallel to loldle's in shape and depth.

## Risk

- **Fetch stub drift** — if word2sim adds fields, tests may not catch breaking
  response-shape changes in prod. Mitigation: add a single optional integration
  test (guarded by env flag) that hits the real URL; skip by default.
- **Flaky timing tests** — avoid real `setTimeout` in the AbortController test;
  use `vi.useFakeTimers()` if needed.

## Security

- Tests must not commit real TELEGRAM tokens or call Telegram.
- `.dev.vars` is gitignored; never add secrets to `.dev.vars.example`.

## Next

- Deploy via `npm run deploy` (handles webhook + setMyCommands).
- Monitor the first day of usage via CF Worker logs; look for upstream errors.
- Potential follow-ups (separate plans):
  - Daily shared secret (Semantle-classic mode)
  - Leaderboard module integration
  - Webhook-side warm-up cron to keep word2sim hot
