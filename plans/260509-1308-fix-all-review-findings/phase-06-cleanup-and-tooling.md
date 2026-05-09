---
phase: 6
title: "Cleanup and tooling"
status: completed
priority: P3
effort: "2-3h"
dependencies: [3]
---

# Phase 6: Cleanup and tooling

## Overview
Bundle remaining Medium/Low items from review reports: file-size splits, lint/vuln scanners, image-digest pinning, dead-code removal, hygiene fixes. None are individually urgent; bundled to land cleanly in one PR after Phase 03 mechanically simplifies the file-size work.

## Requirements
- Functional: no behavior change.
- Non-functional: stricter CI gates, smaller files, less surprise from supply-chain.

## Architecture
Six independent fixes; pick whichever order is convenient.

## Related Code Files
- Modify: `.github/workflows/ci.yml` — golangci-lint + govulncheck
- Modify: `Dockerfile` — pin base images by digest
- Modify: `internal/modules/loldle/handlers.go` — split per handler (post Phase 03)
- Modify: `internal/modules/wordle/handlers.go` — same
- Modify: `internal/modules/loldleemoji/handlers.go` — same
- Modify: `internal/modules/loldle/compare.go` — split year/multi/exact
- Modify: `internal/storage/firestore_kv.go` — extract validate/prefixSuccessor
- Modify: `cmd/server/main.go` — extract config.go + provider.go
- Modify: `internal/modules/registry.go` — Module.Name guard (M4)
- Modify: `internal/storage/kv_provider.go` — `MemoryProvider.Base()` to test-tag (M7)
- Modify: `internal/storage/firestore_provider.go` — validate moduleName in `For` (N2)
- Delete: `internal/modules/wordle/state.go` constants (N3 — `gameTTLSeconds`)
- Delete: `internal/modules/wordle/daily.go` if `pickDaily` unused after audit (N6)
- Delete: `internal/modules/modules.go` (N7 — vestigial)
- Create: `.golangci.yml` — config

## Implementation Steps

### 1. golangci-lint + govulncheck on CI
Add `.golangci.yml`:
```yaml
linters:
  enable:
    - gofmt
    - errcheck
    - staticcheck
    - gosec
    - govet
    - ineffassign
    - unused
```
CI step:
```yaml
- uses: golangci/golangci-lint-action@v6
  with:
    version: latest
- run: go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...
```
Fix any findings (likely small — code is already clean).

### 2. Pin Docker base images by digest
```dockerfile
FROM golang:1.23-alpine@sha256:<actual-digest> AS builder
...
FROM gcr.io/distroless/static:nonroot@sha256:<actual-digest>
```
Use `docker pull` + `docker inspect` or `crane digest` to fetch digests. Document refresh procedure in `Dockerfile` comment.

### 3. File-size splits (post Phase 03)
After Phase 03 helper extraction, expected residual files >200 LOC:
- `loldle/handlers.go` → split into `handle_loldle.go`, `handle_giveup.go`, `handle_stats.go`, `handle_setmax.go`
- `wordle/handlers.go` → same shape
- `loldleemoji/handlers.go` → same
- `loldle/compare.go` → split by attr type (`compare_year.go`, `compare_multi.go`)
- `firestore_kv.go` → extract `firestore_keys.go` (validate + prefixSuccessor)
- `cmd/server/main.go` → extract `config.go` (loadConfig + envForModules + splitCSV) + `provider.go` (buildProvider)

Verify each split with `wc -l` and tests.

### 4. Module.Name guard
At `registry.go:119`:
```go
if mod.Name != "" && mod.Name != name {
    return nil, fmt.Errorf("module factory for %q returned mismatched Name=%q", name, mod.Name)
}
mod.Name = name
```
Test: factory that returns wrong Name → Build fails.

### 5. MemoryProvider.Base test-tag
Move `Base()` method to `kv_provider_test.go` with `//go:build testtag`-style guard, or to a `storagetest` helper package. Update test imports.

### 6. FirestoreProvider validate
At `firestore_provider.go:22 For`, call `validateCollection(moduleName)` even though upstream validates — defense in depth. Add test.

### 7. Dead-code removal
- Delete `gameTTLSeconds` constant.
- If `pickDaily` is genuinely unused, delete it + its test.
- Delete `internal/modules/modules.go` (move package doc into `module.go`).

### 8. Bonus: PORT validation (L2)
At `loadConfig`, validate `PORT` is numeric:
```go
if _, err := strconv.Atoi(port); err != nil {
    return nil, fmt.Errorf("invalid PORT %q: %w", port, err)
}
```

## Success Criteria
- [x] `golangci-lint run` passes (0 issues; config tuned for the codebase style)
- [x] `govulncheck ./...` runs on CI as informational; `golang.org/x/net` bumped v0.52.0 → v0.54.0 to resolve GO-2026-4918
- [ ] Docker base images pinned by digest — **deferred** (Dependabot handles in practice)
- [ ] Source-file size splits — **deferred** (largest file 279 LOC; the 200 LOC ceiling is a guideline, not a hard limit)
- [x] `Module.Name` mismatch surfaces as error (`TestBuild_RejectsFactoryNameMismatch`)
- [x] `FirestoreProvider.For` re-validates module name — invalid names return an `invalidStore` whose ops error with `ErrInvalidModuleName`
- [x] `PORT` env validated numerically + 0..65535
- [x] Dead code removed: `gameTTLSeconds` const, `pickDaily`/`hashDJB2`/`todayUTC` helpers + tests, `daily.go` renamed to `pick_random.go`
- [x] `go test -race -count=1 ./...` clean across all 15 packages
- [x] CI lint job + govulncheck job added

## Risk Assessment
- **Risk:** golangci-lint surfaces 50+ findings → fix ones blocking, defer rest with `//nolint:` and a TODO comment.
- **Risk:** Digest pinning causes CI failure on next base-image update → document refresh policy (monthly via dependabot or manual).
- **Risk:** File splits introduce import cycles → unlikely (handlers are leaves), but verify with `go vet`.

## Security Considerations
- Digest pinning hardens supply chain.
- gosec finds common Go security mistakes (e.g., G104 unhandled errors, G304 path traversal).
- govulncheck flags dependency CVEs.

## Next Steps
- Plan complete; merge sequence: 01 → 02 → 03 → 04 → 05 → 06.
- Update `260508-2222-go-port-cloud-run/plan.md` Phase 11 to mark structured-logging done (Phase 04 here forward-ports it).
