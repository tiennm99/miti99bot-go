---
title: "Go port of miti99bot to Google Cloud Run (free tier)"
description: "Full rewrite of the grammY/Cloudflare Worker bot in Go, deployed to Cloud Run with Firestore + Gemini + Cloud Scheduler — all free-tier."
status: in-progress
priority: P2
effort: 5-7d
branch: main
tags: [go, cloud-run, gcp, firestore, gemini, port, telegram-bot]
created: 2026-05-08
blockedBy: []
blocks: []
supersedes: [260425-1945-mongodb-atlas-migration]
---

# Plan: Go port → Google Cloud Run (free tier)

Full rewrite of miti99bot in Go for deployment on Cloud Run, swapping CF KV+D1+Workers AI for Firestore Native + Gemini API + Cloud Scheduler. Source repo lives at a new `miti99bot-go` repo (separate). Cutover via dual-run + soak.

## Locked decisions
- **Compute**: Cloud Run (min-instances=0, scale-to-zero). Free tier: 2M req/mo, 360k vCPU-s, 180k GiB-s.
- **Storage**: Firestore Native, region `asia-southeast1`. Free: 1 GiB, 50k reads/d, 20k writes/d.
- **AI**: Gemini API via `google.golang.org/genai`. `text-embedding-004` (768d) + `gemini-1.5-flash`. Free: 15 RPM / 1500 RPD per model.
- **Cron**: Cloud Scheduler. 3 jobs/mo free → fits 2 current crons (`0 17 * * *`, `0 1 * * *`).
- **Secrets**: Secret Manager. Free: 6 active secret versions, 10k access ops/mo.
- **Image registry**: Artifact Registry. Free: 0.5 GiB storage.
- **Telegram lib**: `github.com/go-telegram/bot` (modern, idiomatic, active).
- **Repo layout**: separate `miti99bot-go` repo. JS/TS repo stays as-is during port.
- **Cutover**: dual-run on test bot → flip prod webhook → 7-day overlap → decommission CF Worker.

## Reports
- [code-reviewer 2026-05-08 — Phase 02-03 bootstrap](reports/code-reviewer-260508-2254-phase02-03-bootstrap.md) (3 critical + 7 high addressed in same session; 2 medium + nits deferred)
- [code-reviewer 2026-05-08 — Phase 04 Firestore](reports/code-reviewer-260508-2333-phase04-firestore-kv.md) (0 critical, 3 high all addressed; mediums deferred)
- [code-reviewer 2026-05-09 — Phase 5a util+misc](reports/code-reviewer-260509-0813-phase5a-util-misc.md) (1 critical /info nil-deref + L2 KV wire-format mismatch with JS, both fixed; M1 doc + L3 escape test applied)
- [code-reviewer 2026-05-09 — Phase 5b wordle](reports/code-reviewer-260509-0918-phase5b-wordle.md) (1 critical defaultRNG race + 1 high Get-mutate-Put race + dead-code; all fixed in same session; per-subject mutex added to serialise compound KV ops)
- [code-reviewer 2026-05-09 — Phase 5c loldle](reports/code-reviewer-260509-0940-phase5c-loldle.md) (1 high winRate truncation in both loldle AND wordle; both fixed; render + keylock test gaps closed)

## Phases

| # | Phase | Status | Effort | Key deliverable |
|---|-------|--------|--------|-----------------|
| 01 | [GCP setup + free-tier baseline](phase-01-gcp-setup.md) | pending | 3h | Hello-world Go on Cloud Run, cold-start P95 captured |
| 02 | [New repo bootstrap + webhook skeleton](phase-02-repo-bootstrap.md) | partial | 3h | `miti99bot-go` repo, `/webhook` validates secret token (Cloud Run deploy + Telegram smoke test deferred to Phase 01) |
| 03 | [Module framework + storage interfaces](phase-03-module-framework.md) | done | 4h | Module/Command/Cron interfaces, registry, dispatcher |
| 04 | [Firestore KVStore + per-module prefixing](phase-04-firestore-kv.md) | done | 4h | `FirestoreKVStore`, emulator tests, KVProvider abstraction (Memory + Firestore) |
| 05 | [Port simple modules (util/misc/wordle/loldle)](phase-05-port-simple-modules.md) | done | 6h | 4 KV-only modules at JS parity; shared `internal/keylock` extracted |
| 06 | [Port loldle variants + lolschedule](phase-06-port-loldle-variants.md) | pending | 5h | 5 modules sharing classic loldle patterns |
| 07 | [Gemini AI + port semantle/doantu/twentyq](phase-07-gemini-ai-modules.md) | pending | 6h | 3 AI modules with rate-limit handling |
| 08 | [Port trading + composite indexes](phase-08-port-trading.md) | pending | 6h | VN-stocks paper trading + daily price cron |
| 09 | [Cloud Scheduler cron wiring](phase-09-cloud-scheduler.md) | pending | 2h | 2 jobs → `/cron/{name}` with OIDC |
| 10 | [CI/CD + Dockerfile + Secret Manager](phase-10-ci-cd.md) | pending | 4h | GHA pipeline → AR → Cloud Run, idempotent |
| 11 | [Test parity + observability](phase-11-tests-observability.md) | pending | 4h | Unit tests ported, Cloud Logging structured JSON |
| 12 | [Cutover + decommission CF Worker](phase-12-cutover.md) | pending | 3h | Prod webhook flipped, soak passed, Worker retired |

## Dependency graph

```
01 ──► 02 ──► 03 ──► 04 ──► 05 ──► 06 ─┐
                          ├──► 07 ─────┤
                          └──► 08 ─────┤
              03 ──────────► 09 ───────┤
              02 ──────────► 10 ───────┤
                              08 ──► 11 ──► 12 ◄── 10
```

## Free-tier budget at peak

| Resource | Cap | Expected | Headroom |
|---|---|---|---|
| Cloud Run req | 2M/mo | ~30k/mo | 99% |
| Cloud Run vCPU-s | 360k | ~5k | 99% |
| Firestore reads | 50k/day | ~5k/day | 90% |
| Firestore writes | 20k/day | ~2k/day | 90% |
| Cloud Scheduler jobs | 3 | 2 | 33% |
| Gemini RPM (flash) | 15 | <5 burst | 67% |
| Gemini RPD | 1500 | ~200 | 87% |
| Secret Manager versions | 6 | 3 | 50% |
| Artifact Registry storage | 0.5 GiB | <50 MiB | 90% |

If Firestore reads cap is hit → enable Cloud Run instance-level cache (warm-instance memo). If Gemini RPD cap is hit → degrade twentyq with a "free tier exhausted, retry tomorrow" reply.

## Abort criteria
- **Cold-start P95 > 1.5s** sustained (Phase 01 baseline + Phase 11 soak): retain JS Worker for time-sensitive surfaces.
- **Firestore reads > 80% of cap** during Phase 11 soak: add KV-style instance cache before cutover.
- **Gemini quota exhaustion** during normal use: switch to lower-RPM-friendly Vertex AI (still free under credit) or accept degraded UX.

## Rollback
Per-phase rollback documented in each phase file. Phase 12 is the only irreversible step; until then, the CF Worker continues to serve prod via existing webhook.

## Open questions
_Resolved 2026-05-08:_
1. ~~Scheduler cron names~~ → **Keep `0 17 * * *` UTC** (= midnight Saigon). Cloud Scheduler stays UTC, no behavior change vs. JS Worker.
2. ~~Migrate KV/D1 data~~ → **Migrate everything**. One-shot export of D1 + KV → Firestore on cutover. Phase 12 owns the migration script.
3. ~~Test Telegram bot~~ → **User creates the bot manually**, token + webhook secret injected via Cloud Run env vars (`TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEBHOOK_SECRET`).
