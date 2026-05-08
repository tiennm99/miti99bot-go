# Phase 03 — Gameplay Handlers + Render

## Context links

- Plan overview: `./plan.md`
- Foundation pieces: `./phase-01-foundation.md`
- AI client: `./phase-02-ai-client.md`
- Handler pattern reference: `src/modules/doantu/handlers.js`
- Render reference: `src/modules/doantu/render.js`
- Subject resolution + grammY ctx: `src/modules/loldle/handlers.js`

## Overview

- **Priority:** P1
- **Status:** planned
- **Description:** Wire all four commands (`/twentyq`, `/twentyq_giveup`,
  `/twentyq_stats`, plus the implicit ask/guess flow via `/twentyq <text>`)
  to the seeds + state + AI client. Build the renderer for board snapshots and
  per-turn replies. Manage round lifecycle: start → answer turns → solve/giveup.

## Key insights

- grammY `ctx.match` holds the slash-command argument string (everything after
  `/twentyq`). Empty `ctx.match` → board view OR start fresh round.
- Subject = user id in DMs (`ctx.chat.type === "private"`), chat id otherwise.
  Mirror `doantu/handlers.js` resolver.
- Auto-start rule: `/twentyq` with no args AND no active game → start a round.
  With args → submit input (start a round first if none).
- After a `solved` round: next `/twentyq` (any form) clears + starts fresh.
- Use Telegram HTML mode for output (matches loldle/doantu).

## Requirements

### Functional
- `/twentyq` (no args) — show board if active, else start a round and show
  intro line + initial hint.
- `/twentyq <text>` — validate input → if invalid, reply with rephrase hint
  (no state mutation, no AI call). If valid, call `judge`, append turn, reply
  with `yes/no + hint`. If `is_guess && answer==="yes"`, mark solved, record
  stats, reveal secret, congratulate.
- `/twentyq_giveup` — if active round, reveal secret + record loss; clear
  game key. Idempotent if no active round (replies "no active round").
- `/twentyq_stats` — render `{played, solved, totalTurns, bestTurnCount}`.
- Repeat-question detection: simple lowercased exact-text dedup against prior
  turns. If repeat → reply `🔁 already asked` and skip AI call (no count).

### Non-functional
- Each handler ≤80 LOC.
- HTML escape all user-rendered text via existing `src/util/escape-html.js`.
- Surface `UpstreamError` as a friendly "AI service hiccup, try again" reply.
- Each file ≤200 LOC.

## Architecture

```
src/modules/twentyq/
├── handlers.js     # handleTwentyq, handleGiveup, handleStats — the entry points
├── render.js       # formatBoard, formatTurnReply, formatGiveup, formatStats, formatIntro
└── index.js        # full module export with all four commands wired
```

```
ctx ──► handleTwentyq ──► loadGame
              │              │
              ├── empty arg ─┴── present? show board   : start round (intro)
              │
              └── arg present ─► validateQuestion → judge → save turn → reply
```

## Related code files

### Create
- `src/modules/twentyq/handlers.js`
- `src/modules/twentyq/render.js`

### Edit
- `src/modules/twentyq/index.js` — replace phase-1 stub with real commands array.

## Implementation steps

1. Create `src/modules/twentyq/render.js`:
   - `formatIntro(state)` → `"🎯 I'm thinking of a <category>.\nHint: <initialHint>"`.
   - `formatTurnReply({ answer, hint, isGuess, solved, target, turnCount })`:
     - solve win → `"🎉 Correct! It was <b>{target}</b>. Solved in {turnCount} questions."`
     - guess miss → `"❌ No. Hint: {hint}"`
     - regular yes → `"✅ Yes. Hint: {hint}"`
     - regular no → `"❌ No. Hint: {hint}"`
   - `formatBoard(state)` — initial hint + numbered list of past Q/A in `<pre>`.
   - `formatGiveup(state)` — `"🏳️ Gave up. The answer was <b>{target}</b>."`
   - `formatStats(stats)` — terse multi-line summary.
   - All target/text values HTML-escaped.
2. Create `src/modules/twentyq/handlers.js`:
   - `resolveSubject(ctx)` — same as doantu (private → user id, else chat id).
   - `handleTwentyq(ctx, { db, env })`:
     - Subject = resolveSubject.
     - `state = await loadGame(db, subject)`.
     - If state and `state.solved` → clearGame + treat as no game.
     - If no state and no `ctx.match` → start a round (call `getRandomSeed`,
       build state, save, reply `formatIntro`).
     - If no state and `ctx.match` → start round THEN process input as turn.
     - If state and no `ctx.match` → reply `formatBoard(state)`.
     - If state and `ctx.match` → process turn (see below).
   - Process-turn block:
     - `validateQuestion(text)` → on fail reply with reason.
     - Repeat-text check against `state.turns[].text` (lowercased) → reply
       `🔁 already asked`.
     - `await judge(env, state, text)`. Catch `UpstreamError` → friendly reply.
     - Append turn to `state.turns`. If `result.is_guess && result.answer === "yes"`:
       set `state.solved = true`; recordResult({solved:true, turnCount: turns.length}); clearGame.
     - Else save updated state.
     - Reply with `formatTurnReply(...)`.
   - `handleGiveup(ctx, { db })`:
     - Load game; if none → "no active round".
     - Else → recordResult({solved:false, turnCount}); reveal target;
       clearGame; reply `formatGiveup(state)`.
   - `handleStats(ctx, { db })` — load + render.
3. Replace `src/modules/twentyq/index.js`:
   - Mirror doantu shape: closure-scoped `db` set in `init`, plus `env` passed
     through to handlers (because we need `env.AI`). Two options:
       - **Option A (cleaner):** capture `env` in `init` alongside `db` and
         hand both to handlers.
       - **Option B:** pass `env` as `ctx.env` (grammY already exposes it via
         the worker handler binding). Confirm at impl time; if not exposed,
         use Option A.
   - Register 4 commands: `twentyq`, `twentyq_giveup`, `twentyq_stats`, plus a
     hidden alias if useful (skip for now per YAGNI).
4. Manual smoke test in `wrangler dev` (use ngrok / cloudflared tunnel + a
   throwaway test bot) — verify start, ask, guess-correct, giveup paths.

## Todo list

- [ ] `render.js` — all five formatters with HTML escape
- [ ] `handlers.js` — three handlers, subject resolver, repeat dedup
- [ ] `index.js` — full module export with `init({ db, env })` capture
- [ ] Confirm `env` propagation pattern (capture-in-init vs ctx.env)
- [ ] Manual smoke test happy path + giveup + repeat input
- [ ] `npm run lint` clean

## Success criteria

- Manual flow works end-to-end through Telegram.
- `is_guess && yes` ends round and records solve.
- `/twentyq_giveup` ends round, reveals, records loss.
- Repeat input does NOT increment turn count and does NOT call AI.
- Open-ended question ("what is it?") gets the validator's rephrase reply.
- KV state persists across cold starts (verified by waiting >30s between turns).

## Risk assessment

- **`env` propagation** — modules currently capture only `db` in `init`.
  Doantu/semantle capture `env` only enough to read URL config at init time,
  not for per-request AI calls. Need to capture the full `env` ref or change
  the dispatcher contract. **Decision: capture in `init` closure** — least
  invasive, no framework change.
- **Race condition on rapid double-send** — two near-simultaneous `/twentyq`
  questions could both load state, both write — last writer wins. Acceptable
  for v1 (KV is eventually consistent anyway; users notice nothing in normal
  pacing).
- **AI hint may leak the secret** despite system-prompt instructions.
  Mitigation: post-process hint to redact case-insensitive substring of
  `target`. Add as a defensive filter in `formatTurnReply` (cheap, ~3 lines).

## Security considerations

- All user-controlled strings (input text, target, hint) HTML-escaped before
  rendering.
- No KV keys derived from raw user text — only subject id + literal prefix.
- Secret-leak filter on hints (see Risk above).

## Next steps

→ Phase 04 — vitest coverage, README, help-command verification.
