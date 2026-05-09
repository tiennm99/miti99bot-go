---
phase: 1
title: "Critical blockers"
status: completed
priority: P1
effort: "30min"
dependencies: []
---

# Phase 1: Critical blockers

## Overview
Two cross-phase blockers that prevent the next merge from working: Go-version mismatch breaks `docker build`; three nil-deref sites match a pattern Phase 5a fixed but never propagated. Plus a CI gap that let issue #1 ship silently.

## Requirements
- Functional: `docker build` succeeds locally and in CI; misc/help handlers tolerate `update.Message == nil`.
- Non-functional: future Go-version drift surfaces in CI, not at deploy time.

## Architecture
Three independent fixes; no design choices needed beyond version-bump direction.

## Related Code Files
- Modify: `Dockerfile` — bump builder image
- Modify: `.github/workflows/ci.yml` — bump `go-version`, add `docker build` step
- Modify: `go.mod` (alternative path: lower `go` directive)
- Modify: `internal/modules/misc/misc.go` — guard at lines 54, 79, 94
- Modify: `internal/modules/util/help.go` — guard at line 100

## Implementation Steps

### 1. Pick Go-version direction
Two options, equivalent outcome:
- **(a) Bump up** — `Dockerfile:1` → `golang:1.25-alpine`; `ci.yml:19` `go-version: '1.25'`. Match local toolchain.
- **(b) Lower go.mod** — `go.mod:3` → `go 1.23.0`. Codebase uses no 1.24/1.25 features; cheapest fix.

Recommended: **(b)** unless team standard is 1.25.

### 2. Add `docker build` to CI
After `go build` step in `.github/workflows/ci.yml`:
```yaml
- name: Docker build
  run: docker build -t miti99bot-go .
```
~3 lines. Catches future Dockerfile/go.mod drift.

### 3. Add nil-message guards
Match the pattern at `internal/modules/util/info.go:36`:
```go
if update.Message == nil {
    return nil
}
```
Apply to:
- `misc.go:54` (pingCommand handler)
- `misc.go:79` (mstatsCommand handler)
- `misc.go:94` (fortytwoCommand handler)
- `util/help.go:100` (helpCommand handler)

## Success Criteria
- [x] `go.mod`, `Dockerfile`, `ci.yml` agree on Go version (all 1.25; bumped Dockerfile + CI up to match go.mod)
- [x] `docker build -t miti99bot-go .` succeeds locally
- [x] CI workflow includes `docker build` step
- [x] Four handlers have `update.Message == nil` guard at top (misc ×3, util/help ×1)
- [x] `go vet ./...` and `go test -race -count=1 ./...` clean

## Risk Assessment
- **Risk:** Bumping go.mod down hides a feature use we missed → `go build` catches at compile time.
- **Risk:** New CI step adds ~30s build time → acceptable; container build is what production uses.
- **Mitigation:** Test locally before push; CI matrix runs on every PR.
