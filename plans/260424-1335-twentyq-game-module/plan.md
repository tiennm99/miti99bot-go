---
name: twentyq-game-module
status: completed
created: 2026-04-24
completed: 2026-04-24
slug: twentyq-game-module
blockedBy: []
blocks: []
---

# TwentyQ Game Module — miti99bot

A reverse-Akinator yes/no guessing game. Bot picks a secret object from a fixed
seeded category list, gives an initial hint. User asks `is it ...?` style
questions; Workers AI (`@cf/google/gemma-4-26b-a4b-it`) judges each input,
returns `{is_guess, answer:"yes"|"no", hint}`. Round ends on correct guess
(`is it an organ?` matches secret) or `/twentyq_giveup`. Unlimited tries.

**Key external dependency:** Workers AI binding `env.AI` (already wired in
`wrangler.toml [ai]`). Gemma 4 26B A4B chosen for function-calling +
reasoning + cheap MoE inference (~4B active params).

## Commands

| Command | Description |
|---------|-------------|
| `/twentyq` | Show current board (initial hint + Q/A history), or start a round if none |
| `/twentyq <question>` | Submit a yes/no question OR a final guess (`is it ...?`) |
| `/twentyq_giveup` | Reveal the secret and end the round (next `/twentyq` starts fresh) |
| `/twentyq_stats` | Show per-subject stats |

## Example flow

```
/twentyq
Bot: 🎯 I'm thinking of an instrument.
     Hint: it uses wind to create sound.

/twentyq does it require hands to play?
Bot: ✅ Yes. Hint: most players use both hands at once.

/twentyq is it made of wood?
Bot: ❌ No. Hint: its body is mostly metal pipes.

/twentyq is it an organ?
Bot: 🎉 Correct! It was an organ. Solved in 3 guesses.
```

## Key design decisions

1. **Module name `twentyq`** — picked over `doandao`/`akiverse` for English clarity.
2. **English-only replies** — single-language prompt simplifies model behavior.
3. **Unlimited turns** — solve or giveup ends the round (matches semantle/doantu).
4. **Fixed seed list** — `seeds.js` has ~60 objects across 6 categories
   (instrument, animal, food, vehicle, sport, household). Cheap, deterministic,
   no AI cost for selection.
5. **AI for answer + hint only** — model receives `{secret, category, history}`
   each turn; emits structured `{is_guess, answer, hint}` via function calling.
6. **Pre-validate input** — reject open-ended questions (`what`/`how`/`why`/`which`)
   client-side. Saves Neurons. Doesn't count toward guess tally.
7. **Same command for ask + guess** — AI sets `is_guess=true` when user asks
   `is it [specific noun matching/close to secret]?`. Bot ends round on match.
8. **Visibility: `public`** — appears in Telegram `/` menu + `/help`.

## Phases

| Phase | File | Focus | Est. LOC |
|-------|------|-------|---------:|
| 1 | `phase-01-foundation.md` | Module scaffold, seeds, KV state, prompt templates, env wiring | ~220 |
| 2 | `phase-02-ai-client.md` | Workers AI client + function-calling schema + input validation | ~150 |
| 3 | `phase-03-gameplay-handlers.md` | Command handlers, render, round lifecycle | ~280 |
| 4 | `phase-04-tests-docs.md` | Vitest coverage, README, help integration | ~200 |

## Critical files

- **Create:** `src/modules/twentyq/{index,ai-client,seeds,state,handlers,render,prompts,validate-input}.js` + `README.md`
- **Edit:** `src/modules/index.js` (loader entry), `wrangler.toml` (`MODULES` list), `.env.deploy.example` (`MODULES` list comment)
- **Test:** `tests/modules/twentyq/{seeds,state,validate-input,ai-client,handlers,render}.test.js`

## Open questions

- **Per-chat shared round vs per-subject?** Defaulting to per-subject (user id in
  DMs, chat id in groups) — matches doantu/semantle. Group play uses chat-id
  scope so all members collaborate on one round.
- **AI temperature?** Locking to `0.3` for consistent yes/no determinism — can
  bump to `0.7` for hint variety in a follow-up tweak.
- **Hint repetition?** Initial implementation makes no effort to track hint
  uniqueness across the round — relying on the model + history context to vary.
  Add dedup later if observed boring.
