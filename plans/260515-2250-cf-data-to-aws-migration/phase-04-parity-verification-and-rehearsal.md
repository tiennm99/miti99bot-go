---
phase: 4
title: "Parity verification and rehearsal"
status: pending
priority: P1
effort: "2-3h"
dependencies: [2, 3]
---

# Phase 04: Parity verification and rehearsal

## Overview
Prove the imported AWS data matches the Cloudflare source closely enough to trust a real cutover. This phase turns migration from a one-off script run into a repeatable, auditable procedure with a staging-table rehearsal.

## Requirements
- Functional: verify counts, sample payload parity, and trading portfolio correctness between CF exports and DynamoDB.
- Non-functional: produce a saved report, support reruns, and define rollback steps before the production webhook is moved.

## Architecture
- Verifier compares source exports against the AWS target table using the same module/key selectors from Phase 01.
- Checks by dataset type:
  - KV durable records: count parity + payload/hash comparisons
  - trading portfolios: count parity + deep field comparison on currency, assets, and invested metadata
- Rehearsal happens against a staging DynamoDB table only. No destructive rerun path is allowed against the live table.
- Final output is a migration report under `plans/reports/` plus runbook updates.

## Related Code Files
- Create: `cmd/verify_cf_aws_parity/main.go`
- Create: `internal/migration/parity_checks.go`
- Create: `internal/migration/rollback_scope.go`
- Modify: `docs/cf-to-aws-migration-runbook.md`
- Create during execution: `plans/reports/migration-260515-2250-cf-data-to-aws-parity.md`

## Implementation Steps
1. Implement count and payload verification per migrated dataset.
2. Add trading-specific deep checks against the current `Portfolio` shape.
3. Save the verifier result as a markdown report under `plans/reports/`.
4. Rehearse import + verify against a staging DynamoDB table.
5. Promote the exact same procedure to the live table only after staging is green.
6. Mark the migration runbook ready only after a green verifier report.

## Success Criteria
- [ ] Verifier reports pass for all migrated datasets.
- [ ] Trading portfolios match expected balances and holdings on spot checks.
- [ ] A staging-table rehearsal completes successfully without touching the live table.
- [ ] The migration report is saved and linked from the runbook.
- [ ] The cutover checklist now depends on a green parity report.

## Risk Assessment
The main risks are false confidence from count-only checks and accidental destructive rehearsal against production storage. Mitigation: include dataset-specific deep comparisons, especially for trading portfolios, and require a saved report plus staging-table-only rehearsal before the cutover phase can begin.
