---
phase: 5
title: "GitHub Actions deploy (OIDC + SAM)"
status: pending
priority: P2
effort: "3h"
dependencies: [2, 3, 4]
---

# Phase 05: GitHub Actions deploy (OIDC + SAM)

## Overview
Push to `main` → CI builds the ARM64 Go binary, packages into ZIP, runs `sam deploy` against the existing stack via OIDC-assumed role. No long-lived AWS keys. Idempotent (zero-diff deploys are no-ops).

## Requirements
- **Functional:** PR validates (`go vet`, `go test`, `sam validate`). Push to `main` deploys. Manual workflow_dispatch redeploy supported.
- **Non-functional:** Deploy < 4 min. Concurrency: only one deploy at a time per ref. Stack name parameterized by env (default `prod`).

## Architecture
```
GitHub push to main
  └─► .github/workflows/deploy.yml
       1. checkout
       2. setup-go (1.25)
       3. setup-sam
       4. configure-aws-credentials (OIDC)  ─► assume github-deploy-miti99bot
       5. make build-lambda
       6. sam build --use-container=false
       7. sam deploy --no-confirm-changeset --no-fail-on-empty-changeset
       8. post-deploy smoke (curl <function-url>/)
```

## Related Code Files
- Create: `.github/workflows/deploy.yml`
- Create: `.github/workflows/ci.yml` — maybe split out validate-only path; or add `if:` guard in `deploy.yml`
- Modify: existing `.github/workflows/ci.yml` — add `sam validate` step
- Modify: `Makefile` — `deploy` target runs `sam build && sam deploy --no-confirm-changeset`

## Implementation Steps
1. Confirm Phase 01's IAM role trust policy includes `repo:tiennm99/miti99bot:ref:refs/heads/main` and the matching repo subject for any manual deploy path.
2. Write `deploy.yml`:
   ```yaml
   name: Deploy to AWS
   on:
     push: { branches: [main] }
     workflow_dispatch:
   permissions:
     id-token: write
     contents: read
   concurrency: { group: deploy-prod, cancel-in-progress: false }
   jobs:
     deploy:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v4
         - uses: actions/setup-go@v5
           with: { go-version: '1.25' }
         - uses: aws-actions/setup-sam@v2
           with: { use-installer: true }
         - uses: aws-actions/configure-aws-credentials@v4
           with:
             role-to-assume: arn:aws:iam::225603493174:role/github-deploy-miti99bot
             aws-region: ap-southeast-1
         - run: make build-lambda
         - run: sam build
         - run: sam deploy --no-confirm-changeset --no-fail-on-empty-changeset --stack-name miti99bot-aws-port
         - name: Smoke test
           run: |
             URL=$(aws cloudformation describe-stacks --stack-name miti99bot-aws-port --query "Stacks[0].Outputs[?OutputKey=='FunctionUrl'].OutputValue" --output text)
             curl -fsSL "$URL/" | jq .
   ```
3. Keep the AWS account ID in the committed role ARN for this repo. If the deploy account changes later, update both the workflow ARN and the IAM trust policy together.
4. PR validation workflow (`ci.yml`): runs `go vet`, `go test`, `sam validate` (no AWS creds needed for validate).
5. Test the full path: open a PR with a trivial change → CI green; merge → deploy fires; smoke step prints health JSON.
6. Add a `rollback.yml` workflow_dispatch path: re-run with a chosen commit SHA. CloudFormation handles the rollback inherently.

## Success Criteria
- [ ] PR triggers `ci.yml` only (no AWS deploy)
- [ ] Merge to `main` triggers `deploy.yml`
- [ ] Deploy succeeds without manual intervention
- [ ] Post-deploy smoke step returns 200 from Function URL
- [ ] Concurrency lock prevents overlapping deploys
- [ ] Re-running a no-op deploy reports "no changes" and exits 0
- [ ] No AWS access keys in repo, GitHub Actions secrets, or anywhere

## Risk Assessment
- **OIDC trust misconfiguration** locks deploy out — Mitigation: keep bootstrap admin user (Phase 01) as glass-break recovery; rotate after 90 days.
- **`sam build` with native deps fails** — pure Go has no native deps; should be fine.
- **CloudFormation drift** between manual changes and CI — Mitigation: forbid console edits; daily `sam deploy --no-execute-changeset` to detect.
- **Build cache cold each run** (~90s for Go deps) — Mitigation: `actions/setup-go` cache enabled by default.
- **Workflow secret leak via debug logs** — Mitigation: never echo `secrets.*`, GH masks them automatically.

## Open questions
1. Separate dev/staging stacks for PR previews? Out of scope for v1; `prod` only.
2. Slack/Telegram notification on deploy success/failure? Defer; CloudWatch logs + `gh run list` suffice initially.
3. Pin SAM version in `setup-sam`? Yes — pin to a specific version to avoid surprise breakage.
