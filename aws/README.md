# AWS account setup

One-time setup steps for a fresh AWS account. After this is done, every push to `main` deploys via GitHub Actions OIDC; no human-in-loop AWS commands needed.

> For the full onboarding walkthrough (prerequisites, Telegram wiring, cost guardrails), see [`../docs/deploy-aws-free-tier-guide.md`](../docs/deploy-aws-free-tier-guide.md). This file is the condensed cheatsheet.

> **Region:** `ap-southeast-1` (Singapore). Change in `samconfig.toml` if needed.
> **Stack name:** `miti99bot`. Change in `samconfig.toml`.

---

## 1. AWS account hygiene

1. Enable MFA on the root user.
2. Create an IAM admin user `admin` (CLI access keys). Use only for the first `sam deploy --guided`.
3. Set CLI default region:
   ```sh
   aws configure set region ap-southeast-1 --profile admin
   aws configure set aws_access_key_id  AKIA…  --profile admin
   aws configure set aws_secret_access_key …   --profile admin
   ```

## 2. SSM Parameter Store secrets

Create the four required secrets. **Names must match `template.yaml`** (`/miti99bot/${StackEnv}/…`).

```sh
aws ssm put-parameter --name /miti99bot/prod/telegram-bot-token \
    --value "<bot-father-token>" --type SecureString --profile admin

aws ssm put-parameter --name /miti99bot/prod/telegram-webhook-secret \
    --value "$(openssl rand -hex 32)" --type SecureString --profile admin

aws ssm put-parameter --name /miti99bot/prod/gemini-api-key \
    --value "<google-ai-studio-key>" --type SecureString --profile admin

aws ssm put-parameter --name /miti99bot/prod/cron-shared-secret \
    --value "$(openssl rand -hex 32)" --type SecureString --profile admin
```

Save the webhook + cron secrets locally — you'll set them on the Telegram side and on the EventBridge schedule headers.

## 3. GitHub OIDC identity provider

One-time per AWS account:

```sh
aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --client-id-list sts.amazonaws.com \
  --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1 \
  --profile admin
```

(GitHub publishes the canonical thumbprint; verify on docs.github.com if rotated.)

## 4. Deploy IAM role for GitHub Actions

Edit `aws/iam-github-oidc-trust.json` if you are changing the AWS account or GitHub repo. This repo is already prefilled for account `225603493174` and `tiennm99/miti99bot`, and the trust allowlist is narrowed to `refs/heads/main` only (see "Trust policy invariants" below). If you change accounts, update `.github/workflows/deploy.yml` to match the same role ARN, then:

```sh
aws iam create-role \
  --role-name github-deploy-miti99bot \
  --assume-role-policy-document file://aws/iam-github-oidc-trust.json \
  --profile admin

# Permissions: stack-scoped inline policy committed at aws/iam-github-deploy-policy.json.
aws iam put-role-policy \
  --role-name github-deploy-miti99bot \
  --policy-name miti99bot-deploy \
  --policy-document file://aws/iam-github-deploy-policy.json \
  --profile admin
```

> Scoped to stacks/resources named `miti99bot*`. See [security audit](../plans/reports/code-reviewer-260518-1019-security-aws-infra.md) F1 for rationale and [plan](../plans/260518-1019-iam-least-privilege/) for the cutover record.

### Updating the deploy policy

When `template.yaml` adds a new resource type, the deploy role may need new IAM actions. Workflow:

1. Edit `aws/iam-github-deploy-policy.json` — add the action(s) + ARN pattern.
2. Apply out-of-band from a maintainer's `admin` profile (NOT via the workflow):
   ```sh
   aws iam put-role-policy --role-name github-deploy-miti99bot \
     --policy-name miti99bot-deploy \
     --policy-document file://aws/iam-github-deploy-policy.json --profile admin
   ```
3. Commit the JSON. Next deploy uses the new permissions.

Drift check — structural compare, not byte-diff. `aws iam get-role-policy` returns JSON whose key ordering / whitespace differs from the local file but may be semantically identical. Compare normalized:
```sh
diff <(aws iam get-role-policy --role-name github-deploy-miti99bot \
         --policy-name miti99bot-deploy --profile admin \
         --query PolicyDocument | jq -S .) \
     <(jq -S . aws/iam-github-deploy-policy.json)
```
Non-empty output = INVESTIGATE before reapplying. AWS-side may have been intentionally patched during an outage; blindly re-applying overwrites that fix.

### Trust policy invariants

`aws/iam-github-oidc-trust.json` constrains which GitHub Actions contexts can assume `github-deploy-miti99bot`. The current allowlist is intentionally narrow: only pushes to `main` can deploy.

**To add a new branch / context** (e.g., a future `dev` preview deploy):

1. Edit `aws/iam-github-oidc-trust.json` — add the new `sub` claim to the `StringLike` array. Examples:
   - `repo:tiennm99/miti99bot:ref:refs/heads/dev` — pushes to `dev` branch
   - `repo:tiennm99/miti99bot:environment:preview` — workflows scoped to a GitHub Environment named `preview` (requires `permissions: id-token: write`)
2. Apply out-of-band:
   ```sh
   aws iam update-assume-role-policy --role-name github-deploy-miti99bot \
     --policy-document file://aws/iam-github-oidc-trust.json --profile admin
   ```
3. Commit. Test by triggering the new workflow path.

**Reasons `pull_request` is NOT in the allowlist** (do not re-add without reviewing): PR-context OIDC tokens are derivable from any contributor's PR. Granting the deploy role to PRs is equivalent to granting deploy access to every contributor. Combined with the inline policy's IAM/Lambda/DynamoDB actions, an attacker-controlled PR could exfiltrate or alter prod state.

## 5. Add GitHub repo secrets

In GitHub repo settings → Secrets and variables → Actions:

| Secret | Value |
|---|---|
| `ALERT_EMAIL` (optional) | Email for the $1 budget alert |

The deploy workflow now uses the repo's fixed AWS account ID directly for the OIDC role ARN, so `AWS_ACCOUNT_ID` no longer needs to be stored in GitHub.

## 6. First deploy (manual)

```sh
make build-lambda
AWS_PROFILE=admin sam deploy --template-file template.yaml --guided
```

Confirm:
- Stack name: `miti99bot`
- Region: `ap-southeast-1`
- Capabilities: `CAPABILITY_IAM`
- Save to `samconfig.toml`: yes (already committed; this just confirms)

After `CREATE_COMPLETE`:
```sh
aws cloudformation describe-stacks --stack-name miti99bot \
  --query "Stacks[0].Outputs" --output table --profile admin
```
Note the `FunctionUrl` — point the Telegram webhook at it (see [`../docs/deploy-aws-free-tier-guide.md`](../docs/deploy-aws-free-tier-guide.md) Step 5).

## 7. Tighten — optional but recommended

Once the first deploy succeeds:
1. Rotate / delete `admin` CLI keys (use only via console for emergencies).
2. Trigger a workflow_dispatch deploy via GH Actions to confirm OIDC path works without the bootstrap user.
3. ~~Replace the broad managed policies on `github-deploy-miti99bot` with stack-scoped custom policies.~~ **Done 2026-05-18** — see step 4 (`aws/iam-github-deploy-policy.json`) and [plan](../plans/260518-1019-iam-least-privilege/).

---

## Lambda Web Adapter layer ARN

Pinned in `template.yaml` parameter `LambdaAdapterLayerArn`. Bump by checking:
- https://github.com/awslabs/aws-lambda-web-adapter/releases (look at the `Releases` page for the latest layer version)
- Format: `arn:aws:lambda:ap-southeast-1:753240598075:layer:LambdaAdapterLayerArm64:<version>`

## Cost expectations

After the stack is up but idle, monthly cost should be **$0**. If you ever see >$0.01 in Cost Explorer, investigate — most likely culprits: CloudWatch Logs ingestion volume, DynamoDB writes from a runaway loop, or accidental egress past the 100 GB free tier.
