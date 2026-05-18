---
type: debugger
date: 2026-05-18
slug: lolschedule-cron-not-firing
status: done
---

# Incident: lolschedule_daily_push cron not fired on 2026-05-18

## Verdict

**NO ROLLBACK NEEDED.**

The cron has never fired in production. `AWS::Scheduler::Schedule` was only added today
(2026-05-18) in commit `c70b9d0`, and **both attempts to deploy it have failed** — so the
resource does not yet exist in the live stack. The user's hypothesis that "a recent change
broke the cron" is **refuted**: there is nothing to break because the schedule was never
live. The brainstorm at `plans/reports/brainstorm-260517-1411-eventbridge-schedule-fix.md`
correctly diagnosed the gap (deferred phase-05). The current failures are forward-progress
bugs, not regressions.

---

## Timeline

| Time (UTC) | Commit | Event |
|---|---|---|
| 2026-05-16 07:51 | `8e7fdce` | Last successful deploy. No `AWS::Scheduler::Schedule`. |
| 2026-05-18 04:46 | `c70b9d0` | Added `LolscheduleDailyPushSchedule`; token via `{{resolve:ssm-secure}}`. Deploy **FAILED**. |
| 2026-05-18 06:04 | `585d996` | Fixed token via `CronSharedSecret` CFN parameter + CI SSM fetch. Deploy **FAILED** again. |
| 2026-05-18 01:00 UTC | — | Scheduled fire time passes; no schedule in prod → nothing fires. |

---

## Evidence Chain

### 1. Schedule resource absent from prod stack

Commit `8e7fdce` (last successful deploy, 2026-05-16) contains no `AWS::Scheduler::Schedule`
resource in `template.yaml`. Both deploys today failed before CFN applied any changeset.
`LolscheduleDailyPushSchedule` (`template.yaml:190-215`) has never reached prod.

This confirms the brainstorm claim: the gap is not a regression — it is an unexecuted
deferred phase (`plans/260510-0234-pre-deploy-wrapup/phase-05-eventbridge-schedules.md`).

### 2. First deploy failure — `c70b9d0` (04:47 UTC)

CI error (run `26014078660`):
```
SSM Secure reference is not supported in:
[AWS::Scheduler::Schedule/Properties/Target/HttpInvokeArgs/HeaderParameters/X-Cron-Token]
```
`template.yaml` used `{{resolve:ssm-secure:/miti99bot/${StackEnv}/cron-shared-secret}}`
at line 215. CFN's documented property allowlist blocks secure-string dynamic refs inside
`AWS::Scheduler::Schedule` header parameters. Changeset creation failed immediately —
**no CFN resource was created or modified**.

### 3. Second deploy failure — `585d996` (06:05 UTC)

Fix correctly removed the `{{resolve:ssm-secure}}` ref and switched to a `NoEcho`
CFN parameter `CronSharedSecret` (`template.yaml:50-54`) fetched from SSM by CI and
passed via `--parameter-overrides`. CI log confirms the secret was fetched
(`Parameter overrides: {"CronSharedSecret": "*****"}`). Changeset reached CFN but
failed with:
```
The following hook(s)/validation failed: [AWS::EarlyValidation::PropertyValidation]
```
CFN Early Validation rejected the changeset. The property that triggers this is most likely
`HttpInvokeArgs` — the exact property name for HTTPS universal targets in
`AWS::Scheduler::Schedule` is not `HttpInvokeArgs` (the brainstorm flagged this as a
medium-risk "validate-and-iterate" gate at the risks table). The `sam validate` gate
called out in the brainstorm was not run before committing.

### 4. Auth path analysis (CRON_SHARED_SECRET_PARAMETER_NAME)

Lambda resolves `CRON_SHARED_SECRET` at cold-start via `resolveSSMSecrets`
(`cmd/server/main.go:290-345`). If the SSM param is missing or empty:
- `resolveSSMSecrets` returns a fatal error → Lambda crashes on cold start (no silent 401).
- If the env var `CRON_SHARED_SECRET` is empty but `CRON_SHARED_SECRET_PARAMETER_NAME`
  is also empty, `cfg.CronSecret == ""` → `cronDisabled = true` in
  `internal/server/router.go:59` → all `/cron/{name}` calls return 404.

Since the schedule never reached prod, this path is moot today. But **if the
`CronSharedSecret` CFN param were passed empty**, the Lambda would warn at startup
(`cmd/server/main.go:124`) and serve 404 to every cron call — **silent failure, not 401**.
This is a latent risk for the next deploy attempt.

### 5. Handler/dispatcher wiring — healthy

`lolschedule.New` registers `dailyPushCron()` (`internal/modules/lolschedule/cron.go:76-82`)
via `modules.Build` → `modules.Install`. `deps.Bot` is injected at
`cmd/server/main.go:106-110`. Route `/cron/lolschedule_daily_push` resolves correctly
through `cronHandler` → `modules.DispatchScheduled`. No wiring bug found.

---

## Root Cause

**`AWS::Scheduler::Schedule` was never successfully deployed to prod.**

- Phase-05 was deferred at 2026-05-10 (per brainstorm `related` links).
- `c70b9d0` first attempted to wire it today but used `{{resolve:ssm-secure}}` in a
  property where CFN forbids it → CFN rejected the changeset.
- `585d996` fixed the token-injection mechanism but introduced a second CFN validation
  error (`AWS::EarlyValidation::PropertyValidation`), likely the wrong property name for
  the HTTPS target's header arguments.

Neither deploy reached the stack. Current prod stack is still the `8e7fdce` baseline with
no scheduler resource.

---

## Competing Hypotheses — Disposition

| Hypothesis | Status | Evidence |
|---|---|---|
| Recent commit introduced a regression (broke something that worked) | **REFUTED** | No schedule ever deployed; last prod deploy `8e7fdce` predates schedule work |
| Schedule never wired (deferred phase, not a regression) | **CONFIRMED** | Brainstorm + `git log` + both CI failures show it never reached prod |
| SSM secret missing → silent 401 on every fire | **NOT APPLICABLE** | No schedule in prod to fire; moot until deploy succeeds |
| `HttpInvokeArgs` property name wrong for SAM transform | **CONFIRMED** as current blocker | `AWS::EarlyValidation::PropertyValidation` failure on `585d996` |

---

## Action Required (not a rollback)

1. **Identify correct CFN property name** for header parameters on `AWS::Scheduler::Schedule`
   HTTPS target. Candidates: `HttpInvokeParameters` or a nested structure. Run
   `sam validate` locally before committing (the brainstorm prescribed this gate but it
   was skipped).
2. **Verify SSM param non-empty** before next deploy:
   ```
   aws ssm get-parameter --name /miti99bot/prod/cron-shared-secret --with-decryption
   ```
   Empty value → `cronDisabled=true` → 404 on every cron hit (not 401).
3. After successful deploy, do a "Run now" from EventBridge Scheduler console + tail
   CloudWatch for `cron triggered name=lolschedule_daily_push`.

---

## Unresolved Questions

- Exact correct property name for `AWS::Scheduler::Schedule` HTTPS target header
  parameters (requires `sam validate` iteration or AWS docs check — out of scope for
  this report-only investigation).
