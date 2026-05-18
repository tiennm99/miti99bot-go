---
phase: 5
title: "Wire EventBridge schedule for lolschedule_daily_push"
status: pending
priority: P1
effort: "1h"
dependencies: [3]
---

> **Status update 2026-05-18:** Reactivated from `deferred`. Brainstorm 2026-05-17 locked in all four design decisions; this phase is now executable. See `plans/reports/brainstorm-260517-1411-eventbridge-schedule-fix.md`.

# Phase 05: Wire EventBridge schedule for lolschedule_daily_push

## Context
- Brainstorm: `plans/reports/brainstorm-260517-1411-eventbridge-schedule-fix.md`
- Diagnosis: `/cron/lolschedule_daily_push` route + auth + dispatcher all work; no `AWS::Scheduler::Schedule` resource exists in `template.yaml` (gap noted at `template.yaml:177`).
- Cron handler: `internal/modules/lolschedule/cron.go:53-82` (`dailyPushCronName = "lolschedule_daily_push"`, schedule `0 1 * * *` UTC = 08:00 ICT).
- Trading module has no cron handler — only one schedule needed.

## Overview
Add one `AWS::Scheduler::Schedule` resource to `template.yaml` that invokes the Lambda Function URL via EventBridge Scheduler HTTPS-target (`arn:aws:scheduler:::http-invoke`), with `X-Cron-Token` header sourced from SSM Parameter Store via `{{resolve:ssm-secure}}`. Verify the SSM param exists pre-deploy; verify auto-fire post-deploy via Console "Run now" + CloudWatch.

## Requirements
- **Functional:** Schedule fires daily at 01:00 UTC. Lambda receives POST with valid `X-Cron-Token`. Handler logs `cron triggered name=lolschedule_daily_push` + `lolschedule daily push complete`. Failures retried 2× with 600s max age. Permanent failures land in `CronDLQ`.
- **Non-functional:** Stays in EventBridge Scheduler free tier (~30 invocations/mo vs 14M limit). Token rotation = SSM update + redeploy (accepted trade-off).

## Architecture
```
EventBridge Scheduler        ─cron(0 1 * * ? *) UTC─►   arn:aws:scheduler:::http-invoke
  SchedulerExecutionRole                                  POST <FunctionURL>cron/lolschedule_daily_push
  RetryPolicy: 2 attempts, 600s                            Header: X-Cron-Token: {{resolve:ssm-secure:.../cron-shared-secret}}
  DLQ: CronDLQ                                             Body: "{}"
                                                              │
                                                              ▼
                                                       Lambda (existing route)
                                                       router → dispatcher → dailyPushHandler
```

IAM `lambda:InvokeFunctionUrl` already granted to `scheduler.amazonaws.com` at `template.yaml:170` — no IAM changes.

## Related Code Files
- Modify: `template.yaml` — append single `AWS::Scheduler::Schedule` resource after `SchedulerExecutionRole` (~line 178), inside existing `# --- Cron ---` block.
- Reference (no edit): existing `SchedulerExecutionRole`, `CronDLQ`, `BotFunctionUrl` output in `template.yaml`.
- Reference (no edit): `aws/README.md` §2 — SSM cron-shared-secret setup.

## Implementation Steps

### 1. Pre-deploy: verify SSM secret exists
```sh
aws ssm get-parameter --name /miti99bot/prod/cron-shared-secret \
  --with-decryption --region ap-southeast-1 \
  --query 'Parameter.Value' --output text
```
Must return a non-empty value. If missing or empty:
```sh
openssl rand -hex 32 | xargs -I{} aws ssm put-parameter \
  --name /miti99bot/prod/cron-shared-secret \
  --value {} --type SecureString --region ap-southeast-1
```
Rationale: `cmd/server/main.go:124` silently disables `/cron/*` (404 all hits) when `CRON_SHARED_SECRET` is empty. `{{resolve:ssm-secure}}` in template fails `sam deploy` loudly if the parameter is missing.

### 2. Append to `template.yaml` (after `SchedulerExecutionRole`, before `# --- Cost guard ---`)
```yaml
  LolscheduleDailyPushSchedule:
    Type: AWS::Scheduler::Schedule
    Properties:
      Name: !Sub "${AWS::StackName}-lolschedule-daily-push"
      ScheduleExpression: "cron(0 1 * * ? *)"   # 01:00 UTC = 08:00 ICT
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
        HttpInvokeArgs:
          EndpointUrl: !Sub "${BotFunctionUrl.FunctionUrl}cron/lolschedule_daily_push"
          HttpMethod: POST
          HeaderParameters:
            X-Cron-Token: !Sub "{{resolve:ssm-secure:/miti99bot/${StackEnv}/cron-shared-secret}}"
```

### 3. Local validate (sam-validate gate)
```sh
make sam-validate
```
If `HttpInvokeArgs` is rejected by current SAM transform, iterate property name:
- Candidate A: `HttpParameters` (older EventBridge Rules shape, may apply)
- Candidate B: nested under `Target.HttpInvokeParameters`
- Candidate C: top-level `Target.HttpInvocationConfig`
- Last resort: switch to `AWS::Events::Connection` + `AWS::Events::ApiDestination` + `AWS::Events::Rule` (three resources instead of one; documented well).

Verify URL trailing-slash: `!GetAtt BotFunctionUrl.FunctionUrl` returns `https://….on.aws/`; concatenation `${...}cron/lolschedule_daily_push` yields a clean single-slash path. Confirm in `sam package` output if uncertain.

### 4. Commit + deploy via CI
- Commit message: `feat(deploy): wire EventBridge schedule for lolschedule daily push`
- Push to `main` → `.github/workflows/deploy.yml` runs `sam deploy` via OIDC.
- Watch the workflow run for `LolscheduleDailyPushSchedule` CREATE_COMPLETE in CloudFormation output.

### 5. Post-deploy: verify schedule fires correctly
1. AWS Console → EventBridge → Scheduler → `miti99bot-lolschedule-daily-push` → **Run now**.
2. CloudWatch Logs `/aws/lambda/miti99bot` → expect within 60s:
   - `cron triggered route=/cron name=lolschedule_daily_push`
   - `lolschedule daily push complete subscribers=N sent=N failed=0 pruned=0`
3. Synthetic 401 test:
   ```sh
   URL=$(aws cloudformation describe-stacks --stack-name miti99bot \
     --query "Stacks[0].Outputs[?OutputKey=='FunctionUrl'].OutputValue" --output text)
   curl -i -X POST "${URL}cron/lolschedule_daily_push" -H "X-Cron-Token: wrong"
   ```
   Expect HTTP 401 + log line `cron rejected reason=secret_mismatch`.
4. DLQ sanity check: `aws sqs get-queue-attributes --queue-url <CronDLQ url> --attribute-names ApproximateNumberOfMessages` — must be 0 after the successful "Run now".

### 6. Next-day auto-fire check
At 01:01 UTC the day after deploy, re-tail CloudWatch — confirm the scheduled fire occurred. Mark phase `status: done` only after this passes.

## Todo
- [ ] Step 1: Verify (or create) `/miti99bot/prod/cron-shared-secret` SSM param
- [ ] Step 2: Append `LolscheduleDailyPushSchedule` resource to `template.yaml`
- [ ] Step 3: `make sam-validate` passes (iterate property name if rejected)
- [ ] Step 4: Commit + push to `main`; CI deploy succeeds
- [ ] Step 5a: Console "Run now" → handler logs in CloudWatch within 60s
- [ ] Step 5b: Wrong-token curl → 401 + audit log
- [ ] Step 5c: DLQ empty
- [ ] Step 6: Next-day 01:00 UTC auto-fire observed → mark phase done

## Success Criteria
- [ ] One new resource in `template.yaml`, zero Go/IAM/secret changes
- [ ] `sam validate` passes
- [ ] CI deploy succeeds without manual intervention
- [ ] "Run now" returns 200 and triggers handler within 60s
- [ ] Wrong/missing `X-Cron-Token` → 401, no handler invocation
- [ ] DLQ remains empty under normal operation
- [ ] Next scheduled fire (01:00 UTC) executes automatically

## Risk Assessment
| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| `HttpInvokeArgs` property name wrong for current SAM transform | Med | Deploy fails | `sam validate` gate locally before commit; iterate property name (see Step 3 candidates) |
| SSM `cron-shared-secret` missing or empty | Low | Deploy fails OR Lambda silently 404s | Pre-deploy verification step (Step 1) |
| Function URL concat double-slash | Low | 404 from Lambda | URL ends `/`, path starts `cron/` (no leading slash) — single slash by construction; confirm in `sam package` output |
| Token rotation breaks production | Low | Cron stops firing until redeploy | Phase-05 brainstorm accepted: rotation = SSM update + redeploy. Document in `aws/README.md` if not already |
| First scheduled fire misses (timezone) | Low | Cron fires 7h early or late | `ScheduleExpressionTimezone: UTC` explicit; matches code constant; documented in inline comment |
| Cold start exceeds 30s timeout during cron | Low | First fire times out, 2 retries | Retry policy covers; if persistent, increase Globals.Function.Timeout from 30s |

## Security Considerations
- `X-Cron-Token` is a shared secret embedded into the schedule definition via `{{resolve:ssm-secure}}` at deploy time. The resolved value is visible in the EventBridge Scheduler console (Target → Headers) to anyone with `scheduler:GetSchedule` IAM. Accepted: same blast radius as the Lambda env var holding the same secret.
- Function URL is `AuthType: NONE` — anyone can hit `/cron/lolschedule_daily_push` with the right token. Constant-time compare at `internal/server/router.go:76` prevents timing-attack leakage.
- DLQ contents may contain the request body (`"{}"`, no sensitive data) but DO contain the failed-invocation metadata. SQS queue is private to the AWS account.

## Next Steps
- After Step 6 passes, mark this phase `status: done` and update parent `plan.md` Phase 5 row.
- This closes the `260510-0234-pre-deploy-wrapup` plan (Phases 01-04 already done).
- Unblocks `260510-0114-aws-port` Phase 04 verification (which becomes a no-op now that this phase delivers the same outcome via the same shape).

## Open Questions
None — brainstorm closed all four discovery items. The only remaining open item is the `HttpInvokeArgs` property-name validation, absorbed into Step 3 as a validate-and-iterate gate.
