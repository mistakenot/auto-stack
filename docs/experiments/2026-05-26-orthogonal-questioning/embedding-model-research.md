# Embedding Model Research Notes

Background research conducted between Phase 1 and Phase 2 to validate model selection. Filed alongside the experiment so the rationale is durable.

## The text-embedding-3 family

| Model | Dims (native → reducible) | Max input | MTEB | MIRACL | $/1M tok |
|---|---|---|---|---|---|
| text-embedding-3-small | 1536 (fixed) | 8191 | 62.3 | 44.0 | $0.02 |
| text-embedding-3-large | 3072 → 256/1024/1536 via Matryoshka | 8191 | 64.6 | 54.9 | $0.13 |
| ada-002 (legacy, don't use) | 1536 | 8191 | 61.0 | 31.4 | $0.10 |

Notable: text-embedding-3-large truncated to **256 dims** still beats ada-002 at 1536. Matryoshka was specifically trained to allow truncation — pass the `dimensions` API parameter and the output comes back pre-normalized; no extra L2 normalization needed.

## What OpenAI explicitly recommends

- **Cosine similarity** for distance. Their embeddings are L2-normalized, so dot product = cosine.
- Six listed use cases: search, clustering, recommendations, anomaly detection, diversity measurement, classification.
- For text > 8191 tokens: chunk and average. No guidance for the opposite (very short text).

## What's conspicuously missing from official docs

Checked the model cards, embeddings guide, launch blog post, and embeddings FAQ. **There is no official guidance on:**

- Embedding short labels (2-5 words) vs full sentences
- Handling near-duplicates / paraphrases
- Minimum useful input length
- Domain-specific preprocessing
- What cosine similarity values mean in practice (no "0.85 = similar" rule of thumb)

The OpenAI community thread on legal-text preprocessing has someone asking exactly these questions — and they got no answers. This is a known empirical gray area.

## What the research and community have converged on

1. **HyDE (Hypothetical Document Embeddings)** — for short queries, generate a hypothetical full document with an LLM, then embed *that*. Bridges the semantic gap between terse inputs and corpus context. Now standard practice in retrieval.
2. **LLM-based input enrichment** (arXiv 2404.12283) — rewriting/expanding short text before embedding measurably improves clustering and similarity benchmarks. **But:** Phase 2 Spike 5 showed this can backfire (HyDE was the worst-performing format) when the LLM produces uniform-style outputs that flatten distinctions.
3. **Consistent enrichment > raw labels for clustering** — context-augmented inputs cluster more reliably than bare labels because tiny wording variations stop dominating distance.

## How this informed the experiment

- **Phase 1** used bare `decision_value` strings (2-5 tokens) — exactly the case OpenAI doesn't document and the literature cautions against. Phase 1's failures partly traced to this.
- **Phase 2 Spike 5** tested enrichment formats. Found that F4 (turn-window, ~600 tokens) won on correlation. F5 (HyDE-style LLM rationale) failed — homogenized style hurt more than added context helped.
- **Phase 2 Spike 6** tested upgrading to text-embedding-3-large at 1536 dims (Matryoshka) and 3072 dims (native). Result: **no measurable improvement** at this data scale. M1 R² changed by 0.011 over small; top-10% rankings agreed 95%.

## The settled empirical answer

For this kind of task (extracting preferences from short decision artifacts), **use text-embedding-3-small at 1536 dims with enriched inputs**. The large model isn't worth the 6.5× cost. The bottleneck is the input format and the analysis method, not encoder capacity.

If a future experiment hits a setting where the small model is clearly the bottleneck (e.g. lots of multilingual data), reach for large-at-1536 first; the Matryoshka truncation gives most of the benefit at half the cost of native 3072.

## Sources

- [OpenAI: new embedding models announcement](https://openai.com/index/new-embedding-models-and-api-updates/)
- [OpenAI API docs: text-embedding-3-large](https://developers.openai.com/api/docs/models/text-embedding-3-large)
- [OpenAI API docs: text-embedding-3-small](https://developers.openai.com/api/docs/models/text-embedding-3-small)
- [OpenAI Embeddings guide](https://developers.openai.com/api/docs/guides/embeddings)
- [DataCamp: text-embedding-3-large guide (benchmarks + Matryoshka)](https://www.datacamp.com/tutorial/exploring-text-embedding-3-large-new-openai-embeddings)
- [HyDE: Hypothetical Document Embeddings](https://www.emergentmind.com/topics/hypothetical-document-embeddings-hyde)
- [LLM-based text enrichment for embeddings (arXiv 2404.12283)](https://arxiv.org/abs/2404.12283)
- [OpenAI community: preprocessing for text-embedding-3-large (notably unanswered)](https://community.openai.com/t/preprocessing-techniques-for-generating-embedding-vectors-from-legal-texts-with-text-embedding-3-large/794302)
