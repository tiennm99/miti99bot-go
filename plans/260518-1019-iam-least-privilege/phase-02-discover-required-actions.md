---
phase: 2
title: "Discover required actions"
status: pending
priority: P1
effort: "1-2h"
dependencies: []
---

# Phase 2: Discover required actions

## Overview

Enumerate every IAM action `sam deploy` invokes for the miti99bot stack, scoped to the specific resources in `template.yaml`. Output is a documented action × resource table that Phase 3 translates into JSON policy statements.

A missing action here = pipeline broken on next push (per F1 risk highlight). A too-broad action = doesn't satisfy least-privilege intent. Bias toward enumerating real call-sites over generic SAM-deploy guides.

**Inventory MUST be fully populated before Phase 3 begins** (RT-1). The "Action Inventory" section below cannot ship with `TBD` placeholders. Phase 3's blocking dependency on this phase requires a complete table.

### Categories that are easy to miss — explicit mandatory checks (RT-10, RT-13)

For each AWS service in `template.yaml`, you MUST enumerate:

1. **Lifecycle:** Create / Update / Delete / Get / List actions on each resource ARN.
2. **Tagging:** `*:TagResource` / `*:UntagResource` / `*:ListTagsForResource` (or service-specific equivalents — `dynamodb:TagResource`, `lambda:TagResource`, `logs:TagLogGroup`/`TagResource`, `sqs:TagQueue`, `scheduler:TagResource`, `iam:TagRole`, `iam:UntagRole`, `iam:ListRoleTags`). CFN applies tags on every CREATE and many UPDATE paths — missing one = guaranteed UPDATE failure.
3. **Sub-resources** for Lambda specifically: `lambda:CreateFunctionUrlConfig`, `lambda:UpdateFunctionUrlConfig`, `lambda:DeleteFunctionUrlConfig`, `lambda:GetFunctionUrlConfig`, `lambda:AddPermission`, `lambda:RemovePermission`, `lambda:GetPolicy`.
4. **Rollback path:** `cloudformation:ContinueUpdateRollback`, `cloudformation:CancelUpdateStack`, `cloudformation:RollbackStack` (RT-3 — without these, a stuck stack cannot be recovered without re-attaching FullAccess).
5. **SAM-managed S3 bucket bootstrap** (RT-13): `s3:CreateBucket`, `s3:GetBucketLocation`, `s3:GetBucketVersioning`, `s3:PutBucketVersioning`, `s3:GetEncryptionConfiguration`, `s3:PutEncryptionConfiguration`, `s3:PutBucketPolicy`, `s3:ListBucket`, `s3:PutObject`, `s3:GetObject`, `s3:DeleteObject`. SAM creates the bucket on first deploy when `resolve_s3 = true` (`samconfig.toml:13`).

## Requirements

**Functional**
- Cover every AWS API call the deploy workflow makes from start (`actions/checkout`) to end (`setMyCommands`).
- Group calls by service. For each: action name, resource ARN pattern, optional Conditions.
- Cover both happy-path (CREATE) and update-path (UPDATE_IN_PROGRESS → COMPLETE) and rollback (UPDATE_ROLLBACK_*) — IAM checks all three on a failed deploy.

**Non-functional**
- Output lives in the phase file's "Action inventory" section so Phase 3 reads directly from here.
- No AWS calls in this phase — pure code reading.

## Architecture

Read `template.yaml` resource-by-resource; for each `Type: AWS::*::*`, look up which IAM actions CFN issues. Cross-reference with the workflow's explicit `aws` CLI calls (`aws ssm get-parameter`, `aws cloudformation describe-stacks`).

## Related Code Files

- Read: `template.yaml` (every Resources entry + Outputs)
- Read: `.github/workflows/deploy.yml` (steps post-`configure-aws-credentials` that issue AWS calls)
- Read: `samconfig.toml` (`resolve_s3 = true` → SAM manages an artifact bucket)
- No files modified in this phase. Output is appended to this phase doc.

## Implementation Steps

1. **Service inventory.** From `template.yaml` resource types:
   - `AWS::DynamoDB::Table`
   - `AWS::Logs::LogGroup`, `AWS::Logs::MetricFilter`
   - `AWS::Serverless::Function` (expands to `AWS::Lambda::Function` + `AWS::IAM::Role` + `AWS::Lambda::Url` + permissions)
   - `AWS::SQS::Queue`
   - `AWS::IAM::Role`
   - `AWS::Scheduler::Schedule`
   - `AWS::Budgets::Budget` (Conditional)
   - Plus framework: CloudFormation (changesets), S3 (artifact bucket), STS (caller identity).

2. **For each resource, enumerate the IAM actions CFN calls on UPDATE+ROLLBACK paths.** Sources: AWS CFN per-resource documentation (the IAM permissions table at the top of each page). Don't trust memory — verify against docs.

3. **Workflow-explicit calls:**
   - `aws cloudformation describe-stacks` → `cloudformation:DescribeStacks`
   - `aws ssm get-parameter --with-decryption` → `ssm:GetParameter` + the SSM service-managed KMS key has implicit access (no extra IAM action required for the AWS-owned key path)
   - SAM internal: `cloudformation:CreateChangeSet` / `DescribeChangeSet` / `ExecuteChangeSet` / `DeleteChangeSet`, `cloudformation:DescribeStackEvents` / `ListStackResources` / `GetTemplateSummary`, S3 multipart upload, `sts:GetCallerIdentity` (SAM probes account/region on startup).

4. **iam:PassRole identification.** SAM creates an execution role for `BotFunction` and an inline role for the SchedulerExecutionRole. Both need `iam:PassRole` so CFN can attach them to the Lambda / Scheduler. Scope: roles whose path or name match `miti99bot*`.

5. **Resource ARN patterns.** Use `miti99bot*` (not `miti99bot`) on stack-scoped resources so a future `miti99bot-dev` parallel stack works (RT-14). For each action, write the tightest ARN pattern still passing on a fresh deploy:
   - Stack: `arn:aws:cloudformation:ap-southeast-1:225603493174:stack/miti99bot*/*`
   - ChangeSet: `arn:aws:cloudformation:ap-southeast-1:225603493174:changeSet/*/miti99bot*/*`
   - DynamoDB: `arn:aws:dynamodb:ap-southeast-1:225603493174:table/miti99bot*` (covers `miti99bot-data` + future dev `miti99bot-dev-data`)
   - Lambda function: `arn:aws:lambda:ap-southeast-1:225603493174:function:miti99bot*`
   - Lambda layer (read-only ref to AWSLabs adapter): `arn:aws:lambda:ap-southeast-1:753240598075:layer:LambdaAdapterLayerArm64:*`
   - SQS queue: `arn:aws:sqs:ap-southeast-1:225603493174:miti99bot*`
   - IAM roles created by stack: `arn:aws:iam::225603493174:role/miti99bot*` (NOTE: the deploy role itself is `github-deploy-miti99bot` — does NOT match because IAM globs are left-anchored)
   - Log group: `arn:aws:logs:ap-southeast-1:225603493174:log-group:/aws/lambda/miti99bot*`
   - SSM parameter: `arn:aws:ssm:ap-southeast-1:225603493174:parameter/miti99bot/*/*` (`/prod/*` AND `/dev/*` — covers both envs without widening to other apps)
   - Budget: `arn:aws:budgets::225603493174:budget/miti99bot*`
   - Scheduler: `arn:aws:scheduler:ap-southeast-1:225603493174:schedule/*/miti99bot*` (wildcard the GroupName segment — `template.yaml:217-228` does not set `GroupName`; EventBridge defaults to `default` today but pinning to `default` is undocumented contract → RT-11)
   - SAM S3 bucket: **discover at execution time** — see step 5a below (RT-13)

5a. **Discover the actual SAM artifact bucket** (RT-13). Do not assume the `aws-sam-cli-managed-default-samclisourcebucket-*` convention is stable. Run:
   ```sh
   aws s3 ls --profile admin | grep -E 'sam|miti99bot' || \
   aws cloudformation describe-stacks --stack-name aws-sam-cli-managed-default \
     --query "Stacks[0].Outputs[?OutputKey=='SourceBucket'].OutputValue" --output text --profile admin
   ```
   Use the discovered name verbatim in the Phase 3 ARN list. If empty (fresh account), the policy must include `s3:CreateBucket` on `arn:aws:s3:::aws-sam-cli-managed-default-*` so SAM can create it on first deploy.

6. **Wildcard-required actions.** Some IAM actions have no resource-level support (must use `Resource: "*"`). Document each:
   - `sts:GetCallerIdentity` — always `*`
   - `cloudformation:ListStacks` (used by SAM during a deploy if it scans) — `*`
   - `s3:ListAllMyBuckets` (SAM uses this to find the managed bucket if not configured) — `*` (low risk: read-only across all buckets, no data exposed)

7. **Output: Action × Resource inventory.** Append a table to this phase doc with columns: Service · Action · Resource ARN · Notes. Phase 3 consumes this verbatim.

## Action Inventory

> Filled during execution. Phase 3 reads from here. **No TBD allowed** — Phase 3 is blocked until every category below has actual actions listed (RT-1).

Each category MUST include: lifecycle actions, tagging actions, sub-resource actions (if applicable), rollback-path actions (if applicable). See "Categories that are easy to miss" in Overview above.

### CloudFormation
- Stack lifecycle: `cloudformation:CreateStack`, `UpdateStack`, `DeleteStack`, `DescribeStacks`, `DescribeStackEvents`, `DescribeStackResources`, `ListStackResources`, `GetTemplate`, `GetTemplateSummary`, `ValidateTemplate`
- ChangeSet: `cloudformation:CreateChangeSet`, `ExecuteChangeSet`, `DescribeChangeSet`, `DeleteChangeSet`, `ListChangeSets`
- Rollback (RT-3): `cloudformation:ContinueUpdateRollback`, `CancelUpdateStack`, `RollbackStack`
- Tagging: `cloudformation:TagResource`, `UntagResource`, `ListStackResources`
- Global read: `cloudformation:ListStacks` (no resource-level support)

### S3 (SAM artifact bucket)
- Bucket lifecycle (RT-13): `s3:CreateBucket`, `GetBucketLocation`, `GetBucketVersioning`, `PutBucketVersioning`, `GetEncryptionConfiguration`, `PutEncryptionConfiguration`, `GetBucketPolicy`, `PutBucketPolicy`
- Objects: `s3:ListBucket`, `PutObject`, `GetObject`, `DeleteObject`, `PutObjectTagging`
- Global read (justified): `s3:ListAllMyBuckets` (no resource-level support; needed by SAM CLI to find the managed bucket on first run)

### IAM
- Role lifecycle: `iam:CreateRole`, `DeleteRole`, `GetRole`, `ListRoles`, `PutRolePolicy`, `DeleteRolePolicy`, `GetRolePolicy`, `ListRolePolicies`, `AttachRolePolicy`, `DetachRolePolicy`, `ListAttachedRolePolicies`
- **NOT included** (RT-2): `iam:UpdateAssumeRolePolicy` — CFN never invokes this on stack-managed roles (trust changes go via Delete+Create). Including it enables trust-rewrite escalation.
- PassRole: `iam:PassRole` (with `iam:PassedToService` Condition — see Phase 3, RT-7)
- AttachRolePolicy Condition (RT-6): scope `iam:PolicyARN` to AWS-managed policies the stack actually attaches (currently none — Lambda execution role uses SAM macros that inline policies, not attach managed; if SAM ever changes, add specific ARNs). Best path: omit `AttachRolePolicy` entirely until proven necessary.
- Tagging: `iam:TagRole`, `UntagRole`, `ListRoleTags`

### Lambda
- Function lifecycle: `lambda:CreateFunction`, `UpdateFunctionCode`, `UpdateFunctionConfiguration`, `GetFunction`, `GetFunctionConfiguration`, `DeleteFunction`, `PublishVersion`, `ListVersionsByFunction`
- Function URL sub-resource (RT-10): `lambda:CreateFunctionUrlConfig`, `UpdateFunctionUrlConfig`, `DeleteFunctionUrlConfig`, `GetFunctionUrlConfig`
- Resource-based policy: `lambda:AddPermission`, `RemovePermission`, `GetPolicy`
- Layer read (cross-account, AWSLabs): `lambda:GetLayerVersion`
- Tagging: `lambda:TagResource`, `UntagResource`, `ListTags`

### DynamoDB
- Table lifecycle: `dynamodb:CreateTable`, `UpdateTable`, `DescribeTable`, `DeleteTable`, `ListTables`
- Tagging: `dynamodb:TagResource`, `UntagResource`, `ListTagsOfResource`
- (No data-plane actions for deploy role; Lambda execution role has those separately.)

### EventBridge Scheduler
- Schedule lifecycle: `scheduler:CreateSchedule`, `UpdateSchedule`, `GetSchedule`, `DeleteSchedule`, `ListSchedules`
- Tagging: `scheduler:TagResource`, `UntagResource`, `ListTagsForResource`

### SQS
- Queue lifecycle: `sqs:CreateQueue`, `DeleteQueue`, `GetQueueAttributes`, `SetQueueAttributes`, `GetQueueUrl`, `ListQueues`
- Tagging: `sqs:TagQueue`, `UntagQueue`, `ListQueueTags`

### CloudWatch Logs
- Log group lifecycle: `logs:CreateLogGroup`, `DeleteLogGroup`, `DescribeLogGroups`, `PutRetentionPolicy`, `DeleteRetentionPolicy`
- Metric filter: `logs:PutMetricFilter`, `DeleteMetricFilter`, `DescribeMetricFilters`
- Tagging: `logs:TagResource`, `UntagResource`, `ListTagsForResource`

### Budgets
- Budget lifecycle: `budgets:CreateBudget`, `ModifyBudget`, `DescribeBudget`, `DeleteBudget`
- Notification: `budgets:CreateNotification`, `DeleteNotification`, `DescribeNotificationsForBudget`, `CreateSubscriber`, `DeleteSubscriber`

### SSM (workflow-explicit)
- `ssm:GetParameter`, `ssm:GetParameters` — used by `.github/workflows/deploy.yml:53,76,80,84,108` (cron secret + telegram token + webhook secret fetches) and by Lambda cold start; scope to `parameter/miti99bot/*/*`

### STS
- `sts:GetCallerIdentity` — used by SAM at deploy start (no resource-level support; wildcard required)

## Todo List

- [ ] Service-by-service walk of template.yaml
- [ ] Cross-reference each AWS::* type against CFN per-resource IAM requirements
- [ ] Enumerate workflow-explicit `aws` CLI calls
- [ ] Identify `iam:PassRole` targets
- [ ] Build action × resource × ARN-pattern table in this doc
- [ ] Identify wildcard-required actions and justify each
- [ ] Mark phase complete via `ck plan check 2`

## Success Criteria

- [ ] Every CFN resource type in `template.yaml` has an entry in the action inventory.
- [ ] Every wildcard `Resource: "*"` has a 1-line "why not scoped" justification.
- [ ] Phase 3 can write the policy file by transcribing the inventory; no further AWS docs lookup needed in Phase 3.

## Risk Assessment

| Risk | Likelihood | Mitigation |
|---|---|---|
| Miss a CFN action (e.g. tagging permission, drift detection) | Med | Test by attaching the eventual policy in dual-mode (Phase 4) before detaching FullAccess. CFN failures surface in CloudFormation events; map to missing action and re-add. |
| AWS adds new required actions after this work | Low | Documented in `aws/README.md` (Phase 5): on `sam deploy` UPDATE failure with AccessDenied, check CloudTrail event → identify missing action → patch policy. |
| Over-tight ARN pattern (e.g. forgot `/index/*` for a future GSI) | Low | Phase 3 will use globs (`miti99bot*` not `miti99bot`) where the stack might extend. |

## Security Considerations

- This phase is read-only — no security implications.
- Output drives Phase 3's security boundary.

## Next Steps

Phase 3 starts after this phase's action inventory is complete.
