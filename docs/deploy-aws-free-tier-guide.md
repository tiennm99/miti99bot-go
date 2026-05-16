# Deploy miti99bot to AWS (Free Tier)

End-to-end onboarding guide for deploying `miti99bot` on AWS. Everything below stays inside the AWS free tier in region `ap-southeast-1` (Singapore).

Related docs:
- One-time bootstrap reference: [`aws/README.md`](../aws/README.md)
- Steady-state operations: [`deploy-aws.md`](./deploy-aws.md)

---

## What you get (all free-tier)

| Resource | Free quota | This bot's usage |
|---|---|---|
| Lambda (ARM64, 256 MB) | 1M req + 400k GB-s / mo, **always-free** | far below |
| Lambda Function URL | included with Lambda | the Telegram webhook entrypoint |
| DynamoDB on-demand | 25 GiB storage + 2.5M read / 1M write request units / mo **always-free** | far below |
| EventBridge Scheduler | 14M invocations / mo always-free | a few crons |
| SSM Parameter Store (Standard) | unlimited free | 4 SecureString params |
| CloudWatch Logs | 5 GB ingest, 5 GB storage / mo | well below at 7-day retention |
| SQS (cron DLQ) | 1M req / mo always-free | near zero |
| CloudFormation, IAM, Budgets | free | |
| Egress | 100 GB / mo always-free | tiny |

Paid traps the template already avoids: DynamoDB PITR disabled, no NAT Gateway, no API Gateway, no provisioned concurrency, no VPC, log retention pinned to 7 days, X-Ray "Active" tracing stays within the 100k traces/mo free tier.

---

## Prerequisites (Ubuntu 24.04 ARM64)

Host arch matches Lambda's `arm64` target, so `make build-lambda` is a native build (still pinned to `GOARCH=arm64` for reproducibility).

```sh
sudo apt update
sudo apt install -y curl jq make git python3 python3-venv python3-pip
```

### AWS CLI + SAM CLI (project-local venv via pip)

Ubuntu 24.04 enforces PEP 668 (externally-managed system Python), so we install both tools inside a project-local `.venv`. From the repo root:

```sh
cd /path/to/miti99bot
python3 -m venv .venv
source .venv/bin/activate

pip install --upgrade pip
pip install awscli aws-sam-cli

aws --version    # aws-cli/1.x (pip ships v1; v2 is not on PyPI)
sam --version
```

Activate the venv at the start of every shell session you use for AWS commands:

```sh
source /path/to/miti99bot/.venv/bin/activate
```

If you want auto-activation, install [`direnv`](https://direnv.net/) (`sudo apt install direnv`, then `eval "$(direnv hook bash)"` in `~/.bashrc`) and drop a `.envrc` in the repo containing `source .venv/bin/activate`. Don't override the shell built-in `cd` — that affects every directory you ever enter.

> **Note:** PyPI's `awscli` is **v1** (v2 is only distributed as the standalone bundle). v1 covers every command used in this guide — `ssm`, `iam`, `cloudformation`, `lambda`, `logs`, `ce`, `cloudwatch`. If you later need v2-only features (e.g. SSO login, new `aws configure sso` flows), install v2 separately from the official ARM zip and keep both. SAM CLI on PyPI tracks upstream releases — `pip install -U aws-sam-cli` to bump.

Add `.venv/` to `.gitignore` if not already there:

```sh
grep -qxF '.venv/' .gitignore || echo '.venv/' >> .gitignore
```

### Go 1.25 (ARM64)

Ubuntu 24.04 ships an older Go. Install the upstream tarball:

```sh
GO_VERSION=1.25.0
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-arm64.tar.gz" -o /tmp/go.tgz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/go.tgz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version       # expect go1.25.x linux/arm64
```

### GitHub CLI (`gh`)

```sh
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
  | sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
sudo chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
  | sudo tee /etc/apt/sources.list.d/github-cli.list >/dev/null
sudo apt update
sudo apt install -y gh
```

### Docker (optional — only for DynamoDB Local tests)

```sh
sudo apt install -y docker.io
sudo usermod -aG docker "$USER"
newgrp docker
```

### Accounts / keys

- AWS account (root access).
- Telegram bot token from BotFather.
- Gemini API key from Google AI Studio (free).
- This repo cloned locally.

---

## Step 1 — AWS account hygiene

1. Log in to the AWS root user, enable MFA.
2. Create an IAM user `admin` with `AdministratorAccess` and CLI access keys. This is used only for the first deploy.
3. Configure the CLI:
   ```sh
   aws configure set region ap-southeast-1 --profile admin
   aws configure set aws_access_key_id AKIA…  --profile admin
   aws configure set aws_secret_access_key … --profile admin
   ```

---

## Step 2 — Store the 4 secrets in SSM Parameter Store

Parameter Store Standard tier is free; SecureString uses the AWS-managed KMS key, also free. `--tier Standard` is the default — never pass `--tier Advanced` (that costs $0.05/param/month).

> **Shell-history hygiene.** Long-lived tokens (BotFather, Gemini) are passed below via `read -s` so they never appear in `~/.bash_history`, `ps`, or backups of either. The two short-lived random tokens (webhook + cron) are generated inline with `openssl rand`.

Real secrets — read each value interactively, no echo:

```sh
read -rsp 'BotFather token: '      BOT_TOKEN  && echo
aws ssm put-parameter --profile admin --type SecureString --overwrite \
    --name /miti99bot/prod/telegram-bot-token --value "$BOT_TOKEN"
unset BOT_TOKEN

read -rsp 'Gemini API key: '       GEMINI_KEY && echo
aws ssm put-parameter --profile admin --type SecureString --overwrite \
    --name /miti99bot/prod/gemini-api-key --value "$GEMINI_KEY"
unset GEMINI_KEY
```

> Skip the Gemini one if you're not using the `twentyq` module — store `"unused"` so the Lambda startup secret fetch still succeeds, then drop `twentyq` from `MODULES`.

Generated secrets — random hex, no input needed:

```sh
aws ssm put-parameter --profile admin --type SecureString --overwrite \
    --name /miti99bot/prod/telegram-webhook-secret --value "$(openssl rand -hex 32)"
aws ssm put-parameter --profile admin --type SecureString --overwrite \
    --name /miti99bot/prod/cron-shared-secret      --value "$(openssl rand -hex 32)"
```

The webhook + cron values are fetched back in Step 5 via `aws ssm get-parameter` — no need to copy them out by hand.

Confirm all four exist (names only, no values):

```sh
aws ssm get-parameters-by-path --profile admin \
  --path /miti99bot/prod/ --query 'Parameters[].Name' --output table
```

---

## Step 3 — Register GitHub OIDC + deploy role (one-time)

> Run all commands in this step from the repo root — `aws/iam-github-oidc-trust.json` is referenced as a relative path.

```sh
cd /path/to/miti99bot
```

Register the OIDC identity provider (idempotent — skips creation if it already exists):

```sh
ACCT=$(aws sts get-caller-identity --profile admin --query Account --output text)
OIDC_ARN="arn:aws:iam::${ACCT}:oidc-provider/token.actions.githubusercontent.com"

aws iam get-open-id-connect-provider --profile admin \
    --open-id-connect-provider-arn "$OIDC_ARN" >/dev/null 2>&1 \
  || aws iam create-open-id-connect-provider --profile admin \
       --url https://token.actions.githubusercontent.com \
       --client-id-list sts.amazonaws.com \
       --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1
```

Edit `aws/iam-github-oidc-trust.json` if you're deploying from a different AWS account or repo fork. This repo is already prefilled for account `225603493174` and `tiennm99/miti99bot`:

```sh
sed -i "s|225603493174|$ACCT|" aws/iam-github-oidc-trust.json   # only if you are changing accounts
```

If you change accounts, update `.github/workflows/deploy.yml` to match the same role ARN.

Create the role (idempotent — `update-assume-role-policy` if it already exists):

```sh
aws iam get-role --profile admin --role-name github-deploy-miti99bot >/dev/null 2>&1 \
  && aws iam update-assume-role-policy --profile admin \
       --role-name github-deploy-miti99bot \
       --policy-document file://aws/iam-github-oidc-trust.json \
  || aws iam create-role --profile admin \
       --role-name github-deploy-miti99bot \
       --assume-role-policy-document file://aws/iam-github-oidc-trust.json
```

Attach the managed policies (re-attaching the same policy is a no-op, so this loop is safely re-runnable):

```sh
for arn in \
  arn:aws:iam::aws:policy/AWSCloudFormationFullAccess \
  arn:aws:iam::aws:policy/AWSLambda_FullAccess \
  arn:aws:iam::aws:policy/AmazonDynamoDBFullAccess \
  arn:aws:iam::aws:policy/AmazonEventBridgeFullAccess \
  arn:aws:iam::aws:policy/AmazonSQSFullAccess \
  arn:aws:iam::aws:policy/AmazonSSMFullAccess \
  arn:aws:iam::aws:policy/CloudWatchLogsFullAccess \
  arn:aws:iam::aws:policy/AWSBudgetsActionsWithAWSResourceControlAccess \
  arn:aws:iam::aws:policy/IAMFullAccess \
  arn:aws:iam::aws:policy/AmazonS3FullAccess; do
  aws iam attach-role-policy --profile admin \
    --role-name github-deploy-miti99bot --policy-arn "$arn"
done
```

These managed policies are intentionally broad for the first deploy. `IAMFullAccess` and `AmazonS3FullAccess` are the widest blast radius — tighten them first when you reach Step 7.

---

## Step 4 — First deploy (manual)

```sh
cd /path/to/miti99bot
make build-lambda                                                              # cross-compiles Go → linux/arm64
AWS_PROFILE=admin sam deploy --template-file template.yaml --guided            # accept samconfig.toml defaults
```

> **Why `--template-file template.yaml`.** By default `sam deploy` looks for `.aws-sam/build/template.yaml` (output of `sam build`). We skip `sam build` because SAM's default builder for `provided.al2023` expects a `Makefile` inside `CodeUri: build/lambda/` — which is the *output* directory of `make build-lambda`, not a source dir. Pointing `sam deploy` at the raw source template tells it to read `CodeUri: build/lambda/` directly, zip the bootstrap binary, upload it, and deploy. The `make build-lambda` step above is the actual compile.

Confirm at the SAM prompt:
- Stack name: `miti99bot`
- Region: `ap-southeast-1`
- Capabilities: `CAPABILITY_IAM`
- Save to `samconfig.toml`: yes

After `CREATE_COMPLETE`, grab the Function URL:

```sh
aws cloudformation describe-stacks --profile admin \
  --stack-name miti99bot \
  --query "Stacks[0].Outputs[?OutputKey=='FunctionUrl'].OutputValue" --output text
```

---

## Step 5 — Point Telegram at the webhook

> For first-time setup only. After `Step 6` wires the GitHub workflow, every push to `main` auto-runs `setWebhook` + `setMyCommands`; this manual block is the break-glass path.

```sh
URL=…   # from previous command
TOKEN=$(aws ssm get-parameter --profile admin \
  --name /miti99bot/prod/telegram-bot-token --with-decryption \
  --query Parameter.Value --output text)
SECRET=$(aws ssm get-parameter --profile admin \
  --name /miti99bot/prod/telegram-webhook-secret --with-decryption \
  --query Parameter.Value --output text)

curl -X POST "https://api.telegram.org/bot$TOKEN/setWebhook" \
  -d "url=${URL}webhook" \
  -d "secret_token=$SECRET" \
  -d "allowed_updates=[\"message\",\"callback_query\"]"

curl "https://api.telegram.org/bot$TOKEN/getWebhookInfo" | jq .
```

Expect: `url` matches Function URL, `pending_update_count` ≈ 0, `last_error_date` empty. Send `/start` to the bot — response should arrive within a couple seconds.

---

## Step 6 — Wire GitHub Actions for future deploys

In GitHub → repo Settings → Secrets and variables → Actions:

| Secret | Value |
|---|---|
| `ALERT_EMAIL` (optional) | Email for the $1 budget alert |

After this, every push to `main` triggers `.github/workflows/deploy.yml`:
1. OIDC assume `github-deploy-miti99bot` role
2. `make build-lambda`
3. `sam deploy --template-file template.yaml`
4. Smoke `curl <function-url>/`

No long-lived keys live in GitHub. The deploy workflow now uses the repo's fixed AWS account ID directly for the OIDC role ARN, so `AWS_ACCOUNT_ID` no longer needs to be stored in GitHub.

---

## Step 7 — Lock down (recommended once it works)

1. Rotate / delete `admin` CLI keys (keep the user for console-only emergencies).
2. Trigger a `workflow_dispatch` deploy via GH Actions to confirm OIDC path works without the bootstrap user.
3. Replace the broad managed policies on `github-deploy-miti99bot` with stack-scoped custom policies (resource ARNs from your stack).

---

## Step 8 — Cost guardrails

- Set `ALERT_EMAIL` → enables a $1/mo AWS Budgets alarm at 80% and 100%.
- Daily checks (logs, DDB throttle, cold-start P95, MTD spend) → see [`deploy-aws.md`](./deploy-aws.md) "Operational checks".
- Idle steady-state cost is **$0**. If you ever see >$0.01 in Cost Explorer, investigate. Most likely culprits:
  - CloudWatch Logs ingestion volume (verbose logging, hot loops).
  - DynamoDB writes from a runaway loop.
  - Accidental egress past the 100 GB free tier.

---

## Free-tier watch table

| Resource | Free | Watch when |
|---|---|---|
| Lambda req / GB-s | 1M / 400k | Past 50% mid-month |
| DynamoDB req | 200M | Past 5% (sign of runaway loop) |
| DynamoDB storage | 25 GiB | Past 100 MiB (suspect leaks) |
| EventBridge invocations | 14M | Past 1k/mo (suspect mis-config) |
| CloudWatch Logs ingest | 5 GB | Past 50% mid-month |
| Egress | 100 GB | Past 1 GB (wildly high) |

The $1 budget alarm catches all of these via cost-side fallout.

---

## Rollback

CloudFormation has three rollback flavors. Pick the one that matches your situation.

### Case A — `sam deploy` is currently failing

CloudFormation auto-initiates a rollback. Nothing to do. If the rollback itself fails (`UPDATE_ROLLBACK_FAILED`):

```sh
aws cloudformation continue-update-rollback --profile admin \
  --stack-name miti99bot
```

### Case B — `sam deploy` is running and you want to abort

```sh
aws cloudformation cancel-update-stack --profile admin \
  --stack-name miti99bot
```

CloudFormation rolls back to the prior `CREATE_COMPLETE` / `UPDATE_COMPLETE` state.

### Case C — Deploy succeeded but the code is bad

CloudFormation has no "redeploy previous template" command (`--use-previous-template` re-applies the *current* template, not an older one — it does **not** roll back). Redeploy from the last known-good commit:

```sh
git checkout <good-sha>
make build-lambda
make sam-deploy
```

To find the last good SHA quickly: `git log --oneline -- template.yaml cmd/server` and pick the commit that matches a passing deploy.

---

## Rotating secrets

```sh
aws ssm put-parameter --profile admin --type SecureString --overwrite \
  --name /miti99bot/prod/telegram-webhook-secret \
  --value "$(openssl rand -hex 32)"
# Recycle the Lambda before switching Telegram to the new secret, or wait for
# AWS to create a fresh execution environment naturally.
```

> `template.yaml` passes only SSM parameter names to Lambda. The app fetches the current SecureString values during cold start, so no secret values are embedded in CloudFormation. Existing warm Lambda environments keep the old value until AWS recycles them or you force a function update; rotate the Telegram webhook secret by refreshing Lambda first, then re-running Step 5 with the new `secret_token`.
