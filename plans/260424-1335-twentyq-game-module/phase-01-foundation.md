# Phase 01 — Foundation

## Context links

- Plan overview: `./plan.md`
- Module pattern reference: `src/modules/doantu/`, `src/modules/loldle/`
- KV state pattern: `src/modules/doantu/state.js`
- Module contract: `CLAUDE.md` § "Module Contract"
- Workers AI binding: `wrangler.toml [ai]` (already wired)

## Overview

- **Priority:** P1 (foundation — blocks all other phases)
- **Status:** planned
- **Description:** Create the module scaffold, seed list, KV state layer, prompt
  templates, and environment wiring. No AI calls yet, no command handlers — just
  the data + structure pieces.

## Key insights

- Workers AI binding `env.AI` already exists (used by semantle/doantu for
  embeddings). New module just calls `env.AI.run(modelId, ...)`.
- Module folder name MUST equal the registry key MUST equal the `name:` field.
- KV is the only storage needed — no D1, no migrations, no cron.
- Seeds live in source (not KV) — small enough (~60 entries) and changes ship
  with deploy. Avoids cold-fetch latency on first round.

## Requirements

### Functional
- Seed list defines categories + objects. Each entry: `{ category, object, initialHint }`.
- Categories: instrument, animal, food, vehicle, sport, household.
- 8–12 objects per category (60–72 total).
- Each entry has a hand-curated initial hint that nudges without revealing.
- KV state per subject: active game + lifetime stats.
- Game record TTL: 7 days (matches doantu pattern).

### Non-functional
- All files <200 LOC each (split if approaching).
- JSDoc typedefs for game/stats/seed shapes.
- No external network calls in this phase.

## Architecture

```
src/modules/twentyq/
├── index.js          # placeholder export — wired up fully in phase 3
├── seeds.js          # SEEDS const + getRandomSeed(rng?)
├── state.js          # loadGame, saveGame, clearGame, loadStats, recordResult
├── prompts.js        # buildSystemPrompt(seed, history) + function-call schema
└── README.md         # initial scaffold docs
```

KV layout (under `twentyq:` prefix):

| Key | Value |
|-----|-------|
| `game:<subject>` | `{ category, target, initialHint, startedAt, solved, turns[] }` (TTL 7d) |
| `stats:<subject>` | `{ played, solved, totalTurns, bestTurnCount, lastResultAt }` |

Each `turns[]` entry: `{ text, isGuess, answer: "yes" \| "no", hint, ts }`.

## Related code files

### Create
- `src/modules/twentyq/index.js` — minimal `{ name: "twentyq", commands: [] }` placeholder
- `src/modules/twentyq/seeds.js` — `SEEDS` array + `getRandomSeed(rng=Math.random)`
- `src/modules/twentyq/state.js` — KV load/save/clear + stats recording
- `src/modules/twentyq/prompts.js` — `buildSystemPrompt(state)` + `ANSWER_FUNCTION_SCHEMA`
- `src/modules/twentyq/README.md` — initial doc stub (filled out fully in phase 4)

### Edit
- `src/modules/index.js` — add `twentyq: () => import("./twentyq/index.js")`
- `wrangler.toml` — append `,twentyq` to `MODULES`
- `.env.deploy.example` — append `,twentyq` to documented `MODULES` line

## Implementation steps

1. Create `src/modules/twentyq/seeds.js`:
   - Export `SEEDS` array of `{ category, target, initialHint }`. Lowercase
     `target`. Initial hint must NOT contain target word or close cognates.
   - Export `getRandomSeed(rng = Math.random)` returning one entry; `rng` param
     enables deterministic tests.
2. Create `src/modules/twentyq/state.js`:
   - Constants: `GAME_TTL_SECONDS = 7 * 24 * 3600`.
   - `gameKey(subject) => "game:" + subject`, `statsKey(subject) => "stats:" + subject`.
   - `loadGame`, `saveGame`, `clearGame`, `loadStats` — direct mirror of
     `doantu/state.js`, but `turns[]` instead of `guesses[]`.
   - `recordResult(db, subject, { solved, turnCount })` — increments stats,
     tracks `bestTurnCount` (lowest among solved rounds).
3. Create `src/modules/twentyq/prompts.js`:
   - `buildSystemPrompt(state)` — string template that injects:
     `secret`, `category`, `initialHint`, last 5 turns of `{question, answer, hint}`.
     Tells the model: judge truthfulness, set `is_guess` when input names a
     specific concrete noun matching/close to `secret`, never reveal `secret`
     unless `is_guess && answer==="yes"`.
   - `ANSWER_FUNCTION_SCHEMA` — JSON schema for `submit_answer` tool with
     `is_guess: boolean`, `answer: "yes"|"no"`, `hint: string` (max 120 chars).
4. Create `src/modules/twentyq/index.js`:
   - Minimal scaffold: `{ name: "twentyq", commands: [] }` + JSDoc header.
   - Phase 3 expands with real handlers.
5. Edit `src/modules/index.js` — add the lazy loader line.
6. Edit `wrangler.toml` `[vars] MODULES` — append `,twentyq`.
7. Edit `.env.deploy.example` — match the comment update.
8. Run `npm run lint` and `npx vitest run` to confirm scaffold doesn't break
   anything (registry conflict check, etc.).

## Todo list

- [ ] `seeds.js` — SEEDS array + getRandomSeed
- [ ] `state.js` — KV layer mirroring doantu pattern, with `turns[]` shape
- [ ] `prompts.js` — system prompt builder + function schema
- [ ] `index.js` — minimal `{ name, commands: [] }` scaffold
- [ ] Update `src/modules/index.js` registry
- [ ] Update `wrangler.toml` MODULES var
- [ ] Update `.env.deploy.example` MODULES comment
- [ ] `npm run lint` + `npx vitest run` pass

## Success criteria

- New module loads without registry errors.
- `npx vitest run` exits 0 (no new tests yet, but no regressions).
- `npm run lint` clean.
- `wrangler dev` boots and `/help` shows no twentyq commands yet (zero commands
  registered — expected).

## Risk assessment

- **Seed quality** — if initial hints are too revealing or too vague, gameplay
  feels off. Mitigation: hand-curate; revise after manual test in phase 3.
- **MODULES var drift** — `wrangler.toml` and `.env.deploy` MUST match. Doc
  the requirement in commit message.

## Security considerations

- Seeds live in source — no PII, no secrets.
- KV writes scoped to `twentyq:` prefix via `createStore` — cannot leak across
  modules.

## Next steps

→ Phase 02 — wrap Workers AI binding into a typed client + add input validator.
