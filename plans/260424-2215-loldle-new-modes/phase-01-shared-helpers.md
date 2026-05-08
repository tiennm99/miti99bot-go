# Phase 01 — Shared scrape + lookup helpers

## Context

- [Research: overview + emoji](../reports/researcher-260424-2215-loldle-emoji-and-modes-overview.md)
- [Research: quote](../reports/researcher-260424-2215-loldle-quote-mode.md)
- [Research: ability + splash](../reports/researcher-260424-2215-loldle-ability-splash-modes.md)
- Existing: `scripts/scrape-loldle-data.js`, `src/modules/loldle/lookup.js`

## Overview

**Priority:** P0 (blocks 02–05).
**Status:** pending.

Lay a minimal shared foundation so the four new modules don't each
re-implement champion-name normalization or re-scrape loldle.net five times.

## Key insights

- **Bundle check (2026-04-24):** loldle.net's bundle contains classic
  attributes only — **zero emoji code points, zero per-champion quote
  strings**. Daily answers are fetched encrypted from
  `cache.loldle.net/cache.json` and decrypted with AES key `D5XCtTOObw`,
  but the cache only holds the single daily rotation, not a full
  champion→emoji / champion→quote pool.
- **Pivot (DEVIATION from plan as written):** Emoji sequences are derived
  **algorithmically** from classic's existing `champions.json` metadata
  (species/regions/positions/resource) via a small mapping table — no new
  fetch, no brittle scrape. Quote text uses DDragon's `title` +
  first-sentence `lore` blurb. Both data sources are stable and official.
- Data Dragon is the right source for ability/splash — official, stable, no
  brittle regex. Scripts hit DDragon once per patch (fortnightly) and cache
  to JSON. Bot imports JSON directly.
- `lookup.js`'s `findChampion` stays coupled to the champion-record shape.
  Don't hoist it; only hoist the tiny `normalize(s)` helper.

## Requirements

**Functional**
- Extended scraper emits three JSONs (keeps `champions.json` plus adds
  `emojis.json`, `quotes.json`).
- New DDragon script emits `abilities.json` + `splashes.json`.
- Shared `normalize(s)` helper in `src/util/` for case/space/punctuation-
  insensitive matching across all modes.

**Non-functional**
- Single loldle.net fetch per scrape run (no re-download for each mode).
- DDragon fetch uses `GET /api/versions.json` → latest → one
  `champion.json` per champion OR one aggregated `en_US/champion.json`
  (list) + per-champion fetches as needed. Prefer aggregated list first,
  drill into per-champion only for `skins[]`.
- Scripts idempotent, safe to re-run, short-circuit on "no change".

## Architecture

```
scripts/
├── scrape-loldle-data.js         (EXISTING, extended)
│    └── writes: src/modules/loldle/champions.json
│               + src/modules/loldle-emoji/emojis.json
│               + src/modules/loldle-quote/quotes.json
└── fetch-ddragon-data.js         (NEW)
     └── writes: src/modules/loldle-ability/abilities.json
                + src/modules/loldle-splash/splashes.json

src/util/
└── normalize-name.js             (NEW, ~10 LOC)

src/modules/{loldle-emoji,loldle-quote,loldle-ability,loldle-splash}/
     └── (created in phases 02–05)
```

## Related code files

**Modify**
- `scripts/scrape-loldle-data.js` — add extraction for emoji + quote fields.
  Regex must accommodate loldle.net's current bundle shape (inspect before
  touching; existing regex is the template).
- `.github/workflows/scrape-loldle-data.yml` — no change needed; the
  extended scraper writes more files, the workflow's `git diff` check
  catches them automatically.

**Create**
- `scripts/fetch-ddragon-data.js` — fetch DDragon, extract ability + skin
  metadata, write two JSONs.
- `src/util/normalize-name.js` — single export `normalize(s)`.
- `src/modules/loldle-emoji/` (empty folder, populated in 02).
- `src/modules/loldle-quote/` (empty folder, populated in 03).
- `src/modules/loldle-ability/` (empty folder, populated in 04).
- `src/modules/loldle-splash/` (empty folder, populated in 05).

**Delete:** none.

## Implementation steps

1. **Create `src/util/normalize-name.js`**:
   ```js
   export const normalize = (s) =>
     String(s || "").toLowerCase().replace(/[^a-z0-9]/g, "");
   ```
   Update `src/modules/loldle/lookup.js` to import it (keeps behaviour,
   removes the inline duplicate). Run `npm test` — classic loldle tests
   must still pass.

2. **Inspect the live loldle.net bundle** for emoji + quote fields:
   ```bash
   node -e "
   const html = await (await fetch('https://loldle.net/emoji')).text();
   const m = html.match(/js\/index\.[^\"]+\.js/);
   const js = await (await fetch('https://loldle.net/' + m[0])).text();
   console.log(js.match(/emoji[s]?:\s*[\"\[][^\n]{0,200}/g)?.slice(0,3));
   console.log(js.match(/quote[s]?:\s*[\"\[][^\n]{0,200}/g)?.slice(0,3));
   "
   ```
   Document the ACTUAL shape in this file. Update regex accordingly.

3. **Extend `scrape-loldle-data.js`**:
   - Reuse the single bundle fetch already there.
   - Add two new regex passes extracting `championName → emoji` pairs and
     `championName → quote` pairs.
   - Write `src/modules/loldle-emoji/emojis.json` and
     `src/modules/loldle-quote/quotes.json`. Sort by championName.
   - Fail LOUDLY if either new regex hits zero matches (prevents silent
     schema drift on loldle.net's next bundle).

4. **Create `scripts/fetch-ddragon-data.js`**:
   ```
   GET /api/versions.json → take versions[0]
   GET /cdn/<v>/data/en_US/champion.json → summary (all champions)
   for each championKey:
     GET /cdn/<v>/data/en_US/champion/<Key>.json → full (spells, passive, skins)
   write abilities.json:
     [{ championName, abilities: [{ slot:"P"|"Q"|"W"|"E"|"R", name, icon:"<full-url>" }] }]
   write splashes.json:
     [{ championName, skins: [{ id:0, name:"Classic", url:"<splash-url>" }, ...] }]
   ```
   Use `ddragon.leagueoflegends.com`. Parallelize per-champion fetches with
   a concurrency cap (10). Cache to a local `.ddragon-cache/` ignored by
   git so re-runs within the same patch are instant.
   Add npm script: `"fetch:ddragon-data": "node scripts/fetch-ddragon-data.js"`.

5. **Run both scripts locally**, commit the resulting JSONs. Verify sizes
   reasonable (emojis.json < 50 KB; quotes.json < 100 KB; abilities.json
   < 500 KB; splashes.json < 300 KB). If abilities.json balloons past 1 MB,
   drop fields (keep only slot + icon URL + ability name).

6. **Run `npm test` + `npm run lint`** — no regressions in classic loldle.

## Todo

- [ ] Create `src/util/normalize-name.js`
- [ ] Refactor `src/modules/loldle/lookup.js` to import `normalize`
- [ ] Inspect loldle.net bundle; document emoji + quote regex shape here
- [ ] Extend `scripts/scrape-loldle-data.js` (emoji + quote extraction)
- [ ] Create `scripts/fetch-ddragon-data.js`
- [ ] Add `fetch:ddragon-data` npm script
- [ ] Run both scripts, commit generated JSONs
- [ ] Create 4 empty module folders (placeholders for phases 02–05)
- [ ] `npm test` + `npm run lint` clean

## Success criteria

- `npm run scrape:loldle-data` writes 3 JSONs (champions + emojis +
  quotes), all non-empty, all sorted by championName.
- `npm run fetch:ddragon-data` writes 2 JSONs with full CDN URLs.
- Classic loldle tests unchanged, still pass.
- No lint warnings.
- Four empty module folders exist, ready for phases 02–05.

## Risks

| Risk | Mitigation |
|------|-----------|
| loldle.net bundle schema drifts between now and scrape | Extraction fails loud, re-inspect and update regex |
| DDragon ability icon URL requires version path; version shifts mid-day | Cache URLs with version baked in; fetch script refreshes on demand |
| Per-champion DDragon fetches (165 requests) hit rate limits | Concurrency cap 10; no published DDragon rate limits but be polite |
| abilities.json > 1 MB bloats Workers bundle | Strip fields, keep only slot + icon URL + name |

## Security

- No secrets introduced.
- DDragon and loldle.net are public endpoints; no auth.
- Scripts write only to `src/modules/**/*.json` (no directory traversal).

## Next steps

Phases 02–05 can start **in parallel** once this phase completes.
