---
phase: 3
title: Tests
status: completed
priority: P1
effort: 1h
dependencies:
  - 1
  - 2
---

# Phase 3: Tests

## Overview

Add unit tests for the new Inc fan-out and each subcommand view, using the in-memory KV store. Keep tests deterministic; no DynamoDB Local required for module-level tests.

## Requirements

- Cover `Inc` writing all three keys when username is non-empty.
- Cover `Inc` skipping `user:`/`pair:` writes when username is empty but still writing `count:`.
- Cover each subcommand view: bare, users, user, cmd, unknown, not-found.
- Cover the 4000-char truncation path with a synthetic dataset.
- Tests must pass with `make test` (no DynamoDB needed); `make test-dynamodb` continues to pass for storage layer.

## Architecture

`internal/storage/memory_kv.go` provides `*MemoryKV` implementing `KVStore`. Tests instantiate a `counter{kv: NewMemoryKV()}` and a fake `*models.Update`. Use `t.Run` subtests, one per view.

For subcommand parsing, prefer driving the public Handler with a constructed `*models.Update` rather than testing the internal parser in isolation — the parser interaction with entity offsets is the riskier surface.

The Telegram bot send-message side effect is hard to assert directly. Two options:
- (a) Refactor the Handler to return the rendered string, with `chathelper.Reply` as a thin wrapper. Lower-risk.
- (b) Mock `bot.Bot.SendMessage`. Heavier.

Pick (a) if it's a small change; otherwise mock or skip the send and assert on the input data passed to `renderTopN`.

## Related Code Files

- Modify: `internal/modules/stats/stats_test.go` — add subtests.
- (Optional) Modify: `internal/modules/stats/stats.go` — extract a pure `renderStatsReply(view, args, ...) string` if needed to make output assertable without touching `bot.Bot`.

## Implementation Steps

1. Read current `internal/modules/stats/stats_test.go` to learn the existing fixture conventions.
2. Add `TestCounterIncWritesAllThreeKeys` — invoke `Inc` with username, assert all three KV entries materialise with N==1; invoke twice, assert N==2.
3. Add `TestCounterIncSkipsUserKeysWhenUsernameEmpty` — invoke with empty username, assert only `count:<cmd>` exists.
4. Add a table-driven `TestStatsViews` with cases for: bare, `users`, `user <known>`, `user <unknown>`, `cmd <known>`, `cmd <unknown>`, `bogus`. Seed the KV with a known fixture, drive the Handler (or `renderStatsReply` per Architecture above), assert substring matches.
5. Add `TestStatsViewTruncates` — seed 200 commands/users to push past 4000 chars; assert the reply ends with `…(truncated)`.
6. Run `make vet test`. Iterate until green.

## Success Criteria

- [ ] `make vet` clean.
- [ ] `make test` passes including the new cases.
- [ ] No flaky tests (re-run 3×).
- [ ] Coverage on `stats.go` ≥ existing baseline.

## Risk Assessment

- **Risk:** Refactoring `Reply` out of the handler to make output assertable touches the existing bare-stats path. Mitigation: keep `chathelper.Reply` call at the leaf; only the string-building moves.
- **Risk:** `*models.Update` is non-trivial to construct. Mitigation: copy an existing fixture from `dispatcher_test.go` if one exists; otherwise build the minimal subset (Message.From.ID/Username, Message.Text, Message.Entities).
