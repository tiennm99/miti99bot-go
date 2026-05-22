---
phase: 2
title: Subcommand parser + new /stats views
status: completed
priority: P1
effort: 2h
dependencies:
  - 1
---

# Phase 2: Subcommand parser + new /stats views

## Overview

Extend the existing `/stats` handler to dispatch on the first whitespace-separated token after the command (e.g. `/stats users`). Implement three new views: `users`, `user <username>`, `cmd <name>`. Bare `/stats` keeps its current behaviour (top commands).

## Requirements

- Functional: `/stats` (no args) → top 20 commands by count. Unchanged.
- Functional: `/stats users` → top 20 users by total invocations. Lines `@<username>: <n>`.
- Functional: `/stats user <username>` → top 20 commands invoked by that user. Accept with or without leading `@`. Reply `User @<username> not found.` if unknown.
- Functional: `/stats cmd <name>` → top 20 users who invoked `<name>`. Reply `Command <name> not found or has no users.` if no `pair:<name>:*` rows.
- Functional: unknown subcommand → reply `Usage:\n/stats\n/stats users\n/stats user <username>\n/stats cmd <name>`.
- Non-functional: every view fans out GetItem reads (existing pattern at `stats.go:94-109`). No sequential per-key reads.
- Non-functional: 4000-char truncation kept (`stats.go:129-137`). Top-K cap = 20 entries.

## Architecture

The handler reads `update.Message.Text`, strips the command entity using the same `@botname`-aware logic used in `dispatcher.matchCommand`, and trims leading whitespace. The remainder is `subargs`. `strings.Fields(subargs)` gives the subcommand token + tail.

Renders share one helper that takes `[]row` (name + n) and returns the truncated reply string — DRYs the four views.

Username → userID resolution: List the `user:` prefix, fan-out GetItem, linear scan for matching `Username`. Worst case is `len(users)` GetItems — same cost as `/stats users`, well under the 10s webhook deadline for any realistic free-tier user count.

`/stats cmd <name>` lists `pair:<name>:`. Each row's sort key is `pair:<name>:<id>`; userID extracted by trimming the prefix. For each such userID, fetch `user:<id>` in parallel to resolve username. Skip rows whose `user:<id>` lookup returns ErrNotFound or empty Username (defensive: pair without user record means race or pre-existing data).

## Related Code Files

- Modify: `internal/modules/stats/stats.go` — extend `statsCommand` handler, add view helpers.

No new files. Total addition expected ~150 lines, keeping `stats.go` under the 200-line guideline becomes tight — if it crosses, extract `stats.go` views into `internal/modules/stats/views.go`.

## Implementation Steps

1. Add helper `parseSubargs(update *models.Update, cmdName string) string` that mirrors `dispatcher.matchCommand`'s entity-stripping. Returns the trimmed remainder after the command token.
2. Add helper `renderTopN(title string, rows []row, maxLen int) string` — folds the existing render+truncate logic. `row` gains an explicit `display string` field so users (`@username`) and commands (`/help`) render with the right prefix.
3. Replace the body of the `statsCommand` Handler with a switch over the first token of `parseSubargs`:
   - `""` → existing top-commands path.
   - `"users"` → fetch `user:` rows, fan-out, sort desc by N, render with `display = "@" + username`. Skip rows with empty Username.
   - `"user"` → require one arg; resolve username → userID via `user:` listing; if not found, reply not-found. Otherwise list `pair:`, filter by `:<userID>` suffix, fan-out GetItem, sort desc, render with `display = "/" + cmd`.
   - `"cmd"` → require one arg; list `pair:<arg>:`, fan-out, resolve usernames via parallel `user:<id>` reads, sort desc, render with `display = "@" + username`.
   - Anything else → reply usage string.
4. If `stats.go` exceeds ~200 lines, extract per-view helpers into `internal/modules/stats/views.go` (same package).
5. Run `make vet test` and a local end-to-end smoke against an in-memory KV (`MODULES=stats go run ./cmd/server` against ngrok or a manual fake update fixture if available).

## Success Criteria

- [ ] `/stats` returns the existing top-commands view byte-for-byte (regression check via test fixture).
- [ ] `/stats users`, `/stats user <name>`, `/stats cmd <name>` each return correctly sorted, truncated output.
- [ ] Unknown subcommand returns the usage string.
- [ ] `/stats user <unknown>` and `/stats cmd <unknown>` return clear not-found messages.
- [ ] Handler completes within 2s wall-clock on local in-memory KV; budget for DynamoDB is generous given fan-out.

## Risk Assessment

- **Risk:** `List("pair:")` is unused — `cmd` view only lists `pair:<name>:` so prefix is bounded. `user` view derives userID then filters; if `pair:` grows large, this becomes O(commands × users). Mitigation: top-K is 20 per view; sort handles the cap. If perf degrades, add a `byuser:<id>:<cmd>` mirror key in a follow-up.
- **Risk:** Race between Phase 1 writes and Phase 2 reads (eventually-consistent reads default in DDB). `DynamoDBKVStore.Get` uses `ConsistentRead: true` (see `dynamodb_kv.go:49`), so reads are strong. No risk.
- **Risk:** A user changes their Telegram username between the `Inc` write and the `/stats users` render — stale display. Acceptable; refresh on next invocation.
