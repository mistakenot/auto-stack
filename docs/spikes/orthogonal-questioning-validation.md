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
