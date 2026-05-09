# Research Report: AWS vs GCP Free Tier — Suitability for miti99bot-go

> **Generated:** 2026-05-10 00:12 (Asia/Saigon)
> **Mode:** /research + /ck:brainstorm (ultrathink)
> **Verdict (TL;DR):** **GCP wins, decisively, not even close.** Stop debating. The code is already written for it, free tier is plenty, switching cost > zero benefit.

---

## Executive Summary

This project (miti99bot-go) is a Go Telegram bot already coded against **Cloud Run + Firestore + Gemini**, with multi-stage Docker → distroless image (~15 MiB), Phase 04 Firestore KV done, Phases 05–07 modules + AI done, only deploy/cron/CI left. Asking "should we switch to AWS for free tier?" is asking "should we throw away weeks of work for a free-tier delta that doesn't matter at this traffic profile?"

The honest answer is no. Both clouds offer permanent (not 12-month) free tiers that cover a personal Telegram bot 100x over. The deciding factor is **integration friction and switching cost**, not headroom. GCP's Cloud Run runs your existing `http.Handler` unmodified; AWS Lambda needs an adapter or rewrite. Firestore is already wired; DynamoDB would force a new KV provider. Cloud Scheduler is two lines of YAML; EventBridge Scheduler needs IAM gymnastics. And Gemini API is **independent of cloud** — same key works on AWS, so AWS gains nothing on the AI side.

The only scenario where AWS wins: if you needed >1 GiB egress/month (AWS gives 100 GB always-free) — irrelevant here, Telegram messages are tiny text.

---

## Research Methodology

- **Sources:** 5 web searches (Cloud Run/Firestore quotas, AWS Lambda/DynamoDB quotas, Telegram-bot deployment patterns, Gemini API free tier, Lambda-vs-Cloud-Run cold starts/egress)
- **Date range:** Sources from 2025-2026 with explicit 2026 quota figures
- **Project context:** Read `README.md` and existing plan structure (`plans/260508-2222-go-port-cloud-run/`)
- **Search keywords:** GCP Always Free 2026, AWS Always Free 2026, Cloud Run egress, Lambda Go cold start, Gemini API free tier 2026

---

## Project Profile (the part that decides everything)

| Aspect | Reality |
|---|---|
| Workload | Telegram bot webhook receiver. Few users. Bursty, low-volume. |
| Already coded for | **Cloud Run** (HTTP server in `cmd/server/`, `/webhook` route) |
| Storage | **Firestore** KVStore impl done (Phase 04) |
| AI | **Gemini API** (cloud-agnostic — uses Google AI Studio key, not GCP-bound) |
| Cron | Planned Cloud Scheduler → `/cron/{name}` (Phase 08+) |
| Container | Multi-stage `golang:1.23-alpine` → `distroless/static:nonroot`, ~15 MiB |
| Phases done | 02–07 (modules, framework, AI). Phase 01 (deploy) and 08+ pending. |

**Observation:** The hard work is done. Deploy is a config exercise, not a redesign. Switching clouds = redo Phases 02 + 04 + 08, partially.

---

## Key Findings

### 1. GCP Always Free Tier (relevant slice, 2026)

| Service | Always-Free Quota | Bot Usage Estimate | Headroom |
|---|---|---|---|
| **Cloud Run** | 2M req/mo, 360k GiB-s mem, 180k vCPU-s, 1 GiB egress/mo (NA) | ~10k req/mo (low-volume bot) | ~200x |
| **Firestore (Native)** | 1 GiB storage, 50k reads/day, 20k writes/day, 20k deletes/day, 10 GiB egress/mo | <1k ops/day per user | ~50x |
| **Cloud Scheduler** | 3 jobs free | Bot needs ~1–3 cron jobs | exact fit |
| **Artifact Registry** | 0.5 GB free | distroless image ~15 MiB | ~30x |
| **Secret Manager** | 6 active versions, 10k accesses/mo | 1 secret (bot token) | trivial |
| **Cloud Build** | 120 build-min/day | A few CI builds/day | fine |

**2026 caveat:** Starting Feb 3, 2026, projects need Blaze (pay-as-you-go) plan to keep default Cloud Storage buckets — but Always-Free quotas still apply, and **this project doesn't use Cloud Storage** (uses Firestore + Artifact Registry). So: enable Blaze, set a $0 budget alert, free tier still works.

### 2. AWS Always Free Tier (relevant slice, 2026)

| Service | Always-Free Quota | Notes |
|---|---|---|
| **Lambda** | 1M req/mo + 400k GB-s | Permanent. Plenty for a bot. |
| **DynamoDB** | 25 GB storage, 200M req/mo (on-demand) | Way more than Firestore's free tier. |
| **EventBridge Scheduler** | 14M scheduled events/mo (default bus) | Plenty. |
| **Lambda Function URL** | Free (no API Gateway needed) | Avoids the 12-month-only API Gateway free tier. |
| **Parameter Store (Standard)** | Free unlimited | Use for bot token. Skip Secrets Manager (paid after 30 days). |
| **Egress** | 100 GB/mo always-free (since Dec 2024) | 100x Cloud Run's 1 GiB. Irrelevant for tiny Telegram messages. |
| **ECR private** | 500 MB **12-month only** ❌ | Use Docker Hub or GHCR free instead. Or pay ~$0.05/GB-mo. |

**Hidden trap:** Many AWS tutorials use API Gateway (1M HTTP API calls = 12-month tier, **not always-free**). To stay free permanently on AWS, you must use **Lambda Function URLs** instead.

### 3. Gemini API (decisive: cloud-agnostic)

- Free tier from **Google AI Studio**, not GCP — same API key works from AWS, GCP, your laptop, anywhere.
- Limits (May 2026): 5–15 RPM, 100–1000 RPD per model, 250k TPM universal cap.
- Models on free tier: 2.5 Pro / 2.5 Flash / 2.5 Flash-Lite. **Gemini 3.x is paid-only in preview.**
- **April 2026 change:** Pro models tightening; Flash/Flash-Lite still free.
- **AWS doesn't disadvantage Gemini.** Choosing AWS doesn't lose Gemini access. Choosing GCP doesn't grant extra Gemini quota.

### 4. Cold Start & Latency

| Platform | Go cold start | Warm latency |
|---|---|---|
| Lambda Go ZIP | 50–200 ms | <5 ms |
| Lambda container (Go) | 0.6–1.4 s | <5 ms |
| Cloud Run (distroless Go, min-instances=0) | 1–3 s | <10 ms |
| Cloud Run (min-instances=1) | none | <10 ms — **costs money** (no longer free) |

For Telegram webhooks: Telegram retries on timeout, and a 1–3 s cold start is acceptable for human-perceived bot latency. **Not a real differentiator at this scale.**

### 5. Egress

- Cloud Run free egress: 1 GiB/mo NA (then $0.12/GB)
- AWS free egress: 100 GB/mo
- Bot egress = small JSON replies to api.telegram.org. ~500 bytes × 10k msgs/mo ≈ 5 MB. **Both wildly under quota.** Non-issue.

---

## Comparative Analysis

### Free-tier headroom at this workload: **TIE** (both 100x more than needed)

### Switching cost (the actual decision driver):

| Item | Stay GCP | Switch to AWS |
|---|---|---|
| HTTP handler in `internal/server/` | works as-is | rewrite to Lambda handler OR add lambda-web-adapter |
| `internal/storage/` Firestore impl | already done | rewrite as DynamoDB provider, retest, redeploy |
| Cron `/cron/{name}` routes | wire Cloud Scheduler (3 lines YAML) | wire EventBridge Scheduler + IAM role + invocation target |
| Container registry | Artifact Registry (gcloud auth) | ECR (12-mo free only) or GHCR (free, simpler) |
| Secrets | Secret Manager | Parameter Store (free) — easy swap |
| Logs | Cloud Logging (built-in) | CloudWatch Logs (built-in) |
| Phase 01 plan | already drafted | needs full rewrite |
| Effort | days | weeks |

### Brainstorm: Steel-manning the AWS case

**"AWS Lambda free tier never expires, more permanent than GCP's"** → False premise. Both have permanent always-free tiers. GCP's Always-Free is also permanent. The 12-month thing applies to AWS extras (EC2, RDS, ECR), not Lambda/DynamoDB.

**"AWS egress is 100 GB free, Cloud Run only 1 GiB"** → True, but irrelevant: bot egress is <10 MB/mo.

**"Lambda has faster cold starts"** → True for ZIP packages, but you'd need to abandon the Docker workflow. Container Lambda is similar to Cloud Run.

**"AWS has more free services to grow into"** → Speculative. YAGNI. Decide based on this project's needs.

**"Vendor independence — AWS skill is more marketable"** → Career argument, not technical. Outside scope.

### Brainstorm: Steel-manning the GCP case

**"Code is already written for it"** → Decisive. Anything else is rationalization for redoing finished work.

**"Cloud Run runs unmodified `http.Handler`"** → Real ergonomic win. Lambda forces an event-shape adapter even with Function URLs.

**"Firestore SDK is already integrated, tests pass"** → Phase 04 is done. Don't rewrite Phase 04.

**"Gemini + Firestore + Cloud Run all share one project, one service account, one IAM model"** → Operational simplicity. Worth real money in time saved.

**"Cloud Scheduler → HTTP target is the simplest cron-to-webhook in the industry"** → Phase 08 will be 30 min on GCP, half a day on AWS.

### Steel-manning the actual neutral choice

There is none. This is a lopsided decision. Anyone telling you AWS is "comparable for this project" is selling you sunk-cost-aversion in reverse — you have NO sunk cost in AWS to recover.

---

## Implementation Recommendations

### Stay on GCP. Proceed with Phase 01 as planned. Specifically:

1. **Enable Blaze plan** on the GCP project. Set a **$1 budget alert** (covers free-tier spillover).
2. **Cloud Run** service: `--cpu 1 --memory 256Mi --min-instances 0 --max-instances 3 --concurrency 80`. Min-instances=0 keeps it free; max-instances cap prevents runaway billing.
3. **Artifact Registry** repo for the distroless image. CI pushes via Workload Identity Federation (no JSON keys).
4. **Firestore** in Native mode, `(default)` database. Already coded.
5. **Cloud Scheduler** jobs → HTTP POST to `/cron/{name}` with OIDC token; verify on the receiver side.
6. **Secret Manager**: store `TELEGRAM_BOT_TOKEN` and `TELEGRAM_WEBHOOK_SECRET`; mount as env vars on Cloud Run.
7. **Gemini API key**: from Google AI Studio (not GCP). Store in Secret Manager. Free tier is independent.
8. **Cost guardrails**: budget alert + `max-instances` + Firestore composite-index review (a runaway query loop is the realistic free-tier killer).

### Common pitfalls to avoid

- ❌ Don't set `min-instances >= 1` — instantly leaves free tier.
- ❌ Don't put image in Cloud Storage (Feb 2026 Blaze trigger) — use Artifact Registry.
- ❌ Don't enable expensive Firestore indexes you don't query — they bill writes.
- ❌ Don't log PII at INFO — Cloud Logging has its own free quota (50 GiB/mo) but verbose logs at scale will blow it.
- ❌ Don't trust the webhook origin without `X-Telegram-Bot-Api-Secret-Token` verification.

### When to reconsider AWS

Switch to AWS Lambda **only if** one of:
- You suddenly need >1 GiB Cloud Run egress/month (very unlikely for a bot).
- You unify with an existing AWS-only org/account.
- GCP changes Always-Free terms hostilely (no signal of this).

None apply today.

---

## Resources & References

### GCP
- [Google Cloud Free Tier](https://cloud.google.com/free)
- [Cloud Run Free Tier (2025 infographic)](https://www.freetiers.com/directory/google-cloud-run)
- [Firestore Quotas & Limits](https://docs.cloud.google.com/firestore/quotas)
- [Cloud Scheduler docs](https://docs.cloud.google.com/scheduler/docs)
- [Cloud Run pricing 2025](https://cloudchipr.com/blog/cloud-run-pricing)

### AWS
- [AWS Free Tier 2026 limits & hidden costs](https://www.cloudoptimo.com/blog/aws-free-tier-isnt-unlimited-know-the-limits-before-you-get-billed/)
- [Lambda pricing & cost guide 2026](https://go-cloud.io/aws-lambda-pricing/)
- [AWS Free Tier comprehensive guide](https://cloudwebschool.com/docs/aws/fundamentals/aws-free-tier/)

### Gemini API
- [Gemini API rate limits (official)](https://ai.google.dev/gemini-api/docs/rate-limits)
- [Gemini API pricing (official)](https://ai.google.dev/gemini-api/docs/pricing)
- [Gemini API free tier 2026 guide](https://yingtu.ai/en/blog/gemini-api-free-tier)
- [Gemini Pro paid changes April 2026](https://help.apiyi.com/en/google-gemini-api-free-tier-changes-april-2026-guide-en.html)

### Telegram bot deployment patterns
- [Comparing Telegram bot hosting providers (Code Capsules)](https://www.codecapsules.io/blog/comparing-telegram-bot-hosting-providers)
- [Deploy AI Telegram bot on AWS for free (Go)](http://golangforall.com/en/post/telegram-bots-zero-cost-aws.html)
- [Cloud Run vs Lambda performance/pricing (Sedai)](https://sedai.io/blog/aws-lambda-google-cloud-functions)
- [Serverless container pricing comparison 2026](https://danubedata.ro/blog/serverless-container-pricing-comparison-2026)

---

## Decision Matrix

| Criterion | Weight | GCP | AWS | Winner |
|---|---|---|---|---|
| Code already written for it | High | ✅ | ❌ | **GCP** |
| Free tier covers workload | High | ✅ (100x) | ✅ (100x) | tie |
| Container deploy ergonomics | Med | ✅ Cloud Run native | ⚠ Lambda container OR adapter | **GCP** |
| Cron→webhook simplicity | Med | ✅ Cloud Scheduler | ⚠ EventBridge + IAM | **GCP** |
| KV store integration | Med | ✅ Firestore done | ❌ rewrite to DynamoDB | **GCP** |
| Egress headroom | Low | 1 GiB | 100 GB | AWS (irrelevant) |
| Cold start | Low | 1–3 s | 0.6–1.4 s | AWS (acceptable on both) |
| AI integration (Gemini) | Med | tie (cloud-agnostic) | tie | tie |
| Switching cost | High | $0 | weeks of work | **GCP** |

**Score:** GCP wins 5 categories outright, ties 2, loses 2 (low-impact). **Stay on GCP.**

---

## Next Steps

1. ✅ **Decision: stay on GCP.** Close this question.
2. Resume **Phase 01** (`plans/260508-2222-go-port-cloud-run/`): provision Cloud Run service, Artifact Registry, Firestore, Secret Manager, set webhook secret.
3. Wire **Phase 08** cron via Cloud Scheduler → `/cron/{name}` with OIDC verification.
4. Add **billing budget alert** at $1 before first deploy.
5. Update README "Status" table when Phase 01 lands.

---

## Unresolved Questions

1. **Region choice.** asia-southeast1 (Singapore, lowest LatAm-to-VN latency) vs us-central1 (cheapest, default 1 GiB egress fully applies)? Bot users are in Vietnam — likely asia-southeast1 wins on user latency, but verify free-tier egress geography (the 1 GiB free egress is **NA-only**; egress from asia-southeast1 to api.telegram.org may bill at $0.12/GB even at low volume — calculate at expected msg rate).
2. **Cold-start tolerance for cron.** If a cron job calls Gemini and takes >10 s on cold start, does it risk Cloud Scheduler retry storms? Decide on min-instances trade-off vs free tier.
3. **Gemini quota saturation.** Free tier 100–1000 RPD per model — if semantle/doantu/twentyq see real users, will we hit RPD before paid tier kicks in? Worth designing a fallback (cache common queries, degrade gracefully).
4. **Workload Identity Federation vs JSON key** for CI image push — recommended is WIF, but it requires GitHub OIDC config. Defer or do it now?
5. **GCP Blaze enablement timing** — enable before Phase 01 deploy or after? Free-tier still works on Blaze, but new project needs to confirm Always-Free quotas auto-apply.
