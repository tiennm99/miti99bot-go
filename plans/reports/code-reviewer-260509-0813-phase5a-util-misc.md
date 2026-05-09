# Phase 5a code review — util + misc modules

**Plan:** plans/260508-2222-go-port-cloud-run/phase-05-port-simple-modules.md (subset)
**Scope:** util (info / help / stickerid) + misc (ping / mstats / fortytwo) + Registry-in-Deps wiring.
**Build/test:** `go vet ./...` clean; `go test -race -count=1 ./...` green (6 pkgs).

## Overall

Clean port. Logic mirrors JS source closely; HTML escaping is consistent; KV best-effort/ErrNotFound handling on the `misc` side is well-thought. The intentional choices listed in the task brief check out — Registry-in-Deps via pointer, cmd/server-owned catalog, and skipped Telegram-side tests are all defensible. Two real bugs to fix and a few footguns worth noting.

---

## Critical

### C1. /info nil-pointer panic when `update.Message == nil`

`internal/modules/util/info.go:36`

```go
ChatID: msg.Chat.ID,
```

The handler builds the `text` defensively (covering `msg == nil`, `msg.From == nil`, etc. via the `if msg != nil` block), then immediately dereferences `msg.Chat.ID` *outside* that guard. If `update.Message` is ever nil, we panic on send.

Today the dispatcher only registers handlers via `bot.HandlerTypeMessageText` + `MatchTypeCommand`, so `update.Message` is always populated and we will not hit it. But:
- The defensive `n/a` fallbacks become misleading dead code if we believe `msg` can be nil.
- This pattern (defensive read, undefended write) is exactly what bites later when `/info` gets reused from a non-message update path (callback_query, edited_message — your point D in the brief).

**Fix:** either drop the `if msg != nil` block (commit to `msg` always non-nil, document at top of file) or guard the send too:

```go
if msg == nil { return nil }
// build text…
_, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: text})
return err
```

Recommend the second form — same posture as `stickerid.go:28`, which already does `if msg == nil { return nil }`. That one-line change makes /info match its sibling.

Note: the JS version (`info-command.js:14-18`) has the same latent issue (`ctx.reply` would explode on nil chat), so this is "JS-parity correctness", not a regression — but worth fixing on the way through.

---

## High

### H1. /info reports `thread id: n/a` for two distinct cases

`info.go:27` treats `MessageThreadID == 0` as "no thread". JS uses `?? "n/a"` against `message_thread_id`, which is `undefined` when the field is absent. The fields collapse to the same output ("n/a") in Telegram's wire model because Telegram simply omits zero/missing thread IDs — so functional parity holds. Just calling out: if Telegram ever sends `0` explicitly we render "n/a" where JS would render "0". Low likelihood, log-only impact. **No action required**, but worth a one-line comment so a future reader does not "fix" it.

### H2. RenderHelp ownership lookup is O(M·C) per command

`help.go:42-49,80-89` calls `ownerOf(reg, c.Name)` for every public + protected command. `ownerOf` linear-scans every module's commands. With M modules and C commands per module, that's O((P+R)·M·C) where P+R is the public+protected total. Today: M≤10, C≤10, so 1k ops per `/help`. Fine.

But `reg.AllCommands` already maps name → Command, and `Build` already knows `owners` (line `registry.go:83,118`). Exposing that owners map (or a reverse lookup) on `Registry` would make the renderer O(P+R) and remove the linear scan. **YAGNI today, flag for if module count grows.**

---

## Medium

### M1. Registry-in-Deps: pointer-during-construction is safe **today**, fragile by design

The brief asks: are there races between `Build` mutating Registry maps and a concurrent handler reading them?

**Verdict: safe today.** Trace:

1. `Build` runs single-goroutine in `cmd/server.main`. By the time it returns, every map is fully populated.
2. `modules.Install(b, reg)` runs single-goroutine *after* `Build` returns. Handler closures capture `reg` by pointer.
3. Handlers are only invoked by `b.ProcessUpdate` from the `/webhook` HTTP handler — which runs *after* `srv.ListenAndServe()`, which runs after `Install`.
4. After Build, `Registry` fields are read-only. There is no code path that mutates a `Registry` field after Build returns.

So: `Build` writes happen-before `srv.ListenAndServe()` start (single goroutine), which happens-before any handler read. No race.

**The fragility:** this is an undocumented invariant. Any future code that does `reg.AllCommands["foo"] = bar` after startup (e.g. dynamic module loading, hot-reload) silently breaks. Suggest:

- Add a `// Registry is read-only after Build returns. Callers must not mutate fields.` comment on the `Registry` struct (currently the docstring at `module.go:69-71` covers Deps.Registry, but the struct itself does not say so).
- Optionally, in a follow-up, freeze the maps by replacing them with a tiny accessor type that panics on `Set` post-build. Not needed for v1.

The pointer's stability across `Build`'s execution is fine because (a) `reg` is heap-allocated once at `registry.go:74` and (b) handler closures capture the *pointer*, not the maps — so even if `reg.AllCommands` were re-assigned (it isn't), capture-time would not be the problem. **Documented intentional choice; no code change needed.**

### M2. /help footer link is hard-coded; module name is hard-coded to "github.com/tiennm99/miti99bot-go"

`help.go:15`. Fine for v1, but if you ever rebrand or fork-publish under a different repo, every test in `help_test.go` plus the live footer needs editing. Consider pulling from a single constant in a top-level `internal/build` or `version` package later. **Not blocking.**

### M3. `misc /mstats` JSON wraps a stricter error contract than JS

JS (`misc/index.js:35-41`): `db?.getJSON("last_ping")` swallows missing-key into "last ping: never" and any DB error into a JS exception that the framework probably surfaces as a generic reply. Go (`misc.go:66-72`) treats `ErrNotFound` as "never" and any *other* error as a wrapped error to the dispatcher (which then logs but still returns success — `dispatcher.go:23`).

Net behaviour: missing → "last ping: never" (parity ✓); transient Firestore error → user sees no reply, logs see a wrapped error (JS would also fail to send a reply, the user-facing parity holds). Better than JS here — explicit error wrapping. **No action.**

---

## Low

### L1. /stickerid usage message is plain text but the success path is HTML

`stickerid.go:34-38` sends the usage message with no `ParseMode`; the success branch sends with `ParseMode: HTML`. Inconsistent, harmless, mirrors JS exactly (`stickerid-command.js:22-25`). **Skip.**

### L2. `lastPing.At` JSON field is `at` (lowercase) — wire-compatible with JS?

`misc.go:24` tags `At time.Time \`json:"at"\``. JS writes `{ at: Date.now() }` — a number (ms epoch). Go writes RFC 3339 string via `encoding/json`'s default `time.Time` marshaler. **These are not wire-compatible.** A Firestore document written by the JS bot will not unmarshal into the Go struct cleanly (the JSON decoder will fail to parse a number into a time.Time and return an error → /mstats says "last ping: never" after an error log).

This is *not* a regression for greenfield Cloud Run deploys (no JS-written data exists), but contradicts the plan-file goal "same KV state shape so a future export-import migration is feasible" (`phase-05`, line 16). Two options:

1. (Cheap) marshal as `int64` ms-epoch to match JS:
   ```go
   type lastPing struct { At int64 `json:"at"` } // ms since epoch
   ```
2. (Clean) custom `UnmarshalJSON` on `lastPing` that accepts both number and RFC 3339.

Recommend (1) for byte-for-byte parity since the brief says "byte-for-byte parity is the goal". **Single-line change** in `misc.go` (model + `time.Now().UnixMilli()` and `time.UnixMilli(last.At)`).

### L3. `/help` test would be sturdier with a description containing `&` and `"`

Existing test covers `<i>desc</i>` → `&lt;i&gt;desc&lt;/i&gt;`. Add one more case with `&` (already-escaped entity that must double-escape to `&amp;`) and `"` (which `html.EscapeString` does escape to `&#34;`). One-line addition:

```go
cmd("b_amp", modules.VisibilityPublic, `Tom & "Jerry"`),
// expect: "Tom &amp; &#34;Jerry&#34;"
```

This locks in `html.EscapeString` semantics (which differ subtly from a hand-rolled escaper — & always escapes; " only outside attrs in HTML5 but Go's escaper does it unconditionally). Worth one extra assertion; lets you swap escapers later without test churn surprise.

### L4. Module-name HTML-metachar test is currently not feasible

Brief asks. `commandNameRe` (`validate.go:8`) is `^[a-z0-9_]{1,32}$`, and `moduleNameRe = commandNameRe` (`registry.go:13`). So no module or command name can contain HTML metacharacters. The `html.EscapeString(mod.Name)` call at `help.go:59` is dead-defensive. **Skip — already noted in the brief, agreed.**

---

## JS-parity spot checks

| JS behaviour | Go parity | Note |
|---|---|---|
| /info: `chatId ?? "n/a"`, `threadId ?? "n/a"`, `senderId ?? "n/a"` | ✓ via `if msg/from != nil` guards | C1 fix recommended for nil-msg send |
| /help: HTML escape every user string, omit modules with no public+protected | ✓ | tests cover both |
| /help: parse_mode HTML, link preview disabled | ✓ `help.go:102-103` | bot.True() pointer dance idiomatic |
| /stickerid: private, requires reply, fallback "(no set)" + "—" | ✓ | exact strings preserved |
| /ping: best-effort KV write, console.warn on failure, still reply | ✓ | uses `log.Printf` instead of warn — fine |
| /mstats: read last_ping, format as ISO if present, else "never" | partial — see L2 | RFC3339 vs JS ms-epoch wire format |
| /fortytwo: private, replies "The answer." | ✓ | |

---

## Recommended actions (priority order)

1. **C1** — guard `msg == nil` before `b.SendMessage` in `/info` (one line). Resolves a latent panic and matches `/stickerid` posture.
2. **L2** — change `lastPing.At` to `int64` ms-epoch JSON for byte-for-byte parity with JS-written KV documents (one line in `misc.go`, plus update test). Without this, plan-stated KV migration goal silently breaks.
3. **M1** — one-line doc comment on `Registry` struct: "read-only after Build returns".
4. **L3** — add `&`/`"` description in help test; locks in escaper contract.

Items 1 and 2 are real correctness fixes. Items 3 and 4 are hygiene.

## Test gaps worth filling (1–2 tests)

- **`util_test.go` for `/info`** — pure formatter test by extracting the text-build logic into a tiny `func formatInfo(msg *models.Message) string`. Catches future regressions like C1 (nil msg) and H1 (zero MessageThreadID) without standing up a fake bot.
- **`stickerid_test.go`** — same shape: extract `func formatStickerReply(s *models.Sticker) string` and test "no set_name → (no set)" + "no emoji → —" + HTML-escaped file_id with metachars (file_ids are base64-ish so unlikely in practice, but the assertion is still cheap).

Skipping these is the brief's explicit ask. Both are 30-line additions if you change your mind later.

## Unresolved questions

- L2 (KV wire format) — confirm whether the migration goal actually needs JS-readability today, or if it's a "nice to have" that lets us defer until a real migration script exists. If deferred, leave the RFC3339 form and note it in the plan's "deferred" section.
- Whether to lock down `Registry` post-Build (M1) by API or by convention. Recommendation: convention + comment for v1; API hardening only if a "hot-reload modules" feature ever shows up on the roadmap.

**Status:** DONE_WITH_CONCERNS
**Summary:** Port is faithful and tests are green; one latent nil-deref in /info (C1) and one wire-format mismatch with JS-written KV (L2) are worth fixing before this lands.
