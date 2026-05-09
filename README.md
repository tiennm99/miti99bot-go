# miti99bot-go

Plug-n-play Telegram bot framework in Go, deployed on Google Cloud Run with Firestore + Gemini. Free-tier port of [miti99bot](https://github.com/tiennm99/miti99bot).

## Status

Early scaffolding. See [`plans/260508-2222-go-port-cloud-run/plan.md`](plans/260508-2222-go-port-cloud-run/plan.md) for the full roadmap.

| Phase | What | Status |
|-------|------|--------|
| 01 | GCP setup, Cloud Run baseline | pending |
| 02 | Repo bootstrap + webhook skeleton | **partial** (local pieces done; Cloud Run deploy deferred to Phase 01) |
| 03 | Module framework + KVStore | **done** |
| 04 | Firestore KV + provider abstraction | **done** |
| 05–07 | Module ports (util/misc/wordle/loldle/lolschedule + AI: semantle/doantu/twentyq) | **done** |
| 08+ | Trading, cron wiring, CI/CD, cutover | pending |

## Layout

```
cmd/server/             entrypoint
internal/server/        HTTP routes (/, /webhook, /cron/{name})
internal/telegram/      Telegram webhook + bot wrapper
internal/modules/       Module framework, registry, dispatchers
internal/storage/       KVStore interface, in-memory impl, prefix wrapper
```

## Run locally

```sh
TELEGRAM_BOT_TOKEN=… \
TELEGRAM_WEBHOOK_SECRET=local \
PORT=8080 \
MODULES= \
go run ./cmd/server
```

End-to-end smoke test against a Telegram dev bot requires `ngrok` (local) or a Cloud Run deployment. The dev bot is created manually; token is injected via env vars only.

## Test

```sh
go vet ./...
go test ./...
```

## Build

```sh
docker build -t miti99bot-go .
```

The image is multi-stage (`golang:1.23-alpine` → `gcr.io/distroless/static:nonroot`); resulting image is ~15 MiB.

## License

[Apache-2.0](LICENSE).
