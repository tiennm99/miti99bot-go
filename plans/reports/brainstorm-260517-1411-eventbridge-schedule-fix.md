---
type: brainstorm
date: 2026-05-17
slug: eventbridge-schedule-fix
status: approved
related:
  - plans/260510-0234-pre-deploy-wrapup/phase-05-eventbridge-schedules.md (deferred → unblocks)
  - plans/260510-0114-aws-port/phase-04-eventbridge-cron.md (superseded by this design)
  - plans/reports/code-reviewer-260510-0244-cron-and-trading.md
---

# Brainstorm — Cron not firing: fix + deploy

## Problem
`lolschedule_daily_push` cron handler never runs in prod. `/cron/lolschedule_daily_push` route, auth, secret loader, and dispatcher all correct — but no `AWS::Scheduler::Schedule` exists in `template.yaml`. Phase-05 (schedules wiring) was deferred at 2026-05-10 and never executed. Comment at `template.yaml:177` confirms the gap.

## Requirements (locked via Discovery Phase)
- **Expected output:** Production EventBridge schedule fires `lolschedule_daily_push` daily at 01:00 UTC (08:00 ICT). Failures land in `CronDLQ`.
- **Acceptance:**
  - `sam validate` passes.
  - `sam deploy` succeeds in CI on push to `main`.
  - Manual "Run now" → HTTP 200, CloudWatch shows `cron triggered name=lolschedule_daily_push` + `lolschedule daily push complete sent=N`.
  - Wrong/missing `X-Cron-Token` → HTTP 401, `cron rejected reason=secret_mismatch`.
  - Next-day 01:00 UTC auto-fire succeeds.
- **Scope OUT:** trading cron (no handler exists); token rotation automation; timezone change in code; Lambda authorizer / API Gateway.
- **Non-negotiable constraints:** Free-tier; AWS SAM; region `ap-southeast-1`; existing `/cron/{name}` route preserved; no Go code changes.
- **Touchpoints:** `template.yaml` only (single new resource).

## Approaches evaluated

| # | Approach | Verdict |
|---|---|---|
| A | **HTTPS to Function URL via `arn:aws:scheduler:::http-invoke`** | ✅ Chosen. Preserves route + dispatcher + local-dev curl parity. IAM (`lambda:InvokeFunctionUrl` on scheduler.amazonaws.com) already wired at `template.yaml:170`. |
| B | Direct Lambda invoke (`Target.Arn: !GetAtt BotFunction.Arn`) | ❌ Rejected. Requires event-shape branch in `cmd/server/main.go` (Scheduler event ≠ LWA HTTP event). More code, less testable, breaks local-dev parity. |
| C | EventBridge Rule + API Destination | ❌ Rejected. Two extra resources (`AWS::Events::Connection` + `AWS::Events::ApiDestination`) vs one. Older pattern. YAGNI. |

## Final design

Single resource appended to `template.yaml` after `SchedulerExecutionRole` (around line 178):

```yaml
LolscheduleDailyPushSchedule:
  Type: AWS::Scheduler::Schedule
  Properties:
    Name: !Sub "${AWS::StackName}-lolschedule-daily-push"
    ScheduleExpression: "cron(0 1 * * ? *)"       # 01:00 UTC = 08:00 ICT
    ScheduleExpressionTimezone: UTC
    FlexibleTimeWindow: { Mode: OFF }
    State: ENABLED
    Target:
      Arn: arn:aws:scheduler:::http-invoke
      RoleArn: !GetAtt SchedulerExecutionRole.Arn
      Input: "{}"
      RetryPolicy:
        MaximumRetryAttempts: 2
        MaximumEventAgeInSeconds: 600
      DeadLetterConfig:
        Arn: !GetAtt CronDLQ.Arn
      HttpInvokeArgs:                              # ← exact property name needs sam-validate gate
        EndpointUrl: !Sub "${BotFunctionUrl.FunctionUrl}cron/lolschedule_daily_push"
        HttpMethod: POST
        HeaderParameters:
          X-Cron-Token: !Sub "{{resolve:ssm-secure:/miti99bot/${StackEnv}/cron-shared-secret}}"
```

## Implementation considerations
- **Pre-deploy:** verify SSM `/miti99bot/prod/cron-shared-secret` exists and is non-empty (`aws ssm get-parameter ... --with-decryption`). Missing param → `sam deploy` fails loudly on `{{resolve:ssm-secure}}`; empty param → Lambda silently 404s all cron hits (`cmd/server/main.go:124`).
- **Validate locally:** `make sam-validate` before commit. If `HttpInvokeArgs` rejected by transform, iterate property name (candidates: `HttpInvokeParameters`, nested under `HttpParameters`). Plan must absorb this as validate-and-iterate gate.
- **Deploy:** push to `main`. `.github/workflows/deploy.yml` runs `sam deploy` via OIDC.
- **Post-deploy verify:** Console → EventBridge Scheduler → "Run now" → CloudWatch tail. Then synthetic wrong-token POST → 401. Then wait one day for auto-fire.

## Risks
| Risk | Likelihood | Mitigation |
|---|---|---|
| `HttpInvokeArgs` property name wrong for current SAM transform | Med | `sam validate` gate in plan; iterate until accepted |
| SSM cron-shared-secret missing/empty | Low | Explicit pre-deploy `aws ssm get-parameter` check |
| Function URL trailing-slash concat produces double `/` | Low | `!GetAtt … FunctionUrl` returns trailing-slashed; concat is correct. Verify in `sam package` output |
| First scheduled fire misses (wrong timezone) | Low | `ScheduleExpressionTimezone: UTC` explicit; matches code constant |
| Token rotation breaks production | Low | Phase-05 accepted: rotation = SSM update + redeploy |

## Success metrics
- `sam validate` passes; `sam deploy` succeeds in CI.
- "Run now" → 200 + handler log lines within 60s.
- Wrong token → 401 + audit log.
- Auto-fire at 01:00 UTC the day after deploy succeeds.
- DLQ empty under normal ops.

## Next steps
1. Hand off to `/ck:plan` with this report path.
2. Plan must contain: (a) SSM pre-deploy verification step, (b) `template.yaml` edit, (c) `sam validate` gate with property-name iteration, (d) commit + deploy via CI, (e) post-deploy "Run now" + CloudWatch tail, (f) next-day auto-fire confirmation.
3. After phase-05 work lands, mark `plans/260510-0234-pre-deploy-wrapup/phase-05-eventbridge-schedules.md` `status: done`.

## Unresolved questions
None — all four discovery questions answered, exact CFN property name absorbed into plan as a validate-and-iterate gate (user opted for "(Recommended) handoff" over pre-research).
