---
title: "Fix all review findings (architecture + security + tests)"
description: "Remediation plan covering Critical/High/Major/Medium findings from the 2026-05-09 whole-project review (architecture, security, test-coverage)."
status: pending
priority: P1
effort: 1.5-2d
branch: main
tags: [fixes, review, hardening, tests, ci]
created: 2026-05-09
blockedBy: []
blocks: []
---

# Plan: Fix all review findings

Six phases ordered by risk-gate. Phase 1 must land before next merge (Dockerfile/go.mod mismatch breaks `docker build`). Phases 2–4 are pre-public-launch hardening. Phase 5 closes the handler-layer test gap. Phase 6 is cleanup.

## Source reports
- [Architecture & code quality](../reports/code-reviewer-260509-1248-whole-project-architecture.md)
- [Security audit](../reports/code-reviewer-260509-1248-whole-project-security.md)
- [Test coverage audit](../reports/tester-260509-1249-whole-project-coverage.md)

## Phases

| # | Phase | Status | Effort | Key deliverable |
|---|-------|--------|--------|-----------------|
| 01 | [Critical blockers](phase-01-critical-blockers.md) | done | 30min | Go-version alignment + 4 nil-deref guards + CI docker-build step |
| 02 | [High-priority hardening](phase-02-high-priority-hardening.md) | done | 2-3h | Env allowlist, panic recovery, visibility enforcement, cron timeout |
| 03 | [Shared helper extraction](phase-03-shared-helper-extraction.md) | done | 1-2h | `internal/modules/util/chathelper` + `internal/champname` (DRY) |
| 04 | [Structured logging](phase-04-structured-logging.md) | done | 2-3h | `internal/log` slog.JSONHandler + 22-site rewire (forward-port from Phase 11) |
| 05 | [Test coverage gaps](phase-05-test-coverage-gaps.md) | pending | 6-8h | Handler integration tests (wordle/misc/util/loldle/loldleemoji) + Firestore emulator on CI |
| 06 | [Cleanup and tooling](phase-06-cleanup-and-tooling.md) | pending | 2-3h | File-size splits, golangci-lint, govulncheck, image-digest pinning, dead-code removal |

## Key dependencies
- Phase 03 must precede next module port in `260508-2222-go-port-cloud-run` (Phase 6b/7) so future modules don't compound helper drift.
- Phase 04 is forward-port of Phase 11 from the active port plan; landing earlier reduces migration cost on each new module.
- Phase 06 file splits are mechanically cleaner after Phase 03 helper extraction.

## Out of scope
- Phase 9 OIDC migration for `/cron/*` (tracked in port plan).
- Async-with-detached-context webhook dispatch (M2 from architecture report — defer until cold-start telemetry exists).
- `pickRandom` cryptographic upgrade (L5 — non-issue today).
- Sticker `file_id` rotation procedure (L6 — operations doc, not code).

## Validation
- `go vet ./...` clean
- `go test -race -count=1 ./...` clean
- `docker build -t miti99bot-go .` succeeds
- New CI workflow steps green
- Coverage rises from 44.7% → ≥60%
