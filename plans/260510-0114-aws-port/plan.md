---
title: "Migrate miti99bot-go from GCP to AWS (Lambda + DynamoDB + EventBridge, free tier)"
description: "Re-target the deploy/runtime layer from Cloud Run + Firestore + Cloud Scheduler to Lambda (Go ZIP + LWA + Function URL) + DynamoDB on-demand + EventBridge Scheduler, region ap-southeast-1, IaC via SAM, CI via GH Actions OIDC. Module code unchanged."
status: in-progress
priority: P2
effort: 3-4d
branch: main
tags: [aws, lambda, dynamodb, eventbridge, sam, port, telegram-bot, free-tier]
created: 2026-05-10
blockedBy: []
blocks: []
supersedes-deploy-of: [260508-2222-go-port-cloud-run]
---

# Plan: AWS port (Lambda + DynamoDB + EventBridge, free tier)

Re-target only the deploy/runtime layer. Module work (Phases 03–07 of GCP plan) is **done and reused unchanged**. The KVStore interface (`internal/storage/`) absorbs the swap; `http.Handler` code (`internal/server/`) is preserved via Lambda Web Adapter.

## Context
- **Why switch:** Strict $0 free-tier goal — DynamoDB 25 GiB / 200M req-mo, EventBridge unlimited rules, 100 GB egress all-region beat Firestore 1 GiB, Cloud Scheduler 3-job cap, GCP NA-only egress. See `plans/reports/research-260510-0021-aws-vs-gcp-greenfield-rethink.md`.
- **Reused as-is:** module framework, registry, dispatcher, Telegram lib, AI clients, all 11 modules, Firestore impl (kept as sibling for parity tests).
- **Replaced:** Cloud Run → Lambda; Firestore → DynamoDB (sibling provider, default switchable via env); Cloud Scheduler → EventBridge Scheduler; Secret Manager → Parameter Store; Artifact Registry → none (ZIP); Cloud Logging → CloudWatch Logs; CF Worker / GCP CI → GH Actions + SAM.

## Locked decisions
- **Compute:** Lambda Go on `provided.al2023`, **ARM64**, ZIP package, binary `bootstrap`, build with `-tags lambda.norpc -ldflags="-s -w"`.
- **HTTP:** Lambda Function URL (`AuthType: NONE`) + AWS Lambda Web Adapter layer → existing `http.Handler` runs unchanged.
- **KV:** DynamoDB single-table `miti99bot`, PK=`pk` (`{module}#{key}`), attr `value` (Binary). On-demand billing.
- **Cron:** EventBridge Scheduler → HTTPS target = Function URL `/cron/{name}` with `X-Cron-Token` header (token in Parameter Store). Preserves existing route shape; alternative (direct Lambda invoke) deferred.
- **Secrets:** SSM Parameter Store SecureString. Names: `/miti99bot/{env}/telegram-token`, `…/webhook-secret`, `…/gemini-api-key`, `…/cron-token`. Fetched at cold start.
- **Region:** `ap-southeast-1` (Singapore).
- **IaC:** AWS SAM (`template.yaml`).
- **CI:** GitHub Actions, OIDC role, `aws-actions/configure-aws-credentials@v4` + `aws-actions/setup-sam@v2`.
- **Logs:** CloudWatch Logs, 7-day retention.
- **Cost guard:** AWS Budgets $1/mo alert.

## Phases

| # | Phase | Status | Effort | Key deliverable |
|---|-------|--------|--------|-----------------|
| 01 | [AWS bootstrap + IAM OIDC + SAM skeleton](phase-01-aws-bootstrap.md) | pending (manual) | 3h | AWS account, OIDC trust, empty SAM stack deployable |
| 02 | [Lambda runtime (Go ZIP + LWA + Function URL)](phase-02-lambda-runtime.md) | code-done; awaits first deploy | 4h | `/` and `/webhook` served from Lambda; secret-token check passes |
| 03 | [DynamoDB KV provider](phase-03-dynamodb-kv.md) | code-done; integration tests skip without DDB Local | 4h | `dynamodb_kv.go` + `dynamodb_provider.go` sibling to Firestore impl, parity tests pass |
| 04 | [EventBridge cron wiring](phase-04-eventbridge-cron.md) | pending (blocked on cron handlers, see 260510-0234-pre-deploy-wrapup) | 3h | Scheduler → `/cron/{name}` with token, two crons firing on schedule |
| 05 | [GitHub Actions deploy (OIDC + SAM)](phase-05-gha-deploy.md) | done | 3h | `deploy.yml` runs on push to `main`, builds + sam deploys idempotently |
| 06 | [Observability + budget alert](phase-06-observability.md) | partial (budget shipped; metric filter in 260510-0234) | 2h | Logs retention set, $1 budget alert, cold-start P95 captured |
| 07 | [Cutover + README + retire GCP paths](phase-07-cutover.md) | pending (deploy-gated) | 3h | Webhook flipped to Function URL, README rewritten, GCP code paths kept but unwired by default |

## Dependency graph
```
01 ──► 02 ──► 03 ──► 04 ──► 05 ──► 06 ──► 07
              └──► 04 ─────►┘
```

## Free-tier budget at peak
| Resource | Cap | Expected | Headroom |
|---|---|---|---|
| Lambda req | 1M/mo | ~30k/mo | 97% |
| Lambda compute | 400k GB-s | <5k | 99% |
| DynamoDB req | 200M/mo | <100k | 99.9% |
| DynamoDB storage | 25 GiB | <50 MiB | 99.8% |
| EventBridge invocations | 14M/mo | ~60 (2 crons × ~30 days) | 99.9% |
| Parameter Store accesses | unlimited (Standard) | <100/cold-start × ~30 starts | n/a |
| Egress | 100 GB/mo | <50 MiB | 99.95% |
| CloudWatch Logs ingest | 5 GB/mo | <500 MiB | 90% |

## Abort criteria
- **Cold-start P95 > 1.5s** sustained: investigate ARM64→x86_64 swap or pre-warm with provisioned concurrency (kills free tier; only if user-facing latency unacceptable).
- **DynamoDB throttle** under normal load: switch to provisioned mode (still free under 25 RCU/WCU).
- **Function URL auth-bypass risk** discovered: switch to API Gateway HTTP API (12-month free, then $1/M).

## Rollback
Until Phase 07 webhook flip, the GCP runtime path remains intact. Per-phase rollback documented in each phase. Phase 07 is the only irreversible cutover step.

## Open questions
1. Direct Lambda invoke for cron vs HTTP loopback via Function URL — final call deferred to Phase 04 implementation.
2. Whether to delete Firestore impl after parity confirmed, or keep as offline test backend permanently.
3. Single SAM stack vs split (data + compute) — start single, split if iteration speed suffers.
