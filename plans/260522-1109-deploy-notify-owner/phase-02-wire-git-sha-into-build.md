---
phase: 2
title: Wire git SHA into build
status: completed
priority: P3
effort: 20m
dependencies:
  - 1
---

# Phase 2: Wire git SHA into build

## Overview

Inject the short git SHA into the binary at link time via
`-X main.gitSHA=…`. Without this, Phase 1's `Run` short-circuits and the
feature is dormant. Touches Makefile only; GitHub Actions already runs
`make build-lambda`, so no workflow change is required.

## Requirements

**Functional**
- `make build-lambda` produces a binary whose `main.gitSHA` equals
  `git rev-parse --short HEAD` at build time.
- `make build` (local host binary) does the same — useful for dogfooding.
- Both targets degrade gracefully if `git` is unavailable: empty SHA →
  Phase 1's `Run` silently skips.

**Non-functional**
- No new tools or actions added to CI.
- `actions/checkout@v6` default depth (shallow) must support
  `git rev-parse --short HEAD` — it does, HEAD is always present.

## Architecture

```makefile
GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null)

LDFLAGS := -s -w -X main.gitSHA=$(GIT_SHA)

build:
    CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o ./bin/server ./cmd/server

build-lambda:
    @mkdir -p $(dir $(LAMBDA_OUT))
    CGO_ENABLED=0 GOOS=$(LAMBDA_GOOS) GOARCH=$(LAMBDA_GOARCH) \
        go build -tags lambda.norpc -ldflags="$(LDFLAGS)" \
        -o $(LAMBDA_OUT) ./cmd/server
```

## Related Code Files

- Modify: `Makefile` — add `GIT_SHA` + `LDFLAGS` vars, swap inline
  `-ldflags="-s -w"` for `-ldflags="$(LDFLAGS)"` in both `build` and
  `build-lambda` targets.

## Implementation Steps

1. **Add Makefile variables** near the top (after existing `LAMBDA_OUT` etc):
   ```makefile
   GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null)
   LDFLAGS := -s -w -X main.gitSHA=$(GIT_SHA)
   ```

2. **Update `build` target** (`Makefile:51-52`):
   - Change `-ldflags="-s -w"` → `-ldflags="$(LDFLAGS)"`.

3. **Update `build-lambda` target** (`Makefile:54-60`):
   - Change `-ldflags="-s -w"` → `-ldflags="$(LDFLAGS)"`.

4. **Verify**:
   - `make build` then `strings ./bin/server | grep -E '^[0-9a-f]{7,}$'`
     should reveal the SHA.
   - Or: add a no-op `--version`-style log line behind a build flag —
     skip for now (YAGNI).

## Success Criteria

- [ ] `make build` builds successfully.
- [ ] `make build-lambda` builds successfully.
- [ ] Resulting binary has `main.gitSHA` populated (verified once via
      `strings` grep or a temporary debug log — not a permanent test).
- [ ] No change to `.github/workflows/deploy.yml` — `make build-lambda`
      in CI picks up the new ldflags automatically.

## Risk Assessment

| Risk | Mitigation |
|---|---|
| Dockerfile builds (CI `docker build -t miti99bot`) bypass Makefile and won't inject SHA | Out of scope — Lambda deploy uses `make build-lambda`, not Docker. CI's `docker build` is just a smoke check. Document the gap; revisit only if Cloud Run path is reactivated. |
| Local `make build` in a tarball download with no `.git/` → SHA empty | Acceptable — feature silently disables on non-git builds. |
| `git rev-parse` outputs spaces / unexpected chars → breaks ldflags | `git rev-parse --short HEAD` output is `[0-9a-f]{7,}` only. Safe. |
| Reproducible builds tooling complains about non-deterministic SHA | Not relevant for this project. |

## Security Considerations

- Short git SHA is published on every GitHub commit page — not sensitive.
- No build-time secrets touched.
