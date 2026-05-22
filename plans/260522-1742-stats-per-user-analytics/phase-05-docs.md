---
phase: 5
title: Docs
status: completed
priority: P3
effort: 15m
dependencies:
  - 2
---

# Phase 5: Docs

## Overview

Update repo docs so future readers understand the new schema and views without reading the diff.

## Requirements

- README module table mentions the extended `/stats` capabilities.
- `docs/system-architecture.md` (or codebase summary) describes the per-user key schema.
- `docs/project-changelog.md` records the feature.

## Architecture

Touch only the minimum doc surface; do not create new files. Per CLAUDE.md, docs live under `./docs/` and are kept current.

## Related Code Files

- Modify: `README.md` — extend the `util`/`stats` module row.
- Modify: `docs/system-architecture.md` (or closest equivalent — check actual file list).
- Modify: `docs/project-changelog.md` — add an entry under today's date.

## Implementation Steps

1. `ls docs/` to confirm which of system-architecture / codebase-summary actually exist.
2. README: extend the `stats` row in the modules table (currently doesn't appear in README — add a one-liner: `stats: /stats, /stats users, /stats user <name>, /stats cmd <name>`).
3. system-architecture (or codebase-summary): add a short subsection under the Stats module describing the three sort-key shapes (`count:`, `user:`, `pair:`).
4. project-changelog: add `## 2026-05-22` (or appropriate date) entry with feature summary.

## Success Criteria

- [ ] README documents the new subcommands.
- [ ] One of the architecture docs lists the three storage keys.
- [ ] Changelog has a dated entry.

## Risk Assessment

- Low. Doc-only.
