# Semantle API Alternatives Research Report
**Date:** 2026-04-22 | **Project:** miti99bot

---

## Executive Summary

**Recommendation: Cloudflare Workers AI (BGE-base-en-v1.5) + Vectorize for production. Runner-up: Self-hosted precomputed embeddings (GloVe/R2).**

ConceptNet's unreliability (502 errors) requires immediate replacement. The consensus winner is **Cloudflare Workers AI embeddings** because:
- Native to CF Workers (no fetch latency overhead, binding-based)
- Proven at scale with edge inference (<100ms cold start, ~50-200ms per embedding)
- Free tier: 10M input tokens/month (sufficient for 10k-20k single-word requests)
- Cosine similarity built-in (768-dim BGE vectors match ConceptNet's semantic space)
- Solves OOV detection via vocabulary check on ingestion

For "free tier only" projects (100% cost-conscious), **precomputed GloVe vectors in R2/KV** is feasible if you accept a one-time ~10MB upload and manual vocab checking.

---

## Comparison Table

| Provider | Auth | Free Tier | Latency | Similarity API | OOV Support | CF Workers Fit | Verdict |
|----------|------|-----------|---------|----------------|-------------|----------------|---------|
| **CF Workers AI (BGE)** | Binding | 10M tokens/mo | ~50-200ms | Cosine (768d) | Via vocab list | Native ⭐⭐⭐ | **RECOMMENDED** |
| **CF Vectorize** | Binding | 30M dimensions/mo | ~30ms | Cosine query | Via storage | Native ⭐⭐⭐ | Best for scale |
| **HuggingFace Inference** | API key | ~100 req/hr free | 500ms-2s cold | Cosine (384d) | Yes | Fetch OK ⭐⭐ | Viable but slow |
| **OpenAI text-embedding-3-small** | API key | $0.02/1M tokens | ~200-500ms | Cosine | Yes | Fetch OK ⭐⭐ | Overkill, cost adds up |
| **Replicate** | API key | Free w/ credits | 500ms+ | Cosine | Yes | Fetch OK ⭐⭐ | Slower than CF AI |
| **Datamuse API** | None (free) | ∞ | ~100-300ms | No (ranking only) | Yes | Fetch OK ⭐⭐⭐ | **No similarity score** ❌ |
| **GloVe (self-hosted KV/R2)** | None | ✓ (one-time) | ~10-50ms | Cosine (300d) | Manual | Fastest ⭐⭐⭐ | Great for small vocab |
| **Word2Vec REST APIs** | Varies | Mostly down | Variable | Cosine | Yes | Fetch | Dead/unreliable |

---

## Detailed Option Analysis

### 1. ⭐ **Cloudflare Workers AI (BGE-base-en-v1.5) — RECOMMENDED**

**What it does:** Generates 768-dimensional sentence/word embeddings via BAAI's BGE model, runs on CF edge infrastructure.

**Call pattern:**
```javascript
const embedding = await env.AI.run("@cf/baai/bge-base-en-v1.5", {
  text: "word_to_embed"
});
// Returns { shape: [768], data: [float...], pooling: "mean" }
```

**Pros:**
- No fetch overhead — native CF Workers binding, direct to inference layer
- Cold start <100ms, typical latency 50-200ms for single words (ideal for game speed)
- 768 dimensions match semantic similarity expectations (ConceptNet-like quality)
- Free tier: 10M input tokens/month = ~100k single-word embeddings (sufficient for 100-200 games/day if each game = 5 guesses)
- Cosine similarity scoring built-in at client (just compute `dot(a, b) / (||a|| * ||b||)`)
- Paid tier: $0.067/M input tokens (cheap relative to OpenAI)

**Cons:**
- Requires Workers paid plan for Workers AI access (~$15/month base, then token-metered on top)
- OOV detection not native to embeddings — must maintain separate vocab list or batch-verify words
- 768 dimensions = ~3KB per cached embedding; not huge but adds up for 10k words

**OOV handling:** Pre-load Google-10000-english into KV (~300KB), check membership before calling similarity. Or call embeddings on both words; if confidence is suspiciously uniform, flag OOV.

**Real-world latency:** One production report: "global latency under 80ms p50" for embeddings via CF Workers.

**Cost at scale:**
- 100 games/day × 5 guesses/game × 2 words/guess = 1000 embeddings/day
- 1000 × 30 days = 30k tokens/month → free tier sufficient (10M tokens)
- At paid tier: negligible ($0.002/month)

**Recommendation:** Ship with this. It's the path of least resistance and best latency.

---

### 2. ⭐⭐ **Cloudflare Vectorize — BEST FOR SCALE**

**What it does:** Managed vector database; store embeddings by word key, query by cosine similarity.

**Architecture:**
1. Pre-compute all 10k words' embeddings via Workers AI in a setup task
2. Store in Vectorize index (768 dimensions, cosine metric)
3. At game start, query Vectorize: `index.query(targetWordEmbedding, topK=1)` to verify it's in vocab
4. On each guess, fetch both embeddings from Vectorize (cached) + compute similarity client-side OR use Vectorize search with custom scoring

**Pros:**
- Median query latency 30-31ms (faster than Workers AI)
- Free tier: 30M queried vectors/month = ~1M queries/month (plenty for hobby traffic)
- Pre-computed vectors cached globally (no re-embedding per-game)
- Deterministic: same two words always return same score

**Cons:**
- Setup overhead: must pre-compute and upload all 10k embeddings once
- Requires Vectorize binding + Workers AI for initial embedding generation
- Query cost if not in free tier ($0.01/1M queried dimensions)
- Overkill for simple 10k-word game; adds operational complexity

**Cost at scale:** Negligible if free tier applies. At paid tier (unlikely): $0.000001 per query.

**Recommendation:** Consider after MVP ships. For initial launch, Workers AI direct is simpler. Migrate to Vectorize if you scale beyond 100k guesses/month or want <50ms guarantees.

---

### 3. **HuggingFace Inference API — VIABLE BUT SLOW**

**What it does:** API to sentence-transformers (all-MiniLM-L6-v2, all-mpnet-base-v2, etc.). Free tier has rate limits.

**Call pattern:**
```javascript
const response = await fetch("https://api-inference.huggingface.co/models/sentence-transformers/all-MiniLM-L6-v2", {
  headers: { Authorization: `Bearer ${HF_TOKEN}` },
  method: "POST",
  body: JSON.stringify({ inputs: ["word"] })
});
```

**Pros:**
- Free tier available (~few hundred req/hr limit)
- Many sentence-transformer models to choose from
- OOV is implicit (any word gets an embedding)
- No account setup beyond free HF profile

**Cons:**
- Cold start latency: 30-60 seconds on free tier (models unloaded on-demand)
- Even after warmup, 500ms-2s per request
- Rate limits strict on free tier (~100 requests/hour)
- 384 dimensions (smaller than BGE, may affect quality)
- Fetch round-trip from CF Workers adds another 50-100ms

**Adoption risk:** Free tier is unreliable for production games (rate limits + cold starts violate 5s timeout). Requires paid tier ($9/month for 2M credits) to be usable.

**Recommendation:** Skip. Workers AI is superior and same cost (or free).

---

### 4. **OpenAI text-embedding-3-small — OVERKILL**

**What it does:** Industry-standard embeddings via OpenAI API.

**Pros:**
- Best-in-class embeddings quality
- Simple API, well-documented
- No cold starts

**Cons:**
- $0.02/1M tokens = $0.0002 per word embedding (adds up fast)
- 100 games/day × 5 guesses × 2 words = 1000 tokens/day = $0.02/day = $0.60/month
- Overkill for single-word similarity (trained on sentences, wasted capacity)
- Slower than CF Workers AI (200-500ms typical)
- Requires API key in Worker env (security overhead)
- Rate limits apply

**Recommendation:** Too expensive and slow. Only if you value maximum embedding quality above cost/latency (not applicable here).

---

### 5. **Replicate — SLIGHTLY WORSE CF OPTION**

**What it does:** Cloud inference platform supporting embeddings models (multilingual-e5-large, all-mpnet-base-v2, etc.).

**Pros:**
- Competitive pricing (~$0.11 per run vs OpenAI's $0.51)
- Wide model choice
- Cloudflare acquired Replicate in Nov 2025 → may improve integration

**Cons:**
- Latency 500ms+ (slower than Workers AI + fetch round-trip)
- Requires separate account + API key
- Not a binding, so full fetch overhead from CF Workers
- Pricing less clear (charged by time, not tokens)

**Recommendation:** Skip in favor of Workers AI. Close competitor if CF AI binding becomes unavailable.

---

### 6. **Datamuse API — INSUFFICIENT**

**What it does:** Free word relationship API (rhyming, meaning, spelling, sound-alike).

**Why it fails:**
- **No numeric similarity score between two words.** Returns ranked lists of related words, not pairwise scores.
- Scores have "no interpretable meaning" (per official API docs); used for ranking results only.
- Cannot compute "target vs guess" similarity in the Semantle game format.

**Recommendation:** Rejected. Core requirement not met.

---

### 7. ⭐⭐⭐ **Self-Hosted Precomputed Embeddings (GloVe/R2) — COST-OPTIMAL**

**What it does:** Pre-download GloVe vectors (300d, 6B tokens, 822MB), extract vectors for google-10k words, store in R2, load into KV for fast lookup.

**Architecture:**
```
1. Download glove.6B.300d.txt (free, public domain)
2. Extract 10k words + vectors → ~30MB JSON
3. Gzip → ~8-10MB, store in R2 (free tier includes 10GB)
4. On Worker startup: fetch from R2 (or lazy-load per region), cache in KV
5. similarity(a, b) = cosine(glove[a], glove[b])
6. OOV: word not in glove dict → return null
```

**Pros:**
- Truly free (no API calls, no Workers AI quota)
- Fastest: cosine similarity is ~5-10ms JS computation
- Completely deterministic, no API reliability concerns
- Can pre-compute all 10k × (10k-1) / 2 similarity pairs into KV (expensive, not needed)
- Offline-first: no upstream dependency

**Cons:**
- One-time setup: download, parse, compress, upload to R2 (~30 min work)
- GloVe is ~7 years old; quality lag behind BGE (but still good for word similarity)
- 300 dimensions vs 768 in BGE (may affect semantic quality, but acceptable for Semantle)
- ~3KB per cached embedding × 10k words = 30MB in memory if fully loaded (within CF Worker limits, but tight)
- Manual OOV check: need separate google-10000-english list in KV

**Adoption risk:** Low. GloVe is stable, no API changes. Can cache vectors indefinitely. Only upgrade path is re-download if you want newer embeddings.

**Real cost:** $0 per month.

**Recommendation:** Highly viable for hobby projects. Better than any API if you're cost-optimizing and accept slight quality trade-off. Hybrid: use GloVe for now, upgrade to Workers AI if you need better semantics later.

---

### 8. **Word2Vec REST APIs — OBSOLETE**

Search found several GitHub repos (quhfus/DoSeR, 3Top/word2vec-api, bmzhao/word2vec-rest-api) but:
- None maintained recently (last commits 2020-2022)
- No public instances available
- Self-hosting them defeats the purpose (you'd run a server alongside CF Workers)

**Recommendation:** Skip. GloVe is better maintained if you want self-hosted embeddings.

---

## Migration Sketch for Recommended Option

### Implementation Plan: Cloudflare Workers AI (BGE) + Vectorize Fallback

**Phase 1: Workers AI (MVP)**

Modify `api-client.js`:

```javascript
export function createClient(options = {}) {
  const { env, useVectorize = false } = options;
  
  // Vocab check: load google-10000-english from KV
  async function isInVocab(word) {
    const vocab = await env.KV.get("semantle:vocab-10000");
    if (!vocab) return true; // pessimistic: assume yes if missing
    return vocab.includes(word.toLowerCase());
  }

  return {
    async randomWord() {
      // Pick from pool; verify it's "in vocab" by checking if we can embed it
      for (let i = 0; i < MAX_RANDOM_ATTEMPTS; i++) {
        const candidate = pickFromPool();
        try {
          const inVocab = await isInVocab(candidate);
          if (inVocab) return { word: candidate, verified: true };
        } catch {
          // continue
        }
      }
      return { word: pickFromPool(), verified: false };
    },

    async similarity(a, b) {
      const inVocabB = await isInVocab(b);
      if (!inVocabB) {
        return {
          a, b, canonical_a: a, canonical_b: b,
          in_vocab_a: true, in_vocab_b: false, similarity: null
        };
      }

      try {
        // Call Workers AI to get embeddings
        const [embA, embB] = await Promise.all([
          env.AI.run("@cf/baai/bge-base-en-v1.5", { text: a }),
          env.AI.run("@cf/baai/bge-base-en-v1.5", { text: b })
        ]);

        // Compute cosine similarity
        const sim = cosineSimilarity(embA.data, embB.data);
        return {
          a, b, canonical_a: a, canonical_b: b,
          in_vocab_a: true, in_vocab_b: true,
          similarity: sim
        };
      } catch (err) {
        throw new UpstreamError("workers-ai embedding failed", { cause: err });
      }
    }
  };
}

function cosineSimilarity(vecA, vecB) {
  let dotProduct = 0, normA = 0, normB = 0;
  for (let i = 0; i < vecA.length; i++) {
    dotProduct += vecA[i] * vecB[i];
    normA += vecA[i] * vecA[i];
    normB += vecB[i] * vecB[i];
  }
  return dotProduct / (Math.sqrt(normA) * Math.sqrt(normB));
}
```

**Phase 2: Setup Task**

Add to `wrangler.toml`:
```toml
[env.production]
ai = true
kv_namespaces = [{ binding = "KV", id = "..." }]
```

Pre-populate KV with vocab list:
```bash
# Download google-10000-english, store in KV
curl -s https://raw.githubusercontent.com/first20hours/google-10000-english/master/google-10000-english.txt | \
  jq -Rs 'split("\n") | map(select(length > 0))' | \
  npx wrangler kv:key put --binding=KV "semantle:vocab-10000" -
```

**Phase 3: Vectorize (Optional, Future)**

Once MVP is stable:
1. Pre-compute all 10k word embeddings
2. Store in Vectorize index
3. Replace `similarity()` with cached lookups
4. ~30ms per query vs ~200ms (7x speedup, negligible for gameplay)

---

## Cost Breakdown (Monthly)

| Option | Setup | Per-Game (avg) | 100 games/mo | 1000 games/mo | 10k games/mo |
|--------|-------|----------------|--------------|---------------|--------------|
| Workers AI (BGE) | $0 | $0 (free tier) | Free | Free | $0.07 |
| Vectorize | 1h | $0 (cached) | Free | Free | $0.001 |
| HuggingFace (paid) | $0 | $0.0002 | $0.02 | $0.20 | $2.00 |
| OpenAI | $0 | $0.0002 | $0.02 | $0.20 | $2.00 |
| Replicate | $0 | $0.0002 | $0.02 | $0.20 | $2.00 |
| GloVe (self-host) | 1h | $0 | Free | Free | Free |

---

## Recommendation Summary

| Use Case | Recommendation |
|----------|---|
| **MVP / Immediate fix** | Cloudflare Workers AI (BGE-base-en-v1.5) + Google-10k vocab in KV |
| **Ultra cost-conscious** | GloVe vectors in R2 + KV (one-time setup, zero ongoing cost) |
| **Production scale (>1k games/mo)** | Workers AI → migrate to Vectorize for caching |
| **Maximum semantic quality** | Workers AI (BGE is excellent, no need for OpenAI overkill) |

**Ship recommendation:** Go with Workers AI. 50-200ms latency is acceptable for game UX (faster than ConceptNet ever was), free tier covers hobby traffic, and if you grow, Vectorize is a drop-in upgrade.

---

## Unresolved Questions

1. **GloVe quality for single-word semantics?** GloVe trained on document context; single-word embeddings may be noisier than BGE (which uses contrastive learning for dense retrieval). Needs A/B testing if semantics matter (probably doesn't for game).

2. **BGE "mean" vs "cls" pooling?** Current approach uses default "mean" pooling. Does "cls" (CLS token) pooling improve single-word similarity? Requires testing on Semantle target words.

3. **OOV detection robustness?** Relying on google-10000-english for vocab checking; what if player guesses a valid English word outside this list (e.g., "cryptocurrency")? Current approach: fallback to "not in vocabulary" (conservative). Could call embeddings on all words and use confidence/variance heuristics, but adds latency.

4. **Vectorize v2 latency in practice?** Cited 30-31ms median; but is that from CF Workers client or external? If external, add 50-100ms fetch. Need real-world benchmark from within a Worker.

5. **Workers AI quota enforcement?** 10M tokens/month free tier — is this enforced? What happens on overage? (Assumed: immediate billing, no auto-overage blocking, similar to other CF quotas.)

6. **Cloudflare API stability for Workers AI?** ConceptNet is failing; is Workers AI more reliable? (Assumption: yes, it's Cloudflare's own service, not external upstream. Still risk, but lower.)

---

## Sources

- [Cloudflare BGE-base-en-v1.5 embeddings docs](https://developers.cloudflare.com/workers-ai/models/bge-base-en-v1.5/)
- [Cloudflare Workers AI models](https://developers.cloudflare.com/workers-ai/models/)
- [Cloudflare Vectorize pricing](https://developers.cloudflare.com/vectorize/platform/pricing/)
- [Cloudflare Vectorize get-started](https://developers.cloudflare.com/vectorize/get-started/embeddings/)
- [HuggingFace Inference API](https://huggingface.co/docs/api-inference/en/index)
- [HuggingFace pricing](https://huggingface.co/docs/inference-providers/pricing)
- [OpenAI text-embedding-3-small pricing](https://developers.openai.com/api/docs/pricing)
- [Datamuse API docs](https://www.datamuse.com/api/)
- [GloVe word embeddings Stanford NLP](https://nlp.stanford.edu/projects/glove/)
- [google-10000-english corpus](https://github.com/first20hours/google-10000-english)
- [Replicate embeddings models](https://replicate.com/collections/embedding-models)
- [Cloudflare Workers AI latency benchmarks](https://www.kalviumlabs.ai/blog/production-ai-on-cloudflare-workers/)
- [Embeddings API comparison 2026](https://supermemory.ai/blog/best-open-source-embedding-models-benchmarked-and-ranked/)
- [Cloudflare KV + Vectorize integration](https://dev.to/andyjessop/building-ai-powered-second-brain-in-a-cloudflare-worker-with-cloudflare-vectorize-and-openai-23di)
