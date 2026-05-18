---
phase: 3
title: "Draft custom policy"
status: pending
priority: P1
effort: "1h"
dependencies: [2]
---

# Phase 3: Draft custom policy

## Overview

Translate Phase 2's action inventory into a single JSON IAM policy document committed to the repo at `aws/iam-github-deploy-policy.json`. Validate JSON syntax and run AWS IAM Policy Simulator dry-run.

## Requirements

**Functional**
- Single `Version: "2012-10-17"` policy with multiple `Statement` entries, grouped by service.
- Each statement: `Effect: Allow`, action list, resource ARN list (with `${AWS::AccountId}` / `${AWS::Region}` interpolated to the actual account `225603493174` and region `ap-southeast-1`).
- `iam:PassRole` statement scoped via `Condition.ForAllValues:StringEquals.iam:PassedToService` to `lambda.amazonaws.com` and `scheduler.amazonaws.com` only (RT-7). Adding a new service later = explicit policy update via documented procedure in Phase 5.
- `iam:UpdateAssumeRolePolicy` deliberately EXCLUDED (RT-2) — CFN does not invoke it on stack-managed roles; including it enables trust-rewrite escalation.
- `iam:AttachRolePolicy` either omitted entirely OR scoped via `Condition.ArnEquals.iam:PolicyARN` to a documented allowlist (RT-6). Current SAM macros use inline `PutRolePolicy` only — start with omission, add only if a deploy fails AccessDenied on this action.
- Wildcard `Resource: "*"` only where the action has no resource-level support (Phase 2 enumerated these).

**Empirical verification (RT-7) before applying:**
- After Phase 2 inventory complete, before Phase 4 starts, run `aws iam simulate-principal-policy` against the draft policy with each Phase-2-enumerated action × target ARN. Pay special attention to `iam:PassRole` on each stack-managed role: the simulator's "ImplicitDeny" result for `iam:PassedToService` mismatches surfaces here, not at deploy time.

**Non-functional**
- Total policy size must stay under 6,144 chars (AWS managed-policy hard limit) OR be split into two inline policies on the same role.
- File committed to repo so future bootstrap reads from version control.
- JSON formatted with 2-space indent; trailing newline; sorted statements by service alphabetically for diff readability.

## Architecture

```
aws/
├── iam-github-deploy-policy.json    ← NEW (this phase)
├── iam-github-oidc-trust.json       ← Phase 1 narrowed this
└── README.md                         ← Phase 5 updates to reference new file
```

Single artifact, version-controlled, applied via `aws iam put-role-policy --policy-name miti99bot-deploy --role-name github-deploy-miti99bot --policy-document file://aws/iam-github-deploy-policy.json` (inline policy, not managed — keeps the policy with the role lifecycle).

Inline vs managed:
- Inline: scoped to role lifecycle, no separate ARN, deleted with role.
- Managed: separate ARN, reusable across roles, has 6,144-char hard limit (same as inline) + 10-policies-per-role limit.
- Choice: **inline.** Single role, no reuse needed, simpler lifecycle.

## Related Code Files

- Create: `aws/iam-github-deploy-policy.json`
- Read-only: `plans/260518-1019-iam-least-privilege/phase-02-discover-required-actions.md` (source of truth for actions × resources)

## Implementation Steps

1. **Build the JSON from the Phase 2 inventory.** No `/* ... */` placeholders — every Action array fully populated by transcribing Phase 2 (RT-1). Statement skeleton (Sids + ARNs ready; transcribe full action lists from Phase 2):
   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       {"Sid": "Budgets",        "Effect": "Allow", "Action": ["<from Phase 2 Budgets>"],  "Resource": "arn:aws:budgets::225603493174:budget/miti99bot*"},
       {"Sid": "CloudFormation", "Effect": "Allow", "Action": ["<from Phase 2 CFN incl. ContinueUpdateRollback, CancelUpdateStack, *TagResource>"], "Resource": ["arn:aws:cloudformation:ap-southeast-1:225603493174:stack/miti99bot*/*", "arn:aws:cloudformation:ap-southeast-1:225603493174:changeSet/*/miti99bot*/*"]},
       {"Sid": "CloudFormationGlobalRead", "Effect": "Allow", "Action": ["cloudformation:ListStacks", "cloudformation:ValidateTemplate"], "Resource": "*"},
       {"Sid": "DynamoDB",       "Effect": "Allow", "Action": ["<from Phase 2 DynamoDB incl. TagResource, UntagResource, ListTagsOfResource>"], "Resource": "arn:aws:dynamodb:ap-southeast-1:225603493174:table/miti99bot*"},
       {"Sid": "EventBridge",    "Effect": "Allow", "Action": ["<from Phase 2 Scheduler incl. TagResource>"], "Resource": "arn:aws:scheduler:ap-southeast-1:225603493174:schedule/*/miti99bot*"},
       {"Sid": "IAMRolesScoped", "Effect": "Allow", "Action": ["iam:CreateRole","iam:DeleteRole","iam:GetRole","iam:ListRoles","iam:PutRolePolicy","iam:DeleteRolePolicy","iam:GetRolePolicy","iam:ListRolePolicies","iam:ListAttachedRolePolicies","iam:TagRole","iam:UntagRole","iam:ListRoleTags"], "Resource": "arn:aws:iam::225603493174:role/miti99bot*"},
       {"Sid": "IAMPassRole",    "Effect": "Allow", "Action": "iam:PassRole", "Resource": "arn:aws:iam::225603493174:role/miti99bot*", "Condition": {"ForAllValues:StringEquals": {"iam:PassedToService": ["lambda.amazonaws.com", "scheduler.amazonaws.com"]}}},
       {"Sid": "Lambda",         "Effect": "Allow", "Action": ["<from Phase 2 Lambda incl. *FunctionUrlConfig, AddPermission, RemovePermission, GetPolicy, TagResource>"], "Resource": "arn:aws:lambda:ap-southeast-1:225603493174:function:miti99bot*"},
       {"Sid": "LambdaLayerRead","Effect": "Allow", "Action": "lambda:GetLayerVersion", "Resource": "arn:aws:lambda:ap-southeast-1:753240598075:layer:LambdaAdapterLayerArm64:*"},
       {"Sid": "Logs",           "Effect": "Allow", "Action": ["<from Phase 2 Logs incl. PutMetricFilter, *TagResource>"], "Resource": ["arn:aws:logs:ap-southeast-1:225603493174:log-group:/aws/lambda/miti99bot*", "arn:aws:logs:ap-southeast-1:225603493174:log-group:/aws/lambda/miti99bot*:*"]},
       {"Sid": "S3SamArtifacts", "Effect": "Allow", "Action": ["<from Phase 2 S3 incl. CreateBucket, GetBucketLocation, GetEncryptionConfiguration etc.>"], "Resource": ["arn:aws:s3:::aws-sam-cli-managed-default-*", "arn:aws:s3:::aws-sam-cli-managed-default-*/*"]},
       {"Sid": "S3GlobalList",   "Effect": "Allow", "Action": "s3:ListAllMyBuckets", "Resource": "*"},
       {"Sid": "SQS",            "Effect": "Allow", "Action": ["<from Phase 2 SQS incl. TagQueue, UntagQueue, ListQueueTags>"], "Resource": "arn:aws:sqs:ap-southeast-1:225603493174:miti99bot*"},
       {"Sid": "SSMRead",        "Effect": "Allow", "Action": ["ssm:GetParameter","ssm:GetParameters"], "Resource": "arn:aws:ssm:ap-southeast-1:225603493174:parameter/miti99bot/*/*"},
       {"Sid": "STS",            "Effect": "Allow", "Action": "sts:GetCallerIdentity", "Resource": "*"}
     ]
   }
   ```

   **NOT in the policy** (intentional, RT-2 + RT-6):
   - `iam:UpdateAssumeRolePolicy` — CFN doesn't need it; including it enables trust-rewrite escalation.
   - `iam:AttachRolePolicy` / `iam:DetachRolePolicy` — current SAM transforms use inline `PutRolePolicy` only. Add later with `Condition.ArnEquals.iam:PolicyARN` to a specific allowlist IF a real deploy fails on it; do not add prophylactically.

2. **Fill action lists** from Phase 2 inventory verbatim. Replace every `<from Phase 2 …>` placeholder with the actual action array. Sort alphabetically within each `Action` array.

3. **Validate JSON syntax:**
   ```sh
   jq . aws/iam-github-deploy-policy.json > /dev/null && echo OK
   ```

4. **Verify byte count** under 6,144:
   ```sh
   wc -c aws/iam-github-deploy-policy.json
   ```
   If over: split S3 + Lambda + IAM statements into a second inline policy `miti99bot-deploy-2`.

5. **IAM Policy Simulator dry-run** (optional but recommended — free). Use AWS Console: IAM → Policies → "Simulate" → paste the JSON → select each Phase-2 action with its target ARN → confirm "Allowed" for every legitimate operation. Note any "Implicit Deny" results and patch.

6. **Commit** the JSON file. Do NOT yet apply to the role (Phase 4).

## Todo List

- [ ] Build JSON skeleton with all statement Sids
- [ ] Fill each Action array from Phase 2 inventory
- [ ] Apply alphabetical sort within Actions for diff readability
- [ ] `jq .` validates
- [ ] Byte count under 6,144 (split if not)
- [ ] (Optional) IAM Policy Simulator dry-run passes for every Phase-2 action
- [ ] Commit `aws/iam-github-deploy-policy.json`
- [ ] Mark phase complete via `ck plan check 3`

## Success Criteria

- [ ] `aws/iam-github-deploy-policy.json` exists, valid JSON, under 6,144 bytes (single-policy form).
- [ ] Every action enumerated in Phase 2 appears in exactly one statement.
- [ ] `iam:PassRole` constrained by `iam:PassedToService` to the two services that need it.
- [ ] Every `Resource: "*"` has a justification comment outside the JSON (in Phase 2 inventory).
- [ ] Commit on `main`. Role NOT yet modified (Phase 4 applies).

## Risk Assessment

| Risk | Likelihood | Mitigation |
|---|---|---|
| Exceed 6,144 char policy limit | Low | Split into 2 inline policies (miti99bot-deploy-cfn-lambda + miti99bot-deploy-data-iam). |
| Typo in action name | Low | `jq .` catches JSON syntax; IAM Policy Simulator catches unknown action names. |
| Forgot a CFN resource-tagging action | Med | Phase 4 dual-attach validate catches at first deploy; iterate. |

## Security Considerations

- No AWS state changes in this phase — policy is on disk only.
- File contains no secrets — safe to commit.
- ARN patterns hardcode account ID `225603493174` and region `ap-southeast-1`; documented as project constants; rotating either invalidates the file but the project's `aws/README.md` already pins them.

## Next Steps

Phase 4 (cutover) consumes this file. Do not start Phase 4 until commit lands.
