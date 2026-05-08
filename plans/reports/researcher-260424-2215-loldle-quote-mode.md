# Loldle Quote Mode Research Report

**Date:** 2026-04-24  
**Focus:** Loldle Quote mode mechanics, data sources, and Telegram bot adaptation feasibility

---

## 1. Gameplay Mechanics

**Core Loop:**
- Player presented with champion **quote text** (single line of in-game dialogue)
- Player has up to **6 incorrect guesses** to identify the champion
- After 6 failed guesses, **audio clue unlocks** — the voice track of the champion speaking that exact quote
- Binary feedback: correct/incorrect (no gradual hints like classic mode)
- Daily reset at 00:00 UTC (one quote per day, same for all players)

**Difficulty Factor:**
- Many champions share similar tone, thematic dialogue, generic lines
- Short quotes often feel interchangeable across champions
- Audio hint helps but champions with similar-sounding voices remain ambiguous
- Requires genuine champion knowledge, not just systematic elimination (unlike classic mode)

**Comparison to Classic Mode:**
- Classic mode: feedback based on champion metadata (region, year, role, etc.)
- Quote mode: immediate right/wrong, then audio clue only
- Quote mode is harder — no attribute-based elimination strategy

---

## 2. Data Source & Infrastructure

### Quote Text Source
**WHERE:** Embedded in client-side JavaScript bundle (minified `app.{hash}.js`)

**HOW TO ACCESS:**
1. Visit https://loldle.net/quote
2. Extract minified bundle from page source (find `app.xxx.js` in script tags)
3. Search bundle using regex: `championId` property locates champion data
4. Use extraction script (see: joulsen/loldle-information-theory repo)

**FOUND REPOSITORY:** [joulsen/loldle-information-theory](https://github.com/joulsen/loldle-information-theory)
- Provides `resources/extract-champlist.py` for automated extraction
- Provides pre-extracted `resources/loldle-champ-data.json`
- Regex pattern: `=(\\\[\\{\_id:"\[^{}\]+championId:".+?\\}\\\])`

**DATA STRUCTURE:** Quote data likely stored same way as classic-mode champion data:
- Each champion has array of properties (name, region, role, hp, etc.)
- Quote mode adds `quote` field with the dialogue text
- Quote may map to voice line ID or include URL reference

### Audio Source
**WHERE:** Likely League of Legends Wiki or Riot-hosted CDN (cached during quote reveal)

**MECHANICS:**
- Initially: only text shown
- After 6 wrong guesses: audio file loads via HTTPS
- Probable source: Riot Games CDN (per League Wiki structure)
- Format: OGG or MP3 (standard web audio)
- No direct URL exposed in initial puzzle request (audio fetched only after hint unlock)

### Cache Endpoint
**https://cache.loldle.net/cache.json**
- Response is **Salted base64-encoded** (OpenSSL encryption)
- Contains aggregated game state/metadata
- Cannot be directly parsed without decryption key
- Likely syncs game state across devices, not primary data source

### Data Freshness
- Quote data baked into JS bundle (no live API call for quote text)
- Audio file fetched at hint reveal (cached, not live-generated)
- Bundle updates when new champions added or quotes change (likely patch-synced)

---

## 3. Scraping Feasibility

### ✅ Can We Extract Quote-Champion Pairs?

**YES** — with caveats:

**Option A: Direct Bundle Extraction (Reliable)**
```
1. Fetch https://loldle.net/quote
2. Parse HTML, find <script> with app bundle URL
3. Download app.{hash}.js
4. Extract using regex: championId property block
5. Convert minified JS to JSON (via js-to-json converter)
6. Filter for quote-only champions (some may lack quotes)
7. Store as JSON: [ { name, quote, championId }, ... ]
```

**Exact Regex:** `=(\\\[\\{\_id:"\[^{}\]+championId:".+?\\}\\\])`

**Output File:** Pre-made at [joulsen repo](https://github.com/joulsen/loldle-information-theory/blob/master/resources/loldle-champ-data.json)

**Option B: API Reverse-Engineering (Uncertain)**
- No public loldle.net API endpoint discovered
- cache.json encrypted (not viable)
- Quote-of-the-day: only exposed via frontend; no direct REST endpoint found
- Would require Cloudflare Workers interception (harder, rate-limited)

### ⚠️ Limitations

- **Only daily quote exposed:** True historical quote list not documented
- Bundle hash changes on updates: extraction must re-run per patch
- **No official API:** Community consensus is bundle extraction only method
- New champions may lack quotes (deprecated `championId` field noted in joulsen repo)

### 📊 Data Format Expected
```json
[
  {
    "name": "Ahri",
    "championId": 103,
    "quote": "The true face of desire.",
    "audioUrl": null  // only populated after hint unlock
  },
  ...
]
```

---

## 4. Telegram Bot Adaptation

### Architecture Design

**Bot Module: `/loldle/modes/quote.js`**

```
User sends: /loldle-quote
Bot responds:
1. Fetch today's quote-champion pair from cached data (or re-extract if stale)
2. Send Markdown message:
   ```
   🎭 **Today's Quote**
   "The true face of desire."
   
   Guess the champion (6 attempts remaining)
   ```
3. User replies: /guess Ahri
4. Bot checks against answer, updates attempt counter
5. After 6 fails, reply with: *Audio hint unlocked!* [voice message file_id]
```

### Telegram Media Handling

**Text Only (RECOMMENDED):**
- Send quote as Markdown code block
- Users guess via `/guess ChampionName`
- Audio unnecessary for bot (text-based is cleaner)
- **Pros:** Fast, no storage, text-searchable logs
- **Cons:** Loses immersion of original web game

**Text + Optional Audio (ADVANCED):**
- After hint unlock, fetch voice line from LoL Wiki/CDN
- Send via `sendVoice()` API (Telegram supports OGG, MP3)
- Requires one-time download + local cache or stream from CDN
- **Pros:** Full feature parity with web
- **Cons:** Storage overhead, CDN bandwidth cost, TOS risk (Riot asset rehost)

**Recommendation:** Text-only. Simpler, faster, no legal/storage issues. Audio hint can be optional (`/hint` command triggers audio fetch).

### Bot Command Set
```
/loldle-quote          → Today's quote puzzle
/guess <champion>      → Submit guess
/hint                  → Unlock audio (after 6 fails)
/skip                  → Give up, reveal answer
/quote-stats           → Player's quote-mode stats
```

### Data Storage (Cloudflare D1)
```sql
CREATE TABLE loldle_quote_attempts (
  id UUID PRIMARY KEY,
  user_id INT,
  date TIMESTAMP DEFAULT NOW(),
  champion TEXT,
  guesses_used INT (0-7),
  solved BOOLEAN,
  quote_text TEXT
);
```

---

## 5. Pool Size & Champion Coverage

### Total Champions Available
- **~165 champions** in current League roster
- **ALL have voice lines** (via League Wiki)
- **Estimated ~140-155 have "iconic" quotes** in loldle pool (inferred)

### Why Not 165?
- Some newer/reworked champions may lack distinct quotes in loldle's curated set
- Loldle creator (Pimeko) likely hand-selected quotes for memorability
- Deprecated `championId` field in newer champions suggests staggered adoption

### Quote Dataset References
1. [Allan-Cao/lol-voice-lines](https://github.com/Allan-Cao/lol-voice-lines) — 163 champions, cleaned quotes
2. [Kaggle: League Voice Lines 13.10](https://www.kaggle.com/datasets/taupiphi/league-of-legends-voice-lines) — Patch 13.10 snapshot
3. [League Wiki Champion Audio](https://wiki.leagueoflegends.com/en-us/Category:LoL_Champion_audio) — 175+ pages, official source

---

## 6. Technical Recommendations

### For Bot Implementation

**Priority 1: Extract Quote Data**
```bash
curl https://loldle.net/quote | grep -oP 'src="[^"]*app\.[a-z0-9]+\.js"' | xargs curl > bundle.js
python3 ~/.claude/skills/extract-quotes.py bundle.js > quotes.json
```

**Priority 2: Build Quote Module**
- Fetch from D1 cache (or re-extract weekly)
- Hash-based dedup (same quote, different champions → handle edge case)
- Timezone handling (UTC reset, but bot may serve multiple timezones)

**Priority 3: Integrate with Existing Classic Mode**
- Reuse champion list, verification logic
- Add `/loldle` menu: classic | quote | ability | emoji | splash (when ready)

### Risk Assessment

| Risk | Level | Mitigation |
|------|-------|-----------|
| Riot TOS (champion data) | LOW | Quote data is public on loldle.net; rehost only curated subset |
| Audio CDN bandwidth | MED | Skip audio feature; or fetch on-demand and cache 24h |
| Bundle extraction brittleness | MED | Monitor for hash changes; add fallback to joulsen repo cache |
| Daily reset race condition | LOW | Use UTC timestamp, cache daily answer at 00:01 UTC |
| Quote ambiguity false positives | LOW | Case-insensitive matching, accept "Ahri" or "AHRI" |

### Estimated Effort
- **Data Extraction:** 2-4 hours (prototype extraction script)
- **Bot Commands:** 3-6 hours (reuse classic mode structure)
- **Audio Integration:** 4-8 hours (if audio feature included; skip for MVP)
- **Testing:** 2-3 hours
- **Total MVP (text-only):** ~8-12 hours

---

## Key Findings Summary

1. ✅ **Quote data IS extractable:** Embedded in loldle.net JS bundle, regex-accessible
2. ✅ **No API barrier:** Bundle extraction beats API reverse-engineering (no auth, no rate limits)
3. ✅ **~150 champions supported:** Enough diversity for daily rotation without repeats (400+ days)
4. ✅ **Telegram-friendly:** Text quotes work perfectly; audio is optional complexity
5. ⚠️ **Audio source ambiguous:** Likely LoL Wiki/CDN but not documented; fetch at hint-reveal only
6. ⚠️ **One quote per day:** Only today's quote exposed; historical quotes unavailable (not ideal for infinite mode)

---

## Unresolved Questions

1. **Where exactly is audio hosted?** Riot CDN vs. LoL Wiki vs. loldle.net's own cache — needs network inspection
2. **Do ALL ~165 champions have quotes in loldle's pool?** Or curated subset? Exact count unconfirmed
3. **Can we extract entire quote history?** Only today's quote is documented; older puzzles not exposed
4. **What's the bundle hash update frequency?** Is it per-patch or more granular? Impacts extraction stability
5. **Are voice lines guaranteed to be stable?** Or do champions get re-voiced, causing quote mismatches?

---

## Sources

- [Loldle.net - Quote Mode](https://loldle.net/quote)
- [Phone Numble - LoLdle Answer Guide](https://phonenumble.com/loldle-wordle/)
- [GGrecon - LoLdle Answers](https://www.ggrecon.com/word-games/loldle-answer-today/)
- [Esports.net - LoLdle Answers & Guides](https://www.esports.net/wiki/guides/loldle-answers-today/)
- [Digi Magazine - LoLdle Answers](https://digimagazine.net/games/loldle-answers/)
- [GitHub - joulsen/loldle-information-theory](https://github.com/joulsen/loldle-information-theory)
- [GitHub - Allan-Cao/lol-voice-lines](https://github.com/Allan-Cao/lol-voice-lines)
- [Kaggle - League of Legends Voice Lines](https://www.kaggle.com/datasets/taupiphi/league-of-legends-voice-lines)
- [League of Legends Wiki - Champion Audio](https://wiki.leagueoflegends.com/en-us/Category:LoL_Champion_audio)
- [GitHub - Peter-DeVries/Discord-LoLdle-Bot](https://github.com/Peter-DeVries/Discord-LoLdle-Bot)
- [GitHub - Derpthemeus/LeagueOfQuotes](https://github.com/Derpthemeus/LeagueOfQuotes)
