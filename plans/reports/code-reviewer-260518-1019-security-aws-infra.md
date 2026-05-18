---
type: code-reviewer
scope: infrastructure
date: 2026-05-18
slug: security-aws-infra
status: complete
related:
  - plans/reports/code-reviewer-260518-1019-security-go-app.md
  - plans/reports/researcher-260518-1019-security-dependencies.md
  - plans/260518-1019-iam-least-privilege/ (consumes findings F1, F2)
---

# Security audit — AWS infrastructure (miti99bot)

Adversarial review of `template.yaml`, `.github/workflows/deploy.yml`, `aws/`, `samconfig.toml`. Findings calibrated to project policy: free-tier hard; accepted trade-offs at `docs/deploy-aws-free-tier-guide.md:11-37` not raised.

## Findings

| # | Sev | Location | Title |
|---|---|---|---|
| F1 | **HIGH** | `aws/README.md:67-82` | GHA OIDC role `github-deploy-miti99bot` has 10× `*FullAccess` managed policies incl. `IAMFullAccess` → account takeover from any compromise of trusted ref. |
| F2 | **HIGH** | `aws/iam-github-oidc-trust.json:14-19` | OIDC trust accepts `repo:tiennm99/miti99bot:pull_request` — dormant today (no PR-trigger workflow has `id-token: write`), but any future workflow adding that permission opens a takeover path combined with F1. |
| F3 | **MEDIUM** | `template.yaml:138-144` | Function URL `Cors.AllowOrigins: ["*"]` + `AllowHeaders: ["*"]`. Telegram + Scheduler are server-to-server; CORS is dead code that turns the bot into a browser-side replay target. |
| F4 | **MEDIUM** | `template.yaml:138-140` | `FunctionUrlConfig.AuthType: NONE` requires every Go route to reject unauthenticated callers. Smoke test `curl "$URL/"` at `deploy.yml:73` confirms `/` returns a JSON body to unauthenticated callers — verify body leaks no version/build/env data. Handoff to Go reviewer. |
| F5 | **LOW** | `template.yaml:62-77` | `BotTable` no explicit `SSESpecification`. Default SSE-S3 is implicit since 2018; set explicit for intent + drift resistance. |
| F6 | **MEDIUM** | `template.yaml:163-167` | `CronDLQ` SQS has no resource policy. May hold cron payload with token on failure. Add resource policy: Scheduler `SendMessage` + explicit operator role `ReceiveMessage`; deny non-TLS. |
| F7 | **LOW** | `template.yaml:33-36, 119` | `LambdaAdapterLayerArn` is third-party-published (AWSLabs account `753240598075`). Pinned-version mitigation acknowledged; supply-chain risk documented + accepted. |
| F8 | **LOW** | `template.yaml:91-95` | Log retention 7d acceptable; policy allows secrets in logs. Confirm Go code does not log secret env values (handoff to Go reviewer). |
| F9 | **LOW** | `template.yaml:102-153` | No `ReservedConcurrentExecutions` cap on BotFunction. Add `: 10` as free DoS / cost-amplification guard. |
| F10 | **LOW** | `template.yaml:217-235` | Scheduler `Target.Input` carries the cron secret plain. IAM-gated by `scheduler:GetSchedule`. F1 fix collapses the blast radius (deploy role currently has `AmazonEventBridgeFullAccess`). |
| F11 | **LOW** | `template.yaml:138-140` | `InvokeMode: BUFFERED` — limits responses to 6 MB. Intentional; no issue. |
| F12 | **LOW** | `template.yaml:264-276` | `BotFunctionUrl` in Outputs — discoverable via `cloudformation:DescribeStacks`. URL is public by design; no action. |
| F13 | **INFO** | `template.yaml:62` | `Tracing: Active` enables X-Ray. No public surface; within free tier. |
| F14 | **INFO** | Function URL spec | AWS guarantees HTTPS-only. No HTTP listener exists. |
| F15 | **INFO** | `template.yaml:239-263` | `MonthlyBudget` triggers alerts at 80% / 100% of $1; does not block spend. Working as designed. |
| F16 | **MEDIUM** | `samconfig.toml:16` | Hardcoded `BotOwnerID`, `AdminUserIDs`, `AlertEmail` committed. Move to GHA `--parameter-overrides`. |

## IAM principal-of-least-privilege summary

### Lambda execution role (SAM-auto-generated)
- DynamoDBCrudPolicy: table-scoped — OK
- Inline `ssm:GetParameter*` on `parameter/miti99bot/${StackEnv}/*` — OK
- AWSLambdaBasicExecutionRole, AWSXrayWriteOnlyAccess — OK

### `SchedulerExecutionRole` (in stack)
- Trust on `scheduler.amazonaws.com` (no `aws:SourceAccount` Condition — minor; consider adding for defence in depth)
- `lambda:InvokeFunction` on `BotFunction.Arn` — OK
- `sqs:SendMessage` on `CronDLQ.Arn` — OK

### `github-deploy-miti99bot` — **OVER-PRIVILEGED (F1)**
10× managed full-access policies. Effective: all stacks, all functions, all tables, **all SSM parameters in account**, all queues, all log groups, **all IAM** (incl. `iam:CreateUser`, `iam:AttachUserPolicy` — account takeover), all S3 buckets, all budgets.

Blast radius if OIDC trust is loosened or trusted ref compromised: full AWS account takeover.

### OIDC trust (`aws/iam-github-oidc-trust.json`)
- Federated provider: OK
- `aud`: `sts.amazonaws.com` — OK
- `sub` allowlist: `refs/heads/main`, `refs/heads/dev`, **`pull_request`** — F2

## Public-surface inventory

| Resource | Reachability | Policy alignment |
|---|---|---|
| `BotFunctionUrl` | **Public HTTPS**, `AuthType: NONE`, app-layer token gate | ✅ Designed public surface |
| `BotTable` | IAM-gated | ✅ Not public |
| `BotFunctionLogGroup` | IAM-gated | ✅ Not public |
| `BotFunction` (direct invoke) | IAM-gated | ✅ Only Scheduler role can invoke |
| `CronDLQ` | IAM-gated same-account | ✅ Not public; see F6 |
| `SchedulerExecutionRole` | Assumable by `scheduler.amazonaws.com` only | ✅ |
| `LolscheduleDailyPushSchedule` | IAM-gated control plane | ✅ |
| SSM SecureString params | IAM-gated, AWS-managed KMS | ✅ |
| X-Ray traces | Outbound only | ✅ |

Only Function URL is internet-public. Matches policy.

## Verified non-issues (policy-accepted; do not re-flag)

- Secret in Scheduler `Target.Input` — `docs/deploy-aws-free-tier-guide.md:29`
- Function URL `AuthType: NONE` with app-layer auth — `docs/deploy-aws-free-tier-guide.md:31`
- NoEcho CFN param + CI SSM fetch — `docs/deploy-aws-free-tier-guide.md:30`
- SSM SecureString at cold start (not Secrets Manager) — `docs/deploy-aws-free-tier-guide.md:31`
- PITR disabled on DynamoDB — free-tier rule (`template.yaml:73-74`)
- 7-day log retention — cost/visibility trade-off
- Third-party Lambda layer — pinned-version mitigation, supply-chain accepted

## Recommended action order

1. **F2** (1-line) — drop `pull_request` from OIDC trust.
2. **F1** (significant) — replace `*FullAccess` with stack-scoped custom inline policy. See `plans/260518-1019-iam-least-privilege/` for the implementation plan.
3. **F3** — drop CORS.
4. **F6** — DLQ resource policy.
5. **F9** — `ReservedConcurrentExecutions: 10`.
6. **F5** — explicit `SSESpecification`.
7. **F16** — move samconfig hardcodes to overrides.
8. **F4** — Go reviewer confirms root handler safety (separate handoff).

## Status

DONE — F1 + F2 captured in `plans/260518-1019-iam-least-privilege/`. F3-F16 await separate work.
