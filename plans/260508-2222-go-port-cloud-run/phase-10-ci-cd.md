---
phase: 10
title: "CI/CD + Dockerfile + Secret Manager"
status: pending
priority: P2
effort: "4h"
dependencies: [2]
---

# Phase 10: CI/CD + Dockerfile + Secret Manager

## Overview
Production-grade build + deploy pipeline. GitHub Actions builds image, pushes to Artifact Registry, deploys to Cloud Run. Secrets pulled from Secret Manager at runtime via Cloud Run's `--set-secrets`. Post-deploy hook runs `setWebhook` + `setMyCommands` against Telegram (replacing JS `scripts/register.js`).

## Requirements
- Functional: PR → CI green; merge to `main` → auto-deploy to Cloud Run; deploy includes Telegram registration.
- Non-functional: build time ≤2 min; no secrets in image, in env yaml, or in repo. Workload Identity Federation between GHA + GCP (no long-lived JSON key).

## Architecture

```
.github/workflows/
├── ci.yml         ← PRs: vet, test, build (no deploy)
└── deploy.yml     ← main: build, push to AR, deploy Cloud Run, register Telegram

cmd/register/main.go         ← Go port of scripts/register.js (setWebhook + setMyCommands)
Dockerfile                    ← finalized multi-stage
firestore.indexes.json        ← composite indexes (Phase 08)
.dockerignore
```

Secret Manager secrets (created in Phase 01, populated here):
- `telegram-bot-token`
- `telegram-webhook-secret`
- `gemini-api-key`

Cloud Run service env (non-secret):
- `MODULES=util,misc,wordle,loldle,loldle-emoji,loldle-quote,loldle-ability,loldle-splash,trading,lolschedule,semantle,doantu,twentyq`
- `GOOGLE_CLOUD_PROJECT`
- `LOG_LEVEL=info`

## Related Code Files
- Create: `.github/workflows/{ci,deploy}.yml`
- Create: `cmd/register/main.go`
- Modify: `Dockerfile` (finalize from Phase 02)
- Create: `infra/cloud-run.yaml` (declarative service spec) OR keep imperative `gcloud run deploy` flags

## Implementation Steps
1. **Workload Identity Federation setup** (one-time):
   - `gcloud iam workload-identity-pools create github-pool --location=global`.
   - `gcloud iam workload-identity-pools providers create-oidc github-provider …`.
   - Bind `roles/iam.workloadIdentityUser` from GHA repo → `miti99bot-deployer` SA.
2. **Dockerfile finalization**:
   - Builder: `FROM golang:1.23-alpine`, install ca-certs, `CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /server ./cmd/server`.
   - Runtime: `FROM gcr.io/distroless/static-debian12:nonroot`, `COPY --from=builder /server /server`, `USER nonroot`, `ENTRYPOINT ["/server"]`.
3. **`ci.yml`**:
   - Triggers: `pull_request`, `push: branches: [main]`.
   - Steps: checkout, setup-go, `go vet ./...`, `go test ./...`, `go build ./...`. (No emulator integration tests in CI — run locally.)
4. **`deploy.yml`**:
   - Trigger: `push: branches: [main]` after `ci` workflow succeeds.
   - Auth via WIF (`google-github-actions/auth@v2`).
   - Build + push: `docker build -t asia-southeast1-docker.pkg.dev/$PROJECT/miti99bot-go/server:$SHA .; docker push …`.
   - Deploy: `gcloud run deploy miti99bot-go --image=… --region=asia-southeast1 --service-account=miti99bot-runtime@… --set-env-vars="MODULES=…,GOOGLE_CLOUD_PROJECT=$PROJECT" --set-secrets="TELEGRAM_BOT_TOKEN=telegram-bot-token:latest,TELEGRAM_WEBHOOK_SECRET=telegram-webhook-secret:latest,GEMINI_API_KEY=gemini-api-key:latest" --min-instances=0 --max-instances=2 --memory=256Mi --cpu=1 --timeout=30s --allow-unauthenticated`.
   - Apply Firestore indexes: `gcloud firestore indexes composite create --collection-group=trading_users --field-config=field-path=pnlVnd,order=descending` (idempotent — errors on already-exists, swallow).
   - Apply Scheduler jobs: invoke `scripts/setup-scheduler.sh` with current Cloud Run URL.
   - Post-deploy: `go run ./cmd/register` reads `MODULES` + Cloud Run URL + bot token from env, calls Telegram `setWebhook` (with secret token) + `setMyCommands` (public commands only). Idempotent.
5. **`cmd/register/main.go`**:
   - Build registry locally (no Firestore — embed an `OfflineKVStore` that no-ops). Walk public commands.
   - HTTP POST to `https://api.telegram.org/bot<TOKEN>/setWebhook` with `{url, secret_token, allowed_updates: ["message"]}`.
   - HTTP POST to `…/setMyCommands` with `{commands: [{command, description}]}`.
   - `--dry-run` flag prints payloads without calling API (parity with JS `register:dry`).
6. **Concurrency-1 lock** on `deploy.yml` to prevent overlapping deploys (Cloud Run handles multiple revisions, but webhook race is annoying).
7. **Smoke after deploy**: GHA waits 10s, curls `/` → expect 200 "miti99bot-go ok"; if not, fail the workflow.

## Success Criteria
- [ ] PR triggers CI, all checks pass
- [ ] Merge → deploy runs end-to-end, Cloud Run revision served
- [ ] No secret values appear in workflow logs
- [ ] Telegram webhook is set after deploy (verify `getWebhookInfo`)
- [ ] `setMyCommands` reflects current `MODULES`
- [ ] Image size ≤30 MiB

## Risk Assessment
- **Risk**: WIF setup is tricky; bad bind → GHA can't auth. **Mitigation**: validate via a manual workflow run before relying on auto-deploy.
- **Risk**: Deploy runs Telegram register before Cloud Run is healthy → Telegram pings new URL, gets 503. **Mitigation**: smoke `/` first, register only after.
- **Risk**: A bad deploy auto-flips webhook to broken revision. **Mitigation**: Cloud Run keeps prior revision; manual `gcloud run services update-traffic` is the rollback. Document in deployment-guide.md.
- **Risk**: Cost spike if `--max-instances` set too high under attack. **Mitigation**: capped at 2 — handles VN-side org load comfortably; raise only if measured.

## Rollback
`gcloud run services update-traffic miti99bot-go --to-revisions=<previous>=100`. Re-register webhook against prev URL not needed (URL is service-level, not revision-level).
