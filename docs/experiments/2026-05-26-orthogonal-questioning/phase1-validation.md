---
hash: "7aa661b1"
id: "1abde83c"
read_when: "evaluating decision intelligence approaches, understanding orthogonal questioning experiment results, or designing session-based ML spikes"
summary: "Four-spike experimental validation of the requirement-vector-space framework for orthogonal questioning, with findings from real session data showing partial geometry structure and a pivot to sparse-support recovery."
title: "Spike: Orthogonal Questioning Validation Experiments"
---

# Spike: Orthogonal Questioning Validation Experiments

Validate the three core technical risks of the requirement-vector-space framework before investing in production infrastructure.

## Prerequisites

- Python 3.12 + uv
- OpenAI API key (embeddings + chat completions)
- autosearch CLI with indexed session data
- Dependencies: numpy, scikit-learn, openai, pandas, pyarrow, matplotlib

## Spike 0: Decision Extraction Pipeline

**Purpose:** Build the foundational dataset that all other spikes consume. Extract structured decision tuples from session history.

**What to build:**
A Python script that:
1. Queries autosearch for decision-bearing messages across three sources:
   - AskUserQuestion answers (`"answered your questions"`, ~241 hits across 56 sessions)
   - /new-task and /process-requirements args (implicit decisions in task descriptions)
   - Mid-session corrections (`"changed my mind"`, `"actually"`, `"lets change"`)
2. Sends each message to OpenAI (gpt-4.1-mini) with a structured extraction prompt
3. Outputs a JSONL file of decision tuples

**Extraction schema per tuple:**
```json
{
  "session_id": "abc123",
  "message_index": 86,
  "decision_type": "storage_backend",
  "decision_value": "postgres",
  "decision_context": "ETL pipeline with concurrent writes",
  "source": "explicit_ask",
  "raw_text": "the original snippet"
}
```

**LLM extraction prompt (sketch):**
```
Given this message from a coding session, extract every decision the user made.
A "decision" is a choice between alternatives about how to build software.

For each decision, output:
- decision_type: a short snake_case category (e.g. storage_backend, test_strategy, output_format, cli_structure, error_handling, scope_boundary)
- decision_value: what they chose, 2-5 words
- decision_context: why or in what context, 1 sentence

If no decisions are present, return an empty array.
```

**Success criteria:**
- Extract 300+ decision tuples across 50+ sessions
- At least 15 distinct decision_type categories emerge
- Spot-check 30 random tuples — 80%+ should be correctly extracted decisions

**Stretch:** Also extract decisions from the parquet data directly (read message content from `~/.auto/etl/output/messages/`) to avoid autosearch snippet truncation.

**Synthetic data fallback:** If extraction quality is poor, hand-craft 200 realistic decision tuples based on the AskUserQuestion samples we've already seen. Use the real Q&A pairs as templates and vary the values. The geometric tests don't care whether the data is mined or crafted — they test the math, not the extraction.

---

## Spike 1: Does the Decision Space Have Geometric Structure?

**Risk being tested:** Risk 1 — the decision space might not be geometrically well-behaved. PCA eigenvectors might be noise rather than meaningful axes.

**What to build:**
A Python notebook/script that:
1. Loads decision tuples from Spike 0
2. Embeds each `decision_value` with OpenAI `text-embedding-3-small` (1536-dim)
3. Builds a session×dimension matrix:
   - Rows = sessions
   - Columns = decision types
   - Cell value = embedding of the decision value chosen in that session (or a default/zero vector if that decision wasn't made in that session)
4. Runs PCA on the matrix
5. Produces diagnostic plots and metrics

**Key analyses:**

### A) Eigenvalue spectrum shape
```python
# Compute PCA
pca = PCA()
pca.fit(session_matrix)

# Plot eigenvalue spectrum
plt.plot(pca.explained_variance_ratio_)
plt.title("Eigenvalue spectrum — looking for elbow")

# Compute cumulative variance
cumvar = np.cumsum(pca.explained_variance_ratio_)
n_90 = np.searchsorted(cumvar, 0.9) + 1
print(f"Components for 90% variance: {n_90} out of {session_matrix.shape[1]}")
```

**What good looks like:** Clean elbow at 8-15 components capturing 80-90% of variance. This means the decision space has low effective dimensionality — orthogonal questioning is feasible.

**What bad looks like:** Flat spectrum (every component explains ~equal variance). This means decisions are all roughly independent and there's no exploitable structure. Orthogonal questioning degrades to asking one question per dimension.

### B) Eigenvector interpretability
For each of the top 5 eigenvectors, find the decision dimensions with the largest loadings. Do they form coherent, interpretable clusters?

```python
for i in range(5):
    loadings = pca.components_[i]
    top_dims = np.argsort(np.abs(loadings))[-5:]
    print(f"PC{i+1}: {[dimension_names[d] for d in top_dims]}")
```

**What good looks like:** PC1 loads on {storage_backend, migration_strategy, connection_pooling} — a coherent "persistence" axis. PC2 loads on {test_strategy, fixture_approach, coverage_target} — a coherent "testing" axis.

**What bad looks like:** PCs load on random, unrelated dimensions with no interpretable theme.

### C) Cluster structure (optional)
Run UMAP on session vectors, visualize in 2D. Do sessions cluster by project type or task category?

**Success criteria:**
- n_90 < 20 (fewer than 20 components for 90% variance)
- At least 3 of the top 5 PCs have interpretable loadings
- If using real data: visual clusters in UMAP correspond to recognizable project types

---

## Spike 2: Do Embeddings Capture Decision Similarity?

**Risk being tested:** Risk 2 — generic text embeddings might encode semantic similarity rather than decision-relevant similarity.

**What to build:**
A Python script that compares two similarity measures and tests whether they agree.

### A) Decision co-occurrence matrix (behavioral similarity)
From the extracted decisions: for each pair of decision values that appear in the same session, count how often they co-occur.

```python
# Build co-occurrence: how often do two decision values appear in the same session?
cooccurrence = defaultdict(int)
for session in sessions:
    values = session.decision_values
    for v1, v2 in combinations(values, 2):
        cooccurrence[(v1, v2)] += 1
```

This is the ground truth: decisions that actually go together in practice.

### B) Embedding similarity matrix (semantic similarity)
Embed all unique decision values. Compute pairwise cosine similarity.

```python
embeddings = {v: openai.embed(v) for v in unique_values}
for v1, v2 in combinations(unique_values, 2):
    embedding_sim[(v1, v2)] = cosine_similarity(embeddings[v1], embeddings[v2])
```

### C) Correlation test
Compute the Spearman rank correlation between the co-occurrence counts and the embedding similarities across all pairs.

```python
from scipy.stats import spearmanr
rho, p = spearmanr(cooccurrence_scores, embedding_sim_scores)
print(f"Spearman ρ = {rho:.3f}, p = {p:.6f}")
```

**What good looks like:** ρ > 0.4 with p < 0.01. Embedding similarity meaningfully predicts which decisions co-occur. The geometric framework is on solid ground.

**What bad looks like:** ρ < 0.15 or p > 0.05. Embedding similarity doesn't predict co-occurrence. The PCA eigenvectors in embedding space don't correspond to real decision structure. The framework needs decision-specific embeddings (trained on co-occurrence) rather than generic text embeddings.

### D) Qualitative failure case analysis
Pull the top 10 pairs where embedding similarity is HIGH but co-occurrence is LOW (false positives), and vice versa (false negatives). Inspect manually.

- False positives tell you: "these decisions seem related but aren't in practice" — the embedding is misleading
- False negatives tell you: "these decisions always go together but don't seem related" — the embedding is missing a connection

**What good looks like:** False positives are near-synonyms (postgres/mysql — semantically close, but user always picks one). False negatives are domain-specific bundles (parquet + fail-fast — unrelated semantically, always co-occur). Both are small in number and explainable.

**What bad looks like:** Large numbers of unexplainable false positives/negatives. The embedding space geometry is fundamentally wrong for this problem.

### E) Enriched embedding experiment
If C shows weak correlation, test whether enriching the embedding input improves things:
- Instead of embedding bare value ("postgres"), embed value+context ("postgres — chosen as storage backend for ETL pipeline with concurrent writes")
- Re-run correlation test
- If enriched embeddings significantly improve ρ, the path forward is context-aware embeddings, not abandoning the geometric framework

**Success criteria:**
- Spearman ρ > 0.3 with bare embeddings, OR ρ > 0.4 with enriched embeddings
- False positive/negative analysis reveals explainable patterns, not random noise

---

## Spike 3: Are Preferences Stable Enough to Learn?

**Risk being tested:** Risk 3 — user preferences might be too context-dependent or non-stationary for a stable prior to form.

**What to build:**
A Python script that tests temporal stability and context-dependence.

### A) Split-half eigenvector stability
Split sessions chronologically into two halves. Run PCA on each half independently. Compare the top-k eigenvectors.

```python
half1 = session_matrix[:n//2]
half2 = session_matrix[n//2:]

pca1 = PCA(n_components=10).fit(half1)
pca2 = PCA(n_components=10).fit(half2)

# Procrustes alignment: find best rotation between the two PCA spaces
# Then measure alignment of corresponding eigenvectors
from scipy.spatial import procrustes
_, _, disparity = procrustes(pca1.components_[:5].T, pca2.components_[:5].T)
print(f"Procrustes disparity: {disparity:.3f}")

# Also: pairwise cosine similarity of top eigenvectors
for i in range(5):
    sim = cosine_similarity(pca1.components_[i], pca2.components_[i])
    print(f"PC{i+1} alignment: {sim:.3f}")
```

**What good looks like:** Procrustes disparity < 0.3. Top 3 eigenvectors have cosine similarity > 0.7. The structure is stable — what you learn from the first half predicts the second half.

**What bad looks like:** Procrustes disparity > 0.6. Eigenvectors are unaligned. The structure is non-stationary — either preferences are drifting or the variance is dominated by context.

### B) Per-dimension consistency test
For each decision type that appears 5+ times, compute the entropy of the value distribution.

```python
for dim in decision_types:
    values = all_values_for(dim)
    if len(values) >= 5:
        probs = Counter(values).values()
        ent = entropy(probs)
        max_ent = log(len(set(values)))
        normalized_ent = ent / max_ent if max_ent > 0 else 0
        print(f"{dim}: entropy={normalized_ent:.2f}, n={len(values)}")
```

Classify each dimension:
- **Stable** (normalized entropy < 0.3): user almost always picks the same value. Safe to default.
- **Contextual** (normalized entropy 0.3-0.7): user varies by context. Worth learning the decision function.
- **Chaotic** (normalized entropy > 0.7): user is all over the place. Might be genuinely stochastic.

**What good looks like:** 60%+ of dimensions are Stable, 20-30% are Contextual, <10% are Chaotic. Most preferences are learnable.

**What bad looks like:** 40%+ of dimensions are Chaotic. The user doesn't have stable preferences — the GP prior won't converge.

### C) Context-prediction test
For "Contextual" dimensions (entropy 0.3-0.7), test whether the task description predicts the decision value.

```python
# For each contextual dimension:
# 1. Embed the task description for each session
# 2. Train a simple classifier (logistic regression) to predict decision value from task embedding
# 3. Measure accuracy vs random baseline

from sklearn.linear_model import LogisticRegression
from sklearn.model_selection import cross_val_score

for dim in contextual_dimensions:
    X = task_embeddings[sessions_with_dim]
    y = decision_values[sessions_with_dim]
    scores = cross_val_score(LogisticRegression(), X, y, cv=5)
    print(f"{dim}: accuracy={scores.mean():.2f} (baseline={1/len(set(y)):.2f})")
```

**What good looks like:** Contextual dimensions have accuracy 2x+ above random baseline. The task description carries enough signal to predict which way the user will go. The GP can condition on context.

**What bad looks like:** Accuracy near baseline. The relevant context is latent — not observable from the task description. The GP can't condition on what it can't see.

**Success criteria:**
- Procrustes disparity < 0.4 on split-half test
- 50%+ of dimensions are Stable or Contextual-with-predictable-context
- Less than 20% of dimensions are Chaotic

---

## Spike 4: Orthogonal vs Random Question Simulation

**Risk being tested:** The full thesis — does orthogonal question selection actually outperform simpler strategies?

**Depends on:** Results from Spikes 1-3 being at least partially positive.

**What to build:**
A simulation that replays historical sessions with different question strategies and measures how quickly each strategy converges to the user's actual decisions.

### Setup
For each session in the dataset:
1. **Ground truth u** = the session's actual decision vector (all decisions made)
2. **Initial estimate a₀** = the default vector (most common value per dimension across all OTHER sessions — leave-one-out)
3. **Available questions** = one per decision dimension, each with a direction vector in embedding space

### Strategies to test

**Random:** Pick a random unresolved dimension, ask about it.

**Frequency:** Ask about the dimension with the highest historical entropy first (the most "contested" dimension).

**Orthogonal-PCA:** Compute PCA on the covariance of historical decision vectors. Ask along the top eigenvector. After each answer, re-project remaining uncertainty and ask along the next orthogonal direction.

**Oracle (upper bound):** Ask about the dimensions where the user's actual value differs from the default. This requires knowing the answer in advance — it's the theoretical best.

### Simulation loop
```python
for session in held_out_sessions:
    u = session.decision_vector  # ground truth
    for strategy in [random, frequency, orthogonal, oracle]:
        a = default_vector.copy()
        questions_asked = 0
        residuals = [np.linalg.norm(u - a)]

        while np.linalg.norm(u - a) > threshold:
            dim = strategy.next_question(a, asked_so_far)
            a[dim] = u[dim]  # simulate: user gives ground-truth answer
            questions_asked += 1
            residuals.append(np.linalg.norm(u - a))

        results[strategy].append(questions_asked)
```

### Metrics per strategy
- **Avg questions to convergence** (||u - a|| < threshold)
- **Residual curve** (||u - a|| vs questions asked, averaged across sessions)
- **AUC of residual curve** (lower = faster convergence)

### Expected results table
```
Strategy       | Avg Questions | Residual AUC
---------------|---------------|-------------
Random         | ~12           | high
Frequency      | ~8            | medium
Orthogonal-PCA | ~4-5          | low
Oracle         | ~3            | lowest
```

**What good looks like:** Orthogonal-PCA is significantly closer to Oracle than to Random. The geometric structure provides real leverage — orthogonal selection isn't just theoretically nice, it measurably reduces question count.

**What bad looks like:** Orthogonal-PCA performs comparably to Frequency, or worse. This means the PCA directions don't correspond to real decision axes, and simpler heuristics work just as well. The geometric framework adds complexity without benefit.

**Success criteria:**
- Orthogonal-PCA requires 30%+ fewer questions than Frequency
- Orthogonal-PCA's residual curve drops significantly faster in the first 3 questions
- The gap between Orthogonal-PCA and Oracle is smaller than the gap between Frequency and Oracle

---

## Execution Order

```
Spike 0 (extraction)  ←  foundation, do first
    │
    ├── Spike 1 (geometry)      ←  parallel
    ├── Spike 2 (embeddings)    ←  parallel
    └── Spike 3 (stability)     ←  parallel
            │
            └── Spike 4 (simulation)  ←  requires 1-3 results, run last
```

Spikes 1-3 are independent and can run in parallel once Spike 0 produces the dataset.

## Go/No-Go Decision Matrix

| Spike 1 (geometry) | Spike 2 (embeddings) | Spike 3 (stability) | Verdict |
|---|---|---|---|
| Pass | Pass | Pass | **Full go.** Build the GP framework. |
| Pass | Fail | Pass | **Pivot embeddings.** Train decision-specific embeddings from co-occurrence data instead of using generic text embeddings. Math still works. |
| Fail | Pass | Pass | **Pivot model.** Decision space is high-dimensional / unstructured. Use the embeddings for similarity-based retrieval (nearest-session lookup) instead of PCA-based orthogonal questioning. |
| Pass | Pass | Fail | **Pivot scope.** Preferences aren't stable. Focus on per-project or per-context models instead of a universal prior. Or focus on the stable dimensions only. |
| Fail | Fail | Pass | **Rethink representation.** Both geometry and embeddings fail — the continuous vector space framing is wrong. Consider discrete models (decision trees, rule-based). |
| Fail | Fail | Fail | **No-go.** The mathematical framework doesn't match the data. Fall back to frequency-based defaults (the simpler approach from better-questions.md). Still valuable, just not the geometric 10x version. |

## Estimated Effort

| Spike | Effort | Blocking? |
|-------|--------|-----------|
| 0 (extraction) | 2-3 hours | Yes — everything depends on it |
| 1 (geometry) | 1-2 hours | No |
| 2 (embeddings) | 1-2 hours | No |
| 3 (stability) | 2-3 hours | No |
| 4 (simulation) | 3-4 hours | No |
| **Total** | **~10-14 hours** | |

Spike 0 can be shortcut with synthetic data (1 hour instead of 3) if extraction proves fiddly. The geometric tests are valid regardless of whether the input decisions are mined or hand-crafted.

---

## Findings (run 2026-05-26)

All four spikes executed against real session data. Artifacts in `.tmp/experiments/orthogonal-questioning/` (decisions.jsonl, scripts, results JSONs, plots).

### Headline verdicts

| Spike | Threshold | Result | Verdict |
|---|---|---|---|
| 0: Extraction | 300+ decisions, 50+ sessions, 15+ types | 725 / 68 / 155 | **PASS** |
| 1: Geometry | n_90 < 20, ≥3 of top 5 PCs interpretable | n_90 = 40, 5/5 PCs interpretable | **PARTIAL** |
| 2: Embeddings | ρ > 0.3 bare OR > 0.4 enriched | ρ_bare = 0.353, ρ_enriched = 0.398 | **PASS** (w/ caveats) |
| 3: Stability | Procrustes < 0.4, <20% chaotic | Procrustes = 0.994, 96% chaotic | **FAIL** |
| 4: Simulation | Ortho-PCA 30%+ fewer Q than Frequency | 27.0% savings (just under bar) | **PARTIAL** |

Mapping to the go/no-go matrix: closest row is `Pass / Pass / Fail → Pivot scope`. With Spike 1 also partially failing, the honest read is **no-go for the geometric framework as originally proposed**, with two specific salvage paths described below.

### The dominant finding (not in the original hypothesis)

**Most decision dimensions are silent in any given session.** Spike 4's pre-flight measured the actual u-vs-default deviation per session: mean **6.2 of 30 dimensions** carry a non-default value. The other ~24 dimensions are noise — the session never made that decision, the centroid default stands. This caps the maximum possible improvement: Oracle averages 6.1 questions because it never asks about the ~24 silent dimensions, while every other strategy burns questions on them.

The corollary: the original framing ("collapse a 50-dim space with 3 orthogonal questions") assumes most dimensions matter and need probing. The data shows most dimensions don't matter per task. The lever isn't *orthogonality between questions* — it's *correctly identifying which dimensions are even alive in this session*. That's a different problem (sparse support recovery), and embeddings don't directly solve it.

Quantitative comparison from Spike 4 (n=47 sessions, threshold = median(initial residual)/10):

| Strategy | Avg Q to converge | AUC | Resid@5 | Converged within 10 Q |
|---|---|---|---|---|
| Random | 25.9 | 35.7 | 1.72 | 0% |
| Frequency | 24.6 | 34.3 | 1.69 | 0% |
| Orthogonal-PCA | 17.9 | 22.6 | 1.49 | 9% |
| Oracle | 6.1 | 7.2 | 0.51 | 91% |

Orthogonal-PCA does provide a real ~27% lift over Frequency — better than nothing. But the gap to Oracle (11.8 questions) is much larger than the gap from Random (8.0 questions). The structural information PCA exploits is small relative to what an oracle who knows which dimensions are alive can do.

### Per-spike detail

**Spike 0** Extracted 725 structured decisions across 68 sessions using gpt-4.1-mini. 155 distinct `decision_type` categories emerged — long-tailed, top 15 cover ~50%. Spot-check on random samples: extracted decisions are real, but values are highly free-form ("split validation into shared validate() function" vs "use shared validate() approach" vs "shared validate() function") — same decision rephrased differently each time. This becomes the central methodology issue for all downstream spikes.

**Spike 1 (Geometry)** Built a 58×46080 session-by-(top-30-types × 1536-dim-embedding) matrix. PCA spectrum is **flat**: n_80=31, n_90=40, n_95=46. No elbow. By the spike doc's own framing, "every component explains ~equal variance" is the "what bad looks like" case. The top-5 PCs each concentrate L2-energy on a single decision type (PC1=file_layout, PC4=api_style, etc.) — they look interpretable, but on inspection the axis meaning is "is this a code project vs a docs project?" That's project identity, not a shared decision axis that would help predict the next question.

**Spike 2 (Embeddings)** Spearman ρ_bare = 0.353 (p≈0) between cosine-similarity-of-embeddings and session-co-occurrence-counts. ρ enriched with type+context = 0.398 — a modest improvement, validating that context helps. But: when filtered to *only pairs with positive co-occurrence*, ρ drops to 0.063 (p=0.21). The correlation is doing one thing well (separating "these never go together" from "these sometimes go together") and one thing badly (ordering within positive pairs). Top-10 false positives are dominated by extractor near-duplicates at sim ≈ 0.9 — strong evidence that without a dedup pre-pass, embeddings will treat paraphrases as if they were distinct decisions.

**Spike 3 (Stability)** Chronological split (timestamps from `autosearch session describe`). After dimension reduction (1536→32 via PCA over value embeddings), Procrustes disparity between the top-5 PCs of half-1 and half-2 = **0.994** — essentially orthogonal. Matched-PC cosine similarities: 0.03–0.13. Per-dimension entropy: 1/25 stable, 0/25 contextual, 24/25 chaotic — even at the most permissive canonicalization threshold, ≥84% remain chaotic. One bright spot: in the context-prediction sub-test, `test_strategy` was predictable from task embeddings at 74% vs 20% baseline (3.7×). But only 1 dimension had enough data after singleton-drop to test. **The dominant signal is non-stationarity caused by extractor verbalization noise**, not a fundamental finding about user preferences.

**Spike 4 (Simulation)** Static orthogonal-PCA ranking (not full dynamic re-projection — justified because Spike 3 showed the basis is unstable). Ortho-PCA beats Frequency by 27% and Random by 31%. Criterion B (faster drop in first 3 questions) fails — Ortho-PCA's lift shows up *later*, not earlier. Criterion C (Ortho-Oracle gap < Freq-Oracle gap) passes. The signal Ortho-PCA exploits is weak but real: PCA finds dimensions that are simultaneously high-entropy *and* high-variance across sessions, which is more discriminating than entropy alone at this sample size.

### What's actually salvageable

Three concrete next steps, in priority order:

1. **Fix the extractor before re-running anything geometric.** Spike 2's false-positive analysis and Spike 3's chaotic entropy both point at the same root cause: the LLM extractor produces a free-form paraphrase per session, and string-level (or embedding-based) canonicalization at threshold 0.55 collapses some but not all duplicates. A second-pass clustering of extracted values *per decision_type* at sim ≥ 0.85, followed by a stable canonical label per cluster, would likely halve the chaotic-dimension count. Re-run Spikes 1 and 3 after this — the results may shift materially.

2. **Context-conditioned models over universal models.** Spike 3's lone positive signal — `test_strategy` predictable from task embedding at 3.7× baseline — is the seed of a viable approach. Rather than a universal prior over all decisions, train per-task-cluster priors: "for sessions about doc tooling, the modal `file_layout` is `docs/tasks/$id/`; for code projects, it's `src/<package>/`." This is the "pivot scope" verdict from the decision matrix, and it's the most defensible path given the data.

3. **Sparse-support recovery, not orthogonal questioning.** The 6.2/30 finding from Spike 4 says the real problem is identifying *which* dimensions are alive in a session, not *in which order* to ask them. A classifier `f(task_description) → set of alive dimensions` operating *before* any question-asking would give Oracle-level convergence. This reframes the whole problem: don't ask 3 orthogonal questions, predict the 6 questions worth asking and ask them.

### What's *not* salvageable

The original headline claim — "3-5 questions instead of 50, via PCA-aligned orthogonal probing" — does not survive contact with this data. The geometric structure required (low effective dimensionality, stable across time, semantic-cosine-similarity ≈ behavioral co-occurrence within positives) is not present at the current data scale and extraction quality.

### Cost

OpenAI spend across all four spikes: well under $1 of the $30 budget. Most of the spend was embeddings (text-embedding-3-small) for ~700 unique values and gpt-4.1-mini for 321 extraction calls. Re-running with a better extractor + dedup pass is cheap.
