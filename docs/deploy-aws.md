# Deploy: AWS (Lambda + DynamoDB + EventBridge)

This is the production deploy path for `miti99bot`. Strict free-tier targets, region `ap-southeast-1`.

> **First-time setup:** see `aws/README.md`. This doc is for steady-state operations.

## Architecture (one diagram)

```
Telegram ──HTTPS──► Lambda Function URL (AuthType: NONE)
                    └─► AWS Lambda Web Adapter ──► localhost:8080
                                                    └─► Go http.Handler (cmd/server)
                                                          ├─► DynamoDB (KV)
                                                          ├─► Gemini API (AI modules)
                                                          └─► Telegram Bot API (replies)

EventBridge Scheduler ──cron──► HTTPS POST <FunctionURL>/cron/{name}
                                + Header X-Cron-Token (from SSM)
```

## Deploy

### Via GitHub Actions (canonical)

```
git push origin main
```

Triggers `.github/workflows/deploy.yml`:
1. OIDC assume `github-deploy-miti99bot` role
2. `make build-lambda` (Go ARM64 ZIP-ready binary)
3. `sam deploy --template-file template.yaml`
4. Smoke `curl <function-url>/`

### Manual (emergency / staging)

```sh
make build-lambda
make sam-deploy            # uses samconfig.toml defaults
ALERT_EMAIL=you@example.com make sam-deploy   # with budget alert wired
```

## Verify

```sh
make logs SINCE=10m

aws cloudformation describe-stacks --stack-name miti99bot \
  --query "Stacks[0].Outputs[?OutputKey=='FunctionUrl'].OutputValue" --output text

curl -fsSL "$(...)/" | jq .                       # health JSON
```

## Set the Telegram webhook

> `.github/workflows/deploy.yml` auto-runs `setWebhook` + `setMyCommands` after every push to `main`. The snippet below is the break-glass equivalent for manual / out-of-band fixes (e.g. rerun from a workstation when CI is unavailable).

```sh
URL=$(aws cloudformation describe-stacks --stack-name miti99bot \
        --query "Stacks[0].Outputs[?OutputKey=='FunctionUrl'].OutputValue" --output text)
SECRET=$(aws ssm get-parameter --name /miti99bot/prod/telegram-webhook-secret \
        --with-decryption --query 'Parameter.Value' --output text)
TOKEN=$(aws ssm get-parameter --name /miti99bot/prod/telegram-bot-token \
        --with-decryption --query 'Parameter.Value' --output text)

curl -X POST "https://api.telegram.org/bot$TOKEN/setWebhook" \
  -d "url=${URL}webhook" \
  -d "secret_token=$SECRET" \
  -d "drop_pending_updates=false" \
  -d "allowed_updates=[\"message\",\"callback_query\"]"
```

Verify:
```sh
curl "https://api.telegram.org/bot$TOKEN/getWebhookInfo" | jq .
```
Expect: `url` matches Function URL, `pending_update_count` ≈ 0, `last_error_date` empty.

## Rotate secrets

```sh
aws ssm put-parameter --name /miti99bot/prod/telegram-webhook-secret \
  --value "$(openssl rand -hex 32)" --type SecureString --overwrite
# template.yaml uses ":1" version pin; redeploy to pick up the new value:
make sam-deploy
# Then re-run setWebhook (above) with the new secret_token.
```

> The `:1` in `{{resolve:ssm-secure:…:1}}` is the parameter **version** — it pins to the latest version at deploy time, not version 1 forever. To force a refresh after rotation, redeploy.

## Rollback

CloudFormation handles failed deploys: a failing `sam deploy` triggers automatic rollback to the prior version. To roll back a successful-but-bad deploy:

```sh
aws cloudformation update-stack \
  --stack-name miti99bot \
  --use-previous-template \
  --capabilities CAPABILITY_IAM
```

Or redeploy from a known-good commit:
```sh
git checkout <good-sha>
make sam-deploy
```

## Operational checks (daily during 7-day soak)

```sh
# Errors / warnings in last 24h
aws logs filter-log-events --log-group-name /aws/lambda/miti99bot \
  --start-time $(($(date +%s%3N) - 86400000)) \
  --filter-pattern '{ $.level = "ERROR" }' --max-items 20

# Cold start P95
aws logs start-query --log-group-name /aws/lambda/miti99bot \
  --start-time $(($(date +%s) - 86400)) --end-time $(date +%s) \
  --query-string 'filter @type = "REPORT" | stats avg(@initDuration), pct(@initDuration, 95)'

# DynamoDB throttle
aws cloudwatch get-metric-statistics --namespace AWS/DynamoDB \
  --metric-name ThrottledRequests --dimensions Name=TableName,Value=miti99bot-data \
  --statistics Sum --start-time $(date -u -d '24 hours ago' +%FT%TZ) \
  --end-time $(date -u +%FT%TZ) --period 3600

# Current month spend
aws ce get-cost-and-usage --granularity MONTHLY \
  --time-period Start=$(date -u +%Y-%m-01),End=$(date -u +%F) \
  --metrics UnblendedCost
```

## Free-tier guardrails

| Resource | Free | Watch when |
|---|---|---|
| Lambda req / GB-s | 1M / 400k | Past 50% mid-month |
| DynamoDB req | 200M | Past 5% (sign of runaway loop) |
| DynamoDB storage | 25 GiB | Past 100 MiB (suspect leaks) |
| EventBridge invocations | 14M | Past 1k/mo (suspect mis-config) |
| CloudWatch Logs ingest | 5 GB | Past 50% mid-month |
| Egress | 100 GB | Past 1 GB (wildly high) |

A `$1` budget alert at 80%/100% catches all of these via cost-side fallout.
