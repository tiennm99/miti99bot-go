# Phase 01 — Wire `telegram-setup` Into `deploy.yml`

**Status:** implemented (pending live verification on next push to main)
**Priority:** P1 (next deploy needs it for full automation)
**Estimate:** ~30 min implementation + 1 deploy cycle to verify

## Context links

- Parent plan: `../plan.md`
- Existing GH workflow: `.github/workflows/deploy.yml`
- Reference Makefile targets: `Makefile` lines 101-148 (`telegram-setup`, `telegram-webhook`, `telegram-commands`)
- Commands payload: `aws/telegram-commands.json`
- Deploy role IAM: `aws/README.md` § 4 (already has `AmazonSSMFullAccess`)
- Cutover docs: `docs/deploy-aws.md` line 64, `docs/deploy-aws-free-tier-guide.md` line 270 (current manual steps)

## Overview

Add two steps to `deploy.yml` after the existing **Smoke test** step:

1. **Register Telegram webhook** — read `FunctionUrl` from CFN, read token + secret from SSM, `POST` `setWebhook`.
2. **Register Telegram command menu** — read token from SSM, `POST` `setMyCommands` with `aws/telegram-commands.json`.

Mirror the existing inline-CLI pattern used by the Smoke step (no `make` indirection — Makefile uses `--profile admin` which is wrong for CI).

## Key insights

- Deploy role already has `AmazonSSMFullAccess` and `AWSCloudFormationFullAccess` → no IAM change.
- `setWebhook` / `setMyCommands` are idempotent → safe to run every push.
- CFN `Outputs.FunctionUrl` (template.yaml:208-210) → reused from smoke step.
- SSM param paths fixed at `/miti99bot/prod/{telegram-bot-token,telegram-webhook-secret}` (per `aws/README.md` § 3, `samconfig.toml`).
- Bot token must be **masked** before any shell echo (it is a credential in URL path).
- `aws/telegram-commands.json` is committed → `--data-binary "@aws/telegram-commands.json"` works directly.

## Requirements

### Functional
1. After a green SAM deploy + smoke test, the workflow registers the webhook + commands.
2. Webhook URL format: `${FunctionUrl%/}/webhook` (trim trailing slash; Function URLs sometimes include it).
3. `allowed_updates` must equal `["message","callback_query"]` (matches `Makefile:120`).
4. `secret_token` from SSM is sent in the `setWebhook` payload — bot validates this on every incoming update.
5. Job fails (non-zero exit) if either Telegram call returns non-2xx **or** `"ok": false`.

### Non-functional
- Token never appears in plaintext logs (`::add-mask::TOKEN` before use).
- No new secrets in GitHub repo settings — everything still flows through SSM.
- No new third-party action — only `aws` CLI + `curl` + `jq` (all preinstalled on `ubuntu-latest`).

## Architecture

```
deploy.yml job: deploy (existing)
  ├─ checkout              (existing)
  ├─ setup-go              (existing)
  ├─ setup-sam             (existing)
  ├─ configure-aws-creds   (existing)
  ├─ build-lambda          (existing)
  ├─ sam-deploy            (existing)
  ├─ smoke-test            (existing) — reads FunctionUrl from CFN
  ├─ register-webhook      (NEW)      — reads FunctionUrl + SSM token/secret, POST setWebhook
  └─ register-commands     (NEW)      — reads SSM token, POST setMyCommands w/ aws/telegram-commands.json
```

The Function URL is fetched twice (smoke + register-webhook). Acceptable: CFN describe-stacks is fast and the steps stay independent / debuggable. Optimization (cache URL in a step output) is out of scope.

## Related code files

**Modify**
- `.github/workflows/deploy.yml` — append two steps after **Smoke test**

**Read (no change)**
- `Makefile` lines 101-148 — reference implementation
- `aws/telegram-commands.json` — payload body

**Possibly update**
- `docs/deploy-aws.md` line 64 — current "manual setWebhook" instructions become "automatic on push; manual command kept for emergencies"
- `docs/deploy-aws-free-tier-guide.md` line 270 — same note

## Implementation steps

1. **Open** `.github/workflows/deploy.yml`. Locate the `- name: Smoke test (Function URL responds)` step (last step today).
2. **Append step `Register Telegram webhook`** after smoke-test:
   - Reuse `STACK_NAME` env (already at job level).
   - `URL=$(aws cloudformation describe-stacks ... FunctionUrl ...)` — copy pattern from smoke step.
   - `TOKEN=$(aws ssm get-parameter --name /miti99bot/prod/telegram-bot-token --with-decryption --query Parameter.Value --output text)`
   - `echo "::add-mask::$TOKEN"` immediately after read.
   - `SECRET=$(aws ssm get-parameter --name /miti99bot/prod/telegram-webhook-secret --with-decryption --query Parameter.Value --output text)`
   - `echo "::add-mask::$SECRET"` immediately after read.
   - `WEBHOOK_URL="${URL%/}/webhook"`
   - `RESP=$(curl -fsS -X POST "https://api.telegram.org/bot${TOKEN}/setWebhook" -d "url=${WEBHOOK_URL}" -d "secret_token=${SECRET}" -d 'allowed_updates=["message","callback_query"]')`
   - `echo "$RESP" | jq -e '.ok == true' >/dev/null || { echo "setWebhook failed: $RESP"; exit 1; }`
   - `echo "$RESP" | jq '{ok, result}'`
3. **Append step `Register Telegram command menu`**:
   - Read `TOKEN` from SSM (same path) and re-mask. (Step env doesn't persist across steps; re-read is fine — single SSM call is cheap.)
   - `RESP=$(curl -fsS -X POST "https://api.telegram.org/bot${TOKEN}/setMyCommands" -H 'Content-Type: application/json' --data-binary "@aws/telegram-commands.json")`
   - Same `jq -e '.ok == true'` validation + pretty-print.
4. **Lint locally** with `actionlint` if available (or just YAML parse): `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/deploy.yml'))"`.
5. **Update docs**:
   - `docs/deploy-aws.md` line 64: add note "as of <date>, push-to-main auto-runs `setWebhook` + `setMyCommands`; manual command below kept for break-glass".
   - Same in `docs/deploy-aws-free-tier-guide.md` line 270.
6. **Commit** with conventional message: `ci(deploy): auto-register Telegram webhook + commands after SAM deploy`.

## Todo list

- [x] Read current `deploy.yml` end (smoke step) to confirm insertion point
- [x] Append `Register Telegram webhook` step (with token mask + `jq -e` validation)
- [x] Append `Register Telegram command menu` step (with token mask + `jq -e` validation)
- [x] Validate YAML parse locally (`yaml.safe_load` → OK)
- [x] Update `docs/deploy-aws.md` + `docs/deploy-aws-free-tier-guide.md` notes
- [ ] Commit + push, observe first run in GH Actions UI
- [ ] Verify `curl https://api.telegram.org/bot$TOKEN/getWebhookInfo` shows the Function URL post-deploy

## Success criteria

| Check | How to verify |
|-------|---------------|
| Step `Register Telegram webhook` shows green | GH Actions run UI |
| Step `Register Telegram command menu` shows green | GH Actions run UI |
| `setWebhook` response logged as `{ok:true, result:true, description:"Webhook was set"}` | step log |
| Token / secret not visible in logs | search step output for first 4 chars of token → must show `***` |
| `make telegram-webhook-info` from local shows `url == <FunctionUrl>/webhook` | local `make` after pipeline finishes |
| `/help` works in Telegram after deploy | live bot smoke test |

## Risk assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|-----------|
| Telegram API blip → CI fails despite healthy deploy | Low | Low (Lambda still serving) | `-fsS` + manual workflow re-run; document break-glass `make telegram-setup` |
| SSM param missing on first-ever deploy | Low | High (deploy red) | Precondition documented in `aws/README.md` § 3 — params must exist before first push (already true today) |
| Bot token printed in `set -x`-style verbose log | Medium | High (token leak) | `::add-mask::` immediately after SSM read; never use `set -x` in these steps |
| `aws/telegram-commands.json` invalid JSON | Low | Low (single-step fail) | `--data-binary @file` + Telegram validates; `jq -e .ok` catches |

## Security considerations

- **Mask tokens / secrets**: `::add-mask::` after every SSM read. GH Actions then redacts that string from all subsequent log lines (including child commands).
- **Path-credential leak**: `curl` URL contains `${TOKEN}` — masking covers it, but additionally avoid `set -x`, `-v`, or `echo "$URL"` in any debug temp.
- **No new IAM**: deploy role keeps existing scope; no expansion of privileges.
- **Webhook secret**: validates inbound requests at `internal/telegram/webhook.go:19-20` (`X-Telegram-Bot-Api-Secret-Token` header). Auto-registration enforces same secret across token rotations.

## Next steps

After this phase merges and one deploy cycle confirms green:
- (Optional follow-up, separate plan) Replace `AmazonSSMFullAccess` with a scoped policy granting only `ssm:GetParameter` on `/miti99bot/*` — pairs with the broader IAM tightening already deferred in `aws/README.md` § 4.
- (Optional) Cache `FunctionUrl` between smoke and register-webhook via `$GITHUB_OUTPUT` — micro-optimization only.

## Unresolved questions

- None. All decisions locked: always-on registration, inline CLI (no `make` from CI), `jq -e` failure semantics, both docs files updated.
