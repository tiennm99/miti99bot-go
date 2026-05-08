---
phase: 9
title: "Cloud Scheduler cron wiring"
status: pending
priority: P2
effort: "2h"
dependencies: [3]
---

# Phase 09: Cloud Scheduler cron wiring

## Overview
Replace CF Worker `[triggers] crons` with Cloud Scheduler. Each module-declared cron becomes a Scheduler job that POSTs to `/cron/{name}` on the Cloud Run service with an OIDC token, which the service validates before dispatching to module cron handlers.

## Requirements
- Functional: 2 jobs run on schedule (`0 17 * * *`, `0 1 * * *`). Each invocation reaches the corresponding module cron handlers and completes within Cloud Run timeout.
- Non-functional: free-tier — 3 jobs/mo cap, fits with 33% headroom. OIDC auth so the `/cron/*` endpoint stays Cloud-Scheduler-only (private). No public bypass.

## Architecture

```
Cloud Scheduler                       Cloud Run
┌───────────────────────┐             ┌───────────────────────────┐
│ job: cron-0-17        │  POST + OIDC│ /cron/0_17_star_star_star │
│ schedule: 0 17 * * *  │────────────►│ ──► validate OIDC          │
│ target: /cron/0_17... │             │ ──► dispatcher.Dispatch    │
│ auth: OIDC            │             │     (cron name = "0 17 * *│
└───────────────────────┘             │     *")                    │
                                       └───────────────────────────┘
```

Path structure: encode the cron expression in the URL (URL-safe form), e.g. `0 17 * * *` → `/cron/0_17_star_star_star`. Or simpler: use a stable name per scheduler job (e.g. `/cron/daily-eod` and `/cron/daily-cleanup`), with the registry mapping name → cron handlers.

Adopt the named-job approach: cleaner than escaping cron syntax in URLs.

## Related Code Files
- Modify: `internal/modules/cron_dispatcher.go` — `DispatchByName(ctx, name string, reg *Registry, deps Deps) error`
- Modify: `internal/server/router.go` — `/cron/{name}` handler, validates OIDC token via `google.golang.org/api/idtoken`
- Create: `scripts/setup-scheduler.sh` — idempotent `gcloud scheduler jobs create http …` for each cron
- Modify: per-module `Cron` declarations to use **stable names** (e.g. `daily-eod-update`, `nightly-cleanup`) instead of cron syntax

## Implementation Steps
1. Refactor `Cron.Schedule` field's role: keep as **documentation only**. Add `Cron.Name` as the stable identifier. Wrangler-style auto-registration is no longer needed.
2. Update `cron_dispatcher.go`:
   - `DispatchByName(ctx, name, reg, deps)`: find all crons across all modules where `c.Name == name`. Run with errgroup. Return aggregate error.
3. Update `/cron/{name}` handler:
   - Reject non-POST → 405.
   - Validate `Authorization: Bearer <id-token>` header via `idtoken.Validate(ctx, token, audience=cloudRunURL)`. Confirm `email` claim matches the runtime SA. Mismatch → 401.
   - Call `DispatchByName(ctx, mux.Vars["name"], reg, deps)`.
   - 200 on success, 500 on dispatcher error (Scheduler retries with backoff).
4. `scripts/setup-scheduler.sh`:
   - For each known cron (currently 2): `gcloud scheduler jobs create http <name> --schedule=<cron> --uri=<cloudrun-url>/cron/<name> --http-method=POST --oidc-service-account-email=<runtime-sa> --oidc-token-audience=<cloudrun-url> --location=asia-southeast1`.
   - Idempotent: try `update` first, fall back to `create` on not-found.
5. Local test: simulate Scheduler call with `gcloud scheduler jobs run <name>`; verify Cloud Run logs show successful dispatch.
6. Document in `docs/using-cron.md` (port from JS repo, adjusted for Cloud Scheduler model).

## Success Criteria
- [ ] 2 Scheduler jobs created in `asia-southeast1`
- [ ] OIDC validation rejects unsigned POSTs (401)
- [ ] Manual `gcloud scheduler jobs run` triggers handler
- [ ] Cron handler error → Scheduler retries (configured retry policy)
- [ ] Stays within 3-job free cap

## Risk Assessment
- **Risk**: 3-job hard cap. Adding a 4th cron later → paid tier. **Mitigation**: collapse multiple module crons into a single dispatcher endpoint sharing one Scheduler job; or rely on internal-time-based-fan-out (cheaper but less precise).
- **Risk**: OIDC token validation requires correct audience. Misconfig → 401 in prod. **Mitigation**: Phase 09 ends only after manual `jobs run` succeeds.
- **Risk**: Cron handler exceeding Cloud Run timeout (default 5 min, our config 30s). Trading daily update fetches ~50 prices serially. **Mitigation**: parallelize price fetches with errgroup + worker pool of 5.

## Rollback
`gcloud scheduler jobs delete <name>`. CF Worker still owns prod cron triggers — no missed runs during transition.
