---
title: "IAM least-privilege + OIDC trust narrowing (F1, F2)"
description: "Replace 10× *FullAccess managed policies on github-deploy-miti99bot with a single stack-scoped custom inline policy; remove pull_request claim from OIDC trust."
status: pending
priority: P1
branch: "main"
tags: [security, iam, deploy]
blockedBy: []
blocks: []
created: "2026-05-18T09:06:20.846Z"
createdBy: "ck:plan"
source: skill
---

# IAM least-privilege + OIDC trust narrowing (F1, F2)

## Overview

Two HIGH-severity findings from the 2026-05-18 security audit. Both target the
GitHub Actions OIDC deploy role `github-deploy-miti99bot`.

Defence-in-depth framing (revised after red-team review):
- F2 today is dormant — no PR-trigger workflow currently has `id-token: write` (verified: `.github/workflows/ci.yml` has `permissions: contents: read` only; `deploy.yml` is the only OIDC consumer and triggers on `push: main` + `workflow_dispatch`). A future workflow addition would make it live. Removing the claim closes the latent path.
- F1 is the bigger lever: combined with the dormant F2 path, the 10× `*FullAccess` set (incl. `IAMFullAccess`) means any future OIDC-loosening + workflow compromise = account takeover.

- **F2 (trivial, 1-line):** drop `repo:tiennm99/miti99bot:pull_request` (and `:ref:refs/heads/dev` if unused) from the OIDC trust `sub` allowlist.
- **F1 (careful):** replace 10× `*FullAccess` managed policies with one stack-scoped inline custom policy. Must enumerate every IAM action `sam deploy` actually invokes for every CFN resource in `template.yaml` — missing one = pipeline broken on next push.

## References

- `plans/reports/code-reviewer-260518-1019-security-aws-infra.md` — finding details (F1, F2)
- `aws/iam-github-oidc-trust.json` — current trust policy
- `aws/README.md` step 4 — current broad-policy provisioning loop (to be replaced)
- `.github/workflows/deploy.yml` — ground truth for what the role needs
- `template.yaml` — every CFN resource sam deploy manages
- `docs/deploy-aws-free-tier-guide.md:11-37` — accepted security trade-off envelope

## Phases

| Phase | Name | Status | Risk |
|-------|------|--------|------|
| 1 | [Narrow OIDC trust (F2)](./phase-01-narrow-oidc-trust-f2.md) | Pending | Low — JSON edit + 1 aws-iam call; rollback via `git show HEAD^:aws/iam-github-oidc-trust.json` (RT-8) |
| 2 | [Discover required actions](./phase-02-discover-required-actions.md) | Pending | Low — read-only enumeration; output is a documented action × resource table |
| 3 | [Draft custom policy](./phase-03-draft-custom-policy.md) | Pending | Low — file creation + JSON validation; no AWS calls |
| 4 | [Cutover + validate](./phase-04-cutover-validate.md) | Pending | **High** — wrong policy = pipeline locked. Two-stage (4a dual-attach trial + 4b cutover) with committed rollback script + workflow-disable gate + `ContinueUpdateRollback` recovery (RT-3, RT-8, RT-9). |
| 5 | [Update bootstrap docs](./phase-05-update-bootstrap-docs.md) | Pending | Low — `aws/README.md` only |

Phase 1 is independent of 2-5 and can land standalone.
Phases 2 → 3 → 4 → 5 are strictly sequential.

## Constraints (locked from project memory)

- **Free tier hard:** no Secrets Manager, no KMS CMK, no Config rules, no IAM Access Analyzer (paid features). Use IAM Policy Simulator only (free).
- **Security envelope:** secret-in-logs / secret-in-Input acceptable; designed public surface = Function URL only; documented at `docs/deploy-aws-free-tier-guide.md:11-37`.
- **Bootstrap chicken-and-egg:** F1 + F2 modify the very role the pipeline uses. All IAM mutations must be applied out-of-band (maintainer local creds with the original `admin` profile or AWS Console), NOT via the workflow being modified.

## Dependencies

None — both findings are repo-internal. F2 has no upstream / downstream dependency.

## Red Team Review

### Session — 2026-05-18

**Findings:** 15 of 30 surviving deduplication (10 accepted-applied, 5 cut as duplicate-of-fix or speculative)
**Severity breakdown:** 4 Critical · 8 High · 3 Medium
**Reviewers:** Security Adversary · Failure Mode Analyst · Assumption Destroyer

| # | Sev | Finding | Disposition | Applied To |
|---|---|---|---|---|
| 1 | CRIT | Phase 2 inventory placeholders + Phase 3 designs against TBD | Accept | Phase 2, Phase 3 (rewrites) |
| 2 | CRIT | `iam:UpdateAssumeRolePolicy` enables trust-rewrite escalation | Accept | Phase 3 (action dropped) |
| 3 | CRIT | UPDATE_ROLLBACK_FAILED unrecoverable; `cloudformation:ContinueUpdateRollback` missing; dual-attach trial rejected too early | Accept | Phase 3 + Phase 4 (re-architect) |
| 4 | CRIT | `--profile admin` everywhere conflicts with `aws/README.md:119` "delete admin keys" | Accept | Phase 1 + 4 + 5 (admin-gate + console fallback) |
| 5 | HIGH | Audit report file `plans/reports/code-reviewer-260518-1019-security-aws-infra.md` did not exist | Accept | Audit file written; references valid |
| 6 | HIGH | `iam:AttachRolePolicy` without `iam:PolicyARN` Condition → admin-policy attach escalation | Accept | Phase 3 (Condition added or action dropped) |
| 7 | HIGH | `iam:PassedToService` `StringEquals` brittle; CFN-internals + future services not covered | Accept | Phase 3 (empirical verification + extensibility note) |
| 8 | HIGH | Rollback script in `/tmp` not repo; `set -e` aborts mid-loop on throttle | Accept | Phase 4 (script committed at `aws/iam-rollback-fullaccess.sh` with retry + verify) |
| 9 | HIGH | `concurrency: deploy-prod` does not gate external IAM mutations | Accept | Phase 4 (workflow-disable during cutover) |
| 10 | HIGH | `*:TagResource` / `*:UntagResource` / `*:ListTagsForResource` not enumerated | Accept | Phase 2 (mandatory categories) + Phase 3 (added) |
| 11 | HIGH | Schedule ARN `schedule/default/miti99bot-*` depends on undocumented default-group folklore | Accept | Phase 3 (`schedule/*/miti99bot-*`) |
| 12 | HIGH | Drift `diff` produces false positives — AWS normalizes JSON server-side | Accept | Phase 5 (`jq -S` structural compare) |
| 13 | MED  | SAM bucket prefix is convention not contract; bucket-bootstrap actions missing | Accept | Phase 2 (discovery step) + Phase 3 (broader S3 actions) |
| 14 | MED  | Stack ARN hardcodes `miti99bot` literal — future `miti99bot-dev` locked out | Accept | Phase 3 (`miti99bot*` globs) |
| 15 | MED  | Dropping `refs/heads/dev` without re-add procedure | Accept | Phase 5 ("Trust policy invariants" section) |

**Cut as duplicate-of-fix or speculative:**
- Function URL config actions (subsumed by Finding 1 inventory rewrite)
- F2 threat narrative inflation (addressed by Overview reframe above)
- Cross-account layer assumption (low actionable impact)
- `CAPABILITY_NAMED_IAM` future need (speculative forward-look)

**Reports written:**
- `plans/reports/code-reviewer-260518-1019-security-aws-infra.md` (was missing; produced from inline audit content)

### Whole-Plan Consistency Sweep — 2026-05-18

Re-read `plan.md` + 5 phase files after edits. Reconciled:

- ✅ ARN patterns match across Phase 2 (discovery) and Phase 3 (policy skeleton): `stack/miti99bot*/*`, `table/miti99bot*`, `function:miti99bot*`, `schedule/*/miti99bot*`, `role/miti99bot*`, `parameter/miti99bot/*/*`.
- ✅ Phase 1 step 0 (admin pre-flight, RT-4) reflected in Todo List + Risk Assessment.
- ✅ Phase 4 two-stage restructure (4a trial + 4b cutover) reflected in Implementation Steps, Todo List, Success Criteria, plan.md risk column.
- ✅ Audit report `plans/reports/code-reviewer-260518-1019-security-aws-infra.md` exists; Phase 5 Success Criteria references it.
- ✅ `aws/iam-rollback-fullaccess.sh` (committed script, RT-8) referenced consistently across Phase 4 step 1, Phase 4 step 8, Success Criteria.
- ✅ `jq -S` structural-diff (RT-12) replaces `diff` in both phase doc and Success Criteria.
- ✅ `iam:UpdateAssumeRolePolicy` (RT-2) and `iam:AttachRolePolicy` (RT-6) marked as deliberately-excluded in Phase 2 + Phase 3, with rationale for re-adding.
- ✅ "F2 dormant today, defence-in-depth fix" framing (RT cuts) in plan.md Overview matches Phase 1 step 6 rewrite.

**Unresolved contradictions:** none. Plan ready for implementation.

## Validation Log

### Session — 2026-05-18 (post-red-team)

Critical-questions interview after red-team. 4 questions; 4 decisions recorded.

| # | Question | Decision | Applied To |
|---|---|---|---|
| V-1 | Phase 1 dev-branch: does user push to `dev` from local? | **No, never push to dev** → drop `refs/heads/dev` from OIDC trust as Phase 1 already proposes. Decisive narrowing. | Phase 1 (already reflected) |
| V-2 | Phase 4 admin pre-flight: add multi-item preconditions checklist? | **No** — "make this workflow simple, just work first, then we will solve problems later." Existing single Step 0 (`aws sts get-caller-identity`) is enough. Don't add MFA/network/console-access checklist. | Phase 4 (no change — minimal step 0 retained) |
| V-3 | Phase 4a trial: make CloudTrail coverage check mandatory? | **No** — same simplicity preference as V-2. Stays optional. Dual-attach trial succeeding + cutover deploy succeeding are the two coverage signals. | Phase 4 step 3c (no change — stays "optional but recommended") |
| V-4 | Phase 1: commit `iam-github-oidc-trust.json` edit BEFORE or AFTER `aws iam update-assume-role-policy`? | **Commit FIRST, then apply.** Repo is source of truth; `git show HEAD^:...` always recovers previous state. AWS/repo drift avoided. | Phase 1 step 3 + 4 + 7 (commit step folded into apply step) |

**Memory captured:** [[simplicity-over-defensive-checklists]] — durable preference for minimum workflow on ops/deploy plans for this project.

### Whole-Plan Consistency Sweep (validation)

After V-4 edit:
- ✅ Phase 1 step 3 now includes commit-before-edit guidance + rationale.
- ✅ Phase 1 step 4 includes both `git commit` and `aws iam update-assume-role-policy` together.
- ✅ Phase 1 step 7 redirected to step 4 (no double-commit).
- ✅ Todo List unchanged — "Commit JSON edit" item still maps to step 4 (just consolidated, not removed).
- ✅ Plan.md risk column for Phase 1 still accurate: "rollback via `git show HEAD^:aws/iam-github-oidc-trust.json`" works because the commit is on `HEAD`.

**Unresolved contradictions:** none. Plan ready for implementation.

## Non-goals (explicit cuts)

- F3 (CORS), F4 (root handler audit), F5-F16 — captured in audit report, separate fixes.
- Moving secrets to Secrets Manager — violates free-tier rule.
- Adding `govulncheck` to CI — separate hygiene work.
- Rotating the existing CronSharedSecret — out of scope.
