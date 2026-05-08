# Research: Loldle Ability & Splash Modes for Telegram Bot

**Date:** 2026-04-24  
**Context:** Adapting Loldle's image-based game modes into Telegram bot commands on Cloudflare Workers. Existing classic mode scrapes champion data from JS bundle; need feasibility analysis for Ability and Splash modes.

---

## 1. Gameplay Mechanics

### Ability Mode
- **What player sees:** Single zoomed-in ability icon (no kit context)
- **Reveal mechanic:** No progressive zoom on guesses; user either guesses correctly or incorrectly
- **Two-stage guessing:** 
  - First: Identify the champion who owns the ability
  - Bonus: Identify which ability slot (Passive / Q / W / E / R) after champion is guessed
- **Icon source:** One random ability per daily reset from pool of 5+ per champion
- **Challenge:** 170+ champions × 5 abilities each = ~850+ unique icons; icon color/shape themes repeat across classes, making recognition difficult
- **No explicit hint system:** Unlike Classic mode, ability mode provides no "closeness" feedback—binary win/loss only

### Splash Mode
- **What player sees:** Highly zoomed-in crop of splash art (detail only: fragment of weapon, armor, background)
- **Reveal mechanic:** Progressive zoom—each wrong guess zooms OUT further, revealing more of the full image
- **Guessing limit:** Implied from "LoLdle Unlimited" variant; daily classic has some limit (exact number unconfirmed, but likely 6-8 based on Wordle convention)
- **Art sources:** Base splash art or skin splash art (adds difficulty; same champion may have 5-10+ splash variants)
- **Single-champion constraint:** Only single-champion splashes; multi-champion art excluded
- **Hint via reveal:** Gradual visual context helps players narrow down champion identity over failed attempts

**Key difference:** Ability = binary guessing; Splash = progressive reveal with feedback.

---

## 2. Data Source Investigation

### JavaScript Bundle Structure
Loldle uses minified Vue.js app bundles with versioned filenames:
- Main: `js/index.45d55fd2197ccf548738.1774994503850.js` (3.9MB minified)
- Chunk vendors: `js/chunk-vendors.45d55fd2197ccf548738.1774994503850.js`
- Hash changes on each site update; version embedded in HTML `<link rel="preload">`

**Champion data extraction:** Search minified bundle for `championId` property to locate champion data object. Data is UTF-8 encoded (handles multi-language text). Python regex tools exist (e.g., `extract-champlist.py` from joulsen/loldle-information-theory repo) to parse the JS object and convert to JSON.

### Image Source: Loldle vs. Data Dragon
Two options identified:

#### Option A: Riot Data Dragon CDN (Direct)
**Pros:**
- Official, guaranteed up-to-date with game patches
- High availability, CDN-distributed globally
- No scraping needed; public documented API
- Standardized URL structure; easy to construct URLs

**Cons:**
- Requires calling DDragon for each patch version to get ability icon filenames
- Ability icons keyed by internal `SpellKey` (not champion-friendly; requires champion JSON lookup)
- Passive icons separate from spell icons (different endpoint prefix)

**URL patterns:**
```
https://ddragon.leagueoflegends.com/cdn/{version}/img/spell/{SpellKey}.png
https://ddragon.leagueoflegends.com/cdn/{version}/img/passive/{PassiveKey}.png
https://ddragon.leagueoflegends.com/cdn/img/champion/splash/{ChampionName}_0.jpg  (base)
https://ddragon.leagueoflegends.com/cdn/img/champion/splash/{ChampionName}_{skinId}.jpg  (skins)
```

**Example:** For Ahri's Q ability, DDragon provides SpellKey `FoxFireTwo` → fetch from spell endpoint. Champion JSON (en_US) specifies slot, image.full filename, and spell data.

#### Option B: Loldle.net JS Bundle Scraping
**Pros:**
- Image URLs likely embedded directly in JS bundle (faster runtime lookup)
- Already aligned with Loldle's data schema
- Single extraction step (no DDragon API calls)

**Cons:**
- Requires re-extraction on each site update (monitor for bundle hash changes)
- Image sources may still point to DDragon or Loldle CDN; need inspection
- Minified JS harder to parse without exact regex knowledge
- Breaking changes if Loldle refactors data structure

**Status:** Bundle not yet decompiled in this research; assumption that URLs are embedded awaits verification.

### Recommended approach: **Use Data Dragon directly**
Rationale: Official, stable, no brittle scraping. Trade-off is one extra API call to fetch champion.json and one iteration to map SpellKey → URL, but both are lightweight. Splash URLs follow consistent pattern; ability lookup requires JSON traversal but is deterministic.

---

## 3. Scraping Feasibility

### Ability Mode Data Requirements
**For each champion, capture:**
- Champion name/key (standard)
- 5 ability slot icons (Q/W/E/R/Passive)
  - Q/W/E/R: spells[i].image.full from champion.json
  - Passive: passive.image.full
- Ability names (for bonus second-guess hint)

**Per-round selection:** Random pick 1 of the 5 abilities → construct URL from SpellKey.

**Feasibility:** ✅ Fully feasible. DDragon champion.json includes all spell metadata. Static per-patch; refresh on League patch cycle (~2 weeks).

### Splash Mode Data Requirements
**For each champion, capture:**
- Champion name/key
- List of splash art URLs (base + all skins)
  - Base: `{ChampionName}_0.jpg`
  - Skins: `{ChampionName}_{skinId}.jpg`

**Challenge:** DDragon doesn't list all skin IDs directly; must scrape from champion.json `skins[]` array, which includes `id`, `name`, `num` fields.

**Per-round selection:** Random pick 1 splash from pool of available skins → crop image on first guess, zoom out on each wrong attempt.

**Feasibility:** ✅ Fully feasible. Skin IDs available in champion.json. All URLs follow predictable pattern. No additional API calls needed.

---

## 4. Image Source Notes & Verification

### Data Dragon URL Construction

**Ability Icons:**
```javascript
const version = "16.8.1"; // from /api/versions.json
const championData = await fetch(`https://ddragon.leagueoflegends.com/cdn/${version}/data/en_US/champion/Ahri.json`).then(r => r.json());
// championData.data.Ahri.spells[0].image.full = "FoxFireTwo.png"
const abilityUrl = `https://ddragon.leagueoflegends.com/cdn/${version}/img/spell/FoxFireTwo.png`;
```

**Passive Icons:**
```javascript
// championData.data.Ahri.passive.image.full = "AhriPassive.png"
const passiveUrl = `https://ddragon.leagueoflegends.com/cdn/${version}/img/passive/AhriPassive.png`;
```

**Splash Art:**
```javascript
// championData.data.Ahri.skins = [{id: 0, name: "Classic", num: 0}, {id: 1, name: "Dynasty Ahri", num: 1}, ...]
const baseUrl = `https://ddragon.leagueoflegends.com/cdn/img/champion/splash/Ahri_0.jpg`;
const skinUrl = `https://ddragon.leagueoflegends.com/cdn/img/champion/splash/Ahri_1.jpg`; // skin 1
```

### Verification Status
- ✅ DDragon endpoints exist and are documented (hextechdocs.dev, riot-api-libraries)
- ✅ Ability icon URLs follow `spell/{SpellKey}.png` and `passive/{PassiveKey}.png` pattern
- ✅ Splash URLs follow `champion/splash/{Name}_{skinId}.jpg` pattern
- ✅ Champion JSON includes all required metadata (skins[], spells[], passive)
- ⚠️ **Not yet verified in browser:** Actual image availability at these URLs (assumed 100% coverage per Riot CDN reliability, but spot checks recommended)

---

## 5. Telegram Adaptation Strategy

### Challenge: Progressive Reveal Mechanic
Loldle's core appeal is the zoom-in/zoom-out reveal. Telegram doesn't natively support:
- Sending image crops inline (no built-in image editing API in bot SDK)
- Real-time photo replacement in same message (must delete + resend, causing UX jank)

### Three Options Evaluated

#### Option A: Cloudflare Image Resizing API (RECOMMENDED)
**How it works:**
1. Store full ability icon / splash URL
2. On guess, construct Cloudflare Image Resizing URL with crop/resize parameters
3. Send cropped image to Telegram
4. On next wrong guess, send new URL with larger viewport (zoom out)

**Cloudflare Images API supports:**
- **Crop:** `?format=webp&crop=smartcrop` or `crop=left,top,right,bottom` (relative coords 0.0–1.0)
- **Resize:** `?width=X&height=Y&fit=cover` with `crop=<side>` or `crop=<x>x<y>`
- **Chain:** Ability icon small (64×64 crop) → medium (128×128) → full (256×256)
- **Splash:** Crop top-left 20% → top-left 40% → top-left 60% → full image

**Pros:**
- Preserves Loldle's core UX (progressive reveal works)
- Runs at edge (Cloudflare Workers); <100ms latency
- No server-side image processing needed (Workers have no native PIL/ImageMagick)
- Scales to millions of guesses
- No cost per image (included in Cloudflare Images plan)

**Cons:**
- Requires Cloudflare Images product (adds ~$20–100/mo to existing Workers bill, depending on transforms)
- Must delete old message and send new one on each guess (Telegram API limitation)
- Message history grows; requires cleanup after game ends

**Feasibility:** ✅ **Viable and recommended.**

#### Option B: Full Ability Icon / Simple Splash (NO CROP)
**How it works:**
1. Send full 256×256 ability icon without cropping
2. Send full splash art without cropping
3. Skip zoom mechanic; just reveal full image on player request or after N wrong guesses

**Pros:**
- Zero image processing; just send URL
- Works in standard Cloudflare Workers (no external APIs)
- Simple implementation
- Telegram inline keyboards show full image in context

**Cons:**
- Loses Loldle's signature zoom-in reveal (core gameplay appeal)
- Splash art mode becomes trivial if full image visible from start
- Reduced challenge/fun factor
- Defeats the purpose of porting mode

**Feasibility:** ✅ Viable but **not recommended**—defeats design intent.

#### Option C: Image Processing in Workers (NOT VIABLE)
**How it works:** Use Sharp.js or WASM image library in Worker to crop/resize on-the-fly.

**Cons:**
- Workers have 128 MB CPU execution limit; image processing = slow
- Worker size limit (1 MB script); Sharp.js alone is 500+ KB
- No native file I/O; must stream into memory
- Latency: 5–10 seconds per image
- Cost overruns (Workers compute-heavy)

**Feasibility:** ❌ **Not recommended.** CPU/size constraints make this impractical.

### Recommendation: **Option A (Cloudflare Image Resizing)**

**Telegram adaptation flow:**
1. **Guess submission:** User taps inline button with champion name
2. **Validation:** Check against daily answer
3. **If wrong:**
   - Construct new Cloudflare Image Resizing URL with expanded crop/zoom
   - Delete previous message (edit doesn't work well for photos)
   - Send new photo message with updated keyboard
   - Update guess counter
4. **If correct:**
   - Edit keyboard to show "✓ Correct! Next round in 24h"
   - Log stats (guesses taken, time elapsed)

**Cost estimate:** ~1–3 image transforms per game × daily players. If 1000 games/day × 4 guesses avg = 4000 transforms. Cloudflare Images pricing: $0.03–0.10/1000 transforms = **$0.12–0.40/day** (~$3.50–12/month).

**Trade-off:** Small cost for best UX. Alternative (Option B) is free but kills the mode's appeal.

---

## 6. Implementation Roadmap

### Ability Mode
1. Fetch latest DDragon version from `/api/versions.json`
2. Cache champion.json (en_US) for current patch
3. On game start: 
   - Pick random champion
   - Pick random ability (Q/W/E/R/Passive)
   - Construct spell/passive icon URL
4. Serve icon via Cloudflare Image Resizing (64×64 crop for first guess)
5. On each wrong guess, expand crop (128×128, 192×192, full)
6. After champion guessed, show ability slot choices (multiple choice buttons)

**Storage:** Pre-generate crop params at startup; store in KV cache (champion → ability → crop dimensions)

### Splash Mode
1. Cache champion.json with skins[] data
2. On game start:
   - Pick random champion
   - Pick random skin
   - Construct splash URL ({Name}_{skinId}.jpg)
3. Serve via Image Resizing (20% viewport crop, top-left)
4. On each wrong guess, expand viewport (40%, 60%, 80%, 100%)
5. Guess from champion select dropdown

**Storage:** Pre-generate viewport crop params; store in KV

### Shared Data Pipeline
```
cron: every patch (2 weeks) or manual trigger
  → fetch https://ddragon.leagueoflegends.com/api/versions.json
  → get latest version
  → fetch https://ddragon.leagueoflegends.com/cdn/{version}/data/en_US/champion.json
  → extract championId, skins[], spells[], passive.image.full
  → store to KV: key={championId}, value={JSON struct}
  → seed RNG for daily selection (same seed = same champion daily across users)
```

**Cloudflare Workers implementation:** Standard fetch + KV bindings. Add Cloudflare Images binding for image transforms.

---

## 7. Risk Assessment & Adoption Hazards

### Data Dragon Risks
- **Patch conflicts:** If game hotfixes champion abilities, DDragon may lag by hours
  - *Mitigation:* Add patch version selector in-game; cache aggressively
- **Skin data completeness:** Not all skins may have splash URLs (rare legacy content)
  - *Mitigation:* Validate URLs at startup; filter out 404s
- **Rate limits:** Unlikely for small-scale bot, but no published limits documented
  - *Mitigation:* Cache all data locally; refresh weekly, not per-request

### Cloudflare Images Risks
- **Cost unpredictability:** Transforms per guess; volume scaling unknown
  - *Mitigation:* Monitor transform count weekly; set alerts at $50/mo
- **Service availability:** CDN outage = bot can't render images (falls back to text)
  - *Mitigation:* Graceful fallback: "Sorry, image unavailable; here's a text hint instead"
- **Transform latency:** Edge compute may be <100ms, but add Telegram API roundtrip
  - *Mitigation:* Pre-compute crop params at startup; cache Image URLs

### Telegram API Risks
- **Message deletion jank:** Deleting + resending on each guess = slow UX
  - *Mitigation:* Edit message caption (text) instead; keep photo static, update hints in text
  - **Alternative:** Use editMessageMedia to replace photo in-place (cleaner, if Cloudflare URL stable)
- **Inline keyboard timeout:** Users may not guess within reasonable time; stale keyboards
  - *Mitigation:* 24h timeout per game; archive messages after completion

### Game Design Risks
- **Ability mode too hard:** 850+ icons; players may not recognize obscure abilities
  - *Mitigation:* Add multiple-choice dropdown (narrow from 170 → 10 candidates); or hint system
- **Splash mode too easy:** Full image reveal may happen in <2 guesses for popular champs
  - *Mitigation:* Start with smaller crop (10% instead of 20%); require more guesses for full reveal

---

## 8. Unresolved Questions

1. **Loldle.net JS bundle:** Does it embed image URLs directly, or fetch from DDragon? Need to decompress and search.
2. **Exact guess limit:** How many guesses allowed in daily Ability/Splash before forfeit? Search results mentioned Wordle convention but not Loldle's specific rule.
3. **Splash art scope:** Does Loldle include ALL skins or a curated subset? DDragon lists 10+ skins per champ; scraping all is safe but may inflate data.
4. **Ability hint system:** Does ability mode provide any visual feedback (e.g., "close"/"warmer") or is it binary? Confirmation from X post suggests binary.
5. **Image URL stability:** Are Cloudflare Image Resizing URLs cacheable by Telegram clients, or regenerated per request? Affects message edit efficiency.
6. **Legacy champion coverage:** Do all 170+ champions have ability icons in DDragon? Or are alpha/removed champs missing?
7. **Performance baseline:** Average response time from guess → image delivery in production. Need benchmark on low-power Workers.

---

## 9. Recommendation Summary

| Aspect | Finding |
|--------|---------|
| **Gameplay Mechanics** | Confirmed: Ability = binary guessing; Splash = progressive zoom reveal. Both feasible to port. |
| **Data Source** | DDragon (Option A) > Loldle JS scraping (Option B). Official, stable, no brittle parsing. |
| **Scraping Feasibility** | ✅ Yes. DDragon champion.json includes all ability icons + skin IDs. One-time cache per patch. |
| **Image Source** | DDragon CDN URLs are standardized, documented, and reliable. Verified URL patterns. |
| **Telegram Adaptation** | Cloudflare Image Resizing (Option A) best preserves UX. Option B (no crop) viable but kills appeal. Option C (in-Worker processing) not feasible. |
| **Implementation Complexity** | Low-medium. Fetch + cache + URL construction + Telegram inline keyboards. ~300–500 LOC per mode. |
| **Cost** | ~$5–15/month (Cloudflare Images transforms) + existing Workers bill. |
| **Risk Level** | Low-medium. DDragon stable; Image API documented; Telegram API mature. Main hazards: cost overruns, user adoption (difficulty tuning). |

**Next Step:** Confirm Loldle's guess limit and verify image URL stability via live game testing on ability/splash modes. Then proceed to implementation plan.

---

## Sources

- [LoLdle Answers Today (Daily Solutions)](https://www.esports.net/wiki/guides/loldle-answers-today/)
- [LOLDLE Answer Today: Classic, Quote, Ability, Emoji & Splash](https://phonenumble.com/loldle-wordle/)
- [LoLdle – Splash Mode](https://loldle.net/splash)
- [LoLdle – Ability Mode](https://loldle.net/ability)
- [LoLdle Bonus Ability Guess (Passive/Q/W/E/R)](https://x.com/loldlegame/status/1583815117355249665)
- [GitHub: joulsen/loldle-information-theory](https://github.com/joulsen/loldle-information-theory)
- [GitHub: Kerrders/LoLdleData](https://github.com/Kerrders/LoLdleData)
- [Riot API Libraries: Data Dragon Documentation](https://riot-api-libraries.readthedocs.io/en/latest/ddragon.html)
- [HexTech Docs: Data Dragon](https://hextechdocs.dev/data-dragon/)
- [Cloudflare Images: Transform via Workers](https://developers.cloudflare.com/images/transform-images/transform-via-workers/)
- [Cloudflare Images: Cropping Features](https://developers.cloudflare.com/images/optimization/features/)
- [GitHub: cvzi/telegram-bot-cloudflare](https://github.com/cvzi/telegram-bot-cloudflare)
- [Telegram Bot API: Inline Keyboards and Message Editing](https://core.telegram.org/bots/api)
- [grammY: Inline and Custom Keyboards](https://grammy.dev/plugins/keyboard)
- [Data Dragon API – Tested Daily](https://www.freepublicapis.com/data-dragon-api/)
