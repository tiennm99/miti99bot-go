---
title: Deploy notification to bot owner with git SHA
description: >-
  On startup the bot DMs BOT_OWNER_ID with the deployed git SHA. Dedup via
  DynamoDB so only real new versions notify, not every Lambda cold start.
status: completed
priority: P3
branch: main
tags:
  - ops
  - observability
  - telegram
blockedBy: []
blocks: []
created: '2026-05-22T04:10:07.522Z'
createdBy: 'ck:plan'
source: skill
---

# Deploy notification to bot owner with git SHA

## Overview

Operator awareness of deploys. After `make build-lambda` + `sam deploy`, the
owner currently has no in-Telegram signal that the new code is running on
Lambda (only the GitHub Actions log). Add a startup hook that DMs the owner
with the baked-in short git SHA, deduped by KV so subsequent cold starts of
the same version stay silent.

Confirmation that the **new code is running**, not just that the deploy
script finished — that's why this lives in the bot binary, not in the deploy
workflow.

## Design Decisions (locked)

- **Dedup**: DynamoDB KV stores `last_notified_sha`. Send only when baked
  `gitSHA != stored`, then write. One `GetItem` per cold start (~free tier).
- **Code placement**: new `internal/deploynotify/` package — testable in
  isolation, ~50 LOC, main.go just calls `deploynotify.Run(...)`.
- **Build wiring**: `-ldflags "-X main.gitSHA=<short-sha>"` in Makefile.
  Empty `gitSHA` (local non-make build) → silently skip.
- **Failure policy**: log + continue. Never block server startup. KV error,
  Telegram error, missing owner — all non-fatal.
- **Timing**: synchronous, ≤3s timeout, runs after `modules.Install` and
  before `srv.ListenAndServe()`. Lambda init phase has 10s headroom.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Implement deploynotify package + main.go hook](./phase-01-implement-deploynotify-package-main-go-hook.md) | Completed |
| 2 | [Wire git SHA into build](./phase-02-wire-git-sha-into-build.md) | Completed |

## Dependencies

None.

## Out of Scope

- Multi-environment routing (prod vs staging) — single owner, single env today.
- Notify on rollback — same dedup mechanism naturally handles it; rollback to
  a previously-notified SHA just resends because stored value moved forward.
  Acceptable.
- Conditional KV write to prevent concurrent-cold-start dupes — KV interface
  has no CAS today; the race window is narrow and the failure mode is
  "two identical DMs", not data corruption.
