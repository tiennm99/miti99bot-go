# Code Review — Auto-Register Telegram Webhook + Commands After Deploy

**Date:** 2026-05-16
**Branch:** main (uncommitted)
**Scope:** `.github/workflows/deploy.yml` (+46 lines), `docs/deploy-aws.md` (+2), `docs/deploy-aws-free-tier-guide.md` (+2)
**Plan:** `plans/260516-1035-auto-register-after-deploy/`

## Status

**DONE — no must-fix issues.** Implementation faithfully mirrors `Makefile:101-148` reference, hardens it with `set -euo pipefail` + `jq -e` validation, and masks credentials before they can reach any log line. All 8 acceptance criteria from the plan are met by the diff.

## Critical findings

None. No blockers, no security regressions, no contract breaks.

## Verified safe

1. **Mask timing — no leak window.**
   - `.github/workflows/deploy.yml:76-79` — `TOKEN=$(...)` then `echo "::add-mask::$TOKEN"` on the very next line. AWS CLI with `--output text --query Parameter.Value` writes nothing else to stdout/stderr, so the value never escapes between capture and mask.
   - Same pattern at lines 80-83 (SECRET) and 100-103 (re-read TOKEN in commands step).

2. **No URL-leak via curl error.** Empirically verified `curl 8.5.0 -fsS` on 4xx prints `curl: (22) The requested URL returned error: <code>` — URL is **not** in the message. Even if it were, `::add-mask::` was issued at line 79 before line 86's `curl`, so GH Actions redacts the token from all subsequent log output (stdout + stderr) in the same job. No `set -x`, no `-v`, no `curl -w` interpolating the URL.

3. **Failure semantics — fail-loud, no silent swallows.**
   - `set -euo pipefail` + `RESP=$(curl -fsS ...)` — empirically confirmed: curl exit-22 propagates through command substitution, `set -e` kills the shell before next line. (Note: `errexit` *does* propagate from `$(...)` since bash 4.0.)
   - `jq -e '.ok == true' >/dev/null || { echo ...; exit 1; }` — catches Telegram returning HTTP 200 with `{"ok":false}` body. The `|| { ... }` keeps `set -e` honest (no pipefail-with-tee anti-patterns).
   - Both steps lack `continue-on-error: true` — failures bubble up to the job.

4. **Webhook contract matches code.**
   - Path: workflow sends to `${URL%/}/webhook`; router accepts at `internal/server/router.go:51` (`mux.Handle("/webhook", ...)`).
   - Secret: workflow forwards `secret_token=${SECRET}`; Telegram echoes back via `X-Telegram-Bot-Api-Secret-Token` header; `internal/telegram/webhook.go:22,48-52` validates with `subtle.ConstantTimeCompare`. Same SSM param (`/miti99bot/prod/telegram-webhook-secret`) feeds both setWebhook and Lambda env (via `template.yaml` `:1` resolver), so values stay in sync.
   - `allowed_updates=["message","callback_query"]` — matches `Makefile:120` reference exactly.

5. **setMyCommands payload format.** `aws/telegram-commands.json` has top-level `{"commands": [...]}` which matches Telegram API spec for `--data-binary @file` with `Content-Type: application/json`. Verified via Telegram Bot API docs.

6. **IAM permissions present.** `aws/README.md:74` lists `AmazonSSMFullAccess` and `:69` lists `AWSCloudFormationFullAccess` attached to `github-deploy-miti99bot`. No new grants needed. Plan claim verified.

7. **Concurrency safe.** `.github/workflows/deploy.yml:12-14` — `concurrency.group: deploy-prod`, `cancel-in-progress: false` → serial queueing, no parallel setWebhook race. SSM secret values are versioned and read-after-deploy, so the newest value wins by ordering, not by collision.

8. **YAML structural validity.** 9 total steps (was 7, +2 new). Indentation consistent with existing steps; `env:`, `run:` blocks well-formed (read at `.github/workflows/deploy.yml:67-93,95-111`). No actionlint available locally to formally validate, but eye-parse is clean.

9. **Docs labeling is unambiguous.**
   - `docs/deploy-aws.md:56` — "auto-runs `setWebhook` + `setMyCommands` after every push to `main`. The snippet below is the break-glass equivalent..."
   - `docs/deploy-aws-free-tier-guide.md:261` — "For first-time setup only. After Step 6 wires the GitHub workflow, every push to `main` auto-runs `setWebhook` + `setMyCommands`; this manual block is the break-glass path."
   - Both clearly tag the manual blocks as break-glass / first-time-only. Low risk of a reader running them every deploy.

10. **Idempotency.** `setWebhook` and `setMyCommands` are documented as idempotent by Telegram. Running on every push is safe and self-healing (per plan's stated goal).

11. **No YAGNI/scope creep.** Diff is exactly the two new steps + the two one-line doc notes. No defensive plumbing, no caching layer, no follow-up IAM tightening (correctly deferred to a separate plan).

## Recommendations (defer — non-blocking)

| # | Location | Note |
|---|----------|------|
| R1 | `.github/workflows/deploy.yml:91-92,109-110` | `echo "setWebhook failed: $RESP"` prints the full Telegram response on failure. Telegram's `description` field is generic ("Bad Request: ..."), so token-in-body leak is implausible, but masking is already in place as belt-and-suspenders. Keep as-is — useful for diagnosing rare API rejections. |
| R2 | `docs/deploy-aws.md:67`, `docs/deploy-aws-free-tier-guide.md:273` | Break-glass manual blocks still use `${URL}webhook` (no `%/` trim). This relies on CFN's `FunctionUrl` always ending with `/`, which is the documented Lambda behavior. Not a regression (pre-existing). Tighten next time the file is touched. |
| R3 | `.github/workflows/deploy.yml:76-78,100-102` | Two SSM calls in two adjacent steps to read the same token. Plan already acknowledges this as acceptable (single SSM call is cheap, keeps steps independently re-runnable). Single-step consolidation or `$GITHUB_OUTPUT` is the documented follow-up. |
| R4 | `.github/workflows/deploy.yml:67-93` | No `if: success()` guard on the new steps. GH Actions defaults to running a step only when prior steps succeed, so this is redundant — but adding it explicitly would make the intent obvious to a reader. Optional. |

## Unresolved questions

None. All adversarial vectors closed by verification against codebase + empirical curl/bash tests.

---

## Citations

- Workflow diff: `.github/workflows/deploy.yml:67-111`
- Webhook handler validation: `internal/telegram/webhook.go:22,48-52`
- Router mount: `internal/server/router.go:51`
- IAM policy attachments: `aws/README.md:69,74`
- CFN output declaration: `template.yaml:207-210`
- Reference Makefile targets: `Makefile:101-148`
- Commands JSON: `aws/telegram-commands.json:1-96`
- Doc break-glass labels: `docs/deploy-aws.md:56`, `docs/deploy-aws-free-tier-guide.md:261`
- Concurrency control: `.github/workflows/deploy.yml:12-14`

**Status:** DONE
**Summary:** Implementation is correct, secure, and matches the plan. Token masking is in place before any line that could log the value; `curl -fsS` + `jq -e` + `set -euo pipefail` produce loud failures with no silent swallows; `/webhook` path and secret-token contract match `internal/server/router.go:51` and `internal/telegram/webhook.go:48-52`. Docs clearly label manual blocks as break-glass. No must-fix issues; safe to commit.
