---
phase: 2
title: "Lambda runtime (Go ZIP + LWA + Function URL)"
status: pending
priority: P1
effort: "4h"
dependencies: [1]
---

# Phase 02: Lambda runtime (Go ZIP + LWA + Function URL)

## Overview
Make the existing Go HTTP server run as a Lambda behind a Function URL with zero handler-code changes, using AWS Lambda Web Adapter. Routes `/` (healthcheck) and `/webhook` work end-to-end with secret verification.

## Requirements
- **Functional:** Function URL responds to `GET /` with the existing health JSON, and to `POST /webhook` with the existing Telegram dispatcher logic. Secret-token verification (`X-Telegram-Bot-Api-Secret-Token`) preserved.
- **Non-functional:** Cold start P95 < 1.5s for ARM64 Go ZIP. Memory 256 MiB. Timeout 15s. Binary size <30 MiB.

## Architecture
```
Telegram ──HTTPS──► Function URL ──► Lambda runtime
                                     │
                                     ├── LWA layer (extension) translates Lambda event → HTTP
                                     │   └── localhost:8080 (LWA listens here)
                                     │
                                     └── bootstrap binary (existing Go server)
                                         starts http.ListenAndServe(":8080", ...)
                                         dispatcher → modules → DynamoDB / Gemini
```

LWA is added as a Lambda layer; binary just runs `http.ListenAndServe` — no Lambda SDK import required.

## Related Code Files
- Create: `cmd/server/lambda.go` (build-tag `lambda`, sets `PORT=8080` defaults; minimal — possibly empty)
- Modify: `cmd/server/main.go` — accept `PORT` env (likely already does), confirm graceful shutdown on `SIGTERM` (LWA sends it on shutdown)
- Modify: `template.yaml` — wire `BotFunction` properly:
  - `CodeUri: build/` (ZIP staging)
  - `Handler: bootstrap`
  - `Layers: [arn:aws:lambda:ap-southeast-1:753240598075:layer:LambdaAdapterLayerArm64:<latest>]`
  - `Environment.Variables`: `AWS_LAMBDA_EXEC_WRAPPER=/opt/bootstrap`, `PORT=8080`, `READINESS_CHECK_PATH=/`, `MODULES=util,misc,wordle,...`, `TELEGRAM_BOT_TOKEN={{resolve:ssm-secure:...}}`, etc.
- Modify: `Makefile` — add `build-lambda` target: `GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -ldflags="-s -w" -o build/bootstrap ./cmd/server && chmod +x build/bootstrap`
- Reference: `internal/server/router.go` (unchanged)
- Reference: `internal/telegram/*.go` (unchanged)

## Implementation Steps
1. Confirm `cmd/server/main.go` reads `PORT` env (it does per inspection). Confirm graceful shutdown on `SIGTERM`/`SIGINT`.
2. Add `build-lambda` Makefile target. Test locally: `make build-lambda && file build/bootstrap` shows ARM64 ELF.
3. Pick latest LWA layer ARN for `ap-southeast-1` ARM64 — pin major version in `template.yaml` with comment linking to release notes.
4. Add Function env vars in `template.yaml`. Use `{{resolve:ssm-secure:...}}` for secrets so values never appear in template. Reference Phase 01's Parameter Store names.
5. Wire DynamoDB IAM permissions (read/write on table) via `Policies: - DynamoDBCrudPolicy`. Wire SSM read perms via `SSMParameterReadPolicy`.
6. Build + deploy: `sam build && sam deploy`. Tail logs: `sam logs --tail`.
7. Smoke test:
   - `curl <function-url>/` → 200 with health JSON
   - `curl -XPOST <function-url>/webhook -H "X-Telegram-Bot-Api-Secret-Token: wrong"` → 401
   - `curl -XPOST <function-url>/webhook -H "X-Telegram-Bot-Api-Secret-Token: <real>" -d '{"update_id":1,"message":{"text":"/start","chat":{"id":1},"from":{"id":1}}}'` → 200 (or expected dispatcher response)
8. Set Telegram dev-bot webhook to Function URL. Send `/start` from real client. Confirm response in chat.
9. Capture cold-start P95 from CloudWatch Logs `Init Duration` field over 20+ invocations (use Powertools or grep). Record in this phase's "Risks" if >1s.

## Success Criteria
- [ ] `make build-lambda` produces ARM64 binary <30 MiB
- [ ] `sam deploy` updates `BotFunction` successfully
- [ ] `curl <function-url>/` returns health JSON
- [ ] Wrong webhook secret → 401
- [ ] Correct webhook secret + valid update → dispatcher responds
- [ ] Telegram dev bot exchanges messages end-to-end via Function URL
- [ ] Cold start P95 < 1.5s

## Risk Assessment
- **LWA cold-start tax** adds ~100ms — acceptable; if not, fall back to `lambda.Start()` adapter path (rewrite handler, more invasive).
- **`{{resolve:ssm-secure:...}}` requires CloudFormation perms** — SAM handles this if role has `ssm:GetParameter*`.
- **Webhook secret leakage in logs** — confirm `internal/server/router.go` does not log header value; if it does, redact.
- **Binary too large** (>50 MiB unzipped) — strip with `-ldflags="-s -w"` (already done); if still too big, audit deps with `go build -ldflags="-s -w" -trimpath` + `goweight`.
- **ARM64 incompatibility** with any cgo dep — confirm `CGO_ENABLED=0` in build (existing Dockerfile does this).

## Open questions
1. Should LWA `READINESS_CHECK_PATH` be `/` or a dedicated `/healthz`? `/` works since handler is cheap; revisit if `/` ever does work.
2. Telegram delivery reliability with cold-start 1–3s — acceptable in practice (Telegram retries), but document in README.
3. Provisioned concurrency to eliminate cold start — kills free tier, defer indefinitely.
