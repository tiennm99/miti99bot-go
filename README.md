# miti99bot-go

Plug-n-play Telegram bot framework in Go. **Default deploy: AWS Lambda + DynamoDB + EventBridge Scheduler (free tier).** Cloud Run + Firestore retained as an alt path. Free-tier port of [miti99bot](https://github.com/tiennm99/miti99bot).

## Status

Mid-port. Code is on `main`; first AWS deploy still pending the user's manual AWS-account bootstrap.

| Track | What | Status |
|-------|------|--------|
| Modules | util, misc, wordle, loldle, lolschedule, twentyq, trading | **done** |
| Storage | KVStore interface; in-memory + Firestore + **DynamoDB** providers | **done** |
| AI | Gemini API client (`internal/ai`) | **done** |
| AWS IaC | SAM template + Makefile + GH Actions OIDC deploy | **done** |
| AWS bootstrap | account, IAM OIDC, SSM SecureString params, first `sam deploy` | **manual user steps — see [`aws/README.md`](aws/README.md)** |
| Cron handlers | lolschedule daily push, trading daily refresh | pending |
| Trading module | VN-stocks paper trading | pending |
| Cutover | Telegram webhook flip + 7-day soak | deploy-gated |

Plans:
- Active: [`plans/260510-0234-pre-deploy-wrapup/`](plans/260510-0234-pre-deploy-wrapup/plan.md) — cron handlers + trading + cosmetics
- AWS port: [`plans/260510-0114-aws-port/`](plans/260510-0114-aws-port/plan.md)
- Original GCP plan (historical): [`plans/260508-2222-go-port-cloud-run/`](plans/260508-2222-go-port-cloud-run/plan.md) — module work reused; deploy phases superseded by AWS port

## Layout

```
cmd/server/             entrypoint
internal/server/        HTTP routes (/, /webhook, /cron/{name})
internal/telegram/      Telegram webhook + bot wrapper
internal/modules/       Module framework, registry, dispatchers, modules
internal/storage/       KVStore interface; memory / firestore / dynamodb providers
internal/ai/            Gemini client
template.yaml           AWS SAM IaC (Lambda + Function URL + DynamoDB + Logs + Budget)
docs/deploy-aws.md      AWS deploy operations
aws/README.md           One-time AWS account bootstrap cheatsheet
```

## Run locally

In-memory KV (default; no AWS / no GCP needed):

```sh
TELEGRAM_BOT_TOKEN=… \
TELEGRAM_WEBHOOK_SECRET=local \
PORT=8080 \
MODULES= \
go run ./cmd/server
```

End-to-end smoke test against a Telegram dev bot needs `ngrok` (local) or a deployed Function URL. The dev bot is created manually; token injected via env vars only.

For DynamoDB integration tests:
```sh
make dynamodb-local      # docker run amazon/dynamodb-local on :8001
make test-dynamodb       # runs internal/storage tests against DDB Local
```

For Firestore emulator (legacy):
```sh
make firestore-emulator  # in a second shell
make test-emulator
```

## Test

```sh
make vet              # go vet
make test             # full unit suite (no emulator)
make test-dynamodb    # storage tests against DynamoDB Local (requires Docker)
make test-emulator    # storage tests against Firestore emulator
```

## Deploy

**AWS (canonical):** see [`docs/deploy-aws.md`](docs/deploy-aws.md). Push to `main` → GitHub Actions OIDC → SAM deploy. First-time bootstrap: [`aws/README.md`](aws/README.md).

**Cloud Run (alternative, deferred):** the multi-stage `Dockerfile` builds an image suitable for any container runtime (Cloud Run, Fly.io, ECS Fargate, K8s). Image is `golang:1.25-alpine` → `gcr.io/distroless/static:nonroot`, ~15 MiB.

```sh
docker build -t miti99bot-go .
```

## License

[Apache-2.0](LICENSE).
