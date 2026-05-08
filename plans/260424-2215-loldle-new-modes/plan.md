---
name: loldle-new-modes
status: completed
created: 2026-04-24
updated: 2026-04-24
slug: loldle-new-modes
blockedBy: []
blocks: []
---

# Loldle New Modes — miti99bot

Add four new game modules mirroring loldle.net's non-classic modes:
**Emoji**, **Quote**, **Ability**, **Splash**. Existing `loldle/` (classic)
stays untouched — each new mode is its own sibling module folder.

**Scope principle (YAGNI):** Ship text-based modes first (emoji, quote).
Image modes (ability, splash) ship with **full images, no progressive zoom**
— Loldle's signature "reveal-on-wrong-guess" cropping adds Cloudflare Images
cost + message-delete jank for little gain on mobile Telegram. Users instead
get fewer guesses to compensate.

**Data strategy:**
- Emoji + Quote → scrape from loldle.net JS bundle (same path as classic).
- Ability + Splash → pull from **Riot Data Dragon** CDN directly (official,
  patch-synced, no brittle scraping).
- Audio (quote mode) → **skipped for MVP**. Revisit if users ask.

## Commands (per mode)

| Mode | Commands |
|------|----------|
| emoji | `/loldle_emoji`, `/loldle_emoji_giveup`, `/loldle_emoji_stats` |
| quote | `/loldle_quote`, `/loldle_quote_giveup`, `/loldle_quote_stats` |
| ability | `/loldle_ability`, `/loldle_ability_giveup`, `/loldle_ability_stats` |
| splash | `/loldle_splash`, `/loldle_splash_giveup`, `/loldle_splash_stats` |

All `public`. Conflict-checked at registry load time.

## Phases

| # | Phase | Status | Blocking |
|---|-------|--------|----------|
| 01 | [Shared scrape + lookup helpers](phase-01-shared-helpers.md) | **done** | — |
| 02 | [Emoji module](phase-02-emoji-module.md) | **done** | 01 |
| 03 | [Quote module (text-only)](phase-03-quote-module.md) | **done** | 01 |
| 04 | [Ability module (Data Dragon)](phase-04-ability-module.md) | **done** | 01 |
| 05 | [Splash module (Data Dragon)](phase-05-splash-module.md) | **done** | 01 |
| 06 | [Tests + docs sync](phase-06-tests-docs.md) | **done** | 02,03,04,05 |

**Shipping plan (validated):**
- **Now:** 01 → 02 + 03 in parallel → 06 (tests for emoji + quote only).
- **Later:** 04 + 05 stay in this plan marked `deferred`. Unblocked by 01,
  but held by user decision — pick up after emoji/quote live. Tests for
  them will be added then; phase 06's checklist marks image tests as
  "when 04/05 ship".

## Key decisions

1. **Four new modules, not one refactor.** Classic `loldle/` unchanged. Each
   mode owns its data, handlers, render — matches the project's existing
   per-folder plug-n-play pattern. No cross-module coupling.
2. **Emoji/quote reuse classic's `champions.json` pool** for name validation;
   attach mode-specific payload (emoji string, quote text) from scraper.
3. **Ability/splash skip cropping for v1.** Send full Data Dragon URL
   (`sendPhoto`). Guess budget tuned down (ability: 5; splash: 4) since the
   full image is revealed upfront.
4. **Stats tracked per mode.** Each mode's KV prefix keeps stats isolated.

## Dependencies

- `wrangler.toml` `[vars].MODULES` + `.env.deploy` both updated per module.
- `scripts/scrape-loldle-data.js` extended (new regex paths for emoji,
  quote) — single fetch, mode-aware extraction.
- One new script: `scripts/fetch-ddragon-data.js` (abilities + splash meta
  cached to JSON at build time).

## References

- `plans/reports/researcher-260424-2215-loldle-emoji-and-modes-overview.md`
- `plans/reports/researcher-260424-2215-loldle-quote-mode.md`
- `plans/reports/researcher-260424-2215-loldle-ability-splash-modes.md`
- `src/modules/loldle/` — template patterns (handlers, state, lookup, flavor)
- `docs/adding-a-module.md`

## Execution Log

**Shipped 2026-04-24 (MVP — emoji + quote).**
- Phase 01/02/03/06 complete.
- Phases 04/05 remain deferred (see validation notes).
- **Data-source pivot (critical deviation):** loldle.net bundle contains no
  per-champion emoji/quote data (confirmed zero emoji code points). Cache
  is AES-encrypted and holds only the single daily answer. Pivoted:
  - emoji → algorithmic derivation from classic's `champions.json`
    metadata (species/regions/resource/positions mapping table).
  - quote → DDragon champion `title` + first lore sentence, champion
    name redacted to `___` to avoid giveaways.
- Generator: `scripts/fetch-ddragon-data.js` (new). Handles both JSONs.
  `scrape-loldle-data.js` left untouched (classic only).
- 35 new tests, 484 total passing. Lint clean.

**Shipped 2026-04-24 (deferred phases — ability + splash).**
- Phase 04/05 complete. Plan now fully shipped.
- **Bundle re-probe:** loldle.net's bundle DOES ship the full splash pool
  (var `Ad=[...]` — 172 champs × skin-name lists with translations).
  Scraped it (regex-split on `championName:"…"` markers to handle the
  nested translations arrays). Ability pool still not in bundle — pulled
  from DDragon per-champion (172 parallel fetches, concurrency 10).
- `fetch-ddragon-data.js` extended: now writes all four JSONs in one run
  (emojis, quotes, abilities, splashes). Single DDragon per-champion
  fetch cycle shared between abilities + splash skin IDs.
- Splash pool mirrors loldle.net exactly (non-chroma skins, 1939 total
  skins across 172 champions). URLs from Riot Data Dragon CDN (no
  version segment — stable across patches).
- Credits added to all four loldle-family READMEs + main README.
- 19 more tests (503 total). Lint clean. register:dry shows 12 new
  public commands across the 4 modes with no conflicts.

## Validation Log

**Session 1 — 2026-04-24 (7 questions answered)**

| Question | Decision |
|---|---|
| MVP scope | **Text modes first** (emoji + quote). Image modes deferred. |
| Progressive image crop for ability/splash | **Skip** — full image, tight guess budget. |
| Splash skin pool (when shipped) | **Random across ALL skins** incl. variants. |
| Ability mode flow (when shipped) | **Binary only** — guess champion, done. No slot bonus. |
| Phases 04/05 fate | **Keep in plan, marked `deferred`**. Not moved to a new plan. |
| Quote mode audio | **Skip**, note in quote README as future follow-up. |
| Stats scope | **Per-mode, isolated.** No shared leaderboard. |

All decisions locked. No open questions remain.
