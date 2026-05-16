# Auto-Register Telegram Webhook + Commands After Deploy

**Date:** 2026-05-16
**Slug:** `260516-1035-auto-register-after-deploy`
**Status:** Implemented (pending commit + live verification on next push to main)
**Type:** CI/CD enhancement (single phase)
**Mode:** fast (no research needed — referenced code paths already exist)

## Goal

Every push to `main` already runs `.github/workflows/deploy.yml` → SAM deploy → smoke-test the Function URL. After a successful deploy, register the Telegram webhook + command menu **automatically** (currently done manually via `make telegram-setup`).

## Why

- Eliminates manual `make telegram-setup` step after first deploy / handler-path change / webhook secret rotation.
- Self-healing: if Telegram's `webhook_url` ever drifts from the Function URL (e.g. secret rotated but `setWebhook` forgotten), the next deploy fixes it.
- `setWebhook` and `setMyCommands` are idempotent — running on every deploy is safe and cheap.

## Non-goals

- Do **not** introduce a new Go binary / Lambda hook / CloudFormation custom resource.
- Do **not** rewrite the existing Makefile targets — keep `make telegram-setup` working for local/manual use.
- Do **not** touch `aws/telegram-commands.json` content or module behavior.

## Phases

| # | Phase | File | Status |
|---|-------|------|--------|
| 01 | Wire telegram-setup into deploy.yml | `phase-01-wire-telegram-setup-into-deploy.md` | implemented |

## Key files

- `.github/workflows/deploy.yml` — add post-smoke-test registration step
- `Makefile` (lines 101-148) — reference impl (do not modify unless CI parity requires)
- `aws/telegram-commands.json` — read by `setMyCommands` step
- `aws/README.md` § 4 — deploy role already has `AmazonSSMFullAccess`, no IAM change needed

## Dependencies

- Deploy role `github-deploy-miti99bot` already has `AmazonSSMFullAccess` + `AWSCloudFormationFullAccess` (verified in `aws/README.md` § 4).
- SSM params `/miti99bot/prod/telegram-bot-token` and `/miti99bot/prod/telegram-webhook-secret` already populated (precondition of first deploy).

## Risks

- **Telegram API outage on deploy** → CI fails even though Lambda is healthy. Mitigation: use `curl -fsS` so non-2xx aborts the job; failure surface is loud, recoverable by re-running workflow.
- **Token exposed in logs** → use `::add-mask::` for TOKEN before any echo / curl line; do not pass via `-d` URL arg (token is in path, but mask anyway).
- **Webhook secret rotation race** → SSM read happens after deploy, so newest secret wins. No race in practice.

## Success criteria

After merging this change, the next push to `main`:
1. SAM deploy succeeds.
2. Smoke-test passes.
3. New step: `curl /setWebhook` returns `{"ok":true,...}`.
4. New step: `curl /setMyCommands` returns `{"ok":true,...}`.
5. `getWebhookInfo` shows `url == <FunctionUrl>/webhook`.

## Unresolved questions

- None (locked decisions): always-on registration, inline CLI calls (not `make telegram-setup`), fail-loud on API errors.
