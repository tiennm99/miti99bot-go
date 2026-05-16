# /trongtruonghop — disclaimer one-liner in misc module

**Date:** 2026-05-16
**Slug:** `260516-1409-trongtruonghop-command`
**Status:** Planned
**Mode:** fast (single phase, well-scoped add-on to existing module)

## Goal

Add a public `/trongtruonghop` command to the `misc` module. When invoked it
replies with a fixed Vietnamese disclaimer template, interpolating:

- `<text>` — argument after the command. Empty → `VNG`.
- `@<sender>` — mention of the user who sent the command. Resolved from
  `update.Message.From` (Telegram guarantees this on group/private text
  messages).

Output (single message):

```
Trong trường hợp nhóm này bị điều tra bởi <text>, @<sender> khẳng định không liên quan tới nhóm hoặc những cá nhân khác trong nhóm này. @<sender> không rõ tại sao lại có mặt ở đây vào thời điểm này, có lẽ tài khoản đã được thêm bởi một bên thứ ba.
```

## Phases

| # | Phase | File |
|---|-------|------|
| 01 | Add `trongtruonghop` command to misc module | `phase-01-add-trongtruonghop-command.md` |

## Files

- `internal/modules/misc/misc.go` — register new `trongtruonghopCommand()` in `New`; implement handler.
- `internal/modules/misc/handlers_test.go` — add coverage for default arg, custom arg, username vs no-username sender, HTML-escape of arg.
- `internal/modules/misc/misc_test.go` — extend `TestNew_RegistersExpectedCommands` map with the new command (VisibilityPublic).
- `README.md` — bump `misc` row description (single line) to mention the new command.

## Non-goals

- No KV interaction (this command is stateless).
- No new helpers in `chathelper` — `ArgAfterCommand` + `ReplyHTML` already cover everything we need.
- No localization framework — template is Vietnamese-only and inlined as a constant.
- No rate limiting beyond what the dispatcher already provides (it doesn't, and this is fine; output is a single short message).

## Decisions (locked)

| Question | Decision | Reason |
|---|---|---|
| Reply mode (plain vs HTML)? | **HTML** via `chathelper.ReplyHTML` | We need to mention users who lack a `@username` — only Telegram HTML `<a href="tg://user?id=…">` works for that. Plain `@username` for users with one is rendered verbatim by HTML mode and Telegram still resolves it. Single code path for both cases. |
| `@<sender>` formatting | If `From.Username != ""` → `@<username>` (literal, no HTML wrap). Else → `<a href="tg://user?id=<ID>">First Last</a>`. | Mirrors how Telegram itself renders mentions; `@username` is a native entity. `tg://user?id=` link is the documented fallback for username-less accounts. |
| Display name when no username | `strings.TrimSpace(FirstName + " " + LastName)`; if still empty → `"thành viên"` | Defensive — Telegram allows accounts with no first name (rare; deleted accounts). |
| Default `<text>` when arg empty | `"VNG"` (user spec) | Stored as a package-level const `defaultTarget` for visibility. |
| Visibility | `VisibilityPublic` | Joke/disclaimer command — usable by anyone in any chat the bot is in. |
| `<text>` sanitisation | `html.EscapeString` on the arg before interpolating | Arg is user-controlled. Without escaping, `<text>` containing `<` breaks the HTML parser and Telegram rejects the send (400). |
| Mention sanitisation | `@<username>` is `[A-Za-z0-9_]{5,32}` — safe verbatim. Display name path → `html.EscapeString` on the trimmed name. | First/last names can legitimately contain `<` / `&`. |
| `update.Message.From == nil` guard | Return nil (no reply) | Matches existing misc handlers' defensive shape. Channel posts (no `From`) are the only realistic path; we don't want to spam them. |
| Forum-topic routing | Use `chathelper.ReplyHTML(ctx, b, msg, …)` — it already forwards `MessageThreadID` | Locked by 3a12615 — every new reply MUST go through these helpers. |

## Success criteria

1. `/trongtruonghop` (no arg) in a private chat → reply contains `VNG` literally and `@<sender>` twice.
2. `/trongtruonghop SomeCompany` → reply contains `SomeCompany` and `@<sender>` twice.
3. `/trongtruonghop <script>` → reply renders `&lt;script&gt;` (verify via the recording bot's captured `Text`).
4. Sender without `Username` → reply contains `<a href="tg://user?id=…">FirstName</a>` instead of `@username`.
5. Sender with `Username` → reply contains `@username` (no `<a>` tag).
6. `make vet` + `make test` clean; `golangci-lint run ./...` clean.
7. `/help` lists `trongtruonghop` under `misc` (auto — registry-driven, no help-template change needed).

## Risks

| Risk | Mitigation |
|---|---|
| Telegram rejects HTML on malformed entity (e.g. unclosed `<a>`) | Build the mention via a single small helper that returns a closed tag; unit-test the helper directly. |
| User pastes very long text → message > 4096-char Telegram limit | The template is ~250 chars; arg would need to be ~3.6k to overflow. Acceptable risk for a joke command. Telegram returns 400 which `bot.SendMessage` propagates as an error; the dispatcher logs it. No silent failure. |
| Vietnamese diacritics in the template encoding | Source files are UTF-8 (verified in existing `wordle`/`loldle` strings). Inline the string verbatim — no escape sequences. |
| Command name `trongtruonghop` is unfamiliar | 14 chars, lowercase, alphanumeric — passes `validateCommand` regex per `internal/modules/validate_test.go:32`. |

## Unresolved questions

None.
