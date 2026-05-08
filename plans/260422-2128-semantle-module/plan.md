---
name: semantle-module
status: completed
created: 2026-04-22
completed: 2026-04-22
slug: semantle-module
blockedBy: []
blocks: []
---

# Semantle Module — miti99bot

Add a Telegram game module mirroring `loldle`/`wordle`, but powered by word2vec
cosine similarity via the hosted `word2sim` API. Unlimited guesses per round.

**External dependency:** https://word2sim.sg.miti99.com/ (our own hosted instance;
see `tiennm99/word2sim` repo).

## Commands

| Command | Description |
|---------|-------------|
| `/semantle` | show current board, or submit a guess (arg) |
| `/semantle <word>` | submit a guess |
| `/semantle_giveup` | reveal the secret and end the round |
| `/semantle_new` | abandon round + start fresh (same as wordle pattern) |
| `/semantle_stats` | show per-subject stats |

## Key differences from loldle/wordle

- **Unlimited guesses** — no MAX cap; round ends only on solve, giveup, or `_new`.
- **Continuous score** — each guess returns cosine similarity ∈ [−1, 1] scaled to
  0–100 "warmth" for display. No rank concept.
- **Case-insensitive match** — target stored lowercase; guess canonical form is
  lowercased before equality check.
- **Network-bound** — calls word2sim per guess; needs graceful fallback.
- **Stats model** — `{played, solved, totalGuesses, bestGuessCount}` (no streak,
  since there is no loss state).

## Phases

| Phase | File | Focus | Est. LOC |
|-------|------|-------|---------:|
| 1 | `phase-01-foundation.md` | module scaffold, KV state, word2sim api-client, env wiring | ~220 |
| 2 | `phase-02-gameplay.md` | handlers, lookup, render, format | ~310 |
| 3 | `phase-03-tests-docs.md` | vitest coverage, README, help-command integration | ~180 |

## Critical files

- **Create:** `src/modules/semantle/{index,api-client,state,handlers,lookup,render,format}.js` + `README.md`
- **Edit:** `src/modules/index.js` (register loader), `wrangler.toml` (MODULES list + `WORD2SIM_API_URL` var), `.dev.vars.example` (optional override)
- **Test:** `tests/modules/semantle/*.test.js` (stub `global.fetch`)

## Design decisions (locked in unless overturned)

1. **Subject resolution** — same as loldle: user id in DMs, chat id in groups.
2. **Round start** — call `/random` once to pick the target; store it lowercased.
   No `/neighbors` call, no rank cache.
3. **Random filters** — `/random?min_rank=500&max_rank=20000&min_len=4&max_len=10&alpha_only=true`.
4. **Per guess** — one `/similarity?a=<target>&b=<guess>` call. Solve when
   `canonical_b.toLowerCase() === target` (case-insensitive exact match).
5. **Board sort** — all guesses sorted by similarity desc; highlight latest guess.
6. **OOV handling** — if `/similarity` returns `in_vocab_b: false`, reply "unknown word"
   and do NOT append to guesses (no cost).
7. **Env var** — `WORD2SIM_API_URL` in `wrangler.toml [vars]`, default `https://word2sim.sg.miti99.com/`.

## Open questions

- Per-chat daily shared secret (like the real Semantle) vs per-subject random? Defaulting to per-subject random — daily mode is a later add-on.
