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
Flip the production Telegram webhook to the AWS Function URL only after the Cloudflare→AWS migration plan has produced a green parity report and a rehearsed final-delta procedure. Keep GCP code paths in tree (Firestore impl, Cloud Run Dockerfile) but unwired by default.

## Requirements
- **Functional:** Real production bot serves users from Lambda with durable Cloudflare data already migrated or intentionally archived. No regressions vs prior baseline (whatever ran before — JS Worker or partial GCP).
- **Non-functional:** Accept a brief operator-controlled freeze window for the final delta import; no silent data loss. 7-day soak with logs reviewed daily. Fallback path documented.

## Architecture
- **Migration gate:** `plans/260515-2250-cf-data-to-aws-migration/` must finish first; Phase 04 parity report there is the go/no-go input for this phase.
- **Freeze-window cutover:** pause Cloudflare cron/webhook writes, run final delta export/import + verify, then call `setWebhook` to point Telegram at the AWS Function URL with the production webhook secret.
- **Rollback path:** if the final delta verify fails or AWS smoke fails before the first AWS-served write, restore the prior webhook target. After AWS starts accepting new writes, this cutover is forward-fix only unless a reverse-sync path exists.
- **Code:** Firestore impl stays compilable, gated by `KV_PROVIDER=firestore`. Default `KV_PROVIDER=dynamodb` in Lambda env. Cloud Run Dockerfile retained for offline / non-AWS users.

## Related Code Files
- Modify: `README.md` — full rewrite of "Run locally", "Build", new "Deploy to AWS" section, status table updated, link to AWS plan, archive link to GCP plan
- Modify: `cmd/server/main.go` — default `KV_PROVIDER` selection logic: `dynamodb` if `AWS_LAMBDA_FUNCTION_NAME` set, else `memory`
- Create: `docs/deploy-aws.md` — single source of truth for AWS deploy ops (parameter store names, IAM role ARN, smoke commands)
- Modify: `docs/cf-to-aws-migration-runbook.md` — freeze-window delta import + rollback sequence
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
   - [ ] `plans/260515-2250-cf-data-to-aws-migration/phase-04-parity-verification-and-rehearsal.md` passed with a saved green report
   - [ ] Final delta import commands rehearsed during the freeze window
2. Pause Cloudflare writes and run the final delta import + verify.
3. Run `setWebhook` against production bot:
   ```sh
   curl -X POST "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/setWebhook" \
     -d "url=$AWS_FUNCTION_URL/webhook" \
     -d "secret_token=$TELEGRAM_WEBHOOK_SECRET" \
     -d "drop_pending_updates=false" \
     -d "allowed_updates=[\"message\",\"callback_query\"]"
   ```
4. Verify with `getWebhookInfo`:
   ```sh
   curl "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/getWebhookInfo" | jq .
   ```
   Confirm `url`, `pending_update_count` near 0, `last_error_date` empty.
5. Send a test command (`/start`, `/wordle`, `/twentyq` to exercise Gemini path). Confirm responses match prior behavior and expected migrated state is visible.
6. Verify at least one migrated trading account, one existing lolschedule subscriber path, and `/mstats` if `last_ping` was migrated.
7. Soak for 7 days: each morning, check CloudWatch Logs for ERROR / WARN, DynamoDB throttle metrics (should be zero), budget current spend (should be $0), Gemini RPD usage (should be far under cap).
8. After 7-day soak, update README:
   - Status table: AWS port phases marked "done"
   - Replace "Run locally" with two paths: in-memory (no AWS) and DynamoDB Local (with AWS deps)
   - "Deploy" section: link `docs/deploy-aws.md`, drop Cloud Run instructions
   - Status badge / link to `plans/260510-0114-aws-port/`
9. Add a top-of-file note in `plans/260508-2222-go-port-cloud-run/plan.md` redirecting deploy questions to the AWS plan.
10. Tag a release: `git tag v1.0.0-aws -m "AWS deploy default"` and push.

## Success Criteria
- [ ] Telegram webhook `getWebhookInfo` shows AWS Function URL
- [ ] Production bot answers `/start` from real users with normal latency and expected migrated data
- [ ] Final Cloudflare→AWS migration report is green before any CF teardown
- [ ] 7-day soak: zero unrecovered errors, zero throttles, zero unexpected spend
- [ ] README accurately reflects AWS as the default deploy
- [ ] `docs/deploy-aws.md` is sufficient for a fresh dev to redeploy from scratch
- [ ] GCP plan file annotated; old phase files preserved for history
- [ ] Release tag pushed

## Risk Assessment
- **Final import misses writes** if Cloudflare stays writable during cutover — Mitigation: use the migration plan's freeze-window delta import before `setWebhook`, then verify parity again.
- **Latency regression vs JS Worker** — Cloud Run / JS Worker had different cold-start profiles; if users complain, document and consider ARM→x86 swap or provisioned concurrency (kills free tier).
- **Hidden Firestore dependency** still wired in some module — Mitigation: grep for `firestore.NewClient` and confirm all paths are gated by `KV_PROVIDER=firestore` env. Add a CI test that builds with `KV_PROVIDER=dynamodb` and asserts Firestore client is not initialized.
- **GCP project quietly billing** because resources weren't deleted — Mitigation: explicit step in this phase: `gcloud projects delete <project>` OR `gcloud run services delete` for any deployed services. Check Cloud Console for any orphaned resources.
- **Lambda Web Adapter unsupported on a future runtime** — Mitigation: pin LWA layer version, monitor AWS Labs repo.

## Open questions
1. Delete the old GCP project entirely or leave it dormant? Dormant is safe (no GCP free-tier abandonment penalty); delete after 30 days if no regret.
2. Keep Dockerfile in repo? Yes — useful for non-Lambda local runs and as reference for any future Cloud Run revival.
3. Keep Firestore impl forever or drop after 90 days? Drop only if the parity test proves redundant; the impl itself is small and tested.
4. Announce the change anywhere (README badge, release notes) — depends on whether this is a public bot. User decides.
