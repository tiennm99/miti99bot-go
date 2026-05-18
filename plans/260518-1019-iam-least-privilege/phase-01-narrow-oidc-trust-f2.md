---
phase: 1
title: "Narrow OIDC trust (F2)"
status: pending
priority: P1
effort: "30m"
dependencies: []
---

# Phase 1: Narrow OIDC trust (F2)

## Overview

Remove the `pull_request` claim (and `refs/heads/dev` if unused) from the OIDC trust policy on `github-deploy-miti99bot`. After this lands, only pushes to `main` can assume the deploy role.

## Requirements

**Functional**
- The `Condition.StringLike.token.actions.githubusercontent.com:sub` array on the role's trust policy must contain only branch refs that are actually used to deploy.
- Current set: `refs/heads/main`, `refs/heads/dev`, `pull_request`.
- Target set: `refs/heads/main` (plus `refs/heads/dev` ONLY if it's used; default-drop otherwise).

**Non-functional**
- Apply out-of-band via maintainer's local `admin` AWS profile — not through the workflow being modified.
- Rollback: re-apply the previous `iam-github-oidc-trust.json` snapshot via the same `aws iam update-assume-role-policy` call.

## Architecture

Single JSON file (`aws/iam-github-oidc-trust.json`) is the source of truth committed to the repo; AWS-side trust policy is updated via `aws iam update-assume-role-policy`. No CloudFormation involvement.

## Related Code Files

- Modify: `aws/iam-github-oidc-trust.json` (drop 1-2 lines from `sub` allowlist)
- Read-only: `.github/workflows/deploy.yml` (confirms only `main` is on the `push` trigger)

## Implementation Steps

0. **Verify admin profile is reachable** (RT-4). `aws/README.md:119` recommends deleting admin keys as hardening posture, so before proceeding confirm the operator can authenticate:
   ```sh
   aws sts get-caller-identity --profile admin
   ```
   If this fails with `InvalidClientTokenId` / `Unable to locate credentials`: recreate admin access keys via console (root → IAM → Users → admin → Security credentials → Create access key), or perform every step in this phase via the AWS Console fallback below.

   **Console fallback for Phase 1:** IAM → Roles → `github-deploy-miti99bot` → Trust relationships → Edit trust policy → paste the JSON from step 3 → Update policy.

1. **Confirm `dev` is not used.** Inspect `.github/workflows/deploy.yml:5` — `on.push.branches` is `[main]`. Search every workflow file:
   ```sh
   rg -l 'dev|id-token' .github/workflows/
   ```
   Also check for any workflow with `permissions: id-token: write` (only OIDC-capable workflows matter). Current state (verified 2026-05-18): only `deploy.yml` has `id-token: write`. `ci.yml` has `permissions: contents: read` only — physically cannot mint OIDC. So `refs/heads/dev` is dormant; drop it. The procedure to re-add for a future preview env is documented in Phase 5 (RT-15).

2. **No /tmp snapshot needed** (RT-8). `aws/iam-github-oidc-trust.json` is git-tracked. Rollback = `git show HEAD:aws/iam-github-oidc-trust.json | aws iam update-assume-role-policy --role-name github-deploy-miti99bot --policy-document file:///dev/stdin --profile admin`. Capture the pre-edit `HEAD` commit hash for explicit rollback:
   ```sh
   git rev-parse HEAD  # save this — recovery uses it
   ```

3. **Edit + commit FIRST, then apply** (V-4 decision). The repo file is the source of truth. Commit the edit before invoking `aws iam update-assume-role-policy` so:
   - `git show HEAD^:aws/iam-github-oidc-trust.json` always recovers the previous state.
   - If the apply fails / hangs, the repo file matches the intended target — no AWS/repo drift.

   Edit `aws/iam-github-oidc-trust.json` to its final shape:
   ```json
   {
     "Version": "2012-10-17",
     "Statement": [{
       "Effect": "Allow",
       "Principal": {"Federated": "arn:aws:iam::225603493174:oidc-provider/token.actions.githubusercontent.com"},
       "Action": "sts:AssumeRoleWithWebIdentity",
       "Condition": {
         "StringEquals": {"token.actions.githubusercontent.com:aud": "sts.amazonaws.com"},
         "StringLike": {"token.actions.githubusercontent.com:sub": [
           "repo:tiennm99/miti99bot:ref:refs/heads/main"
         ]}
       }
     }]
   }
   ```

4. **Commit the edit**, then **apply** out-of-band (V-4 — commit-first):
   ```sh
   git add aws/iam-github-oidc-trust.json
   git commit -m "fix(security): narrow OIDC trust to main only (F2)"

   aws iam update-assume-role-policy \
     --role-name github-deploy-miti99bot \
     --policy-document file://aws/iam-github-oidc-trust.json \
     --profile admin
   ```
   If the `aws iam` call fails, the commit is harmless on its own (workflows still use the live AWS-side trust). Reverse with `git revert HEAD` if abandoning the change.

5. **Smoke test (positive path):** trigger `workflow_dispatch` from GitHub Actions on `main`. Expect the `configure-aws-credentials` step to succeed and the deploy to proceed exactly as before.

6. **Smoke test (positive verifies narrowing):** the trust narrowing is enforced by AWS IAM at `sts:AssumeRoleWithWebIdentity` time, not by workflow trigger configuration. Step 5's successful `workflow_dispatch` on main proves the trust still permits the intended caller. No PR-triggered workflow currently has `id-token: write`, so a negative-path test would require provisioning a throwaway workflow — out of scope here. If you want belt-and-braces, see Phase 5's "Trust policy invariants" subsection for how to add a synthetic OIDC token verification step.

7. **Already committed in step 4** (V-4). Nothing to do here.

## Todo List

- [ ] **Step 0:** Verify `aws sts get-caller-identity --profile admin` succeeds (RT-4)
- [ ] Verify `dev` branch and OIDC `id-token: write` usage with `rg -l 'dev|id-token' .github/workflows/`
- [ ] Capture pre-edit HEAD via `git rev-parse HEAD` (RT-8 — git is the snapshot)
- [ ] Edit `aws/iam-github-oidc-trust.json`
- [ ] Apply via `aws iam update-assume-role-policy`
- [ ] workflow_dispatch deploy succeeds on main
- [ ] Commit JSON edit
- [ ] Mark phase complete via `ck plan check 1`

## Success Criteria

- [ ] `aws iam get-role --role-name github-deploy-miti99bot --query 'Role.AssumeRolePolicyDocument.Statement[0].Condition.StringLike."token.actions.githubusercontent.com:sub"'` returns only `["repo:tiennm99/miti99bot:ref:refs/heads/main"]`.
- [ ] `workflow_dispatch` deploy on `main` succeeds end-to-end.
- [ ] Commit `aws/iam-github-oidc-trust.json` is on `main`.

## Risk Assessment

| Risk | Likelihood | Mitigation |
|---|---|---|
| Rollback needed (e.g. forgot dev branch is used by something) | Low | `git show <pre-edit-HEAD>:aws/iam-github-oidc-trust.json \| aws iam update-assume-role-policy --policy-document file:///dev/stdin --profile admin` — git is the snapshot (RT-8). |
| OIDC-claim format typo locks out all deploys | Very Low | JSON file is small + reviewable; AWS rejects malformed `sub` patterns at update time. |
| User has no `admin` profile / local AWS creds | Med | Step 0 verifies admin reachability before edits. Console fallback documented inline in step 0 (RT-4). |

## Security Considerations

- This phase REDUCES attack surface; no new privileges introduced.
- After this lands, a leaked GitHub PR-context OIDC token cannot assume this role even if other findings remain unfixed.
- Pairs with Phase 4 (F1 cutover) — together they reduce blast radius from "any PR = account takeover" to "any push to main = scoped deploy only".

## Next Steps

Phase 1 is standalone. Phase 2 (Discover required actions) can start in parallel or after — no dependency.
