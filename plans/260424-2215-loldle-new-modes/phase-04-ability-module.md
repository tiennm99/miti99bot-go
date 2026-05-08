# Phase 04 — Ability module (`loldle-ability`)

<!-- Updated: Validation Session 1 - deferred; binary-only confirmed; no cropping -->

## Context

- [Research: ability + splash](../reports/researcher-260424-2215-loldle-ability-splash-modes.md)
- Template: `src/modules/loldle-emoji/` (phase 02).
- Dependency: phase 01 (`abilities.json` from Data Dragon).

## Overview

**Priority:** P2 (image mode, more moving parts than text modes).
**Status:** **DEFERRED** — do not start until emoji + quote modes are live and
user demand for image modes is confirmed. Phase 01 provides the data needed
(`abilities.json`), but DDragon fetch work can also be deferred to this
phase if not required by phases 02/03.

## Validated decisions

- **Binary guess only** (no bonus slot-identification step).
- **No progressive cropping** — full icon from turn 1, 5 guesses.
- Data source confirmed: **Data Dragon CDN** (not loldle.net scrape).

Guess the champion from a single ability icon. Telegram sends the full
Data Dragon icon URL via `sendPhoto`. **No progressive cropping for v1**
(see plan.md for rationale — Cloudflare Images cost + edit-photo jank not
justified by Telegram UX).

## Key insights

- DDragon icon URLs are stable per patch: `cdn/<version>/img/spell/<key>.png`
  (passive uses `/img/passive/`). Version is baked into `abilities.json`
  by phase 01's `fetch-ddragon-data.js` so the bot doesn't need live
  version fetches.
- Pool per champion: 5 abilities (Passive, Q, W, E, R). Pick a random slot
  at round start. Round state stores both target champion AND the slot, so
  subsequent `/loldle_ability` calls re-send the SAME icon.
- Since the full icon is shown from turn 1, difficulty stays high only if
  guesses are tight: **5 guesses**.
- Telegram's `sendPhoto` accepts a URL directly — no download + re-upload.
  Cache the `file_id` returned in the send response? Not worth it for v1.

## Requirements

**Functional**
- `/loldle_ability` → if no active round: pick champion + random slot,
  send photo with caption "Guess the champion from this ability. 0/5 so
  far." If active: re-send the same icon + progress line.
- `/loldle_ability <champion>` → submit guess.
- `/loldle_ability_giveup` → reveal answer + ability name + slot.
- `/loldle_ability_stats` → per-subject stats.
- 5 guesses.

**Non-functional**
- KV prefix: `loldle-ability:`.
- Round state: `{ target, slot:"P|Q|W|E|R", guesses, startedAt }`. Adds
  `slot` vs classic/emoji/quote.
- Re-send photo each turn (no message-edit). Cheap; DDragon CDN is fast.

## Architecture

```
src/modules/loldle-ability/
├── index.js
├── handlers.js
├── state.js          # extended shape: + slot
├── lookup.js
├── abilities.json    # [{ championName, abilities:[{slot, name, icon}] }]
└── README.md
```

Note: no `render.js` — output is a photo + small caption, built inline in
`handlers.js`.

## Related code files

**Modify**
- `src/modules/index.js` — add `"loldle-ability"`.
- `wrangler.toml` + `.env.deploy` MODULES.

**Create**
- Six files listed above.

## Implementation steps

1. **`state.js`** — copy from `loldle-emoji/state.js`, bump shape to
   `{ target, slot, guesses, startedAt }`. `MAX_GUESSES = 5`.

2. **`lookup.js`** — identical to emoji's (pool shape: records still have
   `championName` at top level).

3. **`handlers.js`**:
   - `getSubject`, `argAfterCommand` — copy inline or import from a
     shared helper (optional — three copies is fine).
   - `pickRandomChampion()` — filter to records where abilities array is
     non-empty.
   - `pickRandomSlot(champ)` — uniform over `champ.abilities` slots.
   - On `/loldle_ability` no-arg: send photo (use
     `ctx.replyWithPhoto(url, { caption })`), caption shows guess count.
   - On guess: compare names; on win/loss send a photo reveal with full
     ability name ("That was **Ahri** — _Orb of Deception_ (Q)").
   - On giveup: same reveal.

4. **`index.js`** — three commands, same pattern as phase 02.

5. **Register + MODULES env** per usual.

6. **Smoke-test**: confirm photos render in Telegram, captions show
   counter correctly, wrong guesses retain the same icon across turns.

## Todo

- [ ] `state.js` with `slot` field + MAX 5
- [ ] `lookup.js`
- [ ] `handlers.js` (photo send, random slot pick, caption counter)
- [ ] `index.js` (3 commands)
- [ ] Register + MODULES env
- [ ] README
- [ ] Smoke-test vs real DDragon URLs

## Success criteria

- Photo renders from DDragon URL in Telegram.
- Same ability icon shown across multiple turns of the same round.
- Correct guess reveals champion + ability name + slot.

## Risks

| Risk | Mitigation |
|------|-----------|
| DDragon URL 404 for some legacy champion | Fetch script verifies URLs before write; filter broken entries |
| DDragon version in `abilities.json` goes stale between fortnightly fetches | Icons remain valid (URL still 404-free per CDN retention); acceptable lag |
| Bot bundle size: `abilities.json` could be 500 KB+ | Phase 01 trims to slot + name + icon URL only (no lore, no cost fields) |
| Some champion has fewer than 5 abilities (unusual reworks) | `pickRandomSlot` picks from whatever's available |
| Telegram caches photos by URL — wrong-guess photo same as first photo | That's fine, it's the SAME photo each turn by design |

## Security

- Photo URL is untrusted-feeling but in practice trusted (ddragon.lol
  CDN). Still: restrict `sendPhoto` to HTTPS URLs; don't pass user input
  into URLs anywhere.
- HTML-escape champion + ability names in captions.

## Open questions

- Bonus "which slot" second guess (as loldle.net does)? **Deferred.**
  v1 is binary. Revisit if users request.
- Progressive crop for hardcore mode? **Deferred** — would require
  Cloudflare Images (~$5–15/mo, per research).

## Next steps

Phase 06 adds tests. Phase 05 (splash) reuses this module's photo-send
pattern.
