---
phase: 2
title: "New repo bootstrap + webhook skeleton"
status: pending
priority: P1
effort: "3h"
dependencies: [1]
---

# Phase 02: New repo bootstrap + webhook skeleton

## Overview
Create `miti99bot-go` GitHub repo, init Go module, scaffold HTTP server with `/`, `/webhook`, `/cron/{name}` routes. Wire Telegram webhook secret-token validation. End-to-end test with a dev bot to prove the loop works before any module logic.

## Requirements
- Functional: `POST /webhook` accepts a Telegram update, validates `X-Telegram-Bot-Api-Secret-Token`, replies 200 OK with no-op handler. `GET /` returns 200 "miti99bot-go ok". Unknown routes 404.
- Non-functional: stdlib `net/http` only (no router framework yet — KISS). Static-linked binary, ≤15 MiB. Cold-start ≤500ms target.

## Architecture

```
cmd/server/main.go            ← entrypoint, wires deps + http.ListenAndServe
internal/server/router.go     ← HTTP routes, secret-token middleware
internal/server/health.go     ← GET / handler
internal/telegram/webhook.go  ← /webhook handler, no-op dispatch
internal/telegram/client.go   ← grammY-equivalent: github.com/go-telegram/bot wrapper
go.mod (module github.com/<owner>/miti99bot-go)
Dockerfile                     ← multi-stage, golang:1.23-alpine → gcr.io/distroless/static
.github/workflows/ci.yml       ← go vet + go test + go build (no deploy yet)
README.md
```

Choice of `github.com/go-telegram/bot` (not `go-telegram-bot-api/v5`) — actively maintained, generic-handler API, `bot.MatchTypeCommand`, idiomatic.

## Related Code Files
- Create: `cmd/server/main.go`
- Create: `internal/server/router.go`, `internal/server/health.go`
- Create: `internal/telegram/webhook.go`, `internal/telegram/client.go`
- Create: `Dockerfile`, `.dockerignore`, `.gitignore`
- Create: `.github/workflows/ci.yml`
- Create: `go.mod`, `go.sum`, `README.md`

## Implementation Steps
1. `gh repo create <owner>/miti99bot-go --public --description "Go port of miti99bot for Cloud Run"` (or private — user choice).
2. `git clone` locally. `go mod init github.com/<owner>/miti99bot-go`. Require Go 1.23.
3. Add deps: `go get github.com/go-telegram/bot`.
4. Write `cmd/server/main.go`:
   - Read `PORT`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEBHOOK_SECRET` from env.
   - Construct `*bot.Bot`. Register a single dummy `/ping` command (returns "pong") for smoke test.
   - Build `http.ServeMux`, attach `/`, `/webhook`, `/cron/{name}` handlers.
   - `http.ListenAndServe(":"+port, mux)`.
5. Webhook handler:
   - Reject non-POST → 405.
   - Compare `X-Telegram-Bot-Api-Secret-Token` header to `TELEGRAM_WEBHOOK_SECRET`. Mismatch → 401.
   - Decode JSON `models.Update`, call `b.ProcessUpdate(ctx, &update)`. Return 200.
6. Cron handler stub: returns 200 OK, logs `cron name=<name>`. Real dispatch in Phase 09.
7. Dockerfile multi-stage:
   - Builder: `FROM golang:1.23-alpine`, `CGO_ENABLED=0 go build -ldflags="-s -w" -o /server ./cmd/server`.
   - Runtime: `FROM gcr.io/distroless/static`, `COPY --from=builder /server /`, `ENTRYPOINT ["/server"]`.
8. `.github/workflows/ci.yml`: matrix on Go 1.23, run `go vet ./...`, `go test ./...`, `go build ./...`. No deploy step yet (Phase 10).
9. Local smoke test: `TELEGRAM_BOT_TOKEN=… TELEGRAM_WEBHOOK_SECRET=local PORT=8080 go run ./cmd/server`. Use `ngrok http 8080`, call Telegram `setWebhook` against the dev bot, `/ping` → "pong".
10. Manual deploy to Cloud Run for end-to-end check: `gcloud run deploy miti99bot-go --source=. --region=asia-southeast1 --set-env-vars=… --set-secrets=TELEGRAM_BOT_TOKEN=…,TELEGRAM_WEBHOOK_SECRET=…`. Point dev bot's webhook at the Cloud Run URL. Verify `/ping`.

## Success Criteria
- [ ] Repo exists, CI green
- [ ] `/ping` works against dev bot via Cloud Run URL
- [ ] Secret-token mismatch returns 401
- [ ] Image size ≤20 MiB
- [ ] No-secrets-in-git audit clean (gitleaks or manual scan)

## Risk Assessment
- **Risk**: `gcloud run deploy --source` uses Cloud Build, which has its own free tier (120 build-min/day). **Mitigation**: small Go build is ~30s; well within. CI/CD in Phase 10 may move builds to GHA to keep Cloud Build for fallback.
- **Risk**: `go-telegram/bot` API surface differs from grammY — handler signatures + middleware patterns require relearning. **Mitigation**: stick to common patterns (commands + plain handlers); avoid grammY's plugins/middleware where translation is fuzzy.

## Rollback
Delete repo + Cloud Run service. CF Worker still owns prod webhook so no impact.
