# AWS bootstrap commands

One-time setup steps for Phase 01 of the AWS port. After this is done, every push to `main` deploys via GitHub Actions OIDC; no human-in-loop AWS commands needed.

> **Region:** `ap-southeast-1` (Singapore). Change in `samconfig.toml` if needed.
> **Stack name:** `miti99bot-aws-port`. Change in `samconfig.toml`.

---

## 1. AWS account hygiene

1. Enable MFA on the root user.
2. Create an IAM admin user `bootstrap-admin` (CLI access keys). Use only for the first `sam deploy --guided`.
3. Set CLI default region:
   ```sh
   aws configure set region ap-southeast-1 --profile bootstrap-admin
   aws configure set aws_access_key_id  AKIA…  --profile bootstrap-admin
   aws configure set aws_secret_access_key …   --profile bootstrap-admin
   ```

## 2. SSM Parameter Store secrets

Create the four required secrets. **Names must match `template.yaml`** (`/miti99bot/${StackEnv}/…`).

```sh
aws ssm put-parameter --name /miti99bot/prod/telegram-bot-token \
    --value "<bot-father-token>" --type SecureString --profile bootstrap-admin

aws ssm put-parameter --name /miti99bot/prod/telegram-webhook-secret \
    --value "$(openssl rand -hex 32)" --type SecureString --profile bootstrap-admin

aws ssm put-parameter --name /miti99bot/prod/gemini-api-key \
    --value "<google-ai-studio-key>" --type SecureString --profile bootstrap-admin

aws ssm put-parameter --name /miti99bot/prod/cron-shared-secret \
    --value "$(openssl rand -hex 32)" --type SecureString --profile bootstrap-admin
```

Save the webhook + cron secrets locally — you'll set them on the Telegram side and on the EventBridge schedule headers.

## 3. GitHub OIDC identity provider

One-time per AWS account:

```sh
aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --client-id-list sts.amazonaws.com \
  --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1 \
  --profile bootstrap-admin
```

(GitHub publishes the canonical thumbprint; verify on docs.github.com if rotated.)

## 4. Deploy IAM role for GitHub Actions

Edit `aws/iam-github-oidc-trust.json` to set your AWS account ID and GitHub repo, then:

```sh
aws iam create-role \
  --role-name github-deploy-miti99bot \
  --assume-role-policy-document file://aws/iam-github-oidc-trust.json \
  --profile bootstrap-admin

# Permissions (broad to start; tighten in Phase 06).
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
  aws iam attach-role-policy --role-name github-deploy-miti99bot \
    --policy-arn "$arn" --profile bootstrap-admin
done
```

> Yes, this is broad. SAM creates IAM roles for the Lambda, so the deploy role needs `iam:CreateRole`. **Tighten in Phase 06** with custom policies scoped to the stack's resource ARNs.

## 5. Add GitHub repo secrets

In GitHub repo settings → Secrets and variables → Actions:

| Secret | Value |
|---|---|
| `AWS_ACCOUNT_ID` | 12-digit AWS account ID |
| `ALERT_EMAIL` (optional) | Email for the $1 budget alert |

`AWS_ACCOUNT_ID` is not a credential — it's hidden only to keep the ARN out of the workflow file.

## 6. First deploy (manual, with bootstrap admin)

```sh
make build-lambda
sam build
AWS_PROFILE=bootstrap-admin sam deploy --guided
```

Confirm:
- Stack name: `miti99bot-aws-port`
- Region: `ap-southeast-1`
- Capabilities: `CAPABILITY_IAM`
- Save to `samconfig.toml`: yes (already committed; this just confirms)

After `CREATE_COMPLETE`:
```sh
aws cloudformation describe-stacks --stack-name miti99bot-aws-port \
  --query "Stacks[0].Outputs" --output table --profile bootstrap-admin
```
Note the `FunctionUrl` — Phase 07 sets the Telegram webhook to it.

## 7. Tighten — optional but recommended

Once the first deploy succeeds:
1. Rotate / delete `bootstrap-admin` CLI keys (use only via console for emergencies).
2. Trigger a workflow_dispatch deploy via GH Actions to confirm OIDC path works without the bootstrap user.
3. Replace the broad managed policies on `github-deploy-miti99bot` with stack-scoped custom policies.

---

## Lambda Web Adapter layer ARN

Pinned in `template.yaml` parameter `LambdaAdapterLayerArn`. Bump by checking:
- https://github.com/awslabs/aws-lambda-web-adapter/releases (look at the `Releases` page for the latest layer version)
- Format: `arn:aws:lambda:ap-southeast-1:753240598075:layer:LambdaAdapterLayerArm64:<version>`

## Cost expectations

After the stack is up but idle, monthly cost should be **$0**. If you ever see >$0.01 in Cost Explorer, investigate — most likely culprits: CloudWatch Logs ingestion volume, DynamoDB writes from a runaway loop, or accidental egress past the 100 GB free tier.
