---
phase: 6
title: "Port loldle variants + lolschedule"
status: partial
priority: P2
effort: "5h"
dependencies: [5]
---

# Phase 06: Port loldle variants + lolschedule

## Overview
Port the four loldle variants (`loldle-emoji`, `loldle-quote`, `loldle-ability`, `loldle-splash`) plus `lolschedule`. They share the per-day session pattern from classic loldle, differ only in clue-reveal mechanics.

## Requirements
- Functional: command parity — same commands, same data sources (Riot Data Dragon for ability icons + splash arts; loldle.net derived for emoji + quote pools).
- Non-functional: image-bearing commands (ability icons, splash) reuse remote URLs — do not embed binaries. Reply uses Telegram `sendPhoto` with URL string.

## Architecture

```
internal/modules/loldle-emoji/
├── module.go
├── data/emoji-pool.json    ← embedded
└── game.go

internal/modules/loldle-quote/
├── module.go
├── data/quotes.json
└── game.go

internal/modules/loldle-ability/
├── module.go
├── ability.go              ← URL pattern: ddragon ability icon
└── game.go

internal/modules/loldle-splash/
├── module.go
├── data/skin-pool.json     ← scraped from loldle.net (per credits in README)
├── splash.go               ← ddragon splash URL builder
└── game.go

internal/modules/lolschedule/
├── module.go
├── client.go               ← lolesports/leaguepedia HTTP client
└── format.go               ← schedule formatter
```

A small shared package would help, but keep modules independent (KISS) until duplication exceeds 3 callers — then extract.

## Related Code Files
- Create: above 5 module trees
- Reuse: copy data files from `src/modules/<name>/data/*` verbatim
- Modify: `internal/modules/modules.go` Factories slice
- Update: `MODULES` env var in Cloud Run service yaml

## Implementation Steps
1. **loldle-emoji**: Port emoji clue pool. Game state `{ targetID; guesses []; cluesShown int }`. Reveal one emoji per wrong guess, max 4.
2. **loldle-quote**: Port quote pool. Reveal up to 3 quote chunks across guesses.
3. **loldle-ability**: Build ability icon URL from champion ID + ability slot (Q/W/E/R), e.g. `https://ddragon.leagueoflegends.com/cdn/<v>/img/spell/<spellId>.png`. Cache the latest ddragon version once per cold start.
4. **loldle-splash**: URL pattern `https://ddragon.leagueoflegends.com/cdn/img/champion/splash/<key>_<skin>.jpg`.
5. **lolschedule**: HTTP client to lolesports/leaguepedia API for upcoming match schedule. Format with `/lolschedule [date]` syntax (recent commit shows this is current behavior).
6. Use the same KV `game:<userID>:<yyyy-mm-dd>` namespace pattern (one game per variant per day).
7. Port unit tests for clue-reveal logic, URL builders, schedule formatter.
8. Wire factories.
9. Smoke each command against dev bot.

## Cook scope split
This phase ships in five sub-cooks (one per module — each is large enough to risk context exhaustion):
- **6a (this cook):** loldle-emoji — 172-record emoji clue dict, binary scoring, simplest variant. ✅
- **6b (next):** loldle-quote — same shape as emoji, quote pool. **Prep work:** extract `normalize`, `subjectFor`, `argAfterCommand`, `findChampion` to a shared package; classic loldle and 6a both already duplicate them, 6b would be the third caller — past the YAGNI extraction threshold.
- **6c (next):** loldle-ability — DDragon ability-icon URL builder, sendPhoto.
- **6d (next):** loldle-splash — DDragon splash URL builder, sendPhoto.
- **6e (next):** lolschedule — HTTP client to lolesports/leaguepedia API; no game state, different shape entirely.

## Success Criteria
- [x] loldle-emoji responds to `/loldle_emoji`, `/loldle_emoji_giveup`, `/loldle_emoji_stats`, `/loldle_emoji_setmax`
- [ ] Ability + splash images render in Telegram (no broken-image markers) — deferred to 6c/6d
- [ ] `/lolschedule today` matches JS behavior — deferred to 6e
- [ ] All variants share consistent guess-count limits matching JS recent revert (`commit 29e558b`) — partial: emoji at 5, others pending
- [x] Ported tests pass for loldle-emoji (lookup, state, render, JS-wire-format decode)

## Implementation deviations (6a — loldle-emoji)
- `moduleNameRe` relaxed from `^[a-z0-9_]{1,32}$` to `^[a-z0-9_-]{1,32}$` so JS-source module names like `loldle-emoji` pass validation. The storage prefix delimiter (`:`) remains rejected; tests cover both shapes.
- Go package directory + package name use `loldleemoji` (no separator) per Go convention; the registered MODULE name is `loldle-emoji` (hyphenated, byte-identical to JS) for KV-prefix migration parity.
- `normalize`, `subjectFor`, `argAfterCommand` duplicated from classic loldle. Marked for extraction at the start of cook 6b — three callers will exist by then, past the YAGNI threshold.
- `winRate` uses `math.Round` from day one (lesson from Phase 5c review).
- KV TTL deferred — Cloudflare KV's `expirationTtl` has no Firestore equivalent.
- No sticker pools — JS source has none for emoji mode.

## Code reviews (6a)
- [Phase 6a review](reports/code-reviewer-260509-1206-phase6a-loldle-emoji.md) — 0 critical, 0 high. Concerns: F#1 (JS-wire-format decode test) added in same session; F#2 (`getOrInitGame` cap-reduction edge case) deferred — defensive branch only; E (extract shared helpers) earmarked as 6b prep work.

## Risk Assessment
- **Risk**: Riot Data Dragon version pinning — JS version may use different ddragon version than fresh fetch. **Mitigation**: pin version in env or fetch latest at cold start; document in README.
- **Risk**: lolschedule API surface may have changed since JS implementation. **Mitigation**: re-test against live API; fix forward if drifted.
- **Risk**: Splash skin pool was scraped from loldle.net; legality + freshness. **Mitigation**: reuse the same JSON file already in repo (no re-scrape).

## Rollback
Remove from Factories. Per-variant rollback works independently.
