---
phase: 2
title: "Backfill toolchain and safety rails"
status: pending
priority: P1
effort: "3-4h"
dependencies: [1]
---

# Phase 02: Backfill toolchain and safety rails

## Overview
Build operator-run migration tooling in Go that can read legacy Cloudflare data, write DynamoDB records idempotently, and support dry-runs plus checkpoints. This phase is about controlled mechanics, not the actual production import yet.

## Requirements
- Functional: provide commands for KV export/import, trading import, and parity verification inputs.
- Non-functional: idempotent writes, dry-run mode, resumable progress, zero admin HTTP surface, and no dependency on the running AWS bot process.

## Architecture
- Keep tooling inside this repo and language stack, but keep it small:
  - `cmd/migrate_cf_data/` for inventory + KV import + trading import modes
  - `cmd/verify_cf_aws_parity/` for verification only
- Shared logic lives under `internal/migration/` for Cloudflare REST reads, DynamoDB writes, and report formatting.
- D1 source extraction stays simple: operator uses `wrangler d1 execute ... --json --remote` to create local JSON exports; Go import code consumes those files instead of re-implementing remote SQL access.
- Checkpoint/resume is conditional: add it only if Phase 01 proves the keyspace is large enough to justify it.
- No writes happen during `--dry-run`; output is a machine-readable summary plus human-readable progress logs.

## Related Code Files
- Create: `cmd/migrate_cf_data/main.go`
- Create: `cmd/verify_cf_aws_parity/main.go`
- Create: `internal/migration/cloudflare_kv_client.go`
- Create: `internal/migration/dynamodb_writer.go`
- Create: `internal/migration/report.go`
- Optional create: `internal/migration/checkpoint_store.go`
- Modify: `go.mod`
- Modify: `docs/cf-to-aws-migration-runbook.md`

## Implementation Steps
1. Define CLI flags and env contract for Cloudflare and AWS credentials.
2. Implement KV list/get readers against Cloudflare REST with pagination support.
3. Implement DynamoDB writers against the live runtime shape: `pk = moduleName`, `sk = caller key`.
4. Add checkpoint files only if Phase 01 proves resume support is worth the extra surface area.
5. Add dry-run and report output before any real import path is allowed.
6. Document the exact operator workflow in the runbook.

## Success Criteria
- [ ] Tooling reads CF KV metadata and values without touching app code paths.
- [ ] Trading import mode accepts local D1 JSON exports.
- [ ] Every command supports `--dry-run`.
- [ ] Import path is idempotent or safely merge-based.
- [ ] Checkpoint/resume behavior is either justified and documented or intentionally omitted.

## Risk Assessment
The main risk is embedding too much migration logic into one giant binary. Mitigation: split command entrypoints and keep shared logic in small `internal/migration/` helpers so each command stays reviewable and under the repo's file-size guidance.
