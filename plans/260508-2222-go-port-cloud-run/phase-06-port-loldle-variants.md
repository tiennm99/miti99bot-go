---
phase: 6
title: "Port loldle variants + lolschedule"
status: pending
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

## Success Criteria
- [ ] All 5 modules respond to their commands
- [ ] Ability + splash images render in Telegram (no broken-image markers)
- [ ] `/lolschedule today` matches JS behavior
- [ ] All variants share consistent guess-count limits matching JS recent revert (`commit 29e558b`)
- [ ] Ported tests pass

## Risk Assessment
- **Risk**: Riot Data Dragon version pinning — JS version may use different ddragon version than fresh fetch. **Mitigation**: pin version in env or fetch latest at cold start; document in README.
- **Risk**: lolschedule API surface may have changed since JS implementation. **Mitigation**: re-test against live API; fix forward if drifted.
- **Risk**: Splash skin pool was scraped from loldle.net; legality + freshness. **Mitigation**: reuse the same JSON file already in repo (no re-scrape).

## Rollback
Remove from Factories. Per-variant rollback works independently.
