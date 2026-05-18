---
phase: 5
title: "Update bootstrap docs"
status: pending
priority: P2
effort: "30m"
dependencies: [4]
---

# Phase 5: Update bootstrap docs

## Overview

Update `aws/README.md` step 4 to reflect the new least-privilege bootstrap: a single `aws iam put-role-policy` call from a committed JSON file instead of attaching 10× `*FullAccess` managed policies. Add a "drift detection" note + rollback procedure.

## Requirements

**Functional**
- `aws/README.md` step 4 replaced with new bootstrap flow.
- New step references the committed `aws/iam-github-deploy-policy.json`.
- Section 7 ("Tighten — optional but recommended") updated: bullet 3 (replacing broad managed policies) is now redundant — mark as DONE or remove.
- Add a brief "Updating the deploy policy" subsection explaining: edit the JSON in repo → `aws iam put-role-policy` from `admin` profile (NOT through the workflow) → commit.

**Non-functional**
- Docs explain WHY (link to F1 finding + this plan dir) so future maintainers don't reattach FullAccess for convenience.
- Keep README concise — defer rationale to plan + audit report.

## Architecture

No architecture change. Pure documentation.

## Related Code Files

- Modify: `aws/README.md` (steps 4, 7)
- Read-only: `aws/iam-github-deploy-policy.json` (referenced from README)
- Read-only: `plans/reports/code-reviewer-260518-1019-security-aws-infra.md` (link target)

## Implementation Steps

1. **Read current `aws/README.md`** to confirm section anchors (especially step 4 and section 7).

2. **Rewrite step 4** ("Deploy IAM role for GitHub Actions"):
   - Keep the `aws iam create-role` call (trust policy unchanged from Phase 1 narrowing).
   - Replace the `for arn in ... do attach ... done` loop with:
     ```sh
     aws iam put-role-policy \
       --role-name github-deploy-miti99bot \
       --policy-name miti99bot-deploy \
       --policy-document file://aws/iam-github-deploy-policy.json \
       --profile admin
     ```
   - Add 1-line note: "Scoped to stacks/resources named `miti99bot*`. See [security audit](../plans/reports/code-reviewer-260518-1019-security-aws-infra.md) F1 for rationale and [plan](../plans/260518-1019-iam-least-privilege/) for the cutover record."

3. **Update section 7** ("Tighten — optional but recommended"):
   - Bullet 3 ("Replace the broad managed policies on `github-deploy-miti99bot` with stack-scoped custom policies") is now done — remove or mark `[done in 2026-05]`.
   - Bullet 1 (rotate admin keys) and bullet 2 (workflow_dispatch confirmation) remain.

4. **Add new subsection "Updating the deploy policy"** at the end of section 4:
   ```md
   ### Updating the deploy policy

   When `template.yaml` adds a new resource type, the deploy role may need new IAM
   actions. Workflow:

   1. Edit `aws/iam-github-deploy-policy.json` — add the action(s) + ARN pattern.
   2. Apply out-of-band from a maintainer's `admin` profile (NOT via the workflow):
      ```sh
      aws iam put-role-policy --role-name github-deploy-miti99bot \
        --policy-name miti99bot-deploy \
        --policy-document file://aws/iam-github-deploy-policy.json --profile admin
      ```
   3. Commit the JSON. Next deploy uses the new permissions.

   Drift check — structural compare, not byte-diff (RT-12). `aws iam get-role-policy` returns JSON that differs from the local file in key ordering / whitespace but may be semantically identical. Compare normalized:
   ```sh
   diff <(aws iam get-role-policy --role-name github-deploy-miti99bot \
            --policy-name miti99bot-deploy --profile admin --query PolicyDocument | jq -S .) \
        <(jq -S . aws/iam-github-deploy-policy.json)
   ```
   Non-empty output = INVESTIGATE before reapplying. AWS-side may have been intentionally patched during an outage; blindly re-applying overwrites that fix.
   ```

   ### Trust policy invariants (RT-15)

   `aws/iam-github-oidc-trust.json` constrains which GitHub Actions contexts can
   assume `github-deploy-miti99bot`. The current allowlist is intentionally
   narrow: only pushes to `main` can deploy.

   **To add a new branch / context** (e.g., a future `dev` preview deploy):

   1. Edit `aws/iam-github-oidc-trust.json` — add the new `sub` claim to the
      `StringLike` array. Examples:
      - `repo:tiennm99/miti99bot:ref:refs/heads/dev` — pushes to `dev` branch
      - `repo:tiennm99/miti99bot:environment:preview` — workflows scoped to a
        GitHub Environment named `preview` (requires `permissions: id-token: write`)
   2. Apply out-of-band:
      ```sh
      aws iam update-assume-role-policy --role-name github-deploy-miti99bot \
        --policy-document file://aws/iam-github-oidc-trust.json --profile admin
      ```
   3. Commit. Test by triggering the new workflow path.

   **Reasons `pull_request` is NOT in the allowlist** (do not re-add without
   reviewing): PR-context OIDC tokens are derivable from any contributor's
   PR. Granting the deploy role to PRs is equivalent to granting deploy access
   to every contributor. Combined with the inline policy's IAM/Lambda/DynamoDB
   actions, an attacker-controlled PR could exfiltrate or alter prod state.

5. **Commit** the README edit + the policy JSON file (if not already committed in Phase 4) on the same branch / PR.

## Todo List

- [ ] Read current `aws/README.md` to map anchors
- [ ] Rewrite step 4 with `put-role-policy` flow + link to `plans/reports/code-reviewer-260518-1019-security-aws-infra.md` (RT-5 — file now exists)
- [ ] Add "Updating the deploy policy" subsection with `jq -S` structural-diff drift check (RT-12)
- [ ] Add "Trust policy invariants" subsection documenting how to re-add sub claims safely (RT-15)
- [ ] Update section 7 — mark broad-policy-replacement as done
- [ ] Add "Updating the deploy policy" subsection with drift-check command
- [ ] Commit `aws/README.md` change
- [ ] Mark phase complete via `ck plan check 5`

## Success Criteria

- [ ] `aws/README.md` step 4 no longer references `*FullAccess` managed policies.
- [ ] `aws/README.md` references `aws/iam-github-deploy-policy.json` as the canonical bootstrap source.
- [ ] `aws/README.md` links to `plans/reports/code-reviewer-260518-1019-security-aws-infra.md` for F1/F2 rationale.
- [ ] Section 7 no longer lists "tighten policies" as a TODO.
- [ ] New "Updating the deploy policy" subsection includes the `jq -S` structural-diff drift command (not plain `diff`).
- [ ] New "Trust policy invariants" subsection documents the procedure to re-add a `sub` claim and explains why `pull_request` is excluded.

## Risk Assessment

| Risk | Likelihood | Mitigation |
|---|---|---|
| README drifts from actual role state | Med | "Updating the deploy policy" subsection includes a drift-check command. Future maintainers run it before assuming the README is accurate. |
| Future maintainer re-attaches FullAccess "just to ship a hotfix" | Med | README explicitly references the security audit finding F1 — explanation of why this is bad. Plan dir `260518-1019-iam-least-privilege/` provides full history. |

## Security Considerations

- Docs-only phase; no AWS state changes.
- Preserves the security work done in phases 1-4 by making the new bootstrap discoverable to future maintainers.

## Next Steps

Plan complete. After Phase 5 lands:
- `/ck:journal` — write a session journal entry recording the cutover lesson (in particular, the dual-attach-strategy investigation in Phase 4).
- Archive the plan with `/ck:plan archive`.
- Address remaining audit findings (F3, F4, F5-F16) — out of scope here.
