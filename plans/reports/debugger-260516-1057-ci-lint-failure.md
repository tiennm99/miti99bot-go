# Debug + Fix Report — ci.yml lint failure on commit `1f5f304`

**Date:** 2026-05-16
**Failed run:** https://github.com/tiennm99/miti99bot/actions/runs/25952128154
**Workflow:** `.github/workflows/ci.yml` job `go (1.25)` step `golangci-lint`
**Status:** DONE — fixes verified locally; awaiting CI re-run on next commit.

## Root cause

9 `errcheck` violations introduced by commit `d67517e feat(migration): cf→aws migration toolchain ...` and pushed alongside `39491d1` + my `1f5f304` in the same `git push`. The migration-toolchain commit was the first one to add these unchecked errors; prior commit `75e9360` (docs-only) didn't include them. My deploy-workflow commit `1f5f304` is the first that triggered a CI run after `d67517e` landed, so the lint failure surfaced here even though my change touched zero Go files.

**Not** a regression caused by the auto-register workflow change.

## Violations

| File | Line | Pattern |
|---|---|---|
| `cmd/migrate_cf_data/main.go` | 198 | `defer f.Close()` |
| `internal/migration/cloudflare_d1_client.go` | 73 | `defer resp.Body.Close()` |
| `internal/migration/cloudflare_kv_client.go` | 122 | `defer resp.Body.Close()` |
| `internal/migration/report.go` | 41,42,47,52,54,63 | `fmt.Fprintln` / `fmt.Fprintf` to `io.Writer` |

## Fixes applied

**Close calls** — wrap in anonymous defer with underscore-assignment, matching the established project pattern in `internal/modules/lolschedule/api_client.go:162` and `internal/modules/trading/prices.go:90`:

```go
defer func() { _ = resp.Body.Close() }()
```

**Fprint* calls** — explicit underscore-assignment, idiomatic for best-effort writes to `os.Stdout` / `*bytes.Buffer` (the only callers per `grep .Format(`):

```go
_, _ = fmt.Fprintln(w, "Migration report")
```

Also added one comment to `Report.Format` noting the rationale (write errors not actionable for the actual callers).

## Verification

| Check | Result |
|---|---|
| `go vet ./...` | clean |
| `golangci-lint run ./...` | `0 issues.` |
| `go test -race -count=1 ./...` | all 18 packages pass (incl. `internal/migration`) |
| `go build ./...` | clean |

## Files changed (4 production)

- `cmd/migrate_cf_data/main.go`
- `internal/migration/cloudflare_d1_client.go`
- `internal/migration/cloudflare_kv_client.go`
- `internal/migration/report.go`

## Why not just add `//nolint:errcheck`?

Project has zero `//nolint` directives in `internal/` or `cmd/` (verified by `grep -rn "nolint"` — no hits). Anonymous-defer + `_, _ =` matches the existing house style, so no new lint-suppression convention.

## Why not change `Report.Format` to return `error`?

API change for ~6 lines of cosmetic improvement; only 1 production caller (`cmd/migrate_cf_data/main.go:172 — report.Format(os.Stdout)`) which would discard the error anyway since stdout writes are not recoverable. KISS / YAGNI.

## Unresolved questions

None.
