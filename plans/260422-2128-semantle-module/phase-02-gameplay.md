# Phase 2 — Gameplay

Real handlers, guess lookup, board rendering, similarity formatting.
End of phase: module is fully playable in Telegram.

## Context links

- Overview: `./plan.md`
- Prior phase: `./phase-01-foundation.md`
- Pattern to mirror: `src/modules/loldle/handlers.js`, `src/modules/wordle/handlers.js`
- Render pattern: `src/modules/loldle/render.js`

## Files to create

### `src/modules/semantle/lookup.js` (~25 LOC)
- `normalize(raw) → string` — trim, collapse whitespace, lowercase.
- `isValidShape(word) → boolean` — reject empty, length > 64, non-ASCII-letters-only
  (matches `/random` default filter; avoids wasted API round-trips).

### `src/modules/semantle/format.js` (~30 LOC)
- `formatWarmth(similarity) → string` — signed percent: `Math.round(similarity * 100)`
  shown as `+73` / `-04`.
- `progressBar(similarity) → string` — 10-cell unicode bar from `-1..1`
  (use `░▓█`; helps visual scanning). Optional / can be skipped if time-boxed.
- `warmthEmoji(similarity)` — 🥶 (< 0.2) / 😐 (< 0.4) / 🌡️ (< 0.6) / 🔥 (< 0.8) / 🎯 (≥ 0.8)

### `src/modules/semantle/render.js` (~70 LOC)
Telegram HTML `<pre>` monospace block. Two public exports:
- `renderBoard(guesses, latestIndex)` — sort by similarity desc, show at most top 15;
  highlight the latest guess with a leading marker. Each row:
  ```
  #  warmth  word            emoji
  1  +78     sea             🔥
  2  +45     fish            🌡️
  ```
- `renderGuess(entry, position, total)` — single-line summary for a guess that
  fell outside the rendered top-15: `"Your guess 'carpet' → +12"`.
- Header line: `🎯 Semantle — <N> guesses`.
- Footer when solved: `"✅ Solved in <N> guesses!"`.

### `src/modules/semantle/handlers.js` (~170 LOC)
One exported function per command:
- `handleSemantle(ctx, deps)`
- `handleGiveup(ctx, deps)`
- `handleNew(ctx, deps)`
- `handleStats(ctx, deps)`

where `deps = { db, client }`. Shared helpers (subject resolution, arg parsing)
copied from loldle with minimal change.

**Flow for `/semantle <word>`:**

1. Resolve subject; reject if missing.
2. `game = await loadOrStart(db, client, subject)` — lazy-init calls `client.randomWord(...)`;
   target stored lowercased.
3. Normalize the guess (trim, lowercase).
4. `res = await client.similarity(game.target, guess)`.
5. If `!res.in_vocab_b` → reply "🤔 unknown word" without appending.
6. Append `{word:guess, canonical:res.canonical_b, similarity:res.similarity}`;
   set `startedAt` if null; saveGame.
7. If `res.canonical_b.toLowerCase() === game.target` → mark `solved`, `recordResult({solved:true, guessCount})`,
   `clearGame`, reply with board + win message.
8. Else reply with `renderBoard` — guess pool grows unbounded.

**Flow for `/semantle` (no arg):**
- If no active game → lazy-init (but don't call similarity; just show empty board
  with "🆕 Round ready — send your first guess.").
- Else → `renderBoard`.

**Flow for `/semantle_new`:**
- Load current game; if exists and has ≥1 guess, `recordResult({solved:false})`
  and `clearGame`. Then lazy-init a fresh one; reply "🆕 New round started."

**Flow for `/semantle_giveup`:**
- If no active game → "No active round."
- Else reveal `game.target`, `recordResult({solved:false, guessCount: guesses.length})`,
  `clearGame`, reply.

**Flow for `/semantle_stats`:**
- `loadStats(subject)`, render:
  - Played / Solved / Solve rate
  - Total guesses / Best guess count (lowest number of guesses to solve)
  - Average guesses per solve (if `solved > 0`)

## Error handling

- Wrap every `client.*` call in try/catch. On `Word2SimError` or fetch timeout:
  reply `"⚠️ Upstream hiccup — try again in a few seconds."` and log the error
  structured (`console.log(JSON.stringify({msg:"semantle_upstream_fail", ...}))`).
- If `/random` fails, do NOT persist a partial game — user simply retries.

## Implementation steps

1. `lookup.js` first — pure logic, trivial to verify.
2. `format.js` — pure logic, stub renderings.
3. `render.js` — build HTML using phase-1 state shape.
4. Replace placeholder handlers with real implementations, one command at a time:
   `_stats` → `/semantle` (no-arg, empty board) → `/semantle <word>` → `_giveup` → `_new`.
5. End-to-end manual test via `wrangler dev` + Telegram bot or `curl` against the
   webhook endpoint.

## Todo

- [ ] `lookup.js` normalize + isValidShape
- [ ] `format.js` formatWarmth, warmthEmoji, (optional) progressBar
- [ ] `render.js` renderBoard + renderGuess
- [ ] `handlers.js` subject resolver + arg parser (copy from loldle)
- [ ] `handlers.js` handleStats (simplest path, ensures KV wiring works)
- [ ] `handlers.js` handleSemantle no-arg path
- [ ] `handlers.js` handleSemantle guess path (solve / OOV / score)
- [ ] `handlers.js` handleGiveup
- [ ] `handlers.js` handleNew
- [ ] E2E smoke test in Telegram

## Success criteria

- `/semantle` with no arg shows a clean "round ready" message.
- `/semantle apple` returns similarity within ~500ms p50.
- `/semantle <target>` ends the round and updates stats.
- `/semantle_giveup` reveals the target and clears state.
- Out-of-vocab guess does not count against the guess tally.
- Board stays readable up to 100+ guesses (render caps at top 15).

## Risk

- **Latency** — two KV reads + one fetch per guess. Target ≤ 800ms p95. If the
  hosted word2sim cold-starts too slowly, add a periodic cron warmup later.
- **File-size drift** — `handlers.js` at ~170 LOC is close to the 200 cap; if it
  overruns, split `_stats` into its own `stats-handler.js` (pattern used by
  trading module).

## Security

- Treat the guess string as untrusted: escape-html before rendering.
- Do NOT leak `game.target` in any response path except `/semantle_giveup` and
  `handleSemantle` win reply.

## Next

→ Phase 3 `phase-03-tests-docs.md` — coverage, README, `/help` registration.
