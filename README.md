# miti99bot

Plug-n-play Telegram bot framework in Go. Runs on AWS Lambda + DynamoDB + EventBridge Scheduler. Strictly free-tier.

## Modules

| Module | What it does |
|---|---|
| `util` | `/help`, `/info`, `/stickerid` |
| `misc` | Coin flip, dice, RNG utilities |
| `wordle` | Daily Wordle game |
| `loldle` | League-of-Legends "guess the champion" |
| `lolschedule` | Pro-match schedule + daily push |
| `twentyq` | 20-questions game (requires Gemini API key) |
| `trading` | VN-stocks paper trading |

Disable any module by editing `MODULES` in `template.yaml`.

## Layout

```
cmd/server/             entrypoint
internal/server/        HTTP routes (/, /webhook, /cron/{name})
internal/telegram/      Telegram webhook + bot wrapper
internal/modules/       Module framework, registry, dispatchers, modules
internal/storage/       KVStore interface; memory + dynamodb providers
internal/ai/            Gemini client (used by twentyq)
template.yaml           AWS SAM IaC (Lambda + Function URL + DynamoDB + Logs + Budget)
docs/deploy-aws-free-tier-guide.md   Full onboarding guide
docs/deploy-aws.md                   Steady-state operations
aws/README.md                        One-time AWS account setup
```

## Run locally

In-memory KV (no AWS required):

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

## Test

```sh
make vet              # go vet
make test             # full unit suite (no emulator)
make test-dynamodb    # storage tests against DynamoDB Local (requires Docker)
```

## Deploy

First-time onboarding: see [`docs/deploy-aws-free-tier-guide.md`](docs/deploy-aws-free-tier-guide.md).

Steady-state operations: [`docs/deploy-aws.md`](docs/deploy-aws.md).

After the initial setup, every push to `main` triggers `.github/workflows/deploy.yml` (GitHub Actions OIDC → SAM deploy). No long-lived AWS keys.

## License

[Apache-2.0](LICENSE).
