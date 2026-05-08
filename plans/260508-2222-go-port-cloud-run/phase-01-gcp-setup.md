---
phase: 1
title: "GCP setup + free-tier baseline"
status: pending
priority: P1
effort: "3h"
dependencies: []
---

# Phase 01: GCP setup + free-tier baseline

## Overview
Stand up the GCP project with all needed APIs enabled, deploy a throwaway Go hello-world to Cloud Run, capture cold-start P95 baseline. Validates that all free-tier services work together before any port code is written.

## Requirements
- Functional: GCP project ready, Cloud Run accepts deploys, Firestore Native initialized, Secret Manager + Artifact Registry usable, Gemini API key works.
- Non-functional: cold-start P95 measured (target ≤1.5s for static-link Go binary), free-tier budgets confirmed in Billing dashboard.

## Architecture

```
GCP project (free tier)
├── Cloud Run service (region: asia-southeast1)
├── Firestore Native database (region: asia-southeast1, default db)
├── Artifact Registry repo (region: asia-southeast1, format: docker)
├── Secret Manager
├── Cloud Scheduler (jobs created later in Phase 09)
└── Generative Language API (Gemini, key-based, region-agnostic)
```

Region pinned to `asia-southeast1` (Singapore) to keep RTT to VN users low. Same region for Cloud Run + Firestore + Artifact Registry to avoid cross-region egress.

## Related Code Files
- Create: `scripts/gcp-bootstrap.sh` — idempotent setup script
- Create: `docs/gcp-free-tier.md` — captured caps + measured baseline

## Implementation Steps
1. Create GCP project: `miti99bot-prod`. Confirm billing account linked but free-tier-only.
2. Enable APIs: `run.googleapis.com`, `firestore.googleapis.com`, `cloudscheduler.googleapis.com`, `artifactregistry.googleapis.com`, `secretmanager.googleapis.com`, `generativelanguage.googleapis.com`, `cloudbuild.googleapis.com`.
3. Initialize Firestore Native in `asia-southeast1`. Create `(default)` database.
4. Create Artifact Registry docker repo `miti99bot-go` in `asia-southeast1`.
5. Create runtime service account `miti99bot-runtime@…iam.gserviceaccount.com` with roles: `roles/datastore.user`, `roles/secretmanager.secretAccessor`, `roles/run.invoker` (for Scheduler→Run OIDC).
6. Create deployer service account `miti99bot-deployer@…` for CI with `roles/run.admin`, `roles/artifactregistry.writer`, `roles/iam.serviceAccountUser`.
7. Get a Gemini API key from AI Studio → store as a Secret Manager secret `gemini-api-key` (value-only test now; real wiring in Phase 07).
8. Write a 30-line Go hello-world (stdlib `net/http`) responding "ok" on `/`. Build static binary, multi-stage Dockerfile (`golang:1.23 → distroless/static`).
9. Push image, deploy: `gcloud run deploy miti99bot-baseline --image=… --region=asia-southeast1 --allow-unauthenticated --min-instances=0 --max-instances=2 --memory=128Mi --cpu=1 --timeout=30s`.
10. Cold-start measurement: hit `/` 10× with 5-min spacing (force scale-to-zero between). Record P50/P95/P99 from `gcloud run services logs` or curl `-w "%{time_total}"`.
11. Document baseline in `docs/gcp-free-tier.md`. Tear down baseline service: `gcloud run services delete miti99bot-baseline`.

## Success Criteria
- [ ] All 7 APIs enabled, billing-free confirmed
- [ ] Firestore Native exists in `asia-southeast1`
- [ ] Hello-world deploys end-to-end
- [ ] Cold-start P95 documented (any number — used as Phase 11 soak gate)
- [ ] Baseline service torn down (no idle resources)

## Risk Assessment
- **Risk**: GCP requires billing account even for free tier → unexpected charges. **Mitigation**: enable Billing alert at $1, $5, $10 thresholds.
- **Risk**: Firestore in `asia-southeast1` has higher per-op cost than `us-central1` once free tier exceeded. **Mitigation**: latency wins for VN users; monitor reads in Phase 11.
- **Risk**: Gemini API key has no fine-grained quota controls. **Mitigation**: handle 429s gracefully in Phase 07.

## Rollback
Delete project. No state to preserve.
