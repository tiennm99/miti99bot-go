# Code review — `/trongtruonghop` command

**Date:** 2026-05-16 14:26
**Scope:** unstaged diff (4 files, +134 / -4)
**Verdict:** ship as-is.

## Spec compliance

All 8 acceptance criteria verified against `internal/modules/misc/misc.go` and the new tests:

| # | Criterion | Verified at |
|---|---|---|
| 1 | Default `<text>` → `VNG` | `misc.go:130-132`; test `TestTrongTruongHop_DefaultArgUsesVNG` |
| 2 | Custom arg preserved + HTML-escaped | `misc.go:129,134`; tests `_CustomArg`, `_HTMLEscapesArg` |
| 3 | Username → `@username` literal (×2) | `misc.go:110-112`; `_DefaultArgUsesVNG` asserts `Count(@boss)==2` |
| 4 | No username → `<a href="tg://user?id=…">FirstName</a>` (×2) | `misc.go:113-117`; `_NoUsernameFallsBackToLink` |
| 5 | Empty display name → `thành viên` | `misc.go:113-116`; `_EmptyDisplayNameFallsBackToThanhVien` |
| 6 | Reply via `chathelper.ReplyHTML` (forum-topic safe) | `misc.go:135`; helper forwards `MessageThreadID` at `chathelper.go:81-83` |
| 7 | Silent return on `Message==nil`/`From==nil` | `misc.go:126-128` |
| 8 | Existing misc commands untouched | diff only appends a slice entry at `misc.go:49`; no edits to ping/mstats/fortytwo handlers |

## Adversarial checks

- **HTML injection on `<text>`:** `html.EscapeString` applied at the `Sprintf` call site (`misc.go:134`). All three `%s` slots are escape-safe: target is escaped; both mentions are either `@`+ASCII-restricted username or a closed `<a>` tag with an escaped name.
- **`html.EscapeString` vs. Telegram HTML spec:** stdlib emits `&#34;` / `&#39;` for `"` / `'`. Telegram's HTML parser accepts **all numeric character references** in addition to the four named entities (`&lt;`, `&gt;`, `&amp;`, `&quot;`), so the numeric forms render correctly. Confirmed via Telegram docs / community references. Net: stdlib is the right tool — slightly over-escapes vs. the minimum Telegram requires, which is strictly safer, not broken.
- **`@username` charset:** Telegram enforces `[A-Za-z0-9_]{5,32}`. None of those bytes need HTML escaping. The "safe verbatim" assertion holds for real Telegram traffic. (Test-only hostile `Username` would break — not a production threat.)
- **`tg://user?id=%d`:** `u.ID` is `int64` — `%d` is injection-proof.
- **Blast radius in `misc.go`:** no shared state, no init-order dependency, no signature change. New const + helper + factory func; only mutation is appending to the `Commands` slice. `TestNew_RegistersExpectedCommands` length-check would have caught any drop.
- **Test determinism:** no time/random/parallel. Inline `trongTruongHopUpdate` reuses `NewPrivateMessage` then overwrites `From`; the `botCommandEntity` is keyed off `text`, not `From`, so override is safe. All 5 new tests pass (re-ran locally, 0.033s).
- **README row:** previous text ("Coin flip, dice, RNG utilities") never matched the actual module — fix is accurate.

## Minor observations (non-blocking)

- `senderMention` `u == nil` branch is defensive dead code given the handler's `From == nil` guard; harmless and cheap.
- 4096-char Telegram limit: template ~260 chars + 2× mention (≤ ~70) + arg. User would need a ~3.7 KB arg to overflow; `b.SendMessage` would surface the 400 as a returned error, which the dispatcher logs. Acceptable.

## Unresolved questions

None.

**Status:** DONE
**Summary:** Implementation matches spec, escape boundaries are correct, no regression risk to sibling commands. Ship it.
