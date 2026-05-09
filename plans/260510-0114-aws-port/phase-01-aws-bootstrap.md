---
phase: 1
title: "AWS bootstrap + IAM OIDC + SAM skeleton"
status: pending
priority: P1
effort: "3h"
dependencies: []
---

# Phase 01: AWS bootstrap + IAM OIDC + SAM skeleton

## Overview
Stand up the AWS account with strict $0 footprint: IAM OIDC trust for GitHub Actions, baseline SAM stack that deploys an empty Lambda + DynamoDB table + Function URL placeholder. Nothing wired to real bot yet.

## Requirements
- **Functional:** Empty stack deploys via `sam deploy --guided` from local. GitHub Actions can assume the deploy role via OIDC (no long-lived keys).
- **Non-functional:** Region `ap-southeast-1`. Single AWS account. Stack name `miti99bot-aws-port`. All resources tagged `app=miti99bot, env=prod`. Strict free-tier resources only.

## Architecture
```
GitHub Actions ─OIDC─► AWS IAM Role (github-deploy)
                       │  └─ trust: token.actions.githubusercontent.com
                       │  └─ scoped: repo:tiennm99/miti99bot-go:ref:refs/heads/main
                       │
                       └─► CloudFormation (SAM) ─► Lambda + DynamoDB + ParamStore + EventBridge + Logs
```

## Related Code Files
- Create: `template.yaml` (SAM root, all resources declared here)
- Create: `samconfig.toml` (stack name, region, capabilities)
- Create: `aws/iam-github-oidc-trust.json` (one-shot reference doc, not deployed)
- Create: `aws/README.md` (commands cheat sheet for first-time setup)
- Create: `Makefile` (targets: `build`, `package`, `deploy`, `logs`)
- Modify: `.gitignore` (add `.aws-sam/`, `samconfig.toml.local`)

## Implementation Steps
1. Create AWS account (or reuse existing). Enable MFA on root, create IAM admin user for one-time bootstrap. Set region default `ap-southeast-1`.
2. Create the GitHub OIDC identity provider in IAM: thumbprint, audience `sts.amazonaws.com`. (One-time, manual or via small CloudFormation snippet.)
3. Create IAM role `github-deploy-miti99bot` with trust policy scoped to `repo:tiennm99/miti99bot-go:ref:refs/heads/main` and `repo:tiennm99/miti99bot-go:ref:refs/heads/dev`. Attach managed policies for SAM deploy: CloudFormation, Lambda, DynamoDB, EventBridge, IAM (PassRole only), SSM Parameter Store, Logs, S3 (SAM staging bucket).
4. Write `template.yaml` skeleton:
   - `AWSTemplateFormatVersion: '2010-09-09'`, `Transform: AWS::Serverless-2016-10-31`
   - `Globals.Function`: `Runtime: provided.al2023`, `Architectures: [arm64]`, `MemorySize: 256`, `Timeout: 15`, `Tracing: Active` (still free at this volume)
   - `Resources.BotFunction`: empty handler (`bootstrap` not yet built), Function URL with `AuthType: NONE`
   - `Resources.BotTable`: DynamoDB on-demand, PK=`pk` (S), no GSI yet
   - Outputs: function URL, table name
5. Write `samconfig.toml` with stack name, region, capabilities (`CAPABILITY_IAM`).
6. First deploy: `sam build && sam deploy --guided` from local using bootstrap admin credentials. Confirm stack reaches `CREATE_COMPLETE`. Save the Function URL.
7. Verify GH Actions OIDC by running a one-shot workflow that calls `aws sts get-caller-identity` — confirms trust works without keys.
8. Manual smoke: `curl <function-url>` returns 502 (no handler yet) — proves URL is reachable.

## Success Criteria
- [ ] AWS account active, MFA on root, region default `ap-southeast-1`
- [ ] GitHub OIDC provider created
- [ ] `github-deploy-miti99bot` IAM role assumes successfully from a test GH Actions run
- [ ] `sam deploy` succeeds; stack `miti99bot-aws-port` in `CREATE_COMPLETE`
- [ ] DynamoDB table `miti99bot` exists, on-demand billing mode
- [ ] Function URL reachable (502 expected)
- [ ] AWS Cost Explorer shows $0 spend after 24h

## Risk Assessment
- **OIDC trust scope too loose** (any branch / any repo) → Mitigation: scope to specific repo + ref pattern; review `sub` claim in CloudTrail after first successful run.
- **IAM policy over-broad** → Mitigation: start with managed policies for speed, tighten in Phase 06 once resource ARNs stable.
- **SAM staging bucket created in wrong region / accumulates artifacts** → Mitigation: pin region in samconfig; add lifecycle rule (7-day expiration) on staging bucket.
- **CloudFormation drift** if user edits via console → Mitigation: forbid console edits, document in `aws/README.md`.

## Open questions
1. Single account vs separate dev/prod accounts? Single is simpler for solo dev; defer split until usage warrants.
2. Reuse SAM staging bucket from existing AWS work or fresh one? Fresh, scoped to this stack, easier to clean up.
3. Pin SAM CLI version in `Makefile`? Yes, document expected version (current latest works); rely on `setup-sam` action in CI to pin.
