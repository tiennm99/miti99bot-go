# Phase 01 — Add `/trongtruonghop` to misc module

**Status:** Planned
**Priority:** Low (additive, no migration, no infra change)
**Mode:** fast

## Context links

- Module under change: `internal/modules/misc/misc.go`
- Helper API used: `internal/modules/util/chathelper/chathelper.go` (`ArgAfterCommand`, `ReplyHTML`)
- Visibility / validation rules: `internal/modules/module.go`, `internal/modules/validate.go`
- Forum-topic reply-routing fix that mandates `chathelper.Reply*` over raw `SendMessage`: commit 3a12615

## Overview

Stateless command. Two interpolation points (`<text>`, `@<sender>` × 2) into a fixed Vietnamese template. No KV. No new dependency. No new helper.

## Key insights

- `chathelper.ArgAfterCommand` already strips command + `@botname` correctly — covers `/trongtruonghop arg`, `/trongtruonghop@miti99bot arg`, etc.
- `chathelper.ReplyHTML` already forwards `MessageThreadID` (forum-topic safe). Do NOT bypass.
- Telegram's HTML parser accepts `@username` literally (it's not a tag) and resolves the mention server-side. Mixing `@username` with `<a href="tg://user?id=…">Name</a>` in the same message is allowed and standard.
- `From.Username` can be empty (account never set one). `From.FirstName` is also optional (deleted accounts). Both can be empty simultaneously — handle.

## Requirements

### Functional

- Command name: `trongtruonghop`, Visibility: `VisibilityPublic`, Description: `"Phát biểu disclaimer cho thành viên hiện tại"` (Vietnamese — keep short, fits `/help`).
- On `/trongtruonghop [text]`:
  1. `arg := strings.TrimSpace(chathelper.ArgAfterCommand(msg.Text))`
  2. If `arg == ""` → `arg = defaultTarget` (= `"VNG"`).
  3. Resolve sender mention from `msg.From` (see algorithm below).
  4. Send single HTML message via `chathelper.ReplyHTML`.
- If `msg == nil` or `msg.From == nil` → return `nil` (silent skip).

### Sender-mention algorithm

```go
func senderMention(u *models.User) string {
    if u.Username != "" {
        return "@" + u.Username                // safe verbatim; charset is [A-Za-z0-9_]
    }
    name := strings.TrimSpace(u.FirstName + " " + u.LastName)
    if name == "" {
        name = "thành viên"
    }
    return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, u.ID, html.EscapeString(name))
}
```

### Template

Package-level `const`:

```go
const trongTruongHopTemplate = "Trong trường hợp nhóm này bị điều tra bởi %s, %s khẳng định không liên quan tới nhóm hoặc những cá nhân khác trong nhóm này. %s không rõ tại sao lại có mặt ở đây vào thời điểm này, có lẽ tài khoản đã được thêm bởi một bên thứ ba."
const defaultTarget = "VNG"
```

Render: `fmt.Sprintf(trongTruongHopTemplate, html.EscapeString(arg), mention, mention)`.

## Architecture

No new types, no state, no new files. Single command added to `New(deps modules.Deps)` in `misc.go`. `deps.KV` not used by this command but `New` already receives it for the other two — no signature change.

## Related code files

**Modify:**
- `internal/modules/misc/misc.go`
- `internal/modules/misc/misc_test.go`
- `internal/modules/misc/handlers_test.go`
- `README.md` (misc-row description only)

**Create:** none.
**Delete:** none.

## Implementation steps

1. **misc.go**
   - Add imports: `fmt`, `html`, `strings` (only those not already imported).
   - Add `const trongTruongHopTemplate` and `const defaultTarget` near the existing `const lastPingKey`.
   - Add private function `senderMention(*models.User) string` (algorithm above).
   - Add `trongTruongHopCommand() modules.Command` (no `deps` needed — stateless).
   - Append it to the `Commands` slice in `New`.

2. **misc_test.go**
   - Extend the `want` map in `TestNew_RegistersExpectedCommands` with `"trongtruonghop": modules.VisibilityPublic`. The existing length check then implicitly verifies registration.

3. **handlers_test.go** — new test cases (reuse existing `installMisc`):
   - `TestTrongTruongHop_DefaultArgUsesVNG`: send `/trongtruonghop` from user 999 with username `boss`. Assert reply contains `"VNG"` and `"@boss"` (occurring twice).
   - `TestTrongTruongHop_CustomArg`: send `/trongtruonghop Acme Corp`. Assert reply contains `"Acme Corp"` and not `"VNG"`.
   - `TestTrongTruongHop_HTMLEscapesArg`: send `/trongtruonghop <script>`. Assert reply contains `&lt;script&gt;` and not the literal `<script>`.
   - `TestTrongTruongHop_NoUsernameFallsBackToLink`: build a custom update where `From.Username == ""`, `FirstName == "Anh"`. Assert reply contains `<a href="tg://user?id=42">Anh</a>` (twice).
   - `TestTrongTruongHop_EmptyDisplayNameFallsBackToThanhVien`: `Username == ""`, `FirstName == ""`, `LastName == ""`. Assert reply contains `>thành viên</a>`.

   Note: existing `testutil.NewPrivateMessage` sets `FirstName: "Test"` and no `Username` — for username-bearing cases, build the update inline (see `NewPrivateMessage` source for the shape, ~10 lines).

4. **README.md** — change misc table row from `Coin flip, dice, RNG utilities` to `Coin flip, dice, RNG utilities, /trongtruonghop disclaimer`.

5. Compile + lint + test:
   - `go vet ./...`
   - `make test`
   - `golangci-lint run ./...`

## Todo list

- [ ] Add constants + helper + command in `misc.go`
- [ ] Register command in `New`
- [ ] Update `misc_test.go` `want` map
- [ ] Add 5 handler-level tests in `handlers_test.go`
- [ ] Update README misc row
- [ ] `go vet`, `make test`, lint clean
- [ ] Smoke-test in a real Telegram group post-deploy (manual)

## Success criteria

- All new tests pass; existing tests still pass.
- `/help` automatically lists the command (registry-driven, no extra work).
- Sending `/trongtruonghop` in any chat (private / group / supergroup / forum-topic) produces exactly one reply that mentions the sender twice and the target once, all routed to the originating topic.

## Risk assessment

| Risk | Mitigation |
|---|---|
| HTML parse error if escape is forgotten | Centralise escape in the `Sprintf` call; covered by `TestTrongTruongHop_HTMLEscapesArg`. |
| `@<sender>` for username-bearing user breaks if username contains unexpected chars | Telegram enforces `[A-Za-z0-9_]{5,32}` server-side — no escaping required. Documented in handler comment. |
| Test that constructs a custom `Update` drifts from `testutil.NewPrivateMessage` shape | Keep the custom builder local to the test file; only override `From` fields. Don't add a public helper for one caller. |

## Security considerations

- Auth: none — public command.
- Input handling: user-supplied `<text>` is HTML-escaped before interpolation. No SQL / KV / shell surface.
- Output: HTML mode. Mention link uses Telegram-internal `tg://user?id=` scheme, which the client resolves locally — no external network call.

## Next steps

After this phase merges + deploys, no follow-up. Command is self-contained.
