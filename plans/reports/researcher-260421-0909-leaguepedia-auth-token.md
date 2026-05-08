# Leaguepedia / Fandom API — Auth Token Verification

**Date:** 2026-04-21
**Follow-up to:** `researcher-260421-0845-leaguepedia-api-verification.md`
**Question:** Can we register and use a token to avoid the rate limit?
**Verdict:** **No useful token available. Caching + CF Worker egress is the right answer.**

---

## What's NOT available on Fandom

| Mechanism | Status | Evidence |
|---|---|---|
| `Special:BotPasswords` | Disabled | 403 CF + disabled in UCP platform (documented in Fandom community) |
| OAuth 1.0a / 2.0 (`Special:OAuthConsumerRegistration`) | Not offered | 403; Fandom never enabled the OAuth extension |
| `action=clientlogin` (MW native login) | Disabled | `authmanagerinfo` returns only `RememberMeAuthenticationRequest`, no password field |
| WMF-style API-key header | N/A | MediaWiki has no such thing; Fandom has none either |

```bash
# Live probe
curl '.../api.php?action=query&meta=authmanagerinfo&amirequestsfor=login&format=json'
# → only returns RememberMeAuthenticationRequest — native login is off
```

## What Fandom *does* expose

- **Helios SSO** at `services.fandom.com/mobile-fandom-app/fandom-auth/login`
  - POST `username` + `password` → `access_token` cookie
  - Used by Leaguepedia's official `mwcleric` Python lib (`LoginCredentials`)
  - Cookie is carried on subsequent `api.php` requests from same session

## Does an authenticated cookie lift the rate limit?

**No, not meaningfully.**

- MediaWiki's `noratelimit` right only belongs to specific wiki groups (`sysop`, `bot`). Regular logged-in users have the same API limits as anonymous.
- Joining the `bot` group on Leaguepedia requires wiki-admin (Leaguepedia staff) approval — not practical for a side project.
- The throttling we hit earlier is **Fandom's Cloudflare-edge IP rate limit**, which is session-agnostic. Auth cookies don't bypass it.
- `siprop=ratelimits` is stripped on Fandom (`Unrecognized value`) — we can't even enumerate the limits.

## Right answer for miti99bot (Cloudflare Worker)

No token registration needed. Mitigate via:

1. **Edge caching** — `fetch(url, { cf: { cacheTtl: 60, cacheEverything: true } })`. Many bot users share one cached response.
2. **KV result cache** — wrap the query in `create-store.js`, key = `matches:{from}:{to}`, TTL 60 s for "today", 5 min for "week".
3. **Cron pre-warm** — add a module cron (existing `cron-dispatcher.js` pattern) that refreshes the week window every 15 min. Telegram `/matches` then reads pre-warmed cache.
4. **CF Worker egress diversity** — Worker outbound IPs are many; per-IP buckets rarely hit 429 in practice.
5. **Honor `Retry-After`** on 429 and surface "data momentarily unavailable" to the user instead of stalling.
6. **Proper UA** — `miti99bot/0.1 (https://t.me/miti99bot; minhtienit99@gmail.com)` (already planned). Missing UA is itself a throttle signal on Fandom.

## Unresolved questions

1. Do CF Worker-origin fetches hit the same 429 as this shared egress does? (low risk — worth one real test before shipping)
2. Is the module's read pattern bursty or steady? If steady, cron pre-warm + long TTL removes all pressure. If bursty (many users hit `/matches` at game time), KV cache is still the lever.
