---
phase: 7
title: "Cutover + README + retire GCP paths"
status: pending
priority: P2
effort: "3h"
dependencies: [2, 3, 4, 5, 6]
---

# Phase 07: Cutover + README + retire GCP paths

## Overview
Flip the production Telegram webhook to the AWS Function URL, soak for 7 days, then mark the AWS port as default in README and code defaults. Keep GCP code paths in tree (Firestore impl, Cloud Run Dockerfile) but unwired by default.

## Requirements
- **Functional:** Real production bot serves users from Lambda. No regressions vs prior baseline (whatever ran before — JS Worker or partial GCP).
- **Non-functional:** Zero downtime cutover (Telegram webhook flip is atomic). 7-day soak with logs reviewed daily. Fallback path documented.

## Architecture
- **Cutover op:** `setWebhook` Telegram API call pointing to the AWS Function URL with the production webhook secret.
- **Rollback path:** identical `setWebhook` call to the prior URL; ~5 second op.
- **Code:** Firestore impl stays compilable, gated by `KV_PROVIDER=firestore`. Default `KV_PROVIDER=dynamodb` in Lambda env. Cloud Run Dockerfile retained for offline / non-AWS users.

## Related Code Files
- Modify: `README.md` — full rewrite of "Run locally", "Build", new "Deploy to AWS" section, status table updated, link to AWS plan, archive link to GCP plan
- Modify: `cmd/server/main.go` — default `KV_PROVIDER` selection logic: `dynamodb` if `AWS_LAMBDA_FUNCTION_NAME` set, else `memory`
- Create: `docs/deploy-aws.md` — single source of truth for AWS deploy ops (parameter store names, IAM role ARN, smoke commands)
- Modify: `plans/260508-2222-go-port-cloud-run/plan.md` — top-of-file note: "Deploy phases 01, 09–12 superseded by `plans/260510-0114-aws-port/`. Module work (phases 03–07) reused unchanged."
- Optional remove: `Dockerfile` retained for now; revisit in 30 days
- Optional remove: GCP-specific docs in `docs/` if any (none observed)

## Implementation Steps
1. Pre-flight checklist (run inside this phase):
   - [ ] Phase 02 smoke green (manual curl)
   - [ ] Phase 03 wordle daily state survives deploy + cold start
   - [ ] Phase 04 cron fired at least one real trigger
   - [ ] Phase 05 push-to-main auto-deploys
   - [ ] Phase 06 budget alert email confirmed
   - [ ] Cold-start P95 < 1.5s confirmed
2. Run `setWebhook` against production bot:
   ```sh
   curl -X POST "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/setWebhook" \
     -d "url=$AWS_FUNCTION_URL/webhook" \
     -d "secret_token=$TELEGRAM_WEBHOOK_SECRET" \
     -d "drop_pending_updates=false" \
     -d "allowed_updates=[\"message\",\"callback_query\"]"
   ```
3. Verify with `getWebhookInfo`:
   ```sh
   curl "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/getWebhookInfo" | jq .
   ```
   Confirm `url`, `pending_update_count` near 0, `last_error_date` empty.
4. Send a test command (`/start`, `/wordle`, `/twentyq` to exercise Gemini path). Confirm responses match prior behavior.
5. Soak for 7 days: each morning, check CloudWatch Logs for ERROR / WARN, DynamoDB throttle metrics (should be zero), budget current spend (should be $0), Gemini RPD usage (should be far under cap).
6. After 7-day soak, update README:
   - Status table: AWS port phases marked "done"
   - Replace "Run locally" with two paths: in-memory (no AWS) and DynamoDB Local (with AWS deps)
   - "Deploy" section: link `docs/deploy-aws.md`, drop Cloud Run instructions
   - Status badge / link to `plans/260510-0114-aws-port/`
7. Add a top-of-file note in `plans/260508-2222-go-port-cloud-run/plan.md` redirecting deploy questions to the AWS plan.
8. Tag a release: `git tag v1.0.0-aws -m "AWS deploy default"` and push.

## Success Criteria
- [ ] Telegram webhook `getWebhookInfo` shows AWS Function URL
- [ ] Production bot answers `/start` from real users with normal latency
- [ ] 7-day soak: zero unrecovered errors, zero throttles, zero unexpected spend
- [ ] README accurately reflects AWS as the default deploy
- [ ] `docs/deploy-aws.md` is sufficient for a fresh dev to redeploy from scratch
- [ ] GCP plan file annotated; old phase files preserved for history
- [ ] Release tag pushed

## Risk Assessment
- **Webhook flip causes message loss** during the few-second propagation — Telegram retries failed deliveries automatically; `drop_pending_updates=false` preserves backlog.
- **Latency regression vs JS Worker** — Cloud Run / JS Worker had different cold-start profiles; if users complain, document and consider ARM→x86 swap or provisioned concurrency (kills free tier).
- **Hidden Firestore dependency** still wired in some module — Mitigation: grep for `firestore.NewClient` and confirm all paths are gated by `KV_PROVIDER=firestore` env. Add a CI test that builds with `KV_PROVIDER=dynamodb` and asserts Firestore client is not initialized.
- **GCP project quietly billing** because resources weren't deleted — Mitigation: explicit step in this phase: `gcloud projects delete <project>` OR `gcloud run services delete` for any deployed services. Check Cloud Console for any orphaned resources.
- **Lambda Web Adapter unsupported on a future runtime** — Mitigation: pin LWA layer version, monitor AWS Labs repo.

## Open questions
1. Delete the old GCP project entirely or leave it dormant? Dormant is safe (no GCP free-tier abandonment penalty); delete after 30 days if no regret.
2. Keep Dockerfile in repo? Yes — useful for non-Lambda local runs and as reference for any future Cloud Run revival.
3. Keep Firestore impl forever or drop after 90 days? Drop only if the parity test proves redundant; the impl itself is small and tested.
4. Announce the change anywhere (README badge, release notes) — depends on whether this is a public bot. User decides.
