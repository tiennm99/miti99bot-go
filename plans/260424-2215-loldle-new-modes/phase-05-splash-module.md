# Phase 05 — Splash module (`loldle-splash`)

<!-- Updated: Validation Session 1 - deferred; random-across-all-skins confirmed -->

## Context

- [Research: ability + splash](../reports/researcher-260424-2215-loldle-ability-splash-modes.md)
- Template: `src/modules/loldle-ability/` (phase 04 — nearly identical
  shape, different payload).
- Dependency: phase 01 (`splashes.json`).

## Overview

**Priority:** P2 (image mode).
**Status:** **DEFERRED** — do not start until emoji + quote modes are live.
Scheduled after phase 04 for pattern-reuse.

## Validated decisions

- **Random across ALL skins**, not base-only. Bigger data file, harder
  mode, matches loldle.net behaviour.
- **No progressive cropping** — full splash from turn 1, 4 guesses.

Guess the champion from splash art (random skin). Full splash image sent
once per round; user gets a tight guess budget.

## Key insights

- DDragon splash URL pattern: `cdn/img/champion/splash/<Name>_<skinId>.jpg`
  — note **no version segment**. Stable across patches.
- Phase 01 writes `splashes.json` as
  `[{ championName, skins:[{ id, name, url }] }]`.
- Random skin pick adds difficulty (Elementalist Lux looks nothing like
  Classic Lux). Include ALL skins, not just base — aligns with
  loldle.net's behaviour per research.
- Splash images are large (~1 MB). Telegram auto-compresses photos, so no
  worry about bandwidth.
- Like ability mode: no cropping in v1. Full image from turn 1, tight
  guess budget. **4 guesses** (one less than ability since the reveal is
  even bigger visually — whole-champion art).

## Requirements

**Functional**
- `/loldle_splash` → start round (pick champion + skin) or re-send same
  photo.
- `/loldle_splash <champion>` → submit guess.
- `/loldle_splash_giveup` → reveal champion + skin name.
- `/loldle_splash_stats` → per-subject stats.
- 4 guesses.

**Non-functional**
- KV prefix: `loldle-splash:`.
- Round state: `{ target, skinId, guesses, startedAt }`. skinId persists
  so the same skin art shows across all turns of a round.

## Architecture

```
src/modules/loldle-splash/
├── index.js
├── handlers.js
├── state.js          # shape adds skinId, MAX 4
├── lookup.js
├── splashes.json     # [{ championName, skins:[{id, name, url}] }]
└── README.md
```

## Related code files

**Modify**
- `src/modules/index.js` — add `"loldle-splash"`.
- `wrangler.toml` + `.env.deploy` MODULES env.

**Create**
- Six files above.

## Implementation steps

1. **Copy `loldle-ability/` as scaffold.**

2. **`state.js`** — shape `{ target, skinId, guesses, startedAt }`,
   `MAX_GUESSES = 4`.

3. **`handlers.js`**:
   - `pickRandomChampion()` → record with ≥ 1 skin (always true; base is
     skin 0).
   - `pickRandomSkin(champ)` → uniform over `champ.skins`; keep its
     `.id` and `.url`.
   - On `/loldle_splash` no-arg: `ctx.replyWithPhoto(url, { caption: "Guess the champion. 0/4." })`.
   - On guess: compare names; on win reveal skin name ("That was **Ahri**
     in _Dynasty_ skin.").
   - On giveup: same reveal.

4. **`index.js`** — three public commands
   (`loldle_splash`, `loldle_splash_giveup`, `loldle_splash_stats`).

5. **Register + MODULES env.**

6. **Smoke-test** — verify splash renders, reveal names the skin
   correctly, guesses persist the same skin photo.

## Todo

- [ ] Copy scaffold from ability module
- [ ] `state.js` with `skinId` field + MAX 4
- [ ] `handlers.js` (photo send, random skin pick, skin reveal)
- [ ] `index.js` (3 commands)
- [ ] Register + MODULES env
- [ ] README
- [ ] Smoke-test

## Success criteria

- Random skin shown per round (not always base).
- Same skin persists across guesses in a round.
- Reveal names the specific skin.

## Risks

| Risk | Mitigation |
|------|-----------|
| DDragon splash 404 for legacy/unreleased skin | fetch-ddragon script verifies each URL at build time; filter 404s |
| Too easy for popular champions (Lux, Ahri — recognised instantly) | 4-guess budget balances; some skins are genuinely obscure |
| Multi-champion splashes (e.g. Kayle+Morgana) | Exclude at fetch time — filter skins tagged as multi-champ if DDragon flags; otherwise keep and accept the edge case |
| Large splashes slow first reply | Telegram downloads from URL server-side; user-perceived latency is the `sendPhoto` API call, ~1 s |

## Security

- All splash URLs are on DDragon HTTPS CDN.
- HTML-escape champion + skin names in captions and reveals.

## Open questions

- Include "Classic" skins only for easier mode? **No** — keeps the mode
  too close to ability/classic difficulty. Random skin is the whole point.
- Progressive crop for hardcore mode? **Deferred** (Cloudflare Images).
- Exclude NSFW / retired skins (e.g. Graves' cigar removal)? None flagged
  by DDragon; all shipped skins are safe-for-work.

## Next steps

Phase 06 closes the plan with tests + docs.
