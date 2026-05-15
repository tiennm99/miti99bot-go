---
phase: 5
title: "Cutover integration and Cloudflare decommission"
status: pending
priority: P1
effort: "2-3h"
dependencies: [1, 2, 3, 4]
---

# Phase 05: Cutover integration and Cloudflare decommission

## Overview
Fold the verified migration into the AWS cutover path, then decommission Cloudflare resources only after a successful freeze-window migration and AWS soak. This phase closes the data-consistency gap left by the original AWS port plan.

## Requirements
- Functional: production cutover moves webhook ownership to AWS without losing durable writes from the old Cloudflare stack.
- Non-functional: rollback is fast only before the first AWS-served write; after that, the plan is forward-fix only unless reverse sync is built later. Cloudflare resources are deleted only after verification, and operator steps are explicit.

## Architecture
- There is no legacy dual-write path, so final cutover uses a short **write-freeze window**:
  1. disable/pause Cloudflare cron triggers
  2. stop Cloudflare webhook intake so no new writes land there
  3. run final delta export/import + parity verify
  4. point Telegram webhook to AWS
  5. begin AWS soak
- Before the first AWS-served write, rollback is still a webhook restore.
- After the first AWS-served write, rollback is not symmetry; it becomes forward-fix only unless a reverse-sync mechanism exists.
- This keeps migration correctness simple and avoids inventing temporary cross-runtime replication.
- Cloudflare teardown is a separate final step after the AWS soak, not part of the initial webhook flip.

## Related Code Files
- Modify: `plans/260510-0114-aws-port/plan.md`
- Modify: `plans/260510-0114-aws-port/phase-07-cutover.md`
- Modify: `docs/deploy-aws.md`
- Modify: `docs/cf-to-aws-migration-runbook.md`
- Optional create: `docs/cf-decommission-checklist.md`

## Implementation Steps
1. Update the AWS cutover phase to depend on a green migration report.
2. Add the freeze-window sequence and pre-flip vs post-flip rollback semantics to the runbook.
3. Define the final delta import and verification command set.
4. Flip the Telegram webhook only after the final delta verify succeeds.
5. Add post-flip smoke checks for a migrated trading account, an existing lolschedule subscriber, and `/mstats` if `last_ping` is kept.
6. Soak on AWS, then remove CF Worker/KV/D1 only when rollback is no longer needed.
7. Archive or document any intentionally skipped legacy datasets before teardown.

## Success Criteria
- [ ] AWS cutover docs explicitly require a green migration report.
- [ ] Freeze-window steps are documented end-to-end.
- [ ] Pre-flip rollback and post-flip forward-fix semantics are documented explicitly.
- [ ] Final delta import and rollback commands are ready before webhook flip.
- [ ] Cloudflare resources are not deleted during the initial cutover window.
- [ ] After soak, CF teardown is documented and low-risk.

## Risk Assessment
The biggest risk is write drift between an early backfill and the final webhook flip. Mitigation: use a short freeze window for the last delta import instead of trying to add temporary dual-write behavior to a legacy system that lives outside this repo.
