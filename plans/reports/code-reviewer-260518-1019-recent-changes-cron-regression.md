---
type: code-review
date: 2026-05-18
slug: recent-changes-cron-regression
status: final
related:
  - plans/reports/brainstorm-260517-1411-eventbridge-schedule-fix.md
---

# Audit — Recent commits vs `lolschedule_daily_push` cron

## TL;DR

**Verdict: No regression. The cron was never wired in the first place.**

The brainstorm at `plans/reports/brainstorm-260517-1411-eventbridge-schedule-fix.md` is correct: the `AWS::Scheduler::Schedule` resource did not exist on `main` until commit `c70b9d0` (2026-05-18 11:46 +0700). Every commit in the user's "suspicion window" (`1f5f304` → `8e7fdce`) ran against a stack with zero EventBridge schedules. There was nothing for them to break.

Two commits *after* the suspicion window (`c70b9d0`, `585d996`) execute the brainstorm. They are the path forward — not the cause of an outage.

**Recommendation: PROCEED-WITH-FIX. Do not roll back.** The fix is already on `main` (`c70b9d0` + `585d996`). What remains is deploy + post-deploy verification per the brainstorm's "Next steps".

---

## Per-commit verdicts

| Hash | Subject | Verdict |
|---|---|---|
| `585d996` | fix(deploy): pass cron secret via CFN parameter, not ssm-secure resolve | INNOCENT (corrects c70b9d0; required for `sam deploy` to succeed) |
| `c70b9d0` | feat(deploy): wire EventBridge schedule for lolschedule daily push | INNOCENT (this is the fix — adds the missing schedule) |
| `8e7fdce` | feat(misc): /trongtruonghop disclaimer command | INNOCENT (touches `internal/modules/misc` only) |
| `3402ca9` | refactor(plans): code-review audit reports | INNOCENT (docs-only, `plans/reports/*`) |
| `a8ed67a` | refactor: audit-driven hygiene pass | INNOCENT — see analysis below |
| `3a12615` | fix(reply): forward `message_thread_id` | INNOCENT (no cron-path edits; `lolschedule/handlers.go` change is `/lolschedule` command, not push) |
| `3235ee8` | ci: bump golangci-lint-action v9 | INNOCENT (CI lint runner only) |
| `1e794d1` | ci: bump actions to Node 24-native | INNOCENT (CI runner versions only) |
| `4c81dd6` | fix(dispatcher): match `/cmd@botname` | INNOCENT — see analysis below |
| `1f5f304` | ci(deploy): auto-register Telegram webhook + commands after deploy | INNOCENT (post-deploy curl to Telegram setWebhook/setMyCommands; does not touch Scheduler) |

---

## Closer look at the two commits with the largest blast radius

### `a8ed67a` — audit-driven hygiene pass

This is the only commit in the window that touches cron-relevant Go files (`cmd/server/main.go`, `internal/server/router.go`, `internal/server/log_middleware.go`, `internal/modules/lolschedule/cron.go`). Every change reviewed:

- **`cmd/server/main.go`: WriteTimeout 6m → 75s.** Lambda runtime ignores `http.Server` timeouts (Function URL closes the connection on its own 30s budget). Local-only effect. Cron handler internal deadline is `defaultCronTimeout` (60s) — well inside 75s. NOT a regression.
- **`internal/server/router.go` cron handler:** rejection branches now `w.WriteHeader(401|404|405)` instead of `http.Error(..., "text", ...)`. Status codes preserved (EventBridge Scheduler only inspects status). Happy-path still returns `200`. Inner `recover()` wraps `DispatchScheduled` so a panic becomes a logged 500 instead of an opaque middleware-level recover. Cron *name* extraction (`strings.TrimPrefix(r.URL.Path, "/cron/")`) and regex (`^[a-z0-9_]{1,32}$`) unchanged. NOT a regression.
- **`internal/server/log_middleware.go`:** moves the `req` log line into a `defer` and adds `recoverPanicStatus`. Purely additive observability; the chain `LogRequests → cronHandler` is unchanged. NOT a regression.
- **`internal/modules/lolschedule/cron.go`:** adds `isTerminalSendError` + `pruneDeadSubscribers` after the existing fan-out. `dailyPushCronName = "lolschedule_daily_push"`, `dailyPushSchedule = "0 1 * * *"`, `dailyPushHandler`, `runDailyPush` all preserved. Pruning is best-effort and runs *after* the user-visible send loop — it cannot cause non-delivery. NOT a regression.

Red-team checks that came back clean:
- `cronAuthHeader` constant name and value (`X-Cron-Token`) — unchanged.
- `cronNameRe` regex — unchanged.
- `defaultCronTimeout` — unchanged.
- `modules.Deps.Bot` field — present (`internal/modules/module.go:74`); `BuildOptions.Bot: b` wiring in `cmd/server/main.go:108` — present; `dailyPushHandler` nil-check at `cron.go:88` — present.
- `cfg.CronSecret` flow → `server.New(... CronSecret: cfg.CronSecret)` → `cronHandler(reg, secret)` — present (`cmd/server/main.go:131`).

### `4c81dd6` — dispatcher match-func swap

Touches `internal/modules/dispatcher.go` and `internal/telegram/webhook.go`. Both are on the **Telegram update path**, not the `/cron/{name}` HTTP path. `modules.DispatchScheduled` lives in a separate file (`internal/modules/cron_dispatcher.go`) and uses the cron registry, not the bot-handler registry. The cron HTTP route in `internal/server/router.go:109` calls `modules.DispatchScheduled` directly — it never touches `bot.RegisterHandlerMatchFunc`. NOT a regression for cron.

---

## Module-registry sanity check

- `cmd/server/main.go:43` registers `"lolschedule": lolschedule.New` in the factory map.
- `template.yaml:16` and `samconfig.toml:16` both include `lolschedule` in `ModulesCSV` default. The deployed stack will instantiate the module.
- `internal/modules/lolschedule/lolschedule.go` registers `dailyPushCron()`, which goes into `reg.Crons()` (logged at startup: `crons N`).

The cron handler will fire when invoked. The only missing piece was the schedule, now added.

---

## Recommendation

**PROCEED-WITH-FIX. No rollback.**

Action plan, in order:
1. Confirm `c70b9d0` + `585d996` are on `main` (they are, as of `git log` at audit time).
2. Verify SSM parameter `/miti99bot/prod/cron-shared-secret` exists and is non-empty before next deploy: `aws ssm get-parameter --name /miti99bot/prod/cron-shared-secret --with-decryption`.
3. Push triggers `.github/workflows/deploy.yml` → fetches secret → `sam deploy` with `CronSharedSecret=...` parameter override.
4. Post-deploy: AWS Console → EventBridge Scheduler → `${StackName}-lolschedule-daily-push` → **Run now**. Tail CloudWatch for `cron triggered name=lolschedule_daily_push` + `lolschedule daily push complete sent=N`.
5. Synthetic 401: `curl -X POST -H 'X-Cron-Token: wrong' <FunctionUrl>cron/lolschedule_daily_push` → expect HTTP 401 + log `cron rejected reason=secret_mismatch`.
6. Wait for the next 01:00 UTC fire and confirm auto-delivery.

If step 3 fails with a CFN error on `HttpInvokeArgs`, the brainstorm calls out a property-name iteration gate (candidates: `HttpInvokeParameters`, `HttpParameters`). The current `HttpInvokeArgs` is correct for current SAM transform per AWS docs at time of writing — but if it rejects, iterate.

---

## Red-team angle — risks the brainstorm did not call out

| Risk | Severity | Note |
|---|---|---|
| `CronSharedSecret` default is `""` in `template.yaml:53`. If CI step that fetches the SSM secret runs but `aws ssm get-parameter` returns empty, `set -euo pipefail` will catch unset; but a *blank* SecureString value would pass through. Lambda would then start with `CRON_SHARED_SECRET=""` and `cronHandler` returns 404 on every request (`router.go:cronDisabled`). | Med | Add pre-deploy guard: `[ -n "$CRON_SECRET" ] \|\| { echo "empty cron secret"; exit 1; }` in the deploy step. |
| EventBridge Scheduler IAM role uses `Resource: !GetAtt BotFunction.Arn` for `lambda:InvokeFunctionUrl`. AWS docs require the **Function URL ARN** form (`...:function:name`) — `GetAtt BotFunction.Arn` returns exactly that, so this is fine. Cross-check during `sam validate`. | Low | Brainstorm got this right. |
| Function URL `AuthType: NONE` is assumed (the cron handler does its own header-based auth). If `AuthType: AWS_IAM` were set, the scheduler invocation would 403 even with correct header. Verify in `template.yaml` `BotFunctionUrl` resource. | Low | Already in brainstorm scope as "preserve route". |
| `Input: "{}"` on the schedule: the Go cron handler does not parse a body, so any JSON-or-empty is fine. Confirmed by reading `cronHandler` in `router.go`. | None | — |

---

## Files cross-checked

- `template.yaml` (lines 14–22 ModulesCSV; 40–55 CronSharedSecret param; 160–215 SchedulerRole + LolscheduleDailyPushSchedule)
- `internal/modules/lolschedule/cron.go:1-145`
- `internal/modules/lolschedule/lolschedule.go`
- `internal/modules/module.go:67-80` (Deps + BuildOptions)
- `internal/modules/cron_dispatcher.go:12-30` (DispatchScheduled)
- `internal/server/router.go:55-120` (cronHandler)
- `internal/server/log_middleware.go`
- `cmd/server/main.go:43, 100-135, 270-285` (factories, BuildOptions, server.Config)
- `samconfig.toml:16`
- `.github/workflows/deploy.yml` deploy step

## Unresolved questions

- Has `sam deploy` actually been run since `585d996` landed? If not, the schedule does not yet exist in the deployed CloudFormation stack regardless of `main` state. CI on `main` push should handle it; confirm CI run for `585d996` succeeded.
- Is there pre-deploy validation that `aws ssm get-parameter` returned a non-empty value? (See red-team table row 1.) Worth adding a one-line guard.
