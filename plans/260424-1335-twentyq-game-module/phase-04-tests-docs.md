# Phase 04 — Tests, Docs, Help Integration

## Context links

- Plan overview: `./plan.md`
- Test pattern reference: `tests/modules/doantu/`, `tests/modules/loldle/`
- Fakes: `tests/fakes/fake-kv-namespace.js`, `tests/fakes/fake-bot.js`
- Render module integration: `src/modules/util/` (help command auto-discovers)

## Overview

- **Priority:** P2 (ship-gate — module isn't complete without tests + docs)
- **Status:** planned
- **Description:** Write vitest unit tests covering seeds, state, validator,
  ai-client (with stubbed `env.AI`), handlers (with fake `env.AI`), and render.
  Replace the README stub with a complete module guide. Verify `/help`
  surfaces all four commands.

## Key insights

- Workers AI binding is a plain JS object — stub it as
  `{ run: vi.fn().mockResolvedValue({...}) }` in tests. No workerd, no MSW.
- Repo convention: tests use **injected fakes**, not `vi.mock`. Pass fake
  modules through handler `{ db, env }` arg explicitly.
- `/help` auto-includes any module with public/protected commands — no extra
  wiring. Just confirm by inspecting `npm run register:dry` output.

## Requirements

### Functional (test coverage)
- `seeds.test.js` — every seed has non-empty `target`, `category`,
  `initialHint`; `getRandomSeed(rng)` deterministic with seeded rng;
  `initialHint` does NOT contain `target` substring (case-insensitive).
- `state.test.js` — round-trip save/load; clear works; stats start zeroed;
  `recordResult` updates fields correctly (solve increments solved + best;
  loss only increments played + totalTurns).
- `validate-input.test.js` — accepts `is/are/does/do/can/has/will/should`
  questions; rejects `what/how/why/which/who`; rejects empty + too-long;
  normalizes whitespace + case.
- `ai-client.test.js` — happy path: stubbed `env.AI.run` returns valid
  function call → judge returns clean shape; bad shape → defensive fallback
  used; thrown error → wrapped in `UpstreamError`.
- `handlers.test.js` — start round (no game, no arg); board view (game, no
  arg); turn flow (yes path + no path); solve flow (`is_guess && yes` ends
  round, records, clears game); giveup; stats; repeat-question dedup;
  validator rejection bypasses AI.
- `render.test.js` — HTML escape: target/hint with `<script>` neutralized;
  formatStats handles zeroed stats; formatBoard renders empty turns array.

### Non-functional
- All tests pure-logic (no network, no `setTimeout`, no real KV).
- Existing 200+ tests must still pass.
- Coverage parity with `doantu` test count (~25–35 tests).
- Docs ≤200 lines.

## Architecture

```
tests/modules/twentyq/
├── seeds.test.js
├── state.test.js
├── validate-input.test.js
├── ai-client.test.js
├── handlers.test.js
└── render.test.js
```

`tests/fakes/fake-ai.js` (new) — `{ run: vi.fn() }` factory with
result-builder helpers (e.g., `okJudgement({ is_guess, answer, hint })`).

## Related code files

### Create
- `tests/modules/twentyq/seeds.test.js`
- `tests/modules/twentyq/state.test.js`
- `tests/modules/twentyq/validate-input.test.js`
- `tests/modules/twentyq/ai-client.test.js`
- `tests/modules/twentyq/handlers.test.js`
- `tests/modules/twentyq/render.test.js`
- `tests/fakes/fake-ai.js`

### Edit
- `src/modules/twentyq/README.md` — replace phase-1 stub with full doc.
- `docs/codebase-summary.md` — add a one-line entry for the new module
  (only if existing modules are listed there).
- `docs/development-roadmap.md` — mark this plan as completed once shipped
  (per global feedback rule: roadmap tracks future work; completed work
  documented in git log + plan file).

## Implementation steps

1. Create `tests/fakes/fake-ai.js`:
   - `createFakeAi()` returning `{ run: vi.fn() }`.
   - Helper `mockJudgement(ai, { is_guess, answer, hint })` configures the
     mock to return a function-call shape matching what `ai-client` parses.
2. Write each test file in order matching the production-file order, using
   the existing doantu tests as the structural template.
3. Run `npx vitest run tests/modules/twentyq/` iteratively until green.
4. Run full suite (`npm test`) — must stay green.
5. Replace `src/modules/twentyq/README.md` with:
   - One-paragraph game description + Telegram `/` slot.
   - Commands table (visibility column).
   - Example flow (copy from `plan.md`).
   - "Data source" — Workers AI Gemma 4 26B A4B + fixed seed list.
   - "Architecture" — file-by-file, ~1 line each.
   - "Storage" — KV layout table (mirror doantu README format).
   - "Config" — env vars table (none in v1; document `env.AI` binding).
   - "Credits" — game concept (20 questions / Akinator-reverse).
6. `npm run register:dry` — confirm `setMyCommands` payload includes
   `twentyq`, `twentyq_giveup`, `twentyq_stats` (all `public`).
7. `npm run lint` — clean.
8. Manual one more end-to-end sanity check via `wrangler dev` + tunnel.

## Todo list

- [ ] `tests/fakes/fake-ai.js` — AI binding stub + helpers
- [ ] `seeds.test.js` (3–5 tests)
- [ ] `state.test.js` (5–7 tests)
- [ ] `validate-input.test.js` (6–8 tests)
- [ ] `ai-client.test.js` (4–6 tests)
- [ ] `handlers.test.js` (8–12 tests — happy path, edge cases, dedup, errors)
- [ ] `render.test.js` (4–6 tests — escape, all formatters)
- [ ] README replacement
- [ ] `register:dry` shows public commands
- [ ] Full `npm test` green
- [ ] Mark plan status `completed` in `plan.md` frontmatter

## Success criteria

- `npm test` green (all 200+ existing + new).
- `npm run lint` green.
- `register:dry` shows the three public commands.
- README opens cleanly, matches doantu/semantle structure.
- Manual play in `wrangler dev` confirms full game loop.

## Risk assessment

- **AI response shape mismatch** between fixture and real Gemma response →
  ai-client tests pass but production breaks. Mitigation: capture one real
  response in dev (logged + redacted); use it as the test fixture canonical.
- **Test flake** — `getRandomSeed` could non-determ if rng default leaks into
  test. Mitigation: always pass deterministic rng in tests.
- **README drift** — multiple modules have similar README shapes; copy from
  doantu and edit, don't write from scratch (consistency).

## Security considerations

- Test fixtures must NOT contain real bot tokens or webhook secrets (none
  needed — all logic-level).
- README must not document any internal endpoint or account id.

## Next steps

After this phase:
- Update `plan.md` frontmatter `status: completed`.
- Commit + push (conventional commit: `feat(twentyq): add reverse-Akinator
  yes/no game module powered by Workers AI`).
- Run `npm run deploy` (auto-applies migrations + registers webhook/commands).
- Smoke test on production bot via `/twentyq`.
