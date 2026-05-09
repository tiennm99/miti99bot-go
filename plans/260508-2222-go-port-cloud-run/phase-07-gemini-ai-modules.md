---
phase: 7
title: "Gemini AI + port semantle/doantu/twentyq"
status: done
priority: P2
effort: "6h"
dependencies: [4]
---

# Phase 07: Gemini AI + port semantle/doantu/twentyq

## Overview
Wire Gemini API as the Workers AI replacement. Port the three AI-using modules: `semantle` and `doantu` use embeddings; `twentyq` uses chat-style text generation. All must respect Gemini free-tier RPM/RPD; degrade gracefully on 429.

## Requirements
- Functional:
  - `semantle`/`doantu`: target word + user guess → cosine similarity score via embeddings.
  - `twentyq`: 20-question style game, model plays the responder (yes/no/sometimes), tracks remaining questions.
- Non-functional:
  - Free-tier-aware: cache embeddings of game targets (rare changes), retry-with-jitter on 429.
  - Gemini client reused as package-level singleton (gRPC connection).
  - Per-user RPM soft-limit in process to prevent abuse from blowing through 1500 RPD shared quota.

## Architecture

```
internal/ai/
├── gemini.go            ← package-level *genai.Client init
├── embeddings.go        ← Embed(ctx, text) ([]float32, error) using text-embedding-004
├── chat.go              ← Generate(ctx, prompt, history) (string, error) using gemini-1.5-flash
└── ratelimit.go         ← per-user token bucket (in-memory, sync.Map of buckets)

internal/modules/semantle/
├── module.go
├── data/targets-en.json  ← curated daily target pool
├── game.go               ← session state
├── score.go              ← cosine similarity
└── targets.go            ← daily target selection (deterministic from date)

internal/modules/doantu/
├── module.go             ← Vietnamese variant (different target pool, same algorithm)
└── data/targets-vi.json

internal/modules/twentyq/
├── module.go
├── prompt.go             ← system prompt + history serialization
├── game.go
└── parser.go             ← yes/no/maybe extractor from model output
```

`bge-m3` (1024d, multilingual) is replaced by `text-embedding-004` (768d). Different vector space — pre-cached target vectors must be re-computed; do not migrate vectors from CF KV.

## Related Code Files
- Create: `internal/ai/{gemini,embeddings,chat,ratelimit}.go`
- Create: `internal/modules/{semantle,doantu,twentyq}/...`
- Modify: `Deps` struct (already contains `Gemini *genai.Client` from Phase 03)
- Modify: `cmd/server/main.go` to init Gemini client

## Implementation Steps
1. Add dep: `go get google.golang.org/genai` (official Google GenAI Go SDK).
2. `internal/ai/gemini.go`: lazy client init from `GEMINI_API_KEY` (Secret Manager → env var injection at deploy).
3. `internal/ai/embeddings.go`: `Embed(ctx, texts []string) ([][]float32, error)` using `text-embedding-004`. Batch up to 100 inputs per call.
4. `internal/ai/chat.go`: `Generate(ctx, system, history []Msg) (string, error)` using `gemini-1.5-flash`. Output ≤200 tokens, temperature 0.7.
5. `internal/ai/ratelimit.go`: per-user 5 req/min bucket via `golang.org/x/time/rate`. Drop-on-exceed with user-visible "slow down" reply.
6. Pre-compute target embeddings:
   - At cold start, load target pool, embed any not yet cached in Firestore (`semantle_target_cache:<word>` → `[]float32`).
   - 1500 RPD limit means ≤1500 fresh target embeds/day. Curated pool of ~365 targets (one per day) embedded once = ~30 minutes work amortized.
7. Semantle/doantu game flow: user `/semantle`, target picked deterministically from `today's UTC date`. Each `/sguess <word>` → embed user word → cosine similarity → reply with score.
8. Twentyq: user picks a topic, model (system prompt: "you're answering 20-questions about X, reply only yes/no/maybe"). Track Q count, end at 20.
9. Tests:
   - `embeddings_test.go` — fake `*genai.Client` interface; verify cache hit/miss
   - `score_test.go` — cosine math
   - `parser_test.go` — twentyq response parsing
   - `ratelimit_test.go` — bucket refill + drop
10. Smoke each module on dev bot.

## Success Criteria
- [x] `internal/ai` package wraps `google.golang.org/genai` (v1.56) with Embedder/Chatter interfaces; per-user `PerUserLimiter` (5 req / 60s burst).
- [x] `Deps` extended with `Embedder`/`Chatter` (nil when GEMINI_API_KEY unset → modules refuse with config-error).
- [x] `/semantle` ported: 9894-word google-10k pool, JS-parity sigmoid calibration, OOV gate, fast-path dedup, render board with sort+top-15.
- [x] `/doantu` ported via JS-parity `phow2sim` HTTP client (NOT Gemini — see Deviations below).
- [x] `/twentyq` ported with prompts.go (verbatim JS prompt strings), parser.go (JSON-with-fence extraction), redact-secret defense, fallback round-start.
- [x] 429 from Gemini mapped to `ai.ErrRateLimited` → user-visible "rate-limited" reply.
- [x] All factories registered in `cmd/server/main.go`; `go vet ./...` and `go test -race -count=1 ./...` clean.

## Deviations from original plan
- **doantu uses phow2sim HTTP, not Gemini embeddings.** Rationale: text-embedding-004 was not trained for Vietnamese semantic relatedness; phow2sim is a domain-trained PhoW2V model. The JS bot already uses it; switching to embeddings would diverge behaviour, not preserve it. `PHOW2SIM_API_URL` overridable via env (allowlisted in `cmd/server/main.go`).
- **No Firestore-backed target embedding cache** (plan step 6). semantle embeds both target+guess on every call (matches JS bge-m3 path). Cache adds complexity without measurable savings until Phase 11 soak data shows the 1500 RPD ceiling is real.
- **gemini-2.5-flash, not 1.5.** SDK default is the newer flash; behaviour-equivalent for the twentyq use case.
- **Per-day cap deferred.** Token bucket only; if Phase 11 soak shows abuse, add a Firestore counter.

## Risk Assessment
- **Risk**: 768d vs 1024d means similarity scores have different distribution. Game tuning constants (winning threshold) need re-calibration. **Mitigation**: empirical tune against dev bot; document in module file.
- **Risk**: 1500 RPD shared across all users. Heavy semantle play could exhaust. **Mitigation**: per-user 50 req/day soft cap. Cache user-guess embeddings too (most users guess common words).
- **Risk**: `gemini-1.5-flash` cold-start latency (gRPC TLS handshake) on Cloud Run. **Mitigation**: client init at process start, not per-request.
- **Risk**: Gemini may be deprecated or repriced. **Mitigation**: AI ops abstracted behind `internal/ai` package — switching providers (e.g. Vertex AI, OpenRouter free-tier) is a single-package change.

## Rollback
Remove from Factories. AI modules are isolated by package; main framework continues without them.
