---
title: Stats per-user analytics
description: >-
  Extend /stats with per-user breakdowns: top users overall, per-user command
  history, per-command user ranking. Username-only display, all public,
  free-tier safe.
status: completed
priority: P2
branch: main
tags:
  - stats
  - telegram
  - dynamodb
blockedBy: []
blocks: []
created: '2026-05-22T10:42:15.385Z'
createdBy: 'ck:plan'
source: skill
---

# Stats per-user analytics

## Overview

`/stats` today tracks `count:<cmd>` only — a single counter per command. Extend to capture per-user counts so the owner can see who uses the bot most, which command a given user runs most, and who uses a given command most. All views remain public per user decision (no admin gating). Display uses `@username` only; first_name is not stored. Users without a Telegram username are excluded from per-user listings but still increment the global command total.

## Scope decisions (user-confirmed)

- All `/stats *` subcommands are `VisibilityPublic`. No admin gating.
- Store `username` only; do not store `first_name`.
- If `update.Message.From.Username == ""`, skip `user:` / `pair:` writes for that invocation (still increments `count:<cmd>`). Acknowledged trade-off: unnamed users are unattributed.
- Top-K cap = 20 entries per view (existing 4000-char truncation kept as belt-and-braces).

## Storage schema

Single DynamoDB partition `pk = "stats"`. Three sort-key shapes:

| Sort key | Value JSON | Purpose |
|---|---|---|
| `count:<cmd>` | `{"n":<int>}` | Existing. Total per command. |
| `user:<userID>` | `{"username":"<str>","n":<int>}` | New. Per-user total + cached display name. |
| `pair:<cmd>:<userID>` | `{"n":<int>}` | New. Per (command, user) pair. |

`<userID>` is decimal stringified `update.Message.From.ID`. `<cmd>` is the registered command name (no `/`).

## Query patterns

| View | Method |
|---|---|
| `/stats` (existing) | `List("count:")` → fan-out GetItem → sort desc by n |
| `/stats users` | `List("user:")` → fan-out GetItem → sort desc by n |
| `/stats user @foo` | `List("user:")` → fan-out GetItem → find matching username → derive userID → `List("pair:*:userID")` is not supported (sort-key wildcard); instead `List("pair:")` + filter by `:<userID>` suffix |
| `/stats cmd help` | `List("pair:help:")` → fan-out GetItem; for each, GetItem `user:<id>` to resolve username |

`List("pair:")` worst case = N_users × N_commands rows. At scale, replace with a `byuser:<userID>:<cmd>` mirror key to enable prefix scan. **Deferred to a future phase** (see Risks); current free-tier traffic does not justify the extra write.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Hook signature + per-user write path](./phase-01-hook-signature-per-user-write-path.md) | Completed |
| 2 | [Subcommand parser + new /stats views](./phase-02-subcommand-parser-new-stats-views.md) | Completed |
| 3 | [Tests](./phase-03-tests.md) | Completed |
| 4 | [Command menu](./phase-04-command-menu.md) | Completed |
| 5 | [Docs](./phase-05-docs.md) | Completed |

## Dependencies

No cross-plan dependencies. Builds directly on the already-deployed stats module.

## Out of scope

- Atomic increments (DynamoDB UpdateItem ADD). Existing race is documented and accepted.
- Per-chat stats. Bot is single-owner; chat-scope breakdown not requested.
- Backfill. Per-user attribution starts from deploy time. Existing `count:` keys carry over unchanged.
- Privacy gating, anonymization, first_name capture. User declined.

## Unresolved questions

None.
