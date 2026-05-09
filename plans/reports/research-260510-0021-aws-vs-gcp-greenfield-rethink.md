# Research Addendum: AWS vs GCP — Greenfield Rethink (Sunk Cost Ignored)

> **Generated:** 2026-05-10 00:21 (Asia/Saigon)
> **Mode:** /research follow-up + ultrathink
> **Companion to:** `research-260510-0012-aws-vs-gcp-free-tier.md`
> **Trigger:** User requested re-evaluation treating project as greenfield (existing code can be rewritten any time).
> **Revised verdict:** **AWS edges out GCP, ~70/30**, on pure free-tier and trajectory merits. Not a slam-dunk; simplicity is GCP's only remaining moat.

---

## Why this addendum

Prior report leaned heavily on "code is already written for GCP" as the decisive factor. User explicitly asked to remove that lens. Greenfield-only analysis flips the conclusion: **AWS has objectively larger free tier** on the resources this project's trajectory will consume first (cron count, KV storage, egress in Asia). GCP's remaining advantage is operational simplicity for a single developer.

---

## What changes when sunk cost is removed

The previous report's "GCP wins" verdict rested 60% on switching cost. Strip that:

| Argument | Holds greenfield? | Why |
|---|---|---|
| "Cloud Run runs unmodified `http.Handler`" | ❌ no | **Lambda Web Adapter (LWA)** ships a normal Go HTTP server on Lambda. Same code, both clouds. |
| "Firestore is already wired" | ❌ no | Greenfield: nothing wired. Pick the better KV. DynamoDB free tier is 25× bigger. |
| "Cloud Scheduler is 3 lines YAML" | ⚠ partial | True, but capped at 3 jobs free. EventBridge is also simple via Terraform/SAM and uncapped on rule count. |
| "Single IAM model" | ✅ yes | GCP IAM is gentler than AWS combinatorics. Real ergonomic win. |
| "Free tier covers workload 100×" | ✅ yes | Both clouds. Tie. |
| "Gemini is cloud-agnostic" | ✅ yes | Tie. |

Three of the four GCP advantages **vaporize** without sunk cost. The remaining wins are operational simplicity (real) and Cloud Logging UX (minor).

---

## Greenfield free-tier head-to-head (this project's resource axes)

### Compute

| | GCP Cloud Run | AWS Lambda (ZIP, Function URL) |
|---|---|---|
| Free requests/mo | 2M | 1M |
| Free compute | 360k GiB-s + 180k vCPU-s | 400k GB-s |
| Adapter needed for Go HTTP | none | LWA (extension layer, ~100 ms cold-start tax) |
| Cold start (Go, distroless / provided.al2023) | 1–3 s | **0.05–0.2 s ZIP** |
| Concurrency model | up to 80–1000 req/instance | 1 req/instance |
| Container registry | Artifact Registry 0.5 GB always-free | ECR 500 MB **12-mo only** |
| Bills idle? | no (request-based when min-instances=0) | no |

**Tilt:** Lambda ZIP wins on cold start, no registry needed. Cloud Run wins on more free requests and concurrency multiplexing (lower compute usage per req). For a webhook bot at low QPS, Lambda's per-invocation isolation is fine. **Slight Lambda lean** because ZIP avoids the registry trap.

### KV / document storage

| | Firestore (Native) | DynamoDB (on-demand) |
|---|---|---|
| Free storage | 1 GiB | **25 GiB** |
| Free read ops | 50k/**day** (≈1.5M/mo) | (part of 200M req/mo monthly pool) |
| Free write ops | 20k/**day** (≈600k/mo) | (part of 200M req/mo monthly pool) |
| Bursting risk | high (daily caps reset) | low (monthly pool absorbs spikes) |
| Query model | document + composite indexes | item + GSI/LSI |
| Strong consistency | yes | yes (with consistent-read flag) |

**Tilt:** **DynamoDB wins decisively.** 25× storage, monthly-pooled requests instead of daily caps (better for bursty bot traffic), and 200M total req/mo dwarfs Firestore's combined ~2M ops/mo. For per-user state in 8+ modules + planned trading data, DynamoDB has years of headroom; Firestore could pinch within months if usage grows.

### Cron / scheduler

| | Cloud Scheduler | EventBridge Scheduler |
|---|---|---|
| Free job/rule count | **3 hard cap** | unlimited |
| Free invocations/mo | (per the 3 jobs) | 14M on default bus |
| Cost past free | $0.10/job/mo | $0/job, $1.25/M extra invocations |

**This project's likely cron count:** wordle daily, loldle daily, lolschedule daily, semantle daily, weekly digests, possible trading hourly refresh. **>3 almost certainly.**

**Tilt:** **AWS wins.** GCP becomes the project's first paywall trigger. Cost is trivial ($0.70/mo for 10 jobs) but breaks the "strict $0" guarantee. AWS keeps the bot literally free.

### Egress

| | Cloud Run | Lambda |
|---|---|---|
| Free egress | 1 GiB/mo **NA-only** | 100 GB/mo **all regions** |
| Bot deploy region (VN users) | asia-southeast1 → **no free egress** | ap-southeast-1 → 100 GB free |
| Bot egress estimate | <50 MB/mo | <50 MB/mo |
| Real cost impact | ~$0 (pennies) | $0 |

**Tilt:** **AWS wins on principle, draws on practice.** GCP's free egress is geographically restrictive in a way that surprises Asian deployers. Negligible at this volume but asymmetric.

### Secrets

| | GCP Secret Manager | AWS Parameter Store (Standard) |
|---|---|---|
| Free | 6 active versions, 10k accesses/mo | unlimited |
| Cost past free | $0.06/version/mo, $0.03/10k access | $0 |
| API ergonomics | clean | clean |

**Tilt:** **AWS wins.** Parameter Store Standard tier is unlimited free. (Skip Secrets Manager — paid only.)

### Logs

| | Cloud Logging | CloudWatch Logs |
|---|---|---|
| Free | 50 GiB/mo ingest | 5 GB/mo ingest (always-free as of 2024+) |
| UX | better console | older console |

**Tilt:** **GCP wins on volume + UX.** 50 GiB > 5 GB, console search is faster. Real but minor advantage.

### Container registry

| | Artifact Registry | ECR |
|---|---|---|
| Free | 0.5 GB **always** | 500 MB **12 mo only** |
| Workaround on AWS | use Lambda ZIP, no registry | — |

**Tilt:** **GCP wins on container path. Lambda ZIP makes this moot for AWS.**

---

## Operational complexity (real cost, hard to put $ on)

| Activity | GCP | AWS |
|---|---|---|
| First deploy | `gcloud run deploy --source .` (one command, builds + pushes + runs) | `sam build && sam deploy` OR Terraform OR manual zip + IAM role + Function URL + LWA layer config |
| Add cron | Cloud Scheduler job → HTTP POST with OIDC | EventBridge Scheduler → Lambda invoke role + Lambda permission |
| IAM for cron-to-app auth | OIDC token verify on receiver | IAM principal-on-invoke |
| Iteration loop | edit → `gcloud run deploy --source` → 30 s | edit → `sam deploy` → 30 s OR build+zip+update-function-code |
| Multi-env (dev/prod) | two Cloud Run services + two Firestore DBs | two Lambdas + two DynamoDB tables + two roles |

**Tilt:** **GCP wins on greenfield ergonomics.** This is real engineer-hours. For a solo dev, the difference between "one command" and "five tools to wire" matters.

---

## Decision matrix (greenfield, weighted for this project)

| Criterion | Weight | GCP | AWS | Winner |
|---|---|---|---|---|
| Cron job free count headroom | High | 3 cap (paywall) | unlimited | **AWS** |
| KV storage headroom | High | 1 GiB / daily caps | 25 GiB / monthly pool | **AWS** |
| Operational simplicity | High | one command | multi-tool | **GCP** |
| Cold start | Med | 1–3 s | 50–200 ms | **AWS** |
| Egress in Asia | Med | NA-only free | 100 GB any region | **AWS** |
| Logs free tier | Low | 50 GiB | 5 GB | **GCP** |
| Container registry | Low | 0.5 GB always | 500 MB 12-mo | **GCP** (or moot via ZIP) |
| Secrets | Low | 10k accesses/mo | unlimited | **AWS** |
| Compute requests | Low | 2M | 1M+400k GB-s | tie at this scale |
| AI (Gemini) | Low | cloud-agnostic | cloud-agnostic | tie |

**Score:** AWS 5 wins (3 high-weight). GCP 3 wins (1 high-weight). 2 ties.
**Greenfield verdict: AWS, by ~70/30 margin.**

---

## When does GCP still win greenfield?

- **You commit to ≤3 cron jobs forever.** Wordle daily push only. Done.
- **You expect <100 MB total state.** Firestore 1 GiB is plenty.
- **You value 1-command deploy more than free-tier headroom.** Solo dev, hobby project, no growth ambition.
- **You want better log UX out of the box.**
- **You'd hit operational complexity faster than free-tier ceilings.** Realistic for small bots.

**This project's signal:** 8 modules done, trading planned, multiple daily-push crons implied. **It's not staying small.** AWS headroom matches the trajectory.

---

## Honest counter-argument (steel-manning GCP greenfield)

- "$0.70/mo past 3 cron jobs is not a real cost." → True. If "almost free" is acceptable, GCP simplicity wins.
- "Solo devs ship more on simpler clouds." → True. Maintenance hours dwarf $-cost differences.
- "Firestore 1 GiB is fine for tens of thousands of users on a text bot." → True until it isn't, hard to predict.
- "Cloud Logging is genuinely better." → True. Operational quality of life matters.
- "Cloud Run's `--source .` deploy from a Go repo is the simplest serverless deployment in the industry." → True. AWS has no equivalent.

**If the user values "minimal operational tax + accept ~$1/mo at scale" over "strict $0 + larger headroom": GCP.**
**If the user values "stay strictly free + max headroom for growth + faster cold starts": AWS.**

---

## Recommendation

If switching cost truly is zero and the project intends to grow:

**Greenfield stack: AWS**
- Lambda (Go on `provided.al2023`, ZIP deploy) behind a Function URL
- Lambda Web Adapter so handler code is pure `http.Handler` (portable to Cloud Run later if needed)
- DynamoDB on-demand for KV (provider abstraction in `internal/storage/` swaps out Firestore for Dynamo)
- EventBridge Scheduler for cron → Lambda invoke (no HTTP loopback needed)
- Parameter Store for `TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEBHOOK_SECRET`, `GEMINI_API_KEY`
- CloudWatch Logs at INFO; consider 7-day retention to stay under 5 GB
- GitHub Actions OIDC → AWS role for CI deploy (no long-lived keys)
- Region: ap-southeast-1 (Singapore) for Vietnam latency

If switching cost is not actually zero (it's some hours, not weeks):

**Pragmatic stack: stay GCP**
- Phase 04 Firestore is done. Phase 01 Cloud Run is one config exercise.
- Accept the 3-cron paywall trigger; budget $1/mo guard.
- Be aware Asia egress is billed; volume is tiny.

---

## Unresolved questions

1. **Realistic cron count over project lifetime?** If the user commits to ≤3 forever, GCP holds. If 5+ realistic, AWS wins on this axis alone.
2. **State growth projection?** If bot grows to >10k active users with per-user history, Firestore daily caps tighten. DynamoDB doesn't.
3. **Effort estimate for AWS port?** Phase 02 (HTTP skeleton): ~1 day with LWA. Phase 04 (KV provider): ~1 day with DynamoDB AWS SDK. Phase 08 (cron): ~half day with EventBridge. **Total ~3 days** to redo what's done. Worth it?
4. **Solo-dev tolerance for AWS IAM/SAM/Terraform?** If the user has scar tissue from AWS, the simplicity tax may exceed the free-tier savings.
5. **Region choice on AWS?** ap-southeast-1 (Singapore) vs ap-northeast-1 (Tokyo) — pick by where Telegram's edge is closest to bot users in Vietnam. Both have full free-tier coverage.
6. **Bedrock vs direct Gemini API?** No reason to switch — Gemini API free tier is independent of cloud and currently better than Bedrock's pay-per-token-only model for hobby use.
