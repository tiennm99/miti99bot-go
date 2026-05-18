---
type: code-review
date: 2026-05-18
slug: security-go-app
status: final
scope: Go application code only (cmd/, internal/) — infra excluded
threat-model: Public-internet attacker + Telegram user; no AWS IAM creds, no DNS control
---

# Adversarial security review — miti99bot Go app

## TL;DR

**No Critical or High findings.** Auth boundaries at `/webhook` and `/cron/{name}` are tight: constant-time secret compare, body bounded only after auth, path regex enforced after auth (no fingerprinting). Telegram update path HTML-escapes all user-controlled strings before ParseMode=HTML. No `exec.Command`, no SSRF (only hardcoded outbound endpoints). One Medium (float input validation in `/trade_topup`) and a handful of Low/Info items below.

---

## Findings

| # | Severity | File:line | Issue | Fix |
|---|---|---|---|---|
| 1 | Medium | `internal/modules/trading/handlers.go:83-86`, `handlers.go:96`, `portfolio.go:80` | `/trade_topup +Inf` and `/trade_topup NaN` both pass the `amount <= 0` guard (NaN compares false to everything; +Inf is finite-vs-zero passable). `p.AddCurrency("VND", amount)` and `p.Meta.Invested += amount` poison the in-memory portfolio. `SavePortfolio` then errors out at `json.Marshal` (`json: unsupported value: +Inf`), so KV is NOT corrupted — but the user-facing reply is the generic "Could not save portfolio" instead of "amount must be finite". Operator may also incorrectly conclude KV is failing. | Add `math.IsInf(amount,0) \|\| math.IsNaN(amount)` check next to the `amount <= 0` guard in `handleTopup` (line 84). Same defense in `DeductCurrency` / `AddCurrency` would be belt-and-suspenders. |
| 2 | Low | `internal/modules/lolschedule/api_client.go:164` | `io.ReadAll(resp.Body)` is unbounded. A malicious upstream (`esports-api.lolesports.com`) could return a multi-GB body that OOMs the 256 MB Lambda. Threat requires controlling Riot or DNS/TLS MitM (out of stated threat model), but defense-in-depth is cheap. | Wrap with `io.LimitReader(resp.Body, 4<<20)` (4 MiB; typical page is ~50 KB). |
| 3 | Low | `internal/modules/lolschedule/format.go:13-40` | Two-key drift: `leagueOrder` (slice, line 13) and `majorLeagueSlugs` (map, line 29) duplicate the major-league list. Currently in sync; adding a league to one and forgetting the other silently drops it from the filter or from the ordering. Not a security finding per se, but a class of "silent fail" bug that's easy to introduce. | Derive one from the other (e.g. build `majorLeagueSlugs` from `leagueOrder` at init). |
| 4 | Low | `internal/modules/twentyq/parser.go:101-110` (`redactSecret`) | The defense regex `(?i)\bTARGET\b` only catches whole-word ASCII matches. The model could trivially defeat it by hyphenating ("gui-tar"), inserting zero-width chars, or rendering with surrounding non-`\W` Unicode. The prompt also forbids leaking the secret, so this is a defense-in-depth backstop — but its strength is much weaker than the comment suggests. Worst case: a chatty model gives the answer away faster, ruining game UX. No safety impact. | Either widen to handle simple obfuscations (strip `-_ ` from hint before matching) or document the limitation honestly. |
| 5 | Low | `internal/modules/util/info.go:32-44` | `/info` exposes `chat_id`, `thread_id`, `sender_id`. Guarded by `VisibilityProtected`. Correct gating — non-admin sees nothing. BUT: a denied non-admin gets *no reply at all* (silent deny per dispatcher.go:66). This means anyone in the group who knows `/info` exists can confirm the bot is present but learns nothing else. Acceptable; flagging because the visibility-vs-silence pairing is a project invariant worth keeping explicit. | No action. Verify in PR review that any future `/info`-like helper inherits both the Protected visibility AND the silent-deny default. |
| 6 | Info | `internal/server/router.go:66-69` | `cronDisabled=true` short-circuits to 404 *before* method check and *before* path-regex check. An attacker probing `/cron/anything` while the bot is misconfigured (empty secret) gets uniform 404 — no fingerprinting. This is the intended posture per policy "Red flag (d): ships cronDisabled=true". Note: the warn log "CRON_SHARED_SECRET unset" fires once at boot (`main.go:123-125`), so operator visibility relies on log scraping. | No action; verified correct. |
| 7 | Info | `internal/telegram/webhook.go:50-60` | Auth check order: method → header read → constant-time compare → body read+bound. Correct. An unauthenticated caller does NOT cause a 1 MiB read; `MaxBytesReader` is applied only after secret matches. No DoS via large-body floods from non-Telegram callers (modulo the slow-loris angle, mitigated by `ReadHeaderTimeout=10s` and `ReadTimeout=30s` in main.go:137-138). | No action; verified. |
| 8 | Info | `internal/server/router.go:75-79` | Constant-time secret compare for `/cron/{name}`. `subtle.ConstantTimeCompare` returns 0 immediately when lengths differ — *which is a timing oracle for length*. Standard caveat for `subtle.ConstantTimeCompare`. Mitigated because the secret is a fixed-length operator-chosen token; an attacker learning "secret length is 32" gains nothing actionable. | No action. |
| 9 | Info | `internal/modules/dispatcher.go:60-75`, `module.go:26-44` | `auth.Permits` denies silently (no reply) for `Visibility{Protected,Private}` to keep gated command existence private. Correct, but `Install` registers the *handler match* even for Private commands — so the dispatcher *does* run the matcher on every public message looking for `/fortytwo`. That's a tiny CPU cost, not a finding; recording it because a future change to "skip matcher entirely if not permitted" would need to preserve the silent-deny invariant. | No action. |
| 10 | Info | `internal/modules/trading/handlers.go:115`, `handlers.go:172` | `strconv.ParseInt(args[0], 10, 64)` then `qty <= 0` guard. `math.MaxInt64 * price` could overflow `float64` to `+Inf` for very large qty; `DeductCurrency(VND, +Inf)` then returns `(false, balance)` because `balance < +Inf`, so the user just sees "Insufficient VND. Need +Inf". No corruption. | No action. |
| 11 | Info | `internal/modules/loldle/handlers.go:272-279` (`/loldle_setmax`) | `VisibilityPrivate` (owner only). Argument is `Atoi` → range-check `[1, MaxGuessesCap=10]` → KV write. No risk. Verified. | No action. |
| 12 | Info | `internal/modules/lolschedule/subscribers.go:38-53` | Per-subject lock `state.subscribersMu` serializes Get→mutate→Put. Verified at `handlers.go:100-101, 120-121` and `cron.go:164-165` (pruneDeadSubscribers). All three callers acquire the lock. Concurrent `/lolschedule_subscribe` from same chat → second call sees the updated list and replies "Already subscribed". | No action; concurrency verified. |
| 13 | Info | `internal/server/router.go:82-87` | Path regex `^[a-z0-9_]{1,32}$` is enforced *after* successful auth. So an unauthenticated probe of `/cron/foo` returns 401 (same as `/cron/bar`); only an authenticated caller can enumerate valid cron names via 200/404 differences. Replay attacks (re-submitting a captured `X-Cron-Token`) are possible — no nonce/timestamp — but the policy correctly notes this is acceptable given IAM-gated caller. | No action. |
| 14 | Info | `cmd/server/main.go:319-321` | `awsconfig.WithHTTPClient(&http.Client{Timeout: ssmInitTimeout})` sets a 5s timeout on the AWS SDK HTTP client used for SSM GetParameters. Good — guarantees cold start doesn't hang on a stuck SSM endpoint. | No action; verified. |

---

## Red-team verdicts on accepted trade-offs

For each trade-off documented in `docs/deploy-aws-free-tier-guide.md:23-33`, confirm it does NOT enable an attack vector beyond what's documented:

| Trade-off | Verdict | Notes |
|---|---|---|
| Secrets in CloudWatch / Lambda env | **Confirmed scoped to IAM** | Code does not log secret *values* anywhere I could find. `webhook.go:74` logs JSON decode errors which don't echo body content. `main.go:343` logs `"count", len(out.Parameters)` not values. No echo of `TelegramBotToken` / `WebhookSecret` / `CronSecret` anywhere via grep. |
| Secret in Scheduler Target.Input | **Confirmed scoped to IAM** | Lambda receives the header from Scheduler invocation; the bot does not re-publish or store the secret. |
| NoEcho CFN param for cron secret | **No app-level impact** | Resolved into `cfg.CronSecret` at boot. Closure-captured. Process restart re-reads. |
| SSM SecureString fetched at cold start | **Confirmed safe** | `resolveSSMSecrets` at `main.go:290-345`: `WithDecryption: true`, 5s timeout, fails fast on `InvalidParameters`. No retry-storm risk. Secret never re-fetched during the warm container's life — fine because Lambda's max lifetime caps exposure. |

**No new findings escalated from trade-offs.**

---

## Verified non-issues (look suspicious, are fine)

| Pattern | File:line | Why it's fine |
|---|---|---|
| `apiKey = "0TvQnueqKa5mxJntVWt0w4LpLfEkrV1Ta8rQBb9Z"` hardcoded | `internal/modules/lolschedule/api_client.go:35` | Public web-client key embedded in lolesports.com's own JS bundle. Comment explicitly marks `#nosec G101`. Not a credential. |
| `math/rand` (not `crypto/rand`) for picking targets | `internal/modules/loldle/handlers.go:31`, `wordle/pick_random.go:25`, `loldle/stickers.go:35` | Picks game answers / sticker file_ids — not a security primitive. Goroutine-safe via stdlib's internal mutex. Twentyq uses `crypto/rand` to seed a per-state PCG (`twentyq.go:46-52`) — appropriate stronger choice for the seed. |
| `subtle.ConstantTimeCompare` length-leak | `router.go:76`, `webhook.go:56` | Length of secrets is fixed by operator config; no observable advantage to an attacker. Standard pattern. |
| `cmd/server/main.go:124` warns "cronDisabled" but cron handler still returns 404 (not 503) | `router.go:66-68` | Intended: 404 makes the route indistinguishable from a non-existent endpoint to scanners. Operator must read logs to know cron is disabled. |
| `webhook.go:103-105` suppresses trailing 200 on panic | webhook.go | Correct: a panicked handler may have already written a response; double-WriteHeader emits Go's "superfluous response.WriteHeader" warning. LogRequests middleware's recover-path tags status 500 from its side. |
| `dispatcher.go:60-75` matches every command on every text message | dispatcher.go | Acceptable: `matchCommand` is O(entities in message) which is bounded by Telegram. No regex compilation per call. |
| `firestore_kv.go:52-69` rejects keys with `/`, `.`, `..`, `__*__` | firestore_kv.go | Defense-in-depth: Firestore document-id constraints. Keys are module-constructed (e.g. `"user:%d"` → never user-controlled string portion); defensive check catches future regressions where a module concats user input into a key without sanitization. |
| `chathelper.SubjectFor` returns "" for channels | chathelper.go:26-39 | Correct: channels have no `From` user; modules then reply "Cannot identify chat" rather than scope state under a sentinel. |
| `lolschedule cron.go:127-131` calls `b.SendMessage(... ParseMode: ParseModeHTML, Text: text)` where `text=RenderToday(...)` | cron.go, format.go | `RenderToday` HTML-escapes every interpolated string (team labels, league names, BlockName, day labels). Verified at `format.go:95-99, 197, 203, 219-220, 256-258`. |
| `trongtruonghop` user-arg substitution into `tg://user?id=%d` link | misc.go:117, 134 | `arg` HTML-escaped on line 134; `u.ID` is an `int64` formatted with `%d` (no injection vector). `senderMention` escapes `name`. Username path uses `@%s` — Telegram client validates username chars server-side; even if a maliciously-named user could craft `</a><script>`, ParseMode=HTML on Telegram strips/rejects script tags and unknown attributes. |
| `trading/symbols.go:16` `tickerRe = ^[A-Z0-9]{1,16}$` then `url.PathEscape(ticker)` | symbols.go, prices.go:85 | Double-belt: alphanumeric uppercase already URL-safe; PathEscape is harmless redundancy. No SSRF / no path injection possible. |
| `cmd/server/main.go:267-269` `Port` validated by Atoi+range before `:"+port` concat | main.go | Fail-fast on bad port. ListenAndServe gets a clean integer suffix. |

---

## Concurrency verdicts (verified, not findings)

- **lolschedule subscribers**: `state.subscribersMu` held on all three Get→mutate→Put sites (`handlers.go:100, 120`; `cron.go:164`). Daily-push read-only loop (`cron.go:119-140`) does NOT hold the lock during the send fan-out — correct, because the listing is one-shot snapshot before the loop and prune happens after under a fresh lock acquisition. A subscribe arriving mid-push will not see its message today (acceptable — JS source had the same behavior, documented in subscribe reply).
- **wordle / loldle / trading / twentyq**: each uses `keylock.Map` per-subject so concurrent commands for the same chat serialize, distinct chats run in parallel.
- **Telegram dispatcher**: `bot.WithNotAsyncHandlers()` runs handlers in the same goroutine as the inbound webhook. `webhook.go:81-82` derives `ctx` from `r.Context()` with `WithTimeout(handlerTimeout=10s)`. Handler can't outlive the HTTP request. Correct.
- **metrics**: `atomic.Int64` for counters; map-add path takes RWMutex. Verified no read-after-free.
- **rngs**: twentyq uses a single seeded PCG protected by `s.rngMu` (`twentyq/handlers.go:33,40-43`). Loldle/wordle use `math/rand` package globals (`rand.Intn`) which Go's stdlib protects with an internal mutex. No races.

---

## Auth boundary verdicts (verified, not findings)

| Boundary | Mechanism | Verified |
|---|---|---|
| `/webhook` | `X-Telegram-Bot-Api-Secret-Token` header matched via `subtle.ConstantTimeCompare` against `cfg.WebhookSecret` | `webhook.go:55-60`; main fails fast if `WebhookSecret==""` (`main.go:73-76`) — empty secret cannot accidentally accept all. |
| `/cron/{name}` | `X-Cron-Token` header constant-time compare; empty secret → 404 closed | `router.go:66-80`; main.go:123-125 warns on empty. |
| Telegram command visibility | `Auth.Permits(v, update)` checked before dispatching every Protected/Private command; deny is silent | `dispatcher.go:65-67`; Auth gates verified at `dispatcher.go:32-43`. |
| `BotOwnerID == 0` posture | All Private/Protected commands denied | `dispatcher.go:36-41` — `a.BotOwnerID != 0 && ...` short-circuits to false. Main warns at boot (`main.go:120-121`). |

---

## Unresolved questions

- **`telegramRateLimitDelay = 50ms`** at `cron.go:66`: comment says "above 30 subscribers, throttle to ~20 msg/sec". `select` with `time.After(50ms)` between sends means up to ~20/sec — *plus* the handler must complete inside `defaultCronTimeout = 60s`. At 1000 subscribers throttled, the loop alone needs 50s, plus per-call API latency. Likely fine for current scale (<100 subscribers expected), but the cron will silently time out before completing a 1500+ subscriber fan-out and prune-list won't be written. Not a security finding; flag for capacity planning.
- **Memory growth of `keylock.Map` and `PerUserLimiter.buckets`**: both documented as bounded by Lambda lifetime. Verified the comment claims (`keylock.go:8-12`, `ratelimit.go:19-24`) but no automated test for unbounded growth. Outside review scope; mentioned because a regression to long-lived process (non-Lambda runtime) would convert these into slow leaks.

---

**Status:** DONE
