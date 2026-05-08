---
phase: 12
title: "Cutover + decommission CF Worker"
status: pending
priority: P3
effort: "3h"
dependencies: [10, 11]
---

# Phase 12: Cutover + decommission CF Worker

## Overview
Final phase: flip the prod Telegram webhook from CF Worker to Cloud Run, observe a 7-day soak with both deployments side by side (CF still receives bg cron triggers, Cloud Run owns webhook), then decommission the Worker.

## Requirements
- Functional: prod bot answers from Cloud Run after webhook flip. No commands regress. Daily cron jobs continue running.
- Non-functional: rollback to CF Worker is one Telegram API call away (`setWebhook` back to Worker URL) at any time during the soak.

## Architecture

Cutover sequence:

```
Day 0    test-bot soak passed (Phase 11)
         ↓
Day 0    setWebhook(prod-bot, CloudRunURL)
         ↓                    ↑ rollback: setWebhook(prod-bot, WorkerURL)
Day 0–7  observe error rate, AI quota, Firestore quota
         ↓
Day 7    delete CF Worker, KV namespace, D1 database
         ↓
Day 7    update README to point at miti99bot-go repo
         ↓
Day 7+   journal entry, archive plan
```

Data migration (one-shot, optional):
- Trading: export D1 → transform → write to Firestore. Done before cutover so user balances persist. Script: `cmd/migrate-trading/main.go` reads from D1 export JSON, writes via Firestore client.
- Loldle/wordle/semantle: session state is per-day; users start fresh on Day 0. No migration.
- Twentyq: stateless (history embedded in conversation). No migration.

## Related Code Files
- Create: `cmd/migrate-trading/main.go` (one-shot, optional based on user choice)
- Modify: `README.md` of JS repo — point to Go repo, mark archived
- Modify: `wrangler.toml` — comment out crons, set up auto-disable webhook on next deploy
- Create: `docs/cutover-runbook.md` — sequence + rollback exact commands

## Implementation Steps
1. **Pre-flight checklist** (before flipping webhook):
   - [ ] Phase 11 success criteria all green
   - [ ] Cloud Run service has prod-secret values, not test
   - [ ] Telegram `setMyCommands` reflects production module set
   - [ ] Trading data exported from D1 (if migrating)
2. **Trading data import** (only if user opted in):
   - Export D1: `wrangler d1 execute miti99bot-db --command="SELECT * FROM trading_trades" --json > trades.json`. Repeat for users + holdings tables.
   - Transform script: read JSON, write to Firestore `trading_users/{id}` + subcollections.
   - Verify counts match: `gcloud firestore export` count = D1 row count.
3. **Webhook flip**:
   - `curl -F "url=<CloudRunURL>/webhook" -F "secret_token=<secret>" https://api.telegram.org/bot<token>/setWebhook`.
   - `getWebhookInfo` to confirm.
   - Send a `/info` to prod bot, verify response from Cloud Run (check Cloud Logging).
4. **Soak (Day 0–7)**:
   - Daily check: Firestore quota usage, error rate from Cloud Logging, Gemini RPD usage.
   - User-facing channel for issue reports (existing channel).
   - If criticals → `setWebhook` back to CF Worker URL → diagnose → re-attempt cutover.
5. **Decommission (Day 7)**:
   - `wrangler deployments list` — note current revision (rollback insurance).
   - `wrangler delete miti99bot` — removes service.
   - Drop CF KV namespace + D1 database via dashboard.
   - Remove `wrangler.toml` cron triggers + secrets via `wrangler secret delete`.
   - JS repo: add archive notice to README, push final commit `chore: archive — superseded by miti99bot-go`.
6. **Wrap-up**:
   - Run `/ck:journal` to capture lessons learned.
   - `ck plan archive` on this plan.
   - Mark `260425-1945-mongodb-atlas-migration` plan as superseded (already documented in its frontmatter once this plan is created).

## Success Criteria
- [ ] Prod webhook hits Cloud Run, no commands regress
- [ ] 7-day soak completes without rollback
- [ ] CF resources fully torn down (Worker, KV, D1)
- [ ] JS repo archived, README points to Go repo
- [ ] Free-tier budget unbroken throughout soak (no surprise bills)

## Risk Assessment
- **Risk**: Webhook flip is atomic in Telegram but Cloud Run cold-start delays first replies. **Mitigation**: schedule flip during VN low-traffic hours (3-4am Saigon).
- **Risk**: User complaints about lost game state (loldle/wordle in-flight). **Mitigation**: announce cutover in channel, urge users to finish open games.
- **Risk**: Trading data import has subtle schema mismatch leading to corrupt balances. **Mitigation**: import to a `trading_users_staging` collection first, eyeball-verify a few users, then rename collection (Firestore lacks rename — copy then delete original).
- **Risk**: A pending CF cron at the moment of cutover runs against decommissioned data. **Mitigation**: pause CF crons (set `[triggers] crons = []` and deploy) before flipping webhook. Cloud Scheduler picks up at next interval.
- **Risk**: Telegram caches commands client-side; `setMyCommands` takes minutes to propagate. **Mitigation**: tolerate; cosmetic only.

## Rollback
- **During soak**: `setWebhook(prod-bot, WorkerURL)` reverts. CF Worker still alive.
- **After Day 7 decommission**: rollback requires re-deploying Worker from git history. Document last-known-good wrangler version in cutover-runbook.md before deletion.

## Next Steps
- Archive this plan: `ck plan archive 260508-2222-go-port-cloud-run`
- Run `/ck:journal` for retrospective
- Update `docs/development-roadmap.md` of Go repo with post-cutover priorities (e.g. observability dashboards, additional modules)
