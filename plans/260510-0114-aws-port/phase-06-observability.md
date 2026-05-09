---
phase: 6
title: "Observability + budget alert"
status: pending
priority: P2
effort: "2h"
dependencies: [5]
---

# Phase 06: Observability + budget alert

## Overview
Wire CloudWatch Logs retention, metric filters for key counters, AWS Budgets $1/mo alert, and capture cold-start P95 baseline for the abort criterion in `plan.md`.

## Requirements
- **Functional:** Log retention 7 days. Budget alert fires at $1.00 actual. Cold-start P95 measurable from logs.
- **Non-functional:** All observability stays in free tier (5 GB log ingest/mo, 10 custom metrics free, 1k AWS Budgets API ops free).

## Architecture
- **Logs:** Lambda's auto-created log group `/aws/lambda/miti99bot-aws-port-BotFunction-*`. SAM sets retention.
- **Metrics:** Existing `internal/metrics` package emits counters. Lambda env can ship them via stdout — CloudWatch Logs ingests, log-based metric filters extract `request.duration`, `module.dispatched`, `cron.fired`. Free metric filter quota: unlimited filters, paid for resulting metrics past 10/mo.
- **Budget:** `AWS::Budgets::Budget` in SAM template, threshold $1, email alert.
- **Cold start:** parse `REPORT` log lines → `Init Duration` field → P50/P95/P99.

## Related Code Files
- Modify: `template.yaml` — add `LogRetentionInDays: 7` on `BotFunction` (SAM `LoggingConfig`); add `AWS::Budgets::Budget` resource; add `AWS::Logs::MetricFilter` for key metrics
- Create: `aws/dashboards/cold-start-coldwatch.json` (optional, manual import)
- Reference: `internal/log/*.go` (slog JSON emitter — already exists)
- Reference: `internal/metrics/*.go` (counters + 60s flush — already exists)

## Implementation Steps
1. Add to `template.yaml` under `BotFunction.Properties`:
   ```yaml
   LoggingConfig:
     LogFormat: JSON
     ApplicationLogLevel: INFO
     SystemLogLevel: WARN
     LogGroup: !Ref BotFunctionLogGroup
   ```
2. Add explicit log group to control retention:
   ```yaml
   BotFunctionLogGroup:
     Type: AWS::Logs::LogGroup
     Properties:
       LogGroupName: /aws/lambda/miti99bot-aws-port-bot
       RetentionInDays: 7
   ```
3. Add metric filter for cold start:
   ```yaml
   ColdStartFilter:
     Type: AWS::Logs::MetricFilter
     Properties:
       LogGroupName: !Ref BotFunctionLogGroup
       FilterPattern: '[report="REPORT", ..., init_label="Init", init_dur_label="Duration:", init_dur, ...]'
       MetricTransformations:
         - MetricName: ColdStartInitDuration
           MetricNamespace: miti99bot
           MetricValue: $init_dur
   ```
4. Add `AWS::Budgets::Budget` (sends email at 80% and 100% of $1):
   ```yaml
   MonthlyBudget:
     Type: AWS::Budgets::Budget
     Properties:
       Budget:
         BudgetName: miti99bot-monthly
         BudgetLimit: { Amount: '1.00', Unit: 'USD' }
         TimeUnit: MONTHLY
         BudgetType: COST
       NotificationsWithSubscribers:
         - Notification: { ComparisonOperator: GREATER_THAN, NotificationType: ACTUAL, Threshold: 80, ThresholdType: PERCENTAGE }
           Subscribers: [{ Address: <email>, SubscriptionType: EMAIL }]
   ```
5. Deploy. Trigger cold start (`aws lambda update-function-configuration --function-name … --environment 'Variables={…,FORCE_RESTART=$(date +%s)}'`).
6. Capture 50 cold starts manually or via a one-shot load script (`hey -n 50 -c 1 -i 30s <function-url>/`) — concurrency=1 with delay forces fresh inits. Compute P95 from CloudWatch Insights:
   ```
   filter @type = "REPORT"
   | stats avg(@initDuration), pct(@initDuration, 95)
   ```
7. Record P95 in `plan.md`'s "Free-tier budget at peak" or as an addendum here.
8. Confirm budget shows up in AWS Console > Budgets and has the email subscriber.

## Success Criteria
- [ ] Log group `RetentionInDays: 7` set
- [ ] Cold-start P95 captured and < 1.5s (per abort criterion)
- [ ] Budget alert visible in console, email subscriber confirmed (test mail received)
- [ ] No log group accumulates >500 MiB after 7 days of normal traffic
- [ ] CloudWatch Insights query for cold start works without manual setup

## Risk Assessment
- **Email subscriber not confirmed** → first alert silently dropped — Mitigation: send a test from console before relying on it.
- **Log retention deleted by SAM redeploy** if log group not explicit → Mitigation: declare log group explicitly (step 2).
- **Cold start drift** as deps grow — Mitigation: re-measure quarterly; add a CI step that fails if `bootstrap` binary > 30 MiB.
- **Budget delays** (AWS Budgets evaluates ~3× day, not real-time) → Mitigation: also enable Cost Anomaly Detection (free) for spike alerts.

## Open questions
1. Email vs SNS topic for budget alerts? Email is simpler; SNS lets fan-out to webhook later. Start with email, migrate if needed.
2. Custom CloudWatch dashboard? Skip for v1 — Insights queries are enough for solo-dev.
3. Trace via AWS X-Ray? `Tracing: Active` already set in Phase 02 globals — free at this volume; review traces post-Phase 07.
