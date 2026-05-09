---
phase: 1
title: "Cosmetics: README + plan status sync"
status: pending
priority: P3
effort: "30m"
dependencies: []
---

# Phase 01: Cosmetics — README + plan status sync

## Overview
README still says "Cloud Run + Firestore"; AWS-port phases 02/03/05 say "pending" though their code already shipped. Sync both before any new clone or visitor reads stale docs.

## Requirements
- **Functional:** README describes AWS as default deploy, with link to `docs/deploy-aws.md`. AWS-port plan's phase-status table reflects shipped code. GCP plan's tagline note about supersession remains.
- **Non-functional:** No code changes. Markdown-only.

## Architecture
N/A — documentation update.

## Related Code Files
- Modify: `README.md`
- Modify: `plans/260510-0114-aws-port/plan.md` (status column)
- Reference (no edit): `plans/260508-2222-go-port-cloud-run/plan.md` (already annotated)

## Implementation Steps
1. Rewrite `README.md`:
   - Tagline: "Plug-n-play Telegram bot framework in Go. Default deploy: AWS Lambda + DynamoDB + EventBridge (free tier). Cloud Run path retained as alt."
   - Status table: collapse to one row per work-phase; mark current state honestly.
   - "Run locally" section: keep in-memory KV path; add note about `make dynamodb-local` for DynamoDB integration testing; keep `make firestore-emulator` line.
   - "Deploy" section: link `docs/deploy-aws.md` as canonical; mention Dockerfile for non-Lambda hosts.
   - "Test" section: add `make test-dynamodb` line.
2. Update `plans/260510-0114-aws-port/plan.md` phases table:
   - Phase 01 → still pending (manual user steps)
   - Phase 02 → "code-done; awaiting first deploy"
   - Phase 03 → "code-done; integration tests skip without DynamoDB Local"
   - Phase 04 → still pending (this plan unblocks it)
   - Phase 05 → "done"
   - Phase 06 → "partial" — budget alert in template; metric filter deferred to this plan's Phase 02
   - Phase 07 → still pending (deploy-gated)
3. Smoke-render the README locally (`grip` or just open in editor) — confirm headings, links, and code blocks render cleanly.

## Success Criteria
- [ ] README's intro line names AWS as default
- [ ] README's status table accurate (no "pending" rows that are actually done)
- [ ] AWS-port plan.md phase statuses reflect shipped code
- [ ] All link targets resolve (no 404 in `docs/deploy-aws.md`, `aws/README.md`, both plan files)
- [ ] No broken markdown rendering

## Risk Assessment
- **Drift between README and plan.md** if updated separately later — Mitigation: this phase is the single place both get touched together; future drift caught in Phase 06 cutover.
- **Stale Cloud Run instructions misleading new contributors** — Mitigation: prefix the alt-path section with "Alternative: Cloud Run (deferred)" so the canonical path is unambiguous.

## Open questions
1. Move Cloud Run instructions into `docs/deploy-gcp-cloud-run.md` instead of inlining? Cleaner README but adds a file. Default: inline a short note + link to old plan.
2. Add CI badge to README? Skip for v1 — no public bot, no marketing pressure.
