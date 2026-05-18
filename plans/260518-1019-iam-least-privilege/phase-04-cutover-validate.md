---
phase: 4
title: "Cutover + validate"
status: pending
priority: P1
effort: "1-2h"
dependencies: [3]
---

# Phase 4: Cutover + validate

## Overview

Replace the 10× `*FullAccess` managed policies on `github-deploy-miti99bot` with the inline custom policy from Phase 3. Validate via `workflow_dispatch`. This is the highest-risk phase — a wrong policy locks the deploy pipeline.

**Revised strategy after red-team (RT-3):** two-stage cutover with a dual-attach trial deploy first.

- **Stage 4a (Trial):** attach the new inline policy ALONGSIDE the existing FullAccess set. Trigger a deploy. The deploy succeeds because IAM evaluates the UNION — but CloudTrail records which policy authorized each action. This surfaces *missing actions* in the new policy WITHOUT the deploy actually failing. We don't claim sufficiency from this; we use it as a syntax-and-coverage smoke test before risking the cutover.
- **Stage 4b (Cutover):** disable the deploy workflow, detach the 10 FullAccess policies, re-enable the workflow, trigger validation deploy. Rollback path: re-attach FullAccess from a committed script.

The "AccessDenied during ROLLBACK" risk requires `cloudformation:ContinueUpdateRollback` (already in Phase 3 policy per RT-3). Also requires the deploy workflow itself to be DISABLED during 4b so a concurrent push doesn't run mid-cutover (RT-9).

## Requirements

**Functional**
- Custom policy attached as inline policy `miti99bot-deploy` on role.
- All 10 `*FullAccess` managed policies detached from role.
- Subsequent `workflow_dispatch` deploy succeeds end-to-end (includes the smoke test + Telegram webhook setup steps already in the workflow).
- On AccessDenied during deploy: instant rollback re-attaches all 10 FullAccess policies; iterate on the inline policy.

**Non-functional**
- All IAM mutations applied out-of-band via maintainer's local `admin` profile (chicken-and-egg per F1).
- Rollback script prepared and dry-tested BEFORE cutover starts.
- Maintain a < 15-min window where deploys can be re-enabled if cutover fails.

## Architecture

```
Before:                          During:                       After:
[10× *FullAccess managed]    →   [10× FullAccess]          →   [inline: miti99bot-deploy]
                                 [inline: miti99bot-deploy]
                                 ^^^ never the steady state ^^^
```

The middle "both attached" state exists only as a transient — used for the snapshot moment. We do not validate from there; we validate after the FullAccess detach.

## Related Code Files

- Read: `aws/iam-github-deploy-policy.json` (from Phase 3)
- Read: `.github/workflows/deploy.yml` (target for workflow_dispatch)
- No code changes in this phase — only AWS state changes.

## Implementation Steps

### Pre-flight gates (RT-4, RT-8)

0. **Verify admin profile reachable.** `aws/README.md:119` recommends deleting admin keys as hardening posture. Before any cutover:
   ```sh
   aws sts get-caller-identity --profile admin
   ```
   If fails: recreate admin access keys via console (root login → IAM → Users → admin → Security credentials → Create access key). DO NOT proceed until this succeeds. Console-only path is documented but unwieldy for the 11+ IAM calls below.

1. **Commit the rollback script to the repo** (RT-8) at `aws/iam-rollback-fullaccess.sh`. Per-call retry on throttling + post-loop verification:
   ```sh
   #!/bin/sh
   # Re-attaches the 10 FullAccess managed policies to github-deploy-miti99bot.
   # Idempotent: attach-role-policy succeeds even if policy already attached.
   ROLE=github-deploy-miti99bot
   POLICIES="
     arn:aws:iam::aws:policy/AWSCloudFormationFullAccess
     arn:aws:iam::aws:policy/AWSLambda_FullAccess
     arn:aws:iam::aws:policy/AmazonDynamoDBFullAccess
     arn:aws:iam::aws:policy/AmazonEventBridgeFullAccess
     arn:aws:iam::aws:policy/AmazonSQSFullAccess
     arn:aws:iam::aws:policy/AmazonSSMFullAccess
     arn:aws:iam::aws:policy/CloudWatchLogsFullAccess
     arn:aws:iam::aws:policy/AWSBudgetsActionsWithAWSResourceControlAccess
     arn:aws:iam::aws:policy/IAMFullAccess
     arn:aws:iam::aws:policy/AmazonS3FullAccess
   "
   for arn in $POLICIES; do
     for try in 1 2 3 4 5; do
       if aws iam attach-role-policy --role-name "$ROLE" --policy-arn "$arn" --profile admin 2>&1; then
         break
       fi
       echo "retry $try for $arn after throttle…"; sleep $((try * 2))
     done
   done
   # Verify final state — exit non-zero if anything is missing
   ATTACHED=$(aws iam list-attached-role-policies --role-name "$ROLE" --profile admin --query 'AttachedPolicies[].PolicyArn' --output text)
   MISSING=0
   for arn in $POLICIES; do
     echo "$ATTACHED" | grep -q "$arn" || { echo "MISSING: $arn"; MISSING=1; }
   done
   [ "$MISSING" = 0 ] && echo "Rollback complete — all 10 FullAccess policies attached." || { echo "Rollback INCOMPLETE — see MISSING lines above. Re-run or attach via console."; exit 1; }
   ```
   `chmod +x aws/iam-rollback-fullaccess.sh`. Commit alongside `aws/iam-github-deploy-policy.json`. Any teammate can recover, not only the operator.

2. **Verify Phase 3 prerequisites:**
   - `aws/iam-github-deploy-policy.json` exists on `main`; `jq .` validates.
   - No deploy currently in progress (`aws cloudformation describe-stacks --stack-name miti99bot --query 'Stacks[0].StackStatus' --profile admin` returns `*_COMPLETE`).
   - Coordinate with collaborators: announce a deploy freeze in the team channel for the cutover window (~30 min).

### Stage 4a — Dual-attach trial (RT-3)

3a. **Attach the new inline policy alongside existing FullAccess** (does not detach anything yet):
   ```sh
   aws iam put-role-policy \
     --role-name github-deploy-miti99bot \
     --policy-name miti99bot-deploy \
     --policy-document file://aws/iam-github-deploy-policy.json \
     --profile admin
   ```

3b. **Trial deploy:** trigger `workflow_dispatch` on `main`. With BOTH policy sets attached, the deploy MUST succeed (FullAccess covers any gap in the new policy). Confirm success of every workflow step including smoke test + Telegram webhook + Telegram commands.

3c. **CloudTrail sanity check (optional but recommended):** for the trial-deploy invocation, query CloudTrail for `userIdentity.arn` matching the deploy role and look at the `requestParameters` — events authorized only by the FullAccess managed policies (and not by the inline policy) signal a coverage gap in `miti99bot-deploy`. Patch the inline policy + redo step 3a before proceeding to 4b. (This step uses console UI; CLI access not required.)

### Stage 4b — Cutover (the actual narrowing)

4. **Disable the deploy workflow** to prevent concurrent runs (RT-9):
   ```sh
   gh workflow disable deploy-aws.yml
   ```
   Or in GitHub UI: Actions → deploy-aws → "Disable workflow". Re-enabled in step 7.

5. **Detach the 10 FullAccess policies with retry-on-throttle:**
   ```sh
   ROLE=github-deploy-miti99bot
   for arn in \
     arn:aws:iam::aws:policy/AWSCloudFormationFullAccess \
     arn:aws:iam::aws:policy/AWSLambda_FullAccess \
     arn:aws:iam::aws:policy/AmazonDynamoDBFullAccess \
     arn:aws:iam::aws:policy/AmazonEventBridgeFullAccess \
     arn:aws:iam::aws:policy/AmazonSQSFullAccess \
     arn:aws:iam::aws:policy/AmazonSSMFullAccess \
     arn:aws:iam::aws:policy/CloudWatchLogsFullAccess \
     arn:aws:iam::aws:policy/AWSBudgetsActionsWithAWSResourceControlAccess \
     arn:aws:iam::aws:policy/IAMFullAccess \
     arn:aws:iam::aws:policy/AmazonS3FullAccess; do
     for try in 1 2 3 4 5; do
       aws iam detach-role-policy --role-name "$ROLE" --policy-arn "$arn" --profile admin && break
       sleep $((try * 2))
     done
   done
   ```

6. **Verify role state:**
   ```sh
   aws iam list-attached-role-policies --role-name github-deploy-miti99bot --profile admin
   # Expect: empty AttachedPolicies list
   aws iam list-role-policies --role-name github-deploy-miti99bot --profile admin
   # Expect: ["miti99bot-deploy"]
   ```

7. **Re-enable the workflow:**
   ```sh
   gh workflow enable deploy-aws.yml
   ```

8. **Trigger validation deploy:** `workflow_dispatch` on `main` from the GitHub Actions UI. This is the FIRST deploy with ONLY the new inline policy. If it succeeds end-to-end → cutover complete. If AccessDenied appears anywhere:
   - **Immediately run** `bash aws/iam-rollback-fullaccess.sh --profile admin`. The script handles throttling + verifies all 10 re-attached.
   - **If the stack ended in `UPDATE_ROLLBACK_FAILED`** (RT-3): after re-attaching FullAccess, run:
     ```sh
     aws cloudformation continue-update-rollback --stack-name miti99bot --profile admin
     ```
     Wait for `UPDATE_ROLLBACK_COMPLETE`. Then push a fresh build (or `workflow_dispatch`) to re-establish baseline.
   - Capture the failing action from CloudTrail. Update `aws/iam-github-deploy-policy.json`, commit, and re-attempt from step 3a (trial again before cutover).

9. **Smoke test post-deploy:**
   - Workflow's built-in steps (`Smoke test`, `Register Telegram webhook`, `Register Telegram command menu`) must all succeed.
   - Manually trigger the EventBridge Scheduler "Run now" from the AWS Console to confirm the cron pathway still functions end-to-end with the new role.

10. **Final state:** role has only `miti99bot-deploy` inline policy. Document the cutover commit hash + timestamp in the "Cutover Record" section below.

## Cutover Record

> Filled in during execution.

- Cutover started: `YYYY-MM-DD HH:MM:SS UTC`
- Cutover finished: `YYYY-MM-DD HH:MM:SS UTC`
- Validating deploy run ID: `<GHA run URL>`
- Final role policies: `["miti99bot-deploy"]`
- Iterations needed: `<count>`
- Missing actions found mid-cutover: `<list, if any>`

## Todo List

- [ ] **Step 0:** `aws sts get-caller-identity --profile admin` succeeds (RT-4)
- [ ] **Step 1:** `aws/iam-rollback-fullaccess.sh` committed with retry+verify logic (RT-8)
- [ ] **Step 2:** Phase 3 prerequisites verified; deploy freeze announced
- [ ] **Stage 4a step 3a:** new inline policy attached alongside FullAccess
- [ ] **Stage 4a step 3b:** trial `workflow_dispatch` deploy succeeds end-to-end
- [ ] **Stage 4a step 3c:** (optional) CloudTrail confirms no actions authorized solely by FullAccess
- [ ] **Stage 4b step 4:** `gh workflow disable deploy-aws.yml` (RT-9)
- [ ] **Stage 4b step 5:** All 10 FullAccess policies detached (retry-on-throttle)
- [ ] **Stage 4b step 6:** Role state verified (only `miti99bot-deploy` policy listed)
- [ ] **Stage 4b step 7:** `gh workflow enable deploy-aws.yml`
- [ ] **Stage 4b step 8:** Validation `workflow_dispatch` deploy succeeds end-to-end with ONLY the new inline policy
- [ ] **Stage 4b step 9:** EventBridge Scheduler "Run now" succeeds
- [ ] Cutover record filled in
- [ ] Mark phase complete via `ck plan check 4`

## Success Criteria

- [ ] `aws iam list-attached-role-policies --role-name github-deploy-miti99bot` returns empty.
- [ ] `aws iam list-role-policies --role-name github-deploy-miti99bot` returns `["miti99bot-deploy"]`.
- [ ] **Stage 4a trial deploy** succeeds with both policy sets attached (proves new policy syntax is valid and doesn't break anything).
- [ ] **Stage 4b validation deploy** succeeds with ONLY the new inline policy — zero rollbacks needed during cutover.
- [ ] EventBridge Scheduler manual fire succeeds within 60s of "Run now".
- [ ] Cutover record section above is filled in.
- [ ] `aws/iam-rollback-fullaccess.sh` is committed to the repo (RT-8).

## Risk Assessment

| Risk | Likelihood | Severity | Mitigation |
|---|---|---|---|
| Missing IAM action — deploy fails mid-CFN-update | Med | High | Rollback script ready (step 1). Iteration loop documented in step 7. CFN UPDATE_ROLLBACK_COMPLETE state is recoverable — re-attach FullAccess, the next deploy fixes any drift. |
| Missing action AFTER CFN_COMPLETE (e.g. Telegram webhook step uses ssm:GetParameter on a parameter not in scope) | Med | Med | Run rollback. The CFN state is fine; only the post-deploy steps failed. Patch policy and re-run workflow_dispatch — no CFN churn. |
| AccessDenied during ROLLBACK path (worst case) | Low | Critical | If CFN can't roll back due to missing IAM action, run rollback script immediately and let CFN retry with FullAccess. Then patch the missing action and re-run. |
| Maintainer loses local `admin` creds mid-cutover | Low | High | All steps idempotent — re-running from any point produces the same end state. AWS Console works as alternate path for every step. |
| Concurrent push to main during cutover window | Low | Med | `concurrency: deploy-prod` group in workflow prevents overlapping runs. Cutover window <15min; coordinate with anyone else on the repo. |

## Security Considerations

- During the cutover window, the deploy role's permissions ARE temporarily over-broad (both old and new attached). This is < 1 minute.
- After cutover: blast radius reduced from "10× FullAccess incl. account takeover via IAMFullAccess" to "stack-scoped CRUD on `miti99bot*` resources only". Paired with Phase 1 narrowing OIDC trust, the combined reduction is what F1+F2 set out to achieve.
- `iam:PassRole` Condition keeps the role from being able to pass arbitrary roles to Lambda/Scheduler — only `miti99bot-*` roles.
- The new inline policy is committed to git (Phase 3) — auditable, drift-detectable by comparing `aws iam get-role-policy` output to `aws/iam-github-deploy-policy.json`.

## Next Steps

After this phase: Phase 5 updates `aws/README.md` to reflect the new bootstrap. The plan is complete after Phase 5.
