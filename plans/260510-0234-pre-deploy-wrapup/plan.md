---
title: "Pre-deploy wrap-up: cron handlers + trading + cosmetics"
description: "Cloud-agnostic Go work + small SAM additions to land before Phase 01 AWS bootstrap. Outputs feed directly into AWS-port plan's Phase 04 + 06 + 07 verification."
status: in-progress
priority: P2
effort: 8h
branch: main
tags: [aws, modules, lolschedule, trading, observability, readme]
created: 2026-05-10
blockedBy: []
blocks: [260510-0114-aws-port]
---

# Plan: Pre-deploy wrap-up

Five focused phases that finish all **non-deploy** remaining work. Designed to land *before* the user runs Phase 01 of the AWS-port plan (manual AWS account + first `sam deploy`). After this plan ships, AWS-port Phases 04, 06, 07 become genuinely meaningful (real cron handlers, real metric data, accurate README at cutover).

## Why these five

From the punch-list:
1. **Cosmetics** — README still GCP-flavored; AWS-port plan statuses say "pending" for code that already shipped
2. **Metric filter** — small additive SAM change; trivial to land now
3. **lolschedule daily-push cron** — was deferred from GCP plan; without it Phase 04 schedules nothing real
4. **Trading module** — biggest pending Go chunk; cloud-agnostic; from old GCP Phase 08
5. **EventBridge schedules** — wires the new cron handlers to AWS Scheduler

## Phases

| # | Phase | Status | Effort | Key deliverable |
|---|-------|--------|--------|-----------------|
| 01 | [Cosmetics: README + plan status sync](phase-01-cosmetics.md) | done | 30m | README rewritten for AWS default; AWS-port phases marked code-done |
| 02 | [Cold-start metric filter](phase-02-metric-filter.md) | done | 30m | `AWS::Logs::MetricFilter` for `Init Duration` in `template.yaml` |
| 03 | [lolschedule daily-push cron](phase-03-lolschedule-cron.md) | done | 3h | `Crons()` registered; Deps exposes bot for fan-out; daily push at 08:00 ICT |
| 04 | [Trading module port](phase-04-trading-module.md) | done (scope-trimmed: no daily refresh cron, no leaderboard — neither in upstream) | 4h | VN-stocks paper trading: topup/buy/sell/stats/convert; KBS price source |
| 05 | [Wire EventBridge schedules](phase-05-eventbridge-schedules.md) | pending | 1h | `AWS::Scheduler::Schedule` HTTPS-invoke for `lolschedule_daily_push` — design locked by brainstorm 2026-05-17 |

## Dependency graph
```
01 ──┐                    (README + status — independent)
02 ──┤                    (metric filter — independent)
03 ──┐
     ├──► 05              (schedules need real handlers from 03 + 04)
04 ──┘
```

01 and 02 can ship in any order, including parallel. 05 blocks on 03 + 04 having registered crons.

## Relation to other plans
- Builds on: `plans/260510-0114-aws-port/` (the offline artifacts already shipped)
- Carried-over from `plans/260508-2222-go-port-cloud-run/` Phase 08 (trading) — that phase is fulfilled by this plan's Phase 04
- After this lands, AWS-port Phase 04 (EventBridge) and Phase 06 (metric capture) become verifiable on first deploy

## Out of scope (explicit non-goals)
- AWS account creation / IAM OIDC / first `sam deploy` (= AWS-port Phase 01, user-manual)
- Telegram webhook flip (= AWS-port Phase 07, deploy-gated)
- 7-day soak observations (= AWS-port Phase 07)
- Provisioned concurrency, DynamoDB TTL, X-Ray dashboard customization (YAGNI for v1)

## Abort criteria
- Phase 03 hits architectural friction extending `modules.Deps` to expose `*bot.Bot` cleanly → split into a smaller Deps refactor PR first, defer cron handler.
- Phase 04 trading API source unavailable / paywalled → stub the data layer, mark module disabled by default.

## Open questions
1. Daily-push timezone: ICT 08:00 = UTC 01:00 — confirm cron expression matches in Phase 03.
2. Trading data source: original miti99bot uses VN stocks API — confirm it's still free + accessible in Phase 04.
3. Should Phase 01 (README) wait until trading module is done, so the README can advertise it? Tradeoff: ship docs sooner vs. ship complete picture. Default: ship now, update README when trading lands.
