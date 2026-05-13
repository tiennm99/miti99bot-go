# Deploy AWS Free-Tier Guide — Shell Safety & Correctness Review

Doc reviewed: `docs/deploy-aws-free-tier-guide.md` (310 lines).
Cross-refs: `template.yaml`, `samconfig.toml`, `Makefile`, `aws/iam-github-oidc-trust.json`, `.github/workflows/deploy.yml`, `aws/README.md`.

## Summary

Doc is mostly sound but has several copy-paste hazards a new user will hit. Biggest issues:

1. **Rollback step is wrong.** `update-stack --use-previous-template` re-applies the CURRENT template, not the prior one. Won't roll back a bad-but-successful deploy.
2. **No idempotency on bootstrap.** `create-open-id-connect-provider`, `create-role`, and `attach-role-policy` loop have no "already exists" guard. A half-failed Step 3 leaves the user re-running and seeing confusing `EntityAlreadyExists` errors.
3. **The `cd()` override is fragile.** Wraps `builtin cd` globally and silently activates ANY repo's `.venv`. Will surprise users who work in multiple projects and breaks shell tools that rely on `cd`'s normal behavior.
4. **Privilege blast radius.** Bootstrap `admin` is full root-equivalent (AdministratorAccess) and CI role gets 10 `*FullAccess` policies — including `IAMFullAccess` and `AmazonS3FullAccess`. Doc says "tighten later" but offers no starter narrowing.
5. **Webhook URL building is brittle.** `-d "url=${URL}webhook"` assumes a trailing slash on `$URL`. Function URLs end with `/`, so it works — but if a user pastes one without the slash it silently posts to a wrong path.
6. **`<placeholder>` values in `aws ssm put-parameter` will be stored literally** if the user blindly copy-pastes.

Free-tier alignment looks correct vs `template.yaml` (PITR off, retention 7d, ARM64, PAY_PER_REQUEST, Function URL not API GW). X-Ray "Active" tracing is enabled in Globals; doc correctly notes the 100k traces/mo free quota but doesn't tell the user how to monitor it.

---

## Critical (must-fix before sharing)

### C1. Rollback command does not roll back

Lines 286–292:

```sh
aws cloudformation update-stack \
  --stack-name miti99bot-aws-port \
  --use-previous-template --capabilities CAPABILITY_IAM
```

**Failure mode:** `--use-previous-template` means "use the template currently associated with the stack". After a successful but bad deploy, the stack's current template IS the bad one. This command does nothing useful (no-op changeset) — it does NOT revert to the prior version. There is no CFN flag to roll back to a prior template; the user must redeploy from a known-good source.

**Suggested rewrite:**
```sh
# CloudFormation does not store prior templates. To revert a bad deploy,
# redeploy from a known-good commit:
git checkout <good-sha>
AWS_PROFILE=admin make sam-deploy
# (Auto-rollback only triggers on CREATE/UPDATE failures, not on a deploy
#  that succeeded but ships a bug.)
```

Drop the `update-stack --use-previous-template` block entirely, or repurpose it as a "continue-rollback" recipe for a stuck `UPDATE_ROLLBACK_FAILED` state (different command: `aws cloudformation continue-update-rollback`).

---

### C2. Step 3 OIDC provider + role creation is not re-runnable

Lines 149–177. If `create-open-id-connect-provider` succeeds but `create-role` fails (typo in JSON, wrong path), the user fixes the JSON and re-runs the block. Both `create-open-id-connect-provider` and `create-role` will then fail with `EntityAlreadyExists` and the user has to manually delete or skip steps.

**Failure mode:** New users will think bootstrap is broken.

**Suggested rewrite (guard each):**
```sh
# OIDC provider (idempotent)
ACCOUNT_ID=$(aws sts get-caller-identity --profile admin --query Account --output text)
OIDC_ARN="arn:aws:iam::${ACCOUNT_ID}:oidc-provider/token.actions.githubusercontent.com"
if ! aws iam get-open-id-connect-provider --profile admin \
       --open-id-connect-provider-arn "$OIDC_ARN" >/dev/null 2>&1; then
  aws iam create-open-id-connect-provider --profile admin \
    --url https://token.actions.githubusercontent.com \
    --client-id-list sts.amazonaws.com \
    --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1
fi

# Role (idempotent)
if ! aws iam get-role --profile admin --role-name github-deploy-miti99bot >/dev/null 2>&1; then
  aws iam create-role --profile admin \
    --role-name github-deploy-miti99bot \
    --assume-role-policy-document file://aws/iam-github-oidc-trust.json
else
  aws iam update-assume-role-policy --profile admin \
    --role-name github-deploy-miti99bot \
    --policy-document file://aws/iam-github-oidc-trust.json
fi
```

The `attach-role-policy` loop is already idempotent in AWS (attaching twice succeeds), so it's fine — but worth noting in a comment.

---

### C3. Working-directory assumption: relative file paths

Line 161: `--assume-role-policy-document file://aws/iam-github-oidc-trust.json`

**Failure mode:** AWS CLI resolves `file://aws/...` relative to the **current shell working directory**, not to the repo. Step 4 (line 186) tells the user to `cd /path/to/miti99bot`, but Step 3 has no equivalent `cd`. If the user followed Step 1 from `~`, this fails with `Unable to parse parameter ... no such file`.

Same risk applies to anyone who opens a new terminal between Step 2 and Step 3.

**Suggested rewrite:** add the `cd` line at the top of Step 3, OR use an absolute path:
```sh
# At top of Step 3:
cd /path/to/miti99bot   # all file:// paths below are relative to repo root
```

---

### C4. The `cd() { builtin cd …; }` override is dangerous

Lines 56–62:

```sh
miti99bot_venv() {
  [ -f "$PWD/.venv/bin/activate" ] && source "$PWD/.venv/bin/activate"
}
cd() { builtin cd "$@" && miti99bot_venv; }
```

**Failure modes:**
1. **Globally scoped to ALL repos** — auto-activates any `.venv/` the user happens to `cd` into, including unrelated Python projects. Quietly clobbers PYTHONPATH/PATH expectations.
2. **No deactivation** when leaving the repo. The venv stays active forever in that shell.
3. **Inherits no `cd` options** — `cd -P`, `cd -L`, `cd -e`, etc. still work via `"$@"`, but tools that check `type cd` or `command -v cd` (some shell scripts and `pyenv`-style hooks) see a function and may misbehave.
4. **Breaks subshells / scripts that `source` a tool's init** if they `unset -f cd` or override it again.
5. **Persistent in `~/.bashrc`** — survives long after the user finishes onboarding.

**Suggested rewrite:** drop the `cd` override entirely. Recommend `direnv` (already mentioned but only as a comment) with a `.envrc` file checked into the repo, or just tell users to `source .venv/bin/activate` once per shell.

```sh
# Option A (recommended): install direnv, add repo .envrc with `source .venv/bin/activate`
# Option B: just activate manually
source .venv/bin/activate
```

If keeping the auto-activate hint, scope it strictly to this repo path:
```sh
miti99bot_venv() {
  case "$PWD" in
    */miti99bot|*/miti99bot/*) [ -f "$HOME/path/to/miti99bot/.venv/bin/activate" ] && \
       source "$HOME/path/to/miti99bot/.venv/bin/activate" ;;
  esac
}
```

---

### C5. Placeholders in `aws ssm put-parameter` will be stored literally

Lines 134, 138 (and Step 8/rotation block at 304–308):

```sh
aws ssm put-parameter --profile admin --type SecureString --overwrite \
    --name /miti99bot/prod/telegram-bot-token       --value "<BotFather-token>"
aws ssm put-parameter --profile admin --type SecureString --overwrite \
    --name /miti99bot/prod/gemini-api-key           --value "<google-ai-studio-key>"
```

**Failure mode:** A new user copy-pastes the block as-is. The SSM parameter is created with the literal value `<BotFather-token>`. The Lambda then resolves a token that's the string `<BotFather-token>` and Telegram returns 401. The error is hard to debug because the value is `SecureString` — `get-parameter` without `--with-decryption` hides it.

**Suggested rewrite:** prompt for the values:
```sh
read -rsp 'BotFather token: ' BOT_TOKEN; echo
read -rsp 'Gemini API key:  ' GEMINI; echo

aws ssm put-parameter --profile admin --type SecureString --overwrite \
    --name /miti99bot/prod/telegram-bot-token --value "$BOT_TOKEN"
aws ssm put-parameter --profile admin --type SecureString --overwrite \
    --name /miti99bot/prod/gemini-api-key --value "$GEMINI"

unset BOT_TOKEN GEMINI
```

This also addresses the shell-history leak: `aws ssm put-parameter --value "<actual-token-here>"` ends up in `~/.bash_history` and in `/proc/<pid>/cmdline` while the CLI runs. Using `read -r` keeps the value out of history; the env var still shows in process listings briefly but only the CLI's own PID owns it.

---

## Important (should-fix)

### I1. `--value "$(openssl rand …)"` leaks to history (lower-severity)

Lines 136, 140. Even though the generated value is fresh and not a long-term secret in the same way as the bot token, the resulting parameter IS the secret used to authenticate Telegram → webhook and EventBridge → webhook calls. It's saved to shell history as-is once expanded? Actually no — the literal `$(openssl rand -hex 32)` stored in history is harmless because re-running it produces a different value. **However**, the actual generated value briefly appears in `/proc/<pid>/cmdline` of the `aws` process. On a single-user host that's acceptable, but the doc says nothing about it.

**Suggested rewrite:** generate first, store via env var (same pattern as C5).
```sh
WEBHOOK_SECRET=$(openssl rand -hex 32)
aws ssm put-parameter --profile admin --type SecureString --overwrite \
    --name /miti99bot/prod/telegram-webhook-secret --value "$WEBHOOK_SECRET"
echo "Webhook secret (save somewhere safe): $WEBHOOK_SECRET"
unset WEBHOOK_SECRET
```

### I2. OIDC thumbprint is no longer required by AWS

Line 153: `--thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1`

**Failure mode:** Not a failure — but as of mid-2023, IAM OIDC providers for GitHub Actions validate against root CAs and the thumbprint is ignored. The CLI still requires the flag to be present, so the current value is fine. Worth a one-line note that it's a vestigial parameter (so users don't panic when GitHub rotates intermediate certs).

**Suggested rewrite:** add a comment:
```sh
# --thumbprint-list is required by the API but no longer verified by AWS
# (IAM now trusts the upstream root CAs for token.actions.githubusercontent.com).
```

### I3. URL concatenation assumes trailing slash

Lines 219–220:

```sh
URL=…   # from previous command
curl -X POST "https://api.telegram.org/bot$TOKEN/setWebhook" \
  -d "url=${URL}webhook" \
```

**Failure mode:** Lambda Function URL outputs from CloudFormation are `https://<id>.lambda-url.<region>.on.aws/` (trailing slash always present), so `${URL}webhook` resolves to `…/webhook` — correct. BUT if the user re-types or pastes manually without the trailing slash, it becomes `…webhook` and Telegram POSTs to a non-existent host path. Silent failure.

**Suggested rewrite:**
```sh
URL="${URL%/}/"          # normalize: ensure exactly one trailing slash
curl -X POST "https://api.telegram.org/bot$TOKEN/setWebhook" \
  -d "url=${URL}webhook" ...
```

Or use proper URL building:
```sh
curl -X POST "https://api.telegram.org/bot$TOKEN/setWebhook" \
  --data-urlencode "url=${URL%/}/webhook" \
  --data-urlencode "secret_token=$SECRET" \
  --data-urlencode 'allowed_updates=["message","callback_query"]'
```

`--data-urlencode` is also safer than `-d` for `$SECRET` which is hex-only today but might become base64 (`+/=`) in the future.

### I4. SSM parameter version pin `:1` will break after first rotation

`template.yaml` lines 127–130 use `{{resolve:ssm-secure:/miti99bot/${StackEnv}/telegram-bot-token:1}}`. Each `put-parameter --overwrite` bumps the version to 2, 3, 4, ….

**Failure mode:** Step 2 says `--overwrite`. If a user runs Step 2 twice (e.g. corrects a typo'd bot token before deploying), the parameter version is now 2 but `template.yaml` still resolves `:1`, which now points at the OLD value. The rotating-secrets section (line 307) acknowledges this with "template.yaml pins :1 version; redeploy picks up the new value" — but that comment is WRONG. With version pinned to 1, redeploying does NOT pick up a new value (it picks up version 1 forever). The user must either (a) bump the `:1` to `:2` in template, (b) use no version suffix (latest), or (c) avoid `--overwrite` on the first run.

**Cross-ref:** This is a `template.yaml` bug as much as a doc bug. The `:1` pin is incompatible with the rotation flow described.

**Suggested rewrite (doc level):**
```sh
# WARNING: template.yaml pins SSM version :1. If you run put-parameter
# --overwrite, you create version :2 which the template will NOT resolve.
# For first-time setup, omit --overwrite. For rotation, either:
#   (a) drop the :1 pin in template.yaml (resolves latest version), OR
#   (b) bump the :1 → :N in template.yaml after each put-parameter call.
```

Strongly recommend the template be changed to use `{{resolve:ssm-secure:/miti99bot/${StackEnv}/telegram-bot-token}}` (latest) — but that's out of scope here, just flag it.

### I5. `IAMFullAccess` and `AmazonS3FullAccess` are over-broad even for bootstrap

Lines 163–173. SAM does need IAM to create the Lambda execution role, but `IAMFullAccess` lets the CI role create / attach policies to **any** principal in the account — including escalating to admin. `AmazonS3FullAccess` lets CI read/write/delete every bucket in the account.

**Failure mode:** Compromise of the GitHub Actions runner (malicious dependency, leaked OIDC trust) = account takeover.

**Suggested narrower starter set:**
- Replace `IAMFullAccess` with an inline policy allowing only `iam:CreateRole`, `iam:AttachRolePolicy`, `iam:PassRole`, `iam:GetRole`, `iam:DeleteRole`, `iam:PutRolePolicy`, etc. scoped to `arn:aws:iam::<acct>:role/miti99bot-aws-port-*`.
- Replace `AmazonS3FullAccess` with policy allowing only the SAM-managed bucket (`aws-sam-cli-managed-default-samclisourcebucket-*` and the deploy bucket created with `resolve_s3 = true`).
- `AmazonEventBridgeFullAccess` covers all rules in the account; scope to `arn:aws:scheduler:<region>:<acct>:schedule/default/miti99bot-*` once Phase 04 schedules exist.
- `AWSBudgetsActionsWithAWSResourceControlAccess` is unrelated to deploying — it's for Budgets-triggered IAM actions. Probably not needed for the CFN budget resource (which only needs `budgets:CreateBudget` / `ModifyBudget`). Consider dropping or replacing with `AWSBudgetsReadOnlyAccess` + targeted write perms.

At minimum the doc should call this out as a starter step, not a "Phase 06 problem":
```sh
# These 10 managed policies grant near-admin to the GitHub Actions role.
# AT MINIMUM, before merging to main:
#   - Replace IAMFullAccess with a role-prefix-scoped inline policy.
#   - Replace AmazonS3FullAccess with the SAM-managed bucket only.
# See Step 7 for the long-term plan.
```

### I6. `--guided` deploy will create an S3 bucket with no lifecycle / versioning policy

`samconfig.toml` has `resolve_s3 = true`. `sam deploy --guided` and subsequent deploys upload artifacts to a SAM-managed bucket (`aws-sam-cli-managed-default-samclisourcebucket-*`). By default the bucket has versioning ENABLED (SAM creates it that way) and no lifecycle rule — old packaged Lambda zips accumulate indefinitely.

**Failure mode:** Free tier on S3 is 5 GB. A 30 MB Lambda zip × 200 deploys = 6 GB. Bot leaves free tier silently.

**Suggested rewrite:** add a note + lifecycle policy:
```sh
# After first deploy, find the SAM-managed bucket and add a 30-day lifecycle
# rule to expire old artifacts:
BUCKET=$(aws s3 ls --profile admin | awk '/aws-sam-cli-managed-default/ {print $3}')
aws s3api put-bucket-lifecycle-configuration --profile admin --bucket "$BUCKET" \
  --lifecycle-configuration '{"Rules":[{"ID":"expire-old-deploys","Status":"Enabled","Filter":{},"Expiration":{"Days":30},"NoncurrentVersionExpiration":{"NoncurrentDays":7},"AbortIncompleteMultipartUpload":{"DaysAfterInitiation":1}}]}'
```

### I7. `pip install awscli` ships v1 — `aws ssm get-parameter --query` works the same, but worth verifying

Line 50: `aws --version    # aws-cli/1.x (pip ships v1; v2 is not on PyPI)`

This is correct and the doc calls it out. v1 supports `--query`, `--output`, `--profile`, `--with-decryption`. The `get-parameter` invocations on lines 213, 216 are valid for v1.

**Watch:** AWS CLI v1 is in maintenance mode (security only, since 2023). The doc could note an EOL risk but it's low priority.

### I8. CloudWatch Logs retention is set in template (good) but doc claims 7 days unconditionally

Doc line 20 and 25: "log retention pinned to 7 days". `template.yaml` line 86 confirms `RetentionInDays: 7`. ✓ Correct.

### I9. X-Ray "Active" tracing free-tier monitoring not surfaced

Doc line 25 mentions the 100k traces/mo cap. `template.yaml` line 52 sets `Tracing: Active` globally. No alarm or check is provided. For a free-tier-strict doc, a one-liner reminder:

```sh
# Monthly X-Ray traces metric (free tier 100k/mo):
aws cloudwatch get-metric-statistics --namespace AWS/X-Ray \
  --metric-name TracesProcessed --start-time $(date -u -d '30 days ago' +%FT%TZ) \
  --end-time $(date -u +%FT%TZ) --period 2592000 --statistics Sum
```

### I10. `aws cloudformation describe-stacks --query "...[?...]..."` — JMESPath quoting

Lines 201–204:

```sh
aws cloudformation describe-stacks --profile admin \
  --stack-name miti99bot-aws-port \
  --query "Stacks[0].Outputs[?OutputKey=='FunctionUrl'].OutputValue" --output text
```

**Correctness:** valid JMESPath. Returns a single-line `https://….lambda-url.ap-southeast-1.on.aws/` with `--output text`. ✓ Works.

**Minor:** if the stack doesn't have a `FunctionUrl` output (wrong stack name), `--output text` returns empty string with exit 0. User then sets `URL=""` and silently sends a webhook URL of `webhook` to Telegram. Worth wrapping:
```sh
URL=$(aws cloudformation describe-stacks ...)
[ -n "$URL" ] || { echo "FunctionUrl output not found — did the stack deploy?"; exit 1; }
```

---

## Minor / Suggestions

### M1. `make sam-deploy` in rotation step skips `--profile admin`

Line 307: `make sam-deploy   # template.yaml pins :1 version; redeploy picks up the new value`

The Makefile target `sam-deploy` does not set `AWS_PROFILE`. After Step 7 the user has rotated/deleted the `admin` keys — fine, CI handles it. But during early bootstrap (before GH Actions is wired), `make sam-deploy` without `AWS_PROFILE=admin` will use the user's default profile, which may not exist.

**Suggested rewrite:**
```sh
AWS_PROFILE=admin make sam-deploy   # or rely on CI after Step 7
```

### M2. `read -rsp` portability

The suggestions above using `read -rsp` work on bash (Linux Ubuntu 24.04 — the doc's stated target). `dash` does not support `-s`. The doc explicitly targets bash on Ubuntu, so this is fine; flag only as a portability footnote.

### M3. `dpkg --print-architecture` is Debian-only

Line 92: GH CLI install uses `dpkg --print-architecture`. Doc targets Ubuntu 24.04, so OK. Portability note only.

### M4. `sudo tar -C /usr/local -xzf /tmp/go.tgz` after `sudo rm -rf /usr/local/go`

Lines 79–80. The `rm -rf /usr/local/go` is acceptable here (specific path, not interpolated). If a future revision changes this to `sudo rm -rf /usr/local/$GO_DIR` and `GO_DIR` is empty, it deletes `/usr/local`. Guard against future regressions:
```sh
[ -n "$GO_VERSION" ] || { echo "GO_VERSION unset"; exit 1; }
```

Low priority — current code is fine.

### M5. `newgrp docker` (line 103) only affects the current shell

User completes Docker install, then proceeds to other steps in the same shell where `newgrp docker` was run. If they open a new terminal, they'll hit "permission denied" on docker until next login. Worth a one-line note:
```sh
# newgrp docker only affects this shell; log out + back in to make it permanent
```

### M6. `template.yaml` uses `provided.al2023` + `Handler: bootstrap` + ARM64 — doc doesn't explicitly state the binary must be named `bootstrap`

The Makefile builds `build/lambda/bootstrap`. Template references it. If a user customizes the Makefile to output `build/lambda/server` or similar, the deploy succeeds but invocation fails at runtime with `init error: fork/exec /var/task/bootstrap: no such file`.

**Suggested rewrite (one line):**
```sh
# Lambda's provided.al2023 runtime expects an executable named exactly `bootstrap`
# in the deployment package root. Don't rename the make target.
```

### M7. `Architectures: [arm64]` + `LambdaAdapterLayerArm64:25` — region-pinned

`template.yaml` line 35: `arn:aws:lambda:ap-southeast-1:753240598075:layer:LambdaAdapterLayerArm64:25`. If a user changes `region` in `samconfig.toml`, this layer ARN no longer exists in the new region.

**Suggested rewrite:** doc should warn (currently it just says "Change in samconfig.toml if needed" without flagging the layer ARN):
```sh
# Changing region also requires updating LambdaAdapterLayerArn in template.yaml
# (each region publishes its own ARN). See:
# https://github.com/awslabs/aws-lambda-web-adapter/releases
```

### M8. `sam deploy --guided` prompts (Step 4) don't list all required answers

Doc lists: stack name, region, capabilities, save samconfig. Real prompts include:
- "Confirm changes before deploy [y/N]" → recommend `N` (matches CI)
- "Allow SAM CLI IAM role creation [Y/n]" → must be `Y`
- "Disable rollback [y/N]" → must be `N` (free-tier safety: auto-rollback prevents stuck broken stacks)
- "Save arguments to configuration file [Y/n]" → `Y`
- "SAM configuration file" → accept default
- "SAM configuration environment" → accept default

Worth listing the full sequence so a new user doesn't accidentally `y` to "Disable rollback".

### M9. `aws iam attach-role-policy` order of arguments

Lines 174–175 — correct, but `--profile admin` after `--role-name` works the same regardless of order. Cosmetic only.

### M10. Free-tier table claims DynamoDB request free tier of 200M (line 274)

Doc table line 274 says "DynamoDB req 200M". AWS DDB free tier is 25 GB storage + 25 WCU / 25 RCU provisioned-capacity-equivalent always-free (which is approx 200M req/mo if you saturate at 25 RCU). PAY_PER_REQUEST conversion: 25 RCU ≈ 65M strongly-consistent reads + 25 WCU ≈ 65M writes. The "200M" figure conflates request types. Worth a tiny clarification:
```
| DynamoDB on-demand req | ≈ 50M reads + ≈ 50M writes (varies by item size) | ... |
```

Cosmetic. The directional warning (5% = runaway) is correct.

### M11. `dd if=… of=/usr/share/keyrings/githubcli-archive-keyring.gpg`

Line 90. `dd` here is acting as a glorified `cp` but with no useful flag. Common pattern, works fine. Cosmetic.

---

## Cross-references with `template.yaml`

| Free-tier claim in doc | Verified in template? |
|---|---|
| "PITR disabled" | ✓ line 74 |
| "Retention 7 days" | ✓ line 86 |
| "ARM64" | ✓ line 49 |
| "PAY_PER_REQUEST" | ✓ line 66 |
| "Function URL (not API Gateway)" | ✓ line 131–137 |
| "X-Ray active tracing" | ✓ line 52 |
| "$1 budget alarm" | ✓ line 180–203 (conditional on AlertEmail) |
| "Memory 256 MB" | ✓ line 50 |

All free-tier claims are backed by `template.yaml`. No drift.

---

## Unresolved questions

1. Should `template.yaml`'s `:1` SSM version pin be dropped to fix the rotation contradiction (I4)? Doc and template currently disagree on rotation flow.
2. Is the `cd()` override (C4) load-bearing for the user's actual workflow, or just a convenience hint? If just convenience, drop it; if some downstream `make` target requires the venv-on-`cd`, surface that requirement instead.
3. The repo has `aws/README.md` with mostly-overlapping content. Should this guide supersede it, or stay as a "human-readable" wrapper? If both stay, keep them in sync (rotation contradiction exists in both).
4. Is `provided.al2023` the long-term runtime, or is the team planning to switch to `provided.al2` (older) for AL2-LTS support? Affects whether to lock the doc to AL2023-specific behavior.
5. The IAM trust policy at `aws/iam-github-oidc-trust.json` is checked in with a real account ID (`225603493174`) and `tiennm99/miti99bot` — is that intentional (this repo's owner), or should it be templated with placeholders? Doc says "Edit … fill in your 12-digit AWS account ID" but the file is already filled in.

---

**Status:** DONE_WITH_CONCERNS

Concerns: C1 (rollback command incorrect) and C4 (`cd()` override) need real fixes, not just clarification. C5 (literal placeholders) and I4 (SSM `:1` pin vs `--overwrite` contradiction) will burn new users on first run. I5 (IAM blast radius) is a long-term security finding but not a copy-paste hazard.

Sources:
- [update-stack — AWS CLI Command Reference](https://docs.aws.amazon.com/cli/latest/reference/cloudformation/update-stack.html)
- [Continue rolling back an update — AWS CloudFormation](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-continueupdaterollback.html)
- [continue-update-rollback — AWS CLI Command Reference](https://docs.aws.amazon.com/cli/latest/reference/cloudformation/continue-update-rollback.html)
