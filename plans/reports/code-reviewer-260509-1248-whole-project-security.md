# code-reviewer · whole-project security audit

Date: 2026-05-09
Scope: full repo (`cmd/`, `internal/`, Dockerfile, CI, go.mod). Focus on production-readiness for Cloud Run + Firestore + Telegram webhook.
Verdict: **DONE_WITH_CONCERNS** — no Critical issues. Several Medium items worth resolving before public deploy; one High and a few Lows.

Prior reports reviewed (issues already fixed not re-flagged):
- `code-reviewer-260508-2254-phase02-03-bootstrap.md` — C1/C2/C3, H1-H7 all resolved (verified).
- `code-reviewer-260508-2333-phase04-firestore-kv.md` — H1/H2 resolved (verified at firestore_kv.go:75-80 and main.go:135-137).
- `code-reviewer-260509-1206-phase6a-loldle-emoji.md` — `%q` divergence still open (cosmetic).

---

## Critical

None.

---

## High

### H1 — `Deps.Env` still leaks `GOOGLE_CLOUD_PROJECT`, OAuth file paths, Cloud Run env to every module
File: `cmd/server/main.go:188-197`, `internal/modules/module.go:72-76`.

`secretEnvKeys` strips only the three named tokens. Modules still receive every other env var, including: `GOOGLE_CLOUD_PROJECT`, `FIRESTORE_EMULATOR_HOST`, `GOOGLE_APPLICATION_CREDENTIALS` (path), Cloud Run-injected `K_SERVICE`/`K_REVISION`/`K_CONFIGURATION`, and any future `*_API_KEY` (Gemini, etc. — phase 7 will add `GEMINI_API_KEY` and unless that exact string lands in `secretEnvKeys` first, it goes to all modules).

Risk: a future module that reflects/echoes any `Deps.Env` value (debug helper, "system info" command) leaks creds. The previous reviewer's H5 fix was a denylist — denylists don't scale.

Recommendation: invert to allowlist.
- Either pass nothing in `Deps.Env` (modules get truly nothing) and have each module document its own env requirements externally, OR
- Adopt a `MODULE_<NAME>_*` convention; only matching keys flow to that module's Deps.

Phase 07 (Gemini) is the natural moment — once a module needs an external API key, an opt-in allowlist is the only safe pattern. Hardcoding `GEMINI_API_KEY` into `secretEnvKeys` works for one variable but not for the 2nd, 3rd, etc.

---

## Medium

### M1 — `MODULES` env validation rejects bad names *during* module construction; one good factory may have already executed
File: `internal/modules/registry.go:98-153`.

The for-loop validates name → checks dup → looks up factory → **calls factory(moduleDeps)** → validates commands. If a later iteration fails (unknown name, dup name, validation error), modules earlier in the list have already had their factories invoked. For loldle/loldleemoji that's just slice allocation, but a future Factory that opens a file, does a DNS lookup, or holds a long-lived resource (Phase 07 Gemini client) will leak.

Today: low impact (factories are pure). Document with a comment that factories must be allocation-only and never block / open external resources, OR pre-validate all names + dup detection in a first pass before any factory runs.

### M2 — `Visibility` field is decorative; `stickerid` (private), `fortytwo` (private), `loldle_setmax` / `loldle_emoji_setmax` (private) are publicly invocable
Files: `internal/modules/dispatcher.go:15-29`, `internal/modules/util/stickerid.go:24`, `internal/modules/misc/misc.go:90`, `internal/modules/loldle/loldle.go:36`, `internal/modules/loldleemoji/loldleemoji.go:33`.

`Install` registers every command with `bot.RegisterHandler` regardless of `Visibility`. The comment at `module.go:14-15` openly notes "the dispatcher does not enforce visibility today." Result: any user in any chat can:
- `/stickerid` → echo a sticker file_id back. Information disclosure (minor — sticker IDs aren't secret, but they signal which stickers the bot owner privately uploaded).
- `/loldle_setmax 1` → make every group's loldle round trivially solvable. Visible griefing.
- `/loldle_emoji_setmax 1` → same.
- `/fortytwo` → easter egg, no harm.

Risk: low confidentiality, real abuse for `setmax` in groups. The `setmax` commands change *group-shared* state (subjectFor() returns chat.ID for groups), so any group member can set max=1 and break the game for everyone.

Fix options (cheapest first):
1. Document `setmax` is intentionally permissive and remove the `VisibilityPrivate` tag (truth-in-advertising). Acceptable if you accept the griefing.
2. Hard-code an admin allowlist via env: `ADMIN_USER_IDS=12345,67890` and gate `Visibility >= Protected` commands at the dispatcher.
3. For groups, check `getChatMember(chat_id, user_id).status` ∈ {creator, administrator} via the Telegram API before running protected/private commands.

Option 2 is the minimum production-acceptable answer. Option 3 is the right answer; it costs one extra Telegram API call per protected command invocation.

### M3 — `MaxBytesReader`-induced 413 returns 200 instead
File: `internal/telegram/webhook.go:49-54`.

```go
r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBody)
var update models.Update
if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
    http.Error(w, "bad request", http.StatusBadRequest)
    return
}
```

`MaxBytesReader` writes a 413 status to `w` directly when the cap is hit (this is its documented side effect). After that, `Decode` returns an error, the handler tries to call `http.Error(w, "bad request", 400)` — but headers are already sent (413), so it just appends "bad request\n" to the 413 body. Telegram sees a non-2xx and retries. Not a security hole, but operationally noisy. Test `TestWebhookHandler_RejectsOversizedBody` only checks `!= 200`, which passes for 413, so the bug is silent.

Also: a 1 MiB cap is generous (Telegram updates with media references are <100 KiB even with thumbnails). Consider 256 KiB.

Fix: tighten cap and use the documented two-stage check pattern, or accept 413 as the response code (don't shadow it with 400).

### M4 — Cron handler `defaultCronTimeout = 5m` runs serially against single-instance Cloud Run; no parallelism cap
File: `internal/server/timeouts.go:8`, `internal/server/router.go:78-79`.

Cloud Scheduler can fire several crons within a minute. Each request is 5-min capped. Cloud Run free tier is `min=0,max=1` per the plan — so two crons that arrive 30s apart serialize, the second waits up to 5 minutes. Worse: an attacker who steals or guesses the cron secret can fire many `POST /cron/{any-valid-cron-name}` requests; with body=empty they cost nothing on Telegram side but pin the instance for 5 min × N.

Mitigations:
- Tighten `defaultCronTimeout` to the actual wall-clock budget (e.g. 60s) and document that long crons must publish to PubSub and exit fast. 5 minutes is a footgun.
- Cron secret rotation policy: document who rotates and how often. The shared-secret bridge in router.go:23 is "until Phase 09 OIDC" — code comment promises the migration but does not record an SLA.
- Phase 09 OIDC + Cloud Run IAM ingress restriction is the proper fix.

### M5 — Webhook URL secret-token compare leaks length via `MaxBytesReader` ordering
File: `internal/telegram/webhook.go:43-49`.

The constant-time secret compare runs *before* `MaxBytesReader` is installed. An unauthenticated POST with a 100 MiB body still gets streamed up to the auth check… actually no — the auth check only reads the header, body is never read. So no DoS via body upload pre-auth. **But** the body lifetime: an attacker can hold a slow-loris connection sending header bytes; the server's `ReadHeaderTimeout: 10s` (main.go:96) caps that. OK. **Confirmed safe**, just worth a comment that the order matters.

(Demoting from initial Medium to documentation-only after re-reading.)

### M6 — `ReadTimeout: 30s` covers webhook AND cron, but cron handlers may take longer than the *body read* allowance
File: `cmd/server/main.go:97-101`.

`ReadTimeout` includes the body. For cron requests with empty bodies this is fine. If a future cron endpoint accepts a JSON payload and Cloud Scheduler ever delivers it slowly, 30s read could fire. Today: harmless. When phase 9 lands and cron payloads grow, revisit.

### M7 — `loldleemoji` does not HTML-escape the emoji string when rendering
File: `internal/modules/loldleemoji/render.go:20`.

```go
clue := "🎭 " + emojis
```

`emojis` is loaded from the embedded `data/emojis.json` (loldleemoji.go:30) and the comment claims emojis "aren't HTML-escaped in the JS source either." True. But the data file is build-time controlled — a malicious / careless edit that puts `<script>` or `<` into an emoji value injects raw HTML into a Telegram message with `ParseMode: HTML`. Telegram's HTML parser is strict (only specific tags allowed) so the practical impact is limited to sending the bot's own users a message that fails to parse (Telegram returns 400; user sees nothing). Not exploitable for XSS — Telegram clients aren't browsers — but should still escape defensively given the rest of the code does.

Risk: Low (build-time data, Telegram clients sanitize). Fix: `html.EscapeString(emojis)` at render.go:20 or document the data-file invariant.

### M8 — `go.mod` declares `go 1.25.0` but CI builds with `go 1.23`
Files: `go.mod:3`, `.github/workflows/ci.yml:17`.

```
go 1.25.0          # go.mod
go: ['1.23']       # CI matrix
```

`go 1.25` in go.mod is the *minimum required toolchain*. Building with 1.23 should fail at `go build` (the go directive is enforced since 1.21+ for the language spec, since 1.22+ for stdlib). Either the CI is breaking and we don't notice, or `go 1.25.0` is wrong (current is 1.23 era; 1.25 is a future release). The Dockerfile uses `golang:1.23-alpine` (Dockerfile:1), so production also conflicts.

Action: align all three (go.mod / CI / Dockerfile) on the actually-installed toolchain. `1.25.0` looks like a typo for `1.23.0`.

### M9 — No top-level panic recovery around Telegram dispatch
File: `internal/telegram/webhook.go:58`.

`bot.ProcessUpdate(ctx, &update)` runs synchronously (`WithNotAsyncHandlers`). The library does **not** recover panics in this path (verified against `process_update.go` v1.20.0). A panic inside a handler (say, a Phase 7 module that hits nil deref on a malformed Telegram media struct) propagates up.

`net/http`'s per-request recovery catches it, prints the stack trace via the server's ErrorLog → Cloud Run captures the stack to stderr. Two consequences:
- Logs leak Go file paths and line numbers (low risk; logs are private).
- The HTTP response is closed mid-write; Telegram sees non-2xx and retries the same panic-inducing update **forever** (Telegram retries failed webhooks for ~24h).

Wrap `b.ProcessUpdate` in:
```go
func() {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("webhook handler panic: %v", r)
        }
    }()
    b.ProcessUpdate(ctx, &update)
}()
```
Then return 200. Telegram won't retry, the bug is logged, the bot stays up for the next update.

---

## Low

### L1 — `info.go` echoes sender ID + chat ID; visible to anyone in the chat
File: `internal/modules/util/info.go:38-42`.

`/info` is `VisibilityPublic` and echoes `chat_id`, `thread_id`, `sender_id`. None are secrets (Telegram exposes them to clients via the API anyway), but in a public group `/info` reveals the numeric Telegram user ID of whoever runs it. Some users assume their UID is private. Document or mark as `VisibilityProtected` once visibility enforcement lands (M2).

### L2 — `PORT` env var is not validated; non-numeric value crashes net.Listen on `:` addr
File: `cmd/server/main.go:172-175`.

`PORT=abc` → `srv.Addr = ":abc"` → `ListenAndServe` returns `address abc: unknown port`. Fail-fast happens but error is opaque. Cloud Run sets `PORT` correctly, so production-safe; local dev may footgun. One-liner: `if _, err := strconv.Atoi(port); err != nil { log.Fatalf(...) }`.

### L3 — `keylock.Map` grows unbounded; documented but no eviction
File: `internal/keylock/keylock.go:8-12`.

Comment acknowledges 32 MB at 1M distinct keys, says "Eviction is a Phase 11 concern." Cloud Run instances are ephemeral so this is fine in practice. Just confirm: **per-instance** memory ceiling on free tier is ~512 MiB. 32 MB of locks is 6% of that — comfortable.

### L4 — `loldle` and `loldleemoji` `findChampion` return ambiguous-prefix → nil; user input "k" silently matches nothing
File: `internal/modules/loldle/lookup.go:13-36`.

Behavior is JS-faithful and intentional. Just noting: a user typing `/loldle k` gets `Champion not found: "k"` even though many champions start with K. The JS source has the same UX, so parity is correct. No action.

### L5 — `pickRandom` uses `math/rand` global; predictable across instances
Files: `internal/modules/loldle/handlers.go:65`, `internal/modules/loldleemoji/handlers.go:62`, `internal/modules/wordle/daily.go:58`.

Per-instance `math/rand` is mutex-protected (good for concurrency) but seeded with fixed `1` since Go 1.20 (no, actually Go 1.20 changed this — `math/rand` global is now seeded from a global random value at startup). Two instances start with different seeds. If predictability ever matters (it doesn't for game variety), use `math/rand/v2` or `crypto/rand`. Today: fine.

### L6 — Stickers' `file_id` strings are bot-scoped; rotation across bot tokens is undocumented
File: `internal/modules/loldle/stickers.go:9-25`.

Telegram `file_id`s are scoped to the bot that uploaded them. If the dev/prod bot tokens differ (and they should — see README:31), the `winStickers`/`loseStickers` are dev-bot-scoped. In prod, `b.SendSticker` will fail with `STICKER_ID_INVALID`; `trySendSticker` swallows the error (handlers.go:120-129), so users see no sticker but no error either. Operations gap: prod will silently miss stickers. Document the procedure to re-capture stickers via `/stickerid` against the prod bot, or move sticker IDs to env vars.

### L7 — Dockerfile has no signed/verified base image digest
File: `Dockerfile:1,13`.

`golang:1.23-alpine` and `gcr.io/distroless/static:nonroot` are pulled by tag, not digest. Supply-chain risk: an attacker compromising the registry (or a typo squatting registry MITM) substitutes a malicious image. Fix: pin both to digests (`golang:1.23-alpine@sha256:...`). CI/CD update: dependabot has Go module support but image-digest pinning needs a separate policy.

### L8 — `go test -race` is run but no coverage threshold gate, no `golangci-lint`, no `gosec`
File: `.github/workflows/ci.yml`.

Vet + tests + build only. A `gosec` step would catch a future `subtle.ConstantTimeCompare` regression, an `os/exec` slip, etc. Recommended additions:
- `golangci-lint run --timeout=5m` (gofmt, errcheck, staticcheck, gosec all in one).
- Optional `govulncheck ./...` for known CVE detection in deps.

### L9 — `Build` returns the partially-populated registry on error from `factory()` panic
File: `internal/modules/registry.go:118`.

If a factory panics (loldle's `loadChampions()` does on bad data), the panic propagates up out of `Build`. `main.go:74` then `log.Fatalf`'s. OK. But there's no `defer recover` in `Build` to convert the panic to an error — the `log.Fatalf` handler uses `%v` on an error, which would lose the panic type. Today: harmless, panics print clean stacks via Go's default. Document or wrap.

---

## Inputs / Boundaries Verified

- `r.URL.Path` for /cron/* validated `^[a-z0-9_]{1,32}$` (router.go:21,72-75) — no log injection possible.
- Webhook secret: constant-time compare (webhook.go:44).
- Cron secret: constant-time compare (router.go:66).
- Webhook body: bounded via `MaxBytesReader` (webhook.go:49) — see M3 caveat.
- JSON decode: standard `encoding/json` with no `UseNumber` or custom unmarshalers — JSON-bomb risk is bounded by body cap.
- Telegram update parsing: delegated to `models.Update`. The library does not call `ioutil.ReadAll` on inner media; we never read media payloads.
- Outbound HTTP: no `http.Get/NewRequest` anywhere. All Telegram traffic is via `b.SendMessage` etc. — URL is the official Telegram API endpoint. No SSRF surface.
- No shell exec, no `text/template` or `html/template` used in user-facing paths (only `html.EscapeString` for HTML-mode replies, which is the correct primitive).
- KV keys validated against Firestore constraints (firestore_kv.go:52-69). Module names validated `^[a-z0-9_-]{1,32}$` rejecting `:` (registry.go:19).
- `keylock.Map` per-subject serialisation prevents Get→mutate→Put races; same-key tests confirm (keylock_test.go).
- Container: `nonroot:nonroot` user, distroless base, no shell, `CGO_ENABLED=0` static binary. Good.

---

## Cloud Run / Firestore IAM (External — Verify Out-of-Band)

The bot relies on:
1. Cloud Run service account having `roles/datastore.user` (Firestore RW).
2. Cloud Run *not* having `allUsers` Invoker role — otherwise `/cron/*` is publicly invocable AND `/webhook` becomes redundantly auth'd by app-layer secret only.
3. Cloud Scheduler invoking `/cron/*` via OIDC (Phase 09) or with the shared header today.

These cannot be verified from code. Recommend an `infra/` directory (Terraform / gcloud commands) committed to repo so IAM intent is reviewable. Today: unverifiable — listed as unresolved Q.

---

## Positive observations

- Constant-time compares on both webhook and cron secrets.
- Webhook secret + cron secret fail-fast at startup (main.go:53-58, 82-84).
- `bot.WithSkipGetMe()` + `WithNotAsyncHandlers()` correctly chosen for webhook mode.
- Per-update 10s timeout (webhook.go:26) keeps Cloud Run instance from being held by a single hung handler.
- Per-cron 5-min timeout (timeouts.go:8) — see M4 for parallelism caveat but the time bound is correct.
- Distroless + nonroot + static binary + 6 MB image. Well under the 15 MiB target.
- KV layer separates trust boundary cleanly: every key path validated, prefix wrapper `:` delimiter is non-bypassable due to module-name regex.
- HTML rendering escapes user-controlled fields (`html.EscapeString` on champion names, sticker emoji, set name, etc.) — see M7 for the lone exception.
- Sticker errors swallowed only after a comment-justified design choice (handlers.go:118-122).
- Tests for negative paths: oversized body, wrong secret, same-prefix secret (timing edge), bad JSON, invalid cron name, nested cron path, log-injection attempt — all present.
- Secret env stripping (`secretEnvKeys`) addresses the most obvious leak surface.
- `MODULES` env validated with a regex that explicitly rejects `:` (the storage delimiter).

---

## Recommended Action Order

| # | Severity | Action |
|---|----------|--------|
| 1 | M8 | Align go.mod / CI / Dockerfile Go versions. `go 1.25.0` is a typo or premature. |
| 2 | M9 | Wrap `b.ProcessUpdate` in `defer recover` so a buggy module doesn't trigger Telegram retry storms. |
| 3 | M2 | Decide visibility-enforcement strategy (admin allowlist env or per-chat-admin check) — the `setmax` commands let any group member break the game. |
| 4 | H1 | Replace env denylist with allowlist before Phase 07 (Gemini key) ships. |
| 5 | M4 | Tighten `defaultCronTimeout` from 5m → 60s; document long-cron pattern. |
| 6 | M3 | Either accept 413 in oversized-body path (drop the `http.Error 400` shadow) or use a pre-cap content-length check. |
| 7 | M7 | `html.EscapeString` on `emojis` in loldleemoji/render.go:20 (one-liner). |
| 8 | L7 | Pin Docker base images by digest. |
| 9 | L8 | Add `golangci-lint` + `govulncheck` to CI. |
| 10 | L6 | Document sticker `file_id` rotation when switching bot tokens. |

H1, M2, M4, M9 are the only items I'd require before opening the bot to a public Telegram username. The rest are hardening.

---

## Unresolved Questions

1. **Cloud Run ingress policy**: is `/cron/*` reachable from the public internet, or is the service ingress-restricted to `internal-and-cloud-load-balancing`? If the latter, the cron shared-secret is defense-in-depth; if the former, M4 is closer to a High.
2. **Phase 09 OIDC ETA**: the shared-secret bridge in router.go:23 has no documented retire-by date. If Phase 09 slips past the public launch, what's the fallback (rate-limit per cron name, additional IP allowlist)?
3. **Bot token isolation between dev and prod**: README mentions a manually-created dev bot. Is there a documented process for prod bot token issuance + rotation, or does the bot token live in Secret Manager indefinitely? (Secret Manager rotation hooks would be Phase 09+ work.)
4. **`VisibilityPrivate` semantics**: should `setmax` be private-as-in-bot-owner-only, private-as-in-chat-admin-only, or just unenforced? The current implementation accepts any caller; the field's existence implies an intent that has not been implemented. Clarify and either implement or remove.
5. **Cloud Run min-instances**: free tier is min=0. A cold start currently blocks ~1.5s on `firestore.NewClient`. Telegram's webhook timeout is 10s; first update after idle hits the cold start window. Is that latency budget acceptable, or should min=1 (paid)?

---

**Status:** DONE_WITH_CONCERNS
**Summary:** No critical defects; production posture is solid given prior reviews already landed the high-risk fixes. The remaining items are: visibility enforcement (M2), env allowlist before Phase 07 (H1), panic recovery around dispatcher (M9), and a Go-version mismatch (M8). Ship-ready for a private bot; resolve M2/H1/M9 before public username.
