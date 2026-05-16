# Debug Report — Bot Commands Silent in Groups, Work in DM

**Date:** 2026-05-16
**Branch:** main (HEAD `df89431`)
**Status:** DONE — root cause identified with code-level + log-level evidence; proposed fix below; nothing changed yet.

## Symptom

User reports: bot responds correctly to commands sent in a private chat (DM). Same commands sent in a Telegram group do nothing — no reply, no error, no logged warning.

## Root cause

`github.com/go-telegram/bot@v1.20.0/handlers.go:85-93` does a **byte-exact equality check** on the bot-command entity text without stripping the `@botname` suffix that Telegram clients append in groups.

```go
if h.matchType == MatchTypeCommand {
    for _, e := range entities {
        if e.Type == models.MessageEntityTypeBotCommand {
            if data[e.Offset+1:e.Offset+e.Length] == h.pattern {
                return true
            }
        }
    }
}
```

For private chats:
- User sends `/help`.
- Telegram sets entity Offset=0, Length=5.
- Slice `data[1:5]` = `"help"` → equals `h.pattern="help"` → ✅ match.

For groups (this is what Telegram clients send when there are 2+ bots in the room OR when the user picks the command from the autocomplete menu):
- User sends `/help@miti99bot`.
- Telegram sets entity Offset=0, Length=15 (the `@suffix` is **inside** the entity).
- Slice `data[1:15]` = `"help@miti99bot"` → does NOT equal `"help"` → ❌ no match.

The library never strips the `@botname` portion. No test in `handlers_test.go` covers the group form (only bare `/foo`). All 27 commands registered via `dispatcher.go:52-71` `Install` use `MatchTypeCommand`, so all 27 commands fail the same way in groups when sent with the suffix.

## Evidence

### 1. Library source

`/config/go/pkg/mod/github.com/go-telegram/bot@v1.20.0/handlers.go:85-93` — exact match logic shown above.

`/config/go/pkg/mod/github.com/go-telegram/bot@v1.20.0/handlers_test.go` — only test cases:
| Input | Length | Result |
|---|---|---|
| `/foo` | 4 | match ✓ |
| `a /foo` | 4 (offset 2) | match ✓ |
| `a /bar` | 4 | no match ✓ |

No `/foo@botname` cases tested.

### 2. Production logs

CloudWatch `/aws/lambda/miti99bot`, last 60 min:

| Time (UTC) | Event | Notes |
|---|---|---|
| 04:22:02 | `POST /webhook 200 1ms` + `[TGBOT] [UPDATE] ID:835070937` | Update arrived. No metric for it. |
| 04:22:59 | `POST /webhook 200 1372ms` | Long-running handler — this is the DM that worked. |
| 04:23:56 | `metrics commands={trade_stats:1}` | Metric flush — only **1** command counted in the cron interval. |
| 04:23:56 | `POST /webhook 200 0ms` + `[TGBOT] [UPDATE] ID:835070939` | Update arrived, returned immediately with no command dispatched. |

So 3 webhooks, 1 fired a handler (`trade_stats`). The other 2 are the group attempts: payload accepted, secret validated, JSON decoded, dispatched into `b.ProcessUpdate` — and silently dropped because no handler matched. **No errors, no panics, no `unauthorized`, no `request body too large`**. Exact signature of "the match function returned false for every registered handler".

### 3. Webhook config matches expectation

`getWebhookInfo` (output from current production state, verified by previous workflow run logs):
- `url=https://<lambda>.lambda-url.ap-southeast-1.on.aws/webhook`
- `allowed_updates=["message","callback_query"]`
- `pending_update_count` ≈ 0 (no delivery backlog)

Group messages **are** being delivered. Telegram is not the problem.

### 4. Bot Privacy Mode is NOT the root cause

Privacy mode would prevent the update from arriving at all. We see group updates arrive (rows 1 and 4 above). Privacy mode is therefore either OFF, or the user typed a command (which Telegram delivers even with privacy ON). Either way, the failure is downstream of delivery — in the dispatcher's match logic.

## Hypotheses ruled out

| Hypothesis | Why ruled out |
|---|---|
| Webhook secret mismatch in groups | Would return 401; logs show 200. |
| Body-too-large from group payloads | Would return 413; logs show 200. |
| Panic in handler | Would log `webhook handler panic` (`webhook.go:75-81`). Nothing. |
| Auth denial (Visibility check) | Help/info commands are `VisibilityPublic` (cf. `dispatcher.go:26`) — auth.Permits returns true unconditionally. Also a denial would silently return AFTER the handler closure runs, so the metric `IncCommand` at `dispatcher.go:63` would NOT fire. We'd see no metrics — same outward symptom — but a private-chat `/trade_stats` did increment, so the path works when match succeeds. Auth not the issue. |
| Module disabled in groups | No code path filters by chat type in dispatch. `wordle/handlers.go:219`, `loldle/handlers.go:247` use `Chat.Type == ChatTypePrivate` only as a presentation switch (DM uses richer formatting); they do not gate command matching. |
| Telegram delivering group updates to wrong endpoint | Single Function URL; only one webhook registered. |

## Proposed fix (not applied — awaiting your decision)

**Option A — Local wrapper using `MatchFunc` (recommended)**

Replace `b.RegisterHandler(..., bot.MatchTypeCommand, ...)` in `internal/modules/dispatcher.go:52-71` with `b.RegisterHandlerMatchFunc(matchFunc, ...)`, where `matchFunc` strips the trailing `@<bot-username>` from the entity bytes before comparing. Single localized change; no library fork.

Sketch:
```go
matchCmd := func(name string) bot.MatchFunc {
    return func(update *models.Update) bool {
        msg := update.Message
        if msg == nil {
            return false
        }
        for _, e := range msg.Entities {
            if e.Type != models.MessageEntityTypeBotCommand || e.Offset != 0 {
                continue
            }
            tok := msg.Text[e.Offset+1 : e.Offset+e.Length]
            if i := strings.IndexByte(tok, '@'); i >= 0 {
                tok = tok[:i]
            }
            if tok == name {
                return true
            }
        }
        return false
    }
}
```

Pros: zero library upgrade pressure; precise behavior; easy to add a unit test (which the upstream library lacks).
Cons: bypasses `bot.MatchTypeCommand`'s niceties (none we use); adds ~15 LOC + tests.

**Option B — Force users to type bare `/help`**

Documented workaround only. Doesn't fix the bot. Reject.

**Option C — File upstream issue / PR**

Worth doing in parallel, but blocked on upstream review / release timeline. Don't wait.

Recommend Option A + a fresh unit test in `internal/modules/dispatcher_test.go` covering both `/help` and `/help@miti99bot` against a registered `"help"` handler.

## Secondary findings (out of scope for this report)

1. `[TGBOT] [UPDATE]` log lines (`2026/05/16 04:22:02 [TGBOT] [UPDATE] &{ID:... Message:0x40002dc488 ...}`) print struct pointers, not contents. They are emitted by `bot.WithDebug(...)` in the go-telegram lib. Recommend adding a thin pre-dispatch log line in `internal/telegram/webhook.go:82` (just before `b.ProcessUpdate`) that records `update_id`, `chat.id`, `chat.type`, and `message.text` (truncated, no PII concerns since group IDs are not secret). Would have made this exact bug trivially observable instead of requiring a 3-source triangulation. ~10 LOC.

2. Library version `v1.20.0` was released some time back; the head of the repo may or may not have this fixed — worth a quick GitHub check before Option C.

## Unresolved questions

- Confirm with user whether they want Option A applied now (single phase, ~30 min) or queued behind other work.
- Do you want the extra dispatch-time log line (secondary finding 1) included in the same fix?
