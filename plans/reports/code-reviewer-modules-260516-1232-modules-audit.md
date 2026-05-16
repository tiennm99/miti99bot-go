# Modules audit — 260516-1232

## Summary
- 14 findings: 11 safe, 3 I/O-changing
- Overall health: framework is clean, well-tested, idiomatic. Most modules show consistent KV+keylock discipline. Two real correctness gaps: (1) `lolschedule` subscriber list mutated without a lock, (2) `twentyq` `_ = clearGame` swallows error paths. Several DRY/style nits.

## Safe findings (behavior-preserving — assistant will apply)

- **[high] lolschedule subscriber list — unprotected Get→mutate→Put** — internal/modules/lolschedule/subscribers.go:33-48, 52-72
  - What: `addSubscriber`/`removeSubscriber` load → modify slice → save, but lolschedule has no `keylock.Map`. Two concurrent `/lolschedule_subscribe` from different chats lose one write (the second overwrites the first's append).
  - Why it matters: silent data loss on every concurrent subscription burst, which is exactly when daily push wiring is most likely (multiple groups subscribing on a launch announcement).
  - Fix sketch: add `keylock.Map` to `state`, wrap addSubscriber/removeSubscriber with `defer locks.Acquire(subscribersKey)()`, or take a single module-wide lock since the slot is one global key.

- **[med] twentyq drops error on auto-clear of solved-but-lingering games** — internal/modules/twentyq/handlers.go:96
  - What: `_ = clearGame(ctx, s.kv, subject)` silently discards KV errors. If the delete fails, every subsequent `/twentyq` call repeats the same failing delete forever (game stays Solved → branch re-enters).
  - Why it matters: KV error during recovery turns into permanent module deadlock for the subject; no log trail.
  - Fix sketch: log on error; consider returning the error so the user sees `upstreamFail` rather than silently looping.

- **[med] misc /mstats returns error to dispatcher instead of replying** — internal/modules/misc/misc.go:78-80
  - What: KV read failure path returns wrapped error; dispatcher logs but user sees no reply.
  - Why it matters: inconsistent with trading/wordle/loldle where load-failure paths reply with a user-visible hint. Silent timeout on the user side.
  - Fix sketch: log + `chathelper.Reply(...,"Could not load stats. Try again later.")`, return nil.

- **[med] PriceClient builds a fresh `*http.Client` on every call** — internal/modules/trading/prices.go:31-36
  - What: zero-value `PriceClient.httpClient()` returns `&http.Client{Timeout: kbsHTTPTimeout}` on every `FetchPrice`. New client → new Transport → no connection reuse across calls (matters in `handleStats`'s loop over holdings).
  - Why it matters: TLS handshake per stock during /trade_stats on cold cache.
  - Fix sketch: cache the default client on the receiver (sync.Once) or initialise in `newState`.

- **[low] `findChampion` recomputes `normalizeName` for every champion on every guess** — internal/modules/loldle/lookup.go:36, 42
  - What: O(N) per guess, each call allocs a new lowercase byte slice for the same names.
  - Why it matters: champions.json ≈170 entries × 2 normalize calls × every guess. Tiny but pure waste; a built-once `map[normalized]int` would also simplify the prefix-ambiguity loop.
  - Fix sketch: precompute `normalizedNames []string` in `loadChampions` once; lookup runs over precomputed strings.

- **[low] `RenderHelp.ownerOf` is O(modules × commands) per command** — internal/modules/util/help.go:42, 47, 80
  - What: For each public+protected command, a linear scan over every module's commands.
  - Why it matters: cold-path, but DRY: registry already knows the owner (`owners` map in `Build`). Easy to expose `Registry.OwnerOf(name)`.
  - Fix sketch: store `owner map[string]string` on Registry; have ownerOf consult it.

- **[low] loldle/handlers.go does not record a loss on data-refresh round drop** — internal/modules/loldle/handlers.go:144-152
  - What: target gone from champions.json → `clearGame` only, no `recordResult(false)`. Consistent with JS-parity comment but means a guess that hit the dead-target branch consumes nothing.
  - Why it matters: arguably correct (not the user's fault), but inconsistent with /loldle_giveup which records loss when target is gone (handlers.go:220-224 records before target lookup). Worth one line of doc.
  - Fix sketch: leave behavior, add a code comment cross-referencing /loldle_giveup so the asymmetry is intentional, not an oversight.

- **[low] `lolschedule.cacheKey` uses unstable RFC3339 with monotonic risk only in test paths** — internal/modules/lolschedule/lolschedule.go:227-229
  - What: not a real bug today (RFC3339 strips monotonic), but `time.Now().UTC().UnixMilli()` vs `cached.Ts` could go negative under clock skew between Lambda instances → unintended cache hit on backwards-clocked data.
  - Why it matters: theoretical; on Lambda the clock skew is small. Worth `if diff := now-cached.Ts; diff >= 0 && diff < cacheTTL.Ms()` guard.
  - Fix sketch: change comparison to require non-negative age.

- **[low] `lolschedule.truncate` byte-slices UTF-8** — internal/modules/lolschedule/lolschedule.go:264-269
  - What: `s[:maxLen]` slices bytes; can split a multibyte rune. Logs only, so user-invisible, but log JSON encoders may emit replacement chars.
  - Fix sketch: switch to rune-aware slicing (`[]rune(s)[:n]`) since this is cold path.

- **[low] `prices.go` shadows builtin `close`** — internal/modules/trading/prices.go:103
  - What: `close := body.DataDay[0].C` shadows `close` (builtin for channels). No bug; readability nit.
  - Fix sketch: rename to `c` or `lastClose`.

- **[low] `formatStats` parens math for round-half-up is opaque** — internal/modules/twentyq/render.go:85, 89
  - What: `(s.Solved*200 + s.Played) / (2 * s.Played)` is integer round-half-up of solveRate. Works but is unobvious; tests don't probe boundary (e.g. solved=1, played=3 → solveRate=33; solved=1, played=2 → 50).
  - Fix sketch: add a one-line comment naming the formula, or extract `roundPct(num, den int)` helper.

## I/O-changing suggestions (need user approval — DO NOT apply)

- **[med] /lolschedule unknown subscribers should be pruned on 403** — internal/modules/lolschedule/cron.go:91-98
  - What: a chat that blocked the bot returns `Forbidden: bot was blocked by the user`. The cron logs and continues but never removes the dead chat from the subscriber list. Forever-retried.
  - Why it matters: subscriber list grows monotonically with dead chats; daily push wastes API calls; on Telegram global ratelimit (30 msg/s) one bad apple lengthens the throttled window for everyone.
  - User impact: dead subscribers stop receiving anything (already true); user-visible change is auto-removal — a subscribed user who blocks then unblocks the bot would have to /lolschedule_subscribe again to restore.
  - Fix sketch: inspect error from SendMessage for 403 / "bot was blocked" / "chat not found", call `removeSubscriber`. Document in the unsubscribe message.

- **[low] twentyq seed pool is small (51) — fast repeats** — internal/modules/twentyq/seeds.go
  - What: per-subject, after ~20 plays a repeat is near-certain. JS-parity, but JS may have been small too.
  - User impact: noticeably repetitive games for power users.
  - Fix sketch: grow the seed list, or maintain per-subject "recent seeds" exclusion window in KV.

- **[low] /info exposes thread/chat/sender IDs publicly** — internal/modules/util/info.go
  - What: `VisibilityPublic`. Anyone in a group can see the chat ID. Low sensitivity, but in some moderation setups admins prefer not to expose internal IDs.
  - User impact: shifting to `VisibilityProtected` would deny non-admins.
  - Fix sketch: flip visibility OR keep as-is and document the choice.

## Architecture observations (no immediate change)

- Module factory closure pattern is consistent and clean across all six modules — `state{kv, ...}` built in `New`, closures bound to handlers.
- `chathelper` consolidates the obvious helpers; nothing else duplicated across modules looks worth folding (the trading argsAfterCommand is genuinely different from chathelper.ArgAfterCommand — chathelper returns a single trimmed string, trading needs the tokenized slice).
- `keylock.Map` usage discipline is right: `defer locks.Acquire(subject)()` everywhere mutation happens. Only gap: lolschedule subscribers.
- Cron-vs-bot decoupling via `messageSender` interface in lolschedule/cron.go is the right call; mirror it if another module ever adds a cron that sends.
- Visibility enforcement is correct: dispatcher gates BEFORE handler runs, denies are silent — no leak.

## Unresolved questions
- Is the JS source's lolschedule subscribe also unlocked? If yes, the lock omission may be parity-deliberate; either way the Go runtime needs it (Lambda concurrent invocations on same chat possible).
- Should `/loldle` ambiguous-prefix behaviour (`findChampion` returns nil → "Champion not found") be improved to list candidates? UX gap, but I/O-changing.
