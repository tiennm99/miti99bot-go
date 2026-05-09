---
phase: 4
title: "EventBridge cron wiring"
status: pending
priority: P2
effort: "3h"
dependencies: [2]
---

# Phase 04: EventBridge cron wiring

## Overview
Replace the planned Cloud Scheduler design with EventBridge Scheduler. Preserve the existing `/cron/{name}` HTTP route shape inside the Lambda by invoking the Function URL via Scheduler's HTTPS target. Auth via `X-Cron-Token` header sourced from Parameter Store.

## Requirements
- **Functional:** Two scheduled jobs fire on cron expressions matching the GCP plan (`0 17 * * *` for daily push, `0 1 * * *` for cleanup or whatever Phase 09 of GCP plan defined). Both routes execute against the live module dispatcher and complete within Lambda timeout.
- **Non-functional:** Token rotates without code changes (Parameter Store update). Failure retried 2× with exponential backoff. Dead letters logged.

## Architecture
**Decision:** HTTPS target (Function URL) over direct Lambda invoke. **Why:**
- Preserves the existing `/cron/{name}` route + dispatcher code from Phase 03 of GCP plan
- Local dev still works: `curl localhost:8080/cron/dailypush -H "X-Cron-Token: ..."`
- Single ingress path for observability (one URL, one log group)
- Direct invoke would require a separate Lambda entrypoint or routing on event shape — more code, less testable

**Trade-off accepted:** Slightly less AWS-idiomatic; HTTPS adds ~10ms latency vs direct invoke; not material here.

```
EventBridge Scheduler ─cron─► HTTPS POST <function-url>/cron/{name}
                              + Header: X-Cron-Token: <from ParamStore>
                              + AWS Sigv4 NOT used (Function URL AuthType: NONE)
                              │
                              └─► Lambda → router → dispatcher → cron handler
```

**Auth model:** Function URL `AuthType: NONE` (already set in Phase 02 for Telegram). Cron auth = shared-secret header verified server-side. The token lives in Parameter Store (`/miti99bot/prod/cron-token`) and is fetched by Scheduler at invoke time via `SECRETSMANAGER_SECRET` reference (Scheduler supports referencing Parameter Store via `secret reference` in target input transformer, OR plain text in target — for KISS, store the token in Scheduler's invocation HTTP target headers as a templated literal, but referenced from Parameter Store via SAM resource attribute).

**Simpler concrete approach:** SAM template reads the Parameter Store value at deploy time using `{{resolve:ssm-secure:...}}` in the schedule target's HTTP header config. Token rotation = update parameter, redeploy.

## Related Code Files
- Create: SAM resources `Resources.DailyPushSchedule` (AWS::Scheduler::Schedule)
- Create: SAM resources `Resources.CleanupSchedule` (or whichever second cron)
- Create: SAM resource `Resources.SchedulerExecutionRole` with `events:InvokeApiDestination` / equivalent for HTTPS targets (or use built-in `aws.UniversalTarget` for `https`)
- Modify: `internal/server/router.go` — confirm `/cron/{name}` validates `X-Cron-Token` against env-loaded value (currently has `cronAuthHeader = "X-Cron-Token"`, good)
- Modify: `cmd/server/main.go` — load `CRON_TOKEN` env from Parameter Store reference, pass to `Config.CronToken`
- Reference: existing module cron registrations in each module's `Cron()` method

## Implementation Steps
1. Define SAM `AWS::Scheduler::Schedule` for each cron job:
   ```yaml
   DailyPushSchedule:
     Type: AWS::Scheduler::Schedule
     Properties:
       ScheduleExpression: "cron(0 17 * * ? *)"   # 17:00 UTC = 00:00 Saigon
       FlexibleTimeWindow: { Mode: 'OFF' }
       Target:
         Arn: arn:aws:scheduler:::http-invoke
         RoleArn: !GetAtt SchedulerRole.Arn
         Input: '{"name":"dailypush"}'
         HttpParameters:
           HeaderParameters: { X-Cron-Token: '{{resolve:ssm-secure:/miti99bot/prod/cron-token:1}}' }
         RetryPolicy: { MaximumRetryAttempts: 2, MaximumEventAgeInSeconds: 600 }
         DeadLetterConfig: { Arn: !GetAtt CronDLQ.Arn }
       FlexibleTimeWindow: { Mode: OFF }
   ```
   *(Pseudo — confirm exact `aws.HttpInvoke` target syntax against current AWS SAM docs at deploy time; AWS docs note the API surface is evolving.)*
2. Add `CronDLQ` (SQS queue, free tier 1M req/mo).
3. Add `SchedulerRole` IAM with `lambda:InvokeFunctionUrl` (or `events:InvokeApiDestination` if going via API destination).
4. Provision `/miti99bot/prod/cron-token` in Parameter Store with a 32-byte random value (`openssl rand -hex 32`).
5. Verify router rejects requests with wrong/missing token (test exists; confirm).
6. Deploy. From AWS console, "run now" each schedule. Confirm CloudWatch log entry shows successful 200 from Lambda.
7. Wait one full schedule window (or change to `rate(2 minutes)` temporarily) to confirm automatic firing.
8. Restore production cron expressions. Confirm next-fire timestamp.

## Success Criteria
- [ ] Both schedules deploy via SAM
- [ ] Manual "run now" returns HTTP 200 from Function URL
- [ ] Server logs show cron handler executing the right module
- [ ] Wrong/missing token → 401, no module side effects
- [ ] DLQ receives failed invocations on simulated Lambda error
- [ ] Schedule fires automatically once on production cron expression

## Risk Assessment
- **AWS Scheduler HTTPS target maturity** — relatively new feature; if SAM transform doesn't support `aws.HttpInvoke` cleanly, fall back to: Scheduler → SNS → Lambda subscription → existing handler (one extra hop, identical effect). Document fallback in this file.
- **Token in template via `resolve`** — at deploy time the value is fetched and embedded into the schedule target's static config; rotation requires redeploy. If frequent rotation needed, switch to a Lambda authorizer pattern (out of scope for v1).
- **Cron drift / TZ confusion** — EventBridge `cron()` uses UTC by default (matches Cloud Scheduler behavior in GCP plan). Use `?` for day-of-week-or-month constraint per AWS syntax.
- **Cold-start during cron** — first invocation after idle = 1–3s; cron handler logic must complete within Lambda timeout (15s). If a cron handler calls Gemini and exceeds 15s, raise function timeout to 30s (still free).

## Open questions
1. Direct Lambda invoke vs HTTPS target — locked to HTTPS for the reasons above; revisit only if HTTPS proves flaky.
2. Single schedule with dynamic `name` vs one schedule per cron — one per cron is clearer in console; switch to dynamic only if cron count grows past ~5.
3. Cleanup cron (`0 1 * * *`) — confirm what it does in original miti99bot. Likely TTL-style sweep; review and decide if DynamoDB TTL attribute can replace it (eliminates the cron entirely).
