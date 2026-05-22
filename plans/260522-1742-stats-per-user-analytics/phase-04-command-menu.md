---
phase: 4
title: Command menu
status: completed
priority: P3
effort: 10m
dependencies:
  - 2
---

# Phase 4: Command menu

## Overview

Telegram's `setMyCommands` registers the command auto-complete shown in the input field. `/stats` is already registered (commit `db8ee9c`). Subcommands are not — clients won't auto-suggest `/stats users` etc. Decide whether to (a) leave the menu alone (subcommands are discoverable via the usage string), or (b) add the four forms.

## Requirements

- Functional: `/stats` remains in `aws/telegram-commands.json`.
- Optional: add `stats_users`, `stats_user`, `stats_cmd` *or* document subcommands in the existing `/stats` description.

## Architecture

Two paths:

1. **Single entry, updated description.** Edit the existing `/stats` description in `aws/telegram-commands.json` to read `Stats: /stats, /stats users, /stats user <name>, /stats cmd <name>`. Cheap, discoverable via the menu hover.
2. **Multiple entries.** Add aliases that route into the same handler. Requires either (a) registering separate Telegram menu entries that point to the same `/stats *` invocation (Telegram doesn't enforce uniqueness; description is the only hint), or (b) creating real command aliases in code. (b) clutters `/help`.

Default: option 1.

## Related Code Files

- Modify: `aws/telegram-commands.json` — update `/stats` description.

## Implementation Steps

1. Read `aws/telegram-commands.json`.
2. Update the `/stats` description field. New text suggestion: `Show stats. Try: /stats users, /stats user <name>, /stats cmd <name>`.
3. The `Register Telegram command menu` step in `.github/workflows/deploy.yml:108-124` will push the update on next deploy. No code change required.

## Success Criteria

- [ ] JSON is valid (`jq . aws/telegram-commands.json` succeeds).
- [ ] Description fits Telegram's 256-char limit per command.
- [ ] On the next deploy, the bot's command menu hover text shows the subcommand hint.

## Risk Assessment

- **Risk:** Description length limit. Mitigation: keep under 100 chars; the proposed text is ~70.
