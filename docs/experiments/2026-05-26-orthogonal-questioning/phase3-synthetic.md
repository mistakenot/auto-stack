# Spike: Synthetic Latent-Space Recovery (Phase 3)

Phase 1 said the geometric framework didn't fit the real data. Phase 2 said the data wasn't the problem — the *representation* was, since two alternative representations (project-identity residualization, raw Q&A units) produced real structural improvements. But Phase 2 still left a confound: with real data we can never fully tell whether a format is winning because it preserves geometric structure, or because it leaks session identity, or because of dataset-specific quirks.

Phase 3 flips the question. **Assume the hypothesis is true — there's a low-dimensional latent space, users have preference vectors in it, and decisions are samples along directions in that space.** Generate synthetic data where we *control* the latent structure, render it in many candidate input formats, and ask: which format lets the embedding model recover the latent structure we know is there?

If a format wins this test, we reverse-engineer how to make real data match that format. If no format wins, the bottleneck isn't representation — it's the embedding model itself, and the framework needs a different mechanism (learned bilinear classifier, fine-tuned encoder, etc.).

## The experimental setup

**Latent space**: 8 named, semi-orthogonal preference dimensions. Each dimension is a continuous scale in `[-1, +1]`. Chosen because (a) 8 is roughly the manifold dimensionality the Phase 1 deep-dive hypothesized, (b) it's small enough to interpret, (c) it's large enough for "orthogonal questioning" to be a meaningful claim.

| Dim | Name | -1 pole | +1 pole |
|---|---|---|---|
| D1 | validation_strictness | lenient/coercing | strict/rejecting |
| D2 | output_verbosity | terse minimal | verbose detailed |
| D3 | persistence_durability | ephemeral in-memory | durable on disk |
| D4 | error_handling_stance | graceful degradation | fail-fast loud |
| D5 | schema_rigidity | loose flexible | strict typed |
| D6 | api_explicitness | implicit magical | explicit configurable |
| D7 | dependency_appetite | lean stdlib-only | batteries-included |
| D8 | concurrency_taste | sequential simple | parallel maximal |

**Users**: N=30 synthetic developers. Each gets a fixed preference vector `u_i ∈ ℝ⁸` sampled from N(0, I). These are the ground truth we'll try to recover.

**Tasks**: M=5 distinct project contexts per user (Go CLI, ETL pipeline, web API, data viz dashboard, ML inference service). Realistic enough to anchor the LLM renderer.

**Decisions per (user, task)**: K=6. Each decision touches 1-3 latent dimensions (randomized; some decisions are pure, others mixed). The chosen value is `sign(u_i · d_j + noise)` where `d_j` is the dimension weight vector for decision j.

Total dataset: 30 × 5 × 6 = **900 decisions**, each with a known ground-truth latent contribution.

## The eight candidate formats

For each decision, an LLM renderer generates eight separate input strings of varying structure. The renderer is given the latent dimension(s) involved, the project context, and a style instruction; it produces realistic-looking text without knowing how it'll be used.

| ID | Style | What it includes |
|---|---|---|
| S1 | Bare label | 2-5 word phrase ("strict schema validation") |
| S2 | Enriched | `type: value — 1-sentence context` (Phase 2 F2) |
| S3 | Q&A | Question + chosen answer, no options |
| S4 | Q&A + alternatives | Question + all options + chosen + 1-line why |
| S5 | Reasoning rich | 2 paragraphs of user-voice rationale |
| S6 | Decision + consequences | Choice + 1 paragraph on downstream effects |
| S7 | Structured composite | Multi-field: type, value, rejected_alternatives, reasoning, scope |
| S8 | User voice raw | Natural conversational user statement, no template |

S1, S2, S3 are the controls (already tested on real data in Phase 1/2). S4-S8 are the new candidates motivated by Phase 2's "missing distinctiveness" finding.

## Renderer design

The renderer LLM gets a prompt of this shape:

```
You are generating realistic decision text from a software project.

PROJECT CONTEXT: {context_description}
DECISION CONTEXT: {decision_situation}
LATENT POSITION:
  - {dim_name}: {value_phrase}  // e.g. "validation_strictness: strongly strict (+0.8)"
  - {dim_name}: {value_phrase}
STYLE: {style_instruction}

Generate text that {style_specific_instruction}. Be specific. Use vocabulary
appropriate to a senior engineer making this choice. Do not mention "latent
dimensions" or use machine-readable structure unless the style requests it.
```

The LLM is calibrated using a few-shot block drawn from real Phase 1 decisions, so the synthetic decisions look qualitatively similar to real ones.

**Critical property**: The same underlying decision (same latent position) gets eight different rendered texts. Different decisions (different positions) produce different texts in any given style. If a format wins, it's because that format's text encodes the latent vector in a way the embedding model can recover.

## Metrics per format

Four metrics, each measuring a different aspect of "did we recover the structure?"

### M1: Linear probe recoverability (the headline)

For each format Si:
- Split decisions 80/20 by user (so the test users were never seen during training)
- Train a linear regression: `decision_embedding → user_preference_vector`
- Measure R² on held-out users

What good looks like: R² > 0.6. The embedding genuinely encodes the latent dimensions.
What bad looks like: R² < 0.2. The embedding is essentially noise w.r.t. the latent vector.

### M2: PCA spectrum vs known dimensionality

For each format:
- Embed every decision
- Build a session×decision matrix the same way as Phase 1 Spike 1
- Run PCA, compute n_components_90

We *know* the true dimensionality is 8. A format that recovers the structure should produce n_90 ≈ 8-12 (some over-counting from rendering noise).
What bad looks like: n_90 = 50+, indistinguishable from random.

### M3: Pair distance correlation

For each format and every pair of decisions (across all users):
- Compute embedding cosine distance
- Compute *true* latent distance (Euclidean on the underlying vectors, weighted by the dimension's contribution to each decision)
- Spearman ρ between the two

What good looks like: ρ > 0.6. Embeddings preserve relative distances in the latent space.

### M4: Orthogonal-questioning simulation (the application)

For each format, simulate the full GP-style protocol from the original orthogonal-questioning thesis:
- Take a held-out user with unknown preference
- Estimate `μ₀` from the average of training users' preferences
- Ask K questions, each one resolving one dimension by getting that user's decision along it
- After each answer, recompute the posterior and pick the next question along the largest-eigenvalue direction
- Stop when `||μ - u_true|| < ε`

The headline number: average questions until convergence per format. The thesis predicts 3-5 questions for an 8-dim space. We can finally test that claim with ground truth.

## What "winning" looks like

A format Si wins if it dominates the others on ≥3 of the 4 metrics, AND its M1 R² is above 0.5 (so we know it's actually doing recovery, not just doing badly less badly).

If no format clears the bar, the conclusion is sharper than Phase 1: the framework needs more than format engineering — it needs a different mechanism.

## Deliverables

A subdirectory `.tmp/experiments/orthogonal-questioning/phase3/` containing:

- `phase3_synthetic_data.jsonl` — 900 decisions with all 8 format renderings + ground truth vectors
- `phase3_embeddings_cache.sqlite` — embedding cache per format
- `phase3_leaderboard.json` — full metric table
- `phase3_residual_curves.png` — orthogonal-questioning simulation curves (8 formats overlaid)
- `phase3_recovery_scatter.png` — for the winning format, scatter of predicted vs true preference vectors
- `phase3_notes.md` — synthesis

## Why this experiment is decisive

Phase 1 and 2 always had the "is the structure even there?" confound. Phase 3 doesn't — we build the structure, so we know it's there.

Three possible outcomes, each highly actionable:

1. **One format wins clearly.** Reverse-engineer how to make real Phase 1 data match that format. Probably involves a smarter LLM extraction step that produces the winning structure.
2. **All formats win roughly equally (and well).** Embeddings are robust to format; the Phase 1/2 negative results were about data noise, not representation. Focus on cleaning real data, not changing format.
3. **No format wins.** Embeddings can't recover 8-dim latent structure from 100-word text descriptions. The framework needs a learned encoder or a different mechanism entirely. This is the most disruptive but most clarifying outcome.

## Cost estimate

- 900 decisions × 8 formats × gpt-4.1-mini (~100 tokens out each) = 720K output tokens × $1.60/M = **~$1.15**
- 7200 embeddings × text-embedding-3-small (~150 tokens) = 1.08M tokens × $0.02/M = **~$0.02**
- Total: **~$1.20** (well within the remaining ~$28 of the $30 budget)

## Effort

Single worker, ~3-4 hours of run time. Generation is the slow part (paralelizable).

---

## Findings (run 2026-05-27)

Single worker, ~22 minutes wall time, ~$0.65 OpenAI spend. Artifacts in `.tmp/experiments/orthogonal-questioning/phase3/`. Dataset: 30 users × 5 tasks × 6 decisions = 900 decisions, each rendered in 8 formats by gpt-4.1-mini.

### Leaderboard

| Format | Style | M1 R² | M2 n_90 | M3 Spearman ρ | M4 frac-recovered | Wins |
|---|---|---|---|---|---|---|
| S1 | Bare label | 0.191 | 119 | 0.004 | 0.041 | 0 |
| S2 | Enriched | 0.241 | 102 | 0.001 | 0.038 | 0 |
| S3 | Q&A | 0.227 | **92** | -0.007 | 0.037 | 1 |
| S4 | Q&A + alternatives | 0.169 | 92 | -0.001 | 0.025 | 0 |
| S5 | Reasoning rich | 0.243 | 101 | 0.005 | 0.039 | 0 |
| S6 | Decision + consequences | 0.262 | 103 | -0.006 | 0.044 | 0 |
| S7 | Structured composite | 0.231 | 99 | -0.033 | 0.036 | 0 |
| **S8** | **User voice raw** | **0.276** | 144 | **0.028** | **0.049** | **3** |

### Outcome classification

The design doc framed three possible outcomes. Phase 3 landed between (a) and (c):

- **(a) one format wins clearly** — partially true. S8 ranks first on 3 of 4 metrics, but the absolute ceiling (R² 0.276) is well below the "good" bar (0.6) and the spread between best and worst formats is only ~0.1 R². The ranking is real but the gap is small.
- **(c) no format wins** — the more honest read. R² 0.28 on ground truth means most of the variance in the user's preference vector is *not* recoverable from the embedding of a single decision. The orthogonal-questioning protocol on this data recovers only ~5% of the initial residual after 8 questions, where the original thesis predicted full convergence in 3-5.

### The decisive negative finding

The thing that makes Phase 3 sharper than Phase 1+2 is M3.

**Linear probe (M1) gets R² = 0.28. Pairwise cosine similarity (M3) gets Spearman ρ ≈ 0 across all formats.** The information is in the embedding but it is not in the cosine geometry.

This rules out the entire family of approaches built on "decisions that are similar in embedding space are similar in decision space":
- Nearest-neighbour session retrieval
- PCA basis as the orthogonal question basis
- GP with a cosine kernel
- Any UCB/EI acquisition function over an embedding distance metric

A learned probe (linear regression onto the latent dimensions) can extract *some* signal. A geometric procedure (cosine distance, PCA eigenvectors) cannot. These were the load-bearing assumptions of the original thesis.

### Why this is more decisive than Phase 1/2

Phase 1 said the data didn't have structure. Phase 2 said the representation was wrong. Phase 3 built the structure *into the data*, controlled the representation, gave the embedding model the cleanest possible synthetic decisions in eight different styles — and the cosine-geometry recovery still failed. With ground truth in hand, this isn't a "maybe the noise is hiding it" finding any more. The bottleneck is the embedding mechanism itself, not the input format.

### Secondary observations

1. **Natural prose beats structured composition.** S8 (raw user-voice, no labels) beats S7 (multi-field composite), S5 (reasoning-rich), and S4 (Q&A + alternatives). Adding structural scaffolding consumed token budget without adding signal. If you ever do construct structured inputs, keep them terse.

2. **PCA spectrum compression is not a proxy for recoverability.** S3 had the cleanest scree plot (n_90 = 92) but S8 had the best linear-probe R². Optimizing for "low effective dimensionality" by PCA isn't the right target.

3. **Latent dimensions are not equally recoverable.** D3 (persistence/durability) and D7 (dependency appetite) recover at per-dim R² ~ 0.4–0.5. D5 (schema rigidity) and D6 (API explicitness) are nearly unrecoverable. Natural language doesn't have distinct vocabulary for these dimensions, so the embeddings can't separate them — even when the latent positions are clearly different.

4. **The M4 simulation pathology.** The originally-specified simulation (per-decision argmax answers) made the residual *increase* over time — the probe's noisy predictions pushed μ further from u_true. The worker switched to mean-of-predictions, which monotonically decreases residual but converges at a very low ceiling (~5% of initial). Either way, the protocol cannot reach ε convergence in 8 questions on this data.

### What this implies for the orthogonal-questioning framework

The framework as originally specified has three load-bearing assumptions:

1. There's a low-dim latent space of user preferences. ← **plausible** (Phase 3 confirmed an 8-dim latent space can be probed).
2. Embeddings of decision text encode positions in that space. ← **partially true** (some dimensions yes, others no; ~30% of variance recoverable).
3. Cosine geometry / PCA over embeddings recovers the latent structure. ← **false** (M3 is the killer; all formats produce ρ ≈ 0).

So the framework as proposed cannot work via off-the-shelf embeddings, regardless of input format. To make it work you need to replace (3) with a *learned* mechanism — at minimum a linear projection from embedding space to preference space, trained on labelled data, possibly per-dimension classifiers.

### Path forward, if the goal is still preference recovery from text

In rough order of cost/complexity:

1. **Per-dimension linear classifiers.** Train one classifier per latent dimension (one-vs-rest) over the embedding. Each classifier is a learned direction in embedding space that corresponds to one preference axis. The "orthogonal question" becomes "ask about the dimension whose classifier has the largest uncertainty given current evidence." This is the natural reformulation that Phase 3 points at.

2. **Bilinear preference model.** Instead of cosine distance, learn a bilinear scoring function `score(decision_embedding, user_vector) = decision_embedding^T W user_vector` where W is learned. Allows decisions and users to live in different spaces with a trained correspondence.

3. **Fine-tune a small encoder.** Take a small text encoder (e.g. a distilled BERT) and fine-tune it with a contrastive loss: same-user decisions pull together, different-user decisions push apart. This is more expensive but might break the R² ceiling that the off-the-shelf encoder hit.

4. **Active learning with the linear probe.** Even at R² 0.28 the probe has signal. A smart active-learning loop that asks questions to refine the *probe* (not just the user posterior) might converge faster than the protocol Phase 3 tested. This is closer to "ask questions to learn the model" than "ask questions to identify the user."

The Phase 1+2+3 sequence has now decisively ruled out the off-the-shelf embedding + cosine geometry path. Any continued investment should pick one of (1)-(4) and try it on real data.

### Cost across all three phases

Total OpenAI spend across Phase 1 + Phase 2 + Phase 3: under $2 of the original $30 budget. The framework hypothesis was extensively tested for under the price of lunch.
