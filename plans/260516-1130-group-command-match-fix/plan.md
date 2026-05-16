# Fix Group Command Matching + Observable Dispatch Logging

**Date:** 2026-05-16
**Slug:** `260516-1130-group-command-match-fix`
**Status:** Implemented (awaiting deploy + group smoke test)
**Mode:** fast (single phase, well-scoped)
**Linked diagnostic:** `../reports/debugger-260516-1124-group-commands-not-matching.md`

## Goal

Bot commands sent in Telegram groups (`/help@miti99bot` form) must match registered handlers. Currently they silently miss because `github.com/go-telegram/bot@v1.20.0/handlers.go:88` does byte-exact equality without stripping the `@<botname>` suffix.

Add structured pre-dispatch logging so the next "silent drop" symptom is observable in CloudWatch without code archaeology.

## Phases

| # | Phase | File |
|---|-------|------|
| 01 | Strip `@suffix` in dispatcher + add update log | `phase-01-strip-botname-suffix-and-log-dispatch.md` |

## Files

- `internal/modules/dispatcher.go` — swap `RegisterHandler(..., MatchTypeCommand, ...)` for `RegisterHandlerMatchFunc(...)` with a local matcher that strips `@suffix`
- `internal/modules/dispatcher_test.go` — add test cases covering `/help` (DM) and `/help@miti99bot` (group), plus negatives
- `internal/telegram/webhook.go` — add one structured log line just before `b.ProcessUpdate` recording `update_id`, `chat.id`, `chat.type`, `text` (truncated)

## Non-goals

- Don't fork the upstream library.
- Don't add a `TELEGRAM_BOT_USERNAME` env var — Telegram routes `/cmd@otherbot` to the addressed bot, so we never receive a foreign-suffixed command. KISS.
- Don't refactor existing Auth / Visibility logic.

## Success criteria

1. New unit test passes: `/help@miti99bot` with offset=0 length=15 matches handler registered as `"help"`.
2. Existing tests still pass (`make test` clean).
3. `golangci-lint run ./...` clean.
4. After deploy, repeating "send `/help@miti99bot` in group" produces a log line like `dispatch update_id=... chat_type=group chat_id=-100... text="/help@miti99bot"` AND the help reply is sent.

## Risks

| Risk | Mitigation |
|---|---|
| Custom matcher breaks edge case the library handled | Mirror lib semantics: only match `MessageEntityTypeBotCommand` entities, scope to `update.Message.Text` (HandlerTypeMessageText) |
| Log line leaks PII in group chats | Chat IDs and group titles are not secret; user IDs are not logged. Message text truncated to 64 chars. |
| Test mutates global state via library bot.New | Tests already use `testutil.NewRecordingBot` — reuse pattern |

## Unresolved questions

None — locked decisions: no env var, strip-unconditionally, truncate text to 64 chars in log.
