# Leaguepedia API — Verification Report

**Date:** 2026-04-21
**Purpose:** Verify the Leaguepedia MediaWiki/Cargo API can provide today / this-week LoL matches for a miti99bot module. No implementation, verification only.
**Verdict:** **YES — usable.** Endpoint is public, no auth, returns structured JSON. One caveat on `where=` clauses needs rechecking from Cloudflare Workers egress.

---

## Endpoint

- Base: `https://lol.fandom.com/api.php`
- Action: `cargoquery` (MediaWiki Cargo extension)
- Auth: none
- Format: `format=json` → clean `{cargoquery:[{title:{…}}]}` payload
- Related `mw.Api` JS wrapper (doc-wikimedia link) works the same; for a Worker we use plain `fetch`, not `mw.Api`

## Relevant table: `MatchSchedule`

Primary table for both upcoming and played matches. Confirmed fields (live API, 2026-04-21):

| Field | Type | Example |
|---|---|---|
| `DateTime_UTC` | datetime | `"2026-06-14 09:00:00"` |
| `Team1`, `Team2` | string | `"T1"`, `"TBD"` |
| `Tournament` | string | Tournament name/slug |
| `BestOf` | int | |
| `Winner` | string | empty until played |
| `OverviewPage` | string | wiki page for tournament |
| `_pageName` | string | wiki row page |

Complementary tables: `Tournaments` (metadata), `ScoreboardGames` (per-game stats), `Teams`.

## Query syntax (verified working)

Use **table + field aliases** — the bare form `fields=MatchSchedule.DateTime_UTC` hits an `MWException`. Alias form is the idiomatic Leaguepedia convention:

```
tables=MatchSchedule=MS
fields=MS.DateTime_UTC=DateTime, MS.Team1=T1, MS.Team2=T2, MS.Tournament=Tournament
order_by=MS.DateTime_UTC ASC
limit=20
```

Live sample (no where-filter) returned real rows. Ordering, limit, and aliasing all confirmed working.

Intended week-window query (to be re-verified from CF Worker egress):

```
where=MS.DateTime_UTC >= "2026-04-21 00:00:00"
  AND MS.DateTime_UTC <  "2026-04-28 00:00:00"
```

## Limitations & operational notes

- **Strict anonymous rate limit.** From a single shared egress IP the API throttled after 1–2 req/min with `ratelimited`. Mitigations for Workers:
  - Use Worker's distributed egress (many IPs) — in practice won't hit the same bucket
  - Cache responses in KV (e.g. 60–300 s for upcoming schedule, 5–15 min for results)
  - Use `cf: { cacheTtl, cacheEverything: true }` on `fetch`
- **User-Agent required.** Fandom's policy expects a contact UA, e.g. `miti99bot/0.1 (https://t.me/miti99bot; minhtienit99@gmail.com)`.
- **Help page is Cloudflare-challenged.** `https://lol.fandom.com/wiki/Help:Leaguepedia_API` returns 403 to non-browser UAs — consult it from a browser, not from `fetch` code.
- **No official JS SDK.** MediaWiki's `mw.Api` is on-wiki JS only. Community Python wrapper (`mwrogue` / `leaguepedia_parser`) is the reference implementation — we port the query shape, not the lib.
- **`where=` clause returned MWException from this verification IP** even on trivial filters (`MS.Team1="T1"`). Likely an upstream filter on the shared egress, not a protocol limitation — the exact form is documented and heavily used. **Needs one confirmation curl from a CF Worker before building on it.**

## Feasibility verdict

| Requirement | Feasible? | Notes |
|---|---|---|
| Fetch today's matches | ✅ | `DateTime_UTC >= today AND < tomorrow` |
| Fetch this week's matches | ✅ | 7-day window on `DateTime_UTC` |
| Filter by region/league | ✅ | `Tournament LIKE "LCK%"` or `OverviewPage` |
| Include results/winners | ✅ | `Winner`, `BestOf` already on row |
| Run from Cloudflare Worker | ✅ | plain `fetch` + JSON; add UA + KV cache |
| Scheduled daily digest | ✅ | fits existing `cron-dispatcher.js` pattern |

## Unresolved questions

1. Does `where=` with comparison operators (`>=`, `<`) work from CF Worker egress, or do we need to alternate filter form (`HOLDS`, `LIKE`, full-table-scan + client-side filter)?
2. Timezone UX — show UTC, VN time (UTC+7), or let the `/matches` command take a region arg?
3. Caching window — ~60 s for "live today" vs ~5 min for week-view; confirm TTL with a real command spec before implementing.
4. Do we want the command to also surface `Winner`/score once a match has finished, or keep it schedule-only?
