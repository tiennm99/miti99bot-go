---
phase: 5
title: "Wire EventBridge schedules to live cron handlers"
status: deferred
priority: P3
effort: "30m"
dependencies: [3, 4]
---

> **Status update 2026-05-10:** Deferred to first-deploy decision. Two issues surfaced during Phase 03/04 implementation:
> 1. **Trading module has no cron** in upstream — only one schedule needed (lolschedule_daily_push), not two
> 2. **Lambda Web Adapter only handles HTTP-shape events** — direct Scheduler→Lambda invokes bypass LWA, requiring either an event-shape detector in `main.go` or the HTTPS-target universal-invoke pattern (`arn:aws:scheduler:::http-invoke`, added in 2024)
>
> The HTTPS-target syntax in `AWS::Scheduler::Schedule` needs validation against the deploy-region SAM transform; doing this offline without `sam validate` access risks committing infra that won't deploy. Decision deferred to deploy-time. Once user runs Phase 01 of AWS-port plan and has SAM available, add a single schedule for `lolschedule_daily_push` per the prose below — pick HTTPS or direct invoke based on what `sam validate` accepts.

# Phase 05: Wire EventBridge schedules to live cron handlers

## Overview
With Phases 03 + 04 landed, two cron routes exist (`/cron/lolschedule_daily_push`, `/cron/trading_daily_refresh`). This phase adds concrete `AWS::Scheduler::Schedule` resources to `template.yaml` so AWS Scheduler invokes them on schedule via the existing `SchedulerExecutionRole` + `CronDLQ` already provisioned by AWS-port Phase 01.

## Requirements
- **Functional:** Two schedules deploy via SAM. Each fires at the correct cron expression with `X-Cron-Token` header sourced from Parameter Store. Failures land in `CronDLQ`.
- **Non-functional:** Stays inside EventBridge Scheduler free tier (14M invocations/mo; we use ~60). Token rotation = update SSM param + redeploy (acceptable trade-off).

## Architecture
```
EventBridge Scheduler (rule: 0 1 * * ? *)  ─HTTPS POST─► <FunctionURL>/cron/lolschedule_daily_push
                                              + Headers: X-Cron-Token: {{from SSM}}
                                              + Retry: max 2, max-age 600s
                                              + DLQ: CronDLQ on permanent failure

EventBridge Scheduler (rule: 0 8 * * ? *)  ─HTTPS POST─► <FunctionURL>/cron/trading_daily_refresh
                                              (same auth + retry + DLQ shape)
```

**HTTPS target syntax:** EventBridge Scheduler uses `arn:aws:scheduler:::http-invoke` with `HttpParameters` carrying headers. SAM's `AWS::Scheduler::Schedule` resource passes through to this; no SAM transform magic needed.

## Related Code Files
- Modify: `template.yaml` — append `LolscheduleDailyPushSchedule` + `TradingDailyRefreshSchedule` resources
- Reference (no edit): existing `SchedulerExecutionRole` + `CronDLQ` in `template.yaml`
- Reference: `aws/README.md` (SSM parameter setup for `/miti99bot/prod/cron-shared-secret`)

## Implementation Steps
1. Confirm AWS SDK / CloudFormation supports `aws.HttpInvoke` target via `AWS::Scheduler::Schedule` for the deploy region (`ap-southeast-1`). Check via `aws cloudformation describe-type --type RESOURCE --type-name AWS::Scheduler::Schedule` if uncertain.
2. Append to `template.yaml`:
   ```yaml
   LolscheduleDailyPushSchedule:
     Type: AWS::Scheduler::Schedule
     Properties:
       Name: !Sub "${AWS::StackName}-lolschedule-daily-push"
       ScheduleExpression: "cron(0 1 * * ? *)"   # 01:00 UTC = 08:00 ICT
       FlexibleTimeWindow: { Mode: OFF }
       State: ENABLED
       Target:
         Arn: !GetAtt BotFunction.Arn       # Lambda direct? Or HTTPS? Decide per step 1
         RoleArn: !GetAtt SchedulerExecutionRole.Arn
         RetryPolicy: { MaximumRetryAttempts: 2, MaximumEventAgeInSeconds: 600 }
         DeadLetterConfig: { Arn: !GetAtt CronDLQ.Arn }
         Input: '{"name":"lolschedule_daily_push"}'
         # IF using HTTPS invoke (preferred for route preservation):
         # Replace `Arn: !GetAtt BotFunction.Arn` with the universal target
         # `Arn: arn:aws:scheduler:::http-invoke` and add HttpParameters.

   TradingDailyRefreshSchedule:
     Type: AWS::Scheduler::Schedule
     Properties:
       Name: !Sub "${AWS::StackName}-trading-daily-refresh"
       ScheduleExpression: "cron(0 8 * * ? *)"   # 08:00 UTC = 15:00 ICT (market close)
       FlexibleTimeWindow: { Mode: OFF }
       State: ENABLED
       Target:
         # Same shape as above
         RoleArn: !GetAtt SchedulerExecutionRole.Arn
         RetryPolicy: { MaximumRetryAttempts: 2, MaximumEventAgeInSeconds: 600 }
         DeadLetterConfig: { Arn: !GetAtt CronDLQ.Arn }
         Input: '{"name":"trading_daily_refresh"}'
   ```
3. **Decide direct-invoke vs HTTPS** at implementation time:
   - **HTTPS (preferred):** preserves `/cron/{name}` route; works with existing dispatcher; same shape as local-dev `curl` smoke. Need `HttpParameters` block with `X-Cron-Token` header.
   - **Direct Lambda invoke:** simpler IAM, lower latency, bypasses HTTP layer. Requires a Lambda event-shape branch in `cmd/server/main.go` to detect Scheduler events vs Function URL events.
   - Default: HTTPS for KISS; switch only if HTTPS proves flaky.
4. Validate locally: `make sam-validate` should pass.
5. After AWS-port Phase 01 deploy:
   - Console → EventBridge Scheduler → "Run now" each rule. Confirm 200 from Lambda.
   - Check CloudWatch log group for the cron handler executing.
   - Send a synthetic invocation that fails (wrong token) — confirm DLQ receives the failed message.
6. Watch first scheduled fire from the AWS console (use a temporary `rate(2 minutes)` to verify, then revert).

## Success Criteria
- [ ] Two schedules in `template.yaml`
- [ ] `sam validate` passes
- [ ] Post-deploy: Manual "run now" returns 200 and triggers handler
- [ ] DLQ receives failed invocations (synthetic test)
- [ ] First scheduled fire happens at the correct UTC time

## Risk Assessment
- **`AWS::Scheduler::Schedule` HTTPS-target syntax** still evolving — mitigated by step 1 confirmation and ability to fall back to direct invoke.
- **Token mismatch between SSM and Lambda env** — both resolve at deploy time from the same parameter; no drift unless one is rotated independently.
- **Cron firing before Lambda is deployed** during stack creation — CloudFormation orders dependencies; Schedules `DependsOn: BotFunction` if needed (probably auto from Arn ref).
- **Time-zone confusion** — cron expressions use UTC; verified in comments next to each expression.

## Open questions
1. Direct invoke vs HTTPS — final decision lives here, not Phase 04 of AWS-port plan.
2. Add a third schedule for a manual "ad-hoc" endpoint (e.g. for testing without console)? YAGNI — `aws scheduler invoke-now` works.
3. Schedule `State: ENABLED` vs `DISABLED` initially? ENABLED — first deploy implicitly trusts the cron handlers; if either causes prod issues, disable via console immediately.
