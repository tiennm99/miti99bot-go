# Phase 03 — Quote module (`loldle-quote`)

## Context

- [Research: quote mode](../reports/researcher-260424-2215-loldle-quote-mode.md)
- Template: `src/modules/loldle-emoji/` (phase 02) and `src/modules/loldle/`.
- Dependency: phase 01 (`quotes.json` written, `normalize` helper).

## Overview

**Priority:** P1 (ship in parallel with emoji).
**Status:** pending.

Guess the champion from a voice-line text. Text-only for MVP — audio is
explicitly out of scope (bandwidth, CDN rehost TOS risk, storage overhead
per research doc §4).

## Key insights

- Quote mode on loldle.net: **binary right/wrong, 6 guesses, audio hint
  unlocks after all fails**. We drop audio; keep 6 guesses.
- Quotes can be ambiguous ("For glory!" — Garen, Galio, Jarman...). Fewer
  clues than classic; that's OK — the mode IS meant to be hard.
- Pool: ~150 champions with quotes (per research). Every champion in
  `quotes.json` must have non-empty `quote` string — filter at load time.

## Requirements

**Functional**
- `/loldle_quote` → show current quote or start fresh; submit a guess if arg.
- `/loldle_quote_giveup` → reveal, record loss.
- `/loldle_quote_stats` → per-subject stats.
- 6 guesses.
- Same subject resolution (user id DM / chat id group).

**Non-functional**
- Pure KV. Prefix: `loldle-quote:`.
- Same round state shape as classic and emoji.
- HTML-escape the quote text before putting it inside `<i>…</i>` so
  apostrophes / `<` in a quote don't break render.

## Architecture

```
src/modules/loldle-quote/
├── index.js
├── handlers.js
├── state.js        # MAX_GUESSES = 6
├── lookup.js       # (near-copy of emoji's)
├── render.js       # quote block + guesses list
├── quotes.json     # [{ championName, quote:"..." }, ...] (generated)
└── README.md
```

## Related code files

**Modify**
- `src/modules/index.js` — register `"loldle-quote"`.
- `wrangler.toml` `[vars].MODULES` + `.env.deploy` — append.

**Create**
- Seven files listed in Architecture above.

## Implementation steps

1. **Copy `loldle-emoji/` as scaffold.** It's 95% the same shape.

2. **Swap payload:** `emojis.json` → `quotes.json`. `emojis` string field
   → `quote` string field.

3. **`state.js`** — `MAX_GUESSES = 6`.

4. **`render.js`** — show quote as italic block:
   ```
   🎭 <i>"The true face of desire."</i>

   Guesses (n/6):
     • Ahri  ❌
   ```
   HTML-escape the quote BEFORE wrapping it in `<i>`. HTML-escape each
   champion name.

5. **`handlers.js`** — port emoji handlers with copy tweaked:
   - Welcome line: "🎭 Guess the champion from this quote."
   - Win: "🎉 Nailed it! <champ>."
   - Loss: "❌ Answer: <champ>."
   - No stickers for v1.

6. **`lookup.js`** — identical to emoji's, change import path.

7. **`index.js`**:
   ```js
   commands:
     loldle_quote         (public)
     loldle_quote_giveup  (public)
     loldle_quote_stats   (public)
   ```

8. **Register** in `src/modules/index.js` + MODULES env in both
   `wrangler.toml` and `.env.deploy`.

9. **README.md**: commands, KV prefix, data source note, "audio hint not
   implemented — see phase plan for rationale".

10. **Smoke-test** in `wrangler dev` (same protocol as phase 02).

## Todo

- [ ] Scaffold folder by copying `loldle-emoji/`
- [ ] Repoint JSON import to `quotes.json`
- [ ] MAX_GUESSES = 6
- [ ] Render quote as italic HTML block (escaped)
- [ ] Copy tweaks in handlers
- [ ] Register + MODULES env
- [ ] README
- [ ] Smoke-test

## Success criteria

- Module loads, commands respond.
- Quotes render cleanly in Telegram (no HTML-injection bugs with special
  chars in a champion's quote).
- Stats persist per mode.

## Risks

| Risk | Mitigation |
|------|-----------|
| Quote ambiguity → frustration | Accepted design tradeoff; document in README |
| Quote includes HTML metacharacters (`<`, `&`) from loldle.net | Always HTML-escape before `<i>` wrap |
| `quotes.json` missing quote for some newly-added champion | Filter pool to non-empty quotes at import-time |

## Security

- Escape quote text BEFORE rendering (quote content is third-party data
  from loldle.net scrape).
- Escape user-submitted guess text in replies.

## Open questions

- Should audio hint ever ship? (Post-MVP, gated on user demand. Would
  require a cron that pre-fetches audio URLs from LoL Wiki and stores in
  R2. Not in this plan.)

## Next steps

Phase 06 adds tests. Phases 04/05 (image modes) proceed independently.
