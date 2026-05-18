---
type: debugger
date: 2026-05-18
slug: cfn-http-invoke-unsupported
status: blocker-found
related:
  - plans/reports/debugger-260518-1019-lolschedule-cron-not-firing.md
  - plans/reports/code-reviewer-260518-1019-recent-changes-cron-regression.md
  - plans/reports/brainstorm-260517-1411-eventbridge-schedule-fix.md (Approach A — invalidated)
---

# Debug — CFN does not support HTTPS universal target on AWS::Scheduler::Schedule

## TL;DR

Brainstorm Approach A (`arn:aws:scheduler:::http-invoke` via CloudFormation) is **structurally unimplementable**. CFN spec has no property for HTTP target args (URL/method/headers). Both failed deploys today (`c70b9d0`, `585d996`) hit the same root cause — not a typo on `HttpInvokeArgs`, but a missing schema property entirely.

## Evidence

Official CFN reference [AWS::Scheduler::Schedule Target](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-scheduler-schedule-target.html):

Supported `Target` properties (exhaustive):
- `Arn`, `RoleArn`, `Input`, `RetryPolicy`, `DeadLetterConfig`
- Templated params: `EcsParameters`, `EventBridgeParameters`, `KinesisParameters`, `SageMakerPipelineParameters`, `SqsParameters`

No `HttpInvokeArgs`, no `HttpParameters`, no `HttpInvokeParameters`, no nested HTTP-config block anywhere. The EventBridge Scheduler **API** supports `HttpParameters` (header/query/path), but the **CloudFormation resource** does not surface it.

## Why the brainstorm's "validate-and-iterate gate" couldn't have worked

The brainstorm assumed the property exists under a different name. There is no name to iterate to — the entire HTTPS-universal-target shape is absent from the CFN schema. `sam validate` would have caught this immediately had it been run.

## Verified decision update (per review-audit-self-decision.md §1)

The brainstorm's Approach A locked in by user approval. New data invalidates it: confirmed against two AWS doc pages (Target schema + parent Schedule schema). This is *new* (CFN spec read), not an audit counter-argument. Per rule, surface the reversal to the user with options — do not silently flip.

## Path-forward options

| # | Option | Cost | Risk |
|---|---|---|---|
| **B** | **Direct Lambda invoke** (`Target.Arn: !GetAtt BotFunction.Arn`). Native CFN. Needs Go: handle Scheduler event payload in `cmd/server/main.go` (different shape than LWA HTTP event) and dispatch to cron by `detail`/`Input` content. | Small Go change + module re-wiring; breaks local-curl parity. | Low — well-trodden pattern. |
| **C** | **EventBridge Rule + API Destination + Connection**. Pure CFN; uses `AWS::Events::Connection` + `AWS::Events::ApiDestination` + `AWS::Events::Rule`. Preserves `/cron/{name}` HTTP path, no Go change. | 3 new resources vs 1; older pattern; Connection requires auth config block. | Low. Verified CFN-supported. |
| **D** | **Custom Resource (Lambda-backed)** that calls `scheduler:CreateSchedule` directly with `HttpParameters`. | High — extra Lambda + IAM + lifecycle handling. | Med — drift risk. |
| **E** | **Out-of-band `aws scheduler create-schedule`** in `.github/workflows/deploy.yml` after `sam deploy`. | Low template change; schedule lives outside stack. | Med — drift, manual rotation, no CFN rollback. |

Recommend **C** (no Go change, pure CFN, only cost is 2 extra resources).

## Pre-deploy state right now

Prod stack still at `8e7fdce` (the two attempts today failed at changeset validation, no state mutation). Safe to revert `c70b9d0` + `585d996` from `main` and redesign, OR keep them on a branch and replace.

## Action items (require user decision)

1. **Reverse brainstorm Approach A.** Choose B / C / D / E.
2. If **C**: revert `c70b9d0` + `585d996` on `main`, rewrite `template.yaml` block as `Connection + ApiDestination + Rule`.
3. If **B**: revert same, plus small Go change in `cmd/server/main.go` to discriminate event shapes.
4. Re-run `sam validate` locally before any commit (the gate that got skipped both times).

## Unresolved questions

- Does user accept the small Go change (Approach B) for cleaner single-resource CFN, or prefer Approach C's pure-CFN-no-Go-change at the cost of 3 resources?
- Should the two failed-deploy commits be reverted or amended on the same branch?

**Status:** DONE
