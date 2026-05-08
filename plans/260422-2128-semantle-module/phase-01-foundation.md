# Phase 1 — Foundation

Module scaffold, KV state, word2sim HTTP client, env wiring.
Nothing playable at the end of this phase, but all glue is in place.

## Context links

- Overview: `./plan.md`
- Existing parallel module: `src/modules/loldle/` (follow shape)
- External API contract: `tiennm99/word2sim` repo `README.md`
  - `GET /random?min_rank&max_rank&alpha_only&min_len&max_len` → `{word, rank}`
  - `GET /similarity?a&b` → `{a, b, canonical_a, canonical_b, in_vocab_a, in_vocab_b, similarity}`

## Files to create

### `src/modules/semantle/index.js` (~45 LOC)
Module export. Mirrors `src/modules/loldle/index.js`:
- Captures `{db, env}` in `init({db, env})` — `env` is new vs loldle (need `WORD2SIM_API_URL`).
- Exposes 5 commands (see `plan.md`). Each handler closure gets `(ctx, { db, apiBase })`.

### `src/modules/semantle/api-client.js` (~80 LOC)
Thin wrapper over word2sim HTTP endpoints. Example shape:
```js
export function createClient(apiBase) {
  return {
    randomWord: (opts) => fetchJson(`${apiBase}/random`, opts),
    similarity: (a, b) => fetchJson(`${apiBase}/similarity`, { a, b }),
  };
}
```
- Normalize `apiBase` to strip trailing slash.
- Build query strings with `URLSearchParams`.
- Timeout via `AbortController` (5s).
- Throw `Word2SimError` with `{status, body}` on non-2xx; caller decides user-facing message.
- `User-Agent: miti99bot/semantle` header for traceability.

### `src/modules/semantle/state.js` (~100 LOC)
KV persistence. Key layout under `semantle:` prefix:
- `game:<subject>` → `{target, startedAt, solved, guesses:[{word, canonical, similarity}]}`
  — `target` stored lowercased; solve = `canonical.toLowerCase() === target`.
- `stats:<subject>` → `{played, solved, totalGuesses, bestGuessCount, lastResultAt}`

Exports:
- `loadGame(db, subject) → GameState | null`
- `saveGame(db, subject, state)` — TTL `60*60*24*7` (7d)
- `clearGame(db, subject)`
- `loadStats(db, subject) → Stats` (returns defaults if missing)
- `recordResult(db, subject, {solved, guessCount})`
  - Increments `played`, `solved` (if solved), `totalGuesses += guessCount`.
  - `bestGuessCount = min(bestGuessCount ?? ∞, guessCount)` on solved.
  - Writes `lastResultAt = Date.now()`.

## Files to edit

### `src/modules/index.js`
Add one line to the static import map, alphabetically after `misc`:
```js
semantle: () => import("./semantle/index.js"),
```

### `wrangler.toml`
- In `[vars]` append `semantle` to `MODULES`.
- Add `WORD2SIM_API_URL = "https://word2sim.sg.miti99.com"` to `[vars]`.

### `.dev.vars.example`
Add optional override:
```
# Optional: override for local/self-hosted word2sim instance
# WORD2SIM_API_URL=http://localhost:8000
```

## Implementation steps

1. Create folder + empty stubs for all files.
2. Wire `src/modules/index.js` entry.
3. Wire `wrangler.toml` vars; confirm `npm run dev` boots without error.
4. Implement `api-client.js`; ad-hoc test with `wrangler dev` + curl to ensure the hosted instance responds.
5. Implement `state.js`.
6. `index.js` with placeholder handlers that return "not implemented yet".

## Todo

- [ ] `src/modules/semantle/` folder + empty files
- [ ] Register in `src/modules/index.js`
- [ ] Update `wrangler.toml` `MODULES` + `WORD2SIM_API_URL`
- [ ] Update `.dev.vars.example` with optional override comment
- [ ] Implement `api-client.js` (2 methods + `Word2SimError` + timeout)
- [ ] Implement `state.js` (load/save/clear + stats + recordResult)
- [ ] Placeholder `index.js` export + noop handlers
- [ ] `npm run dev` boots without errors; `/semantle` reply shows "not implemented"

## Success criteria

- Dev server starts with `semantle` listed in modules.
- Placeholder `/semantle` command responds in Telegram (polling via dev webhook or logs).
- `api-client.js` callable from a node REPL or test file against the live service.
- No biome/eslint warnings.

## Risk

- **Cloudflare Worker egress to word2sim** — ensure `fetch()` to the SG subdomain
  is not blocked by any Worker networking policy. Expected fine; same pattern as
  `trading/prices.js` and `lolschedule/api-client.js`.
- **KV size per game** — just target + guess history (a few KB even after hundreds
  of guesses). Well under KV value limit.

## Security

- No secrets added; `WORD2SIM_API_URL` is a public endpoint.
- All user input goes into URL query params — rely on `URLSearchParams` encoding
  to avoid injection; never concatenate user strings into URLs directly.

## Next

→ Phase 2 `phase-02-gameplay.md` — real handlers and rendering.
