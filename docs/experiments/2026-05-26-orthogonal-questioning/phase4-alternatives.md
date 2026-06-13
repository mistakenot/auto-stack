---
hash: "ce0df480"
id: "6e30ecc9"
read_when: "designing preference inference systems or evaluating alternatives to cosine similarity for embedding-based retrieval"
summary: "Phase 4 spike testing per-dimension binary classifiers and active learning as alternatives to cosine similarity for preference inference from embeddings, achieving 4.17 questions to full sign recovery."
title: "Spike: Alternatives to Cosine Similarity (Phase 4)"
---

# Spike: Alternatives to Cosine Similarity (Phase 4)

Phase 3 produced a sharp negative result: with synthetic data having known 8-dim latent structure, linear-probe R² = 0.28 (signal exists in the embedding) but cosine similarity ρ ≈ 0 (signal NOT in cosine geometry). The original orthogonal-questioning framework, which assumed cosine distance tracked decision distance, failed.

But the linear-probe number (R² = 0.28) was already proof-by-example that *some* method can extract signal where cosine cannot. Phase 4 tests whether smarter methods break through the R² = 0.28 ceiling, and whether the relaxed version of the thesis — "recover preference *signs* on the *linguistically legible* dimensions" — is viable.

## The four methods tested

| Method | What it does | Inspired by |
|---|---|---|
| M_joint | Ridge regression: embedding → 8-dim continuous user vector | Phase 3 M1 baseline |
| M_perdim | 8 separate binary classifiers, one per latent dimension | Phase 3's "some dims recoverable, some not" finding |
| M_bilinear | Learned `emb^T W user_vec` scoring; train via target dot-product | Decision + user in different spaces |
| M_mahalanobis | Pairwise distance using inverse embedding covariance | Cosine baseline with variance weighting |

Plus an **active-learning simulation**: use per-dim classifier uncertainty to pick the next question. The "relaxed thesis": how few questions to perfect sign recovery on the legible dimensions?

## Inputs

- Phase 3 S8 (user voice raw) embeddings — Phase 3's winning format
- 900 decisions, 30 users, known ground truth
- Train/test by user (24/6, 5 random splits, averaged)
- No new LLM calls — purely scikit-learn / PyTorch on cached data

## Findings (run 2026-05-27, ~20 min wall time, $0 spend)

### Headline

| Metric | Phase 3 baseline | Phase 4 result |
|---|---|---|
| Joint continuous R² | 0.276 | 0.238 (M_joint, within noise) |
| Joint sign accuracy | not measured | **0.763** |
| Active-learning questions to full sign recovery | never converged | **4.17** |
| Bilinear MRR (top-1 over 30 users) | not tested | 0.586 (0.367 top-1, ~11× random) |
| Best pair-distance ρ | 0.028 (cosine) | 0.074 (Mahalanobis) — still weak |

**Per-dim accuracy leaderboard (S8 embeddings, 5-split mean):**

| Rank | Dimension | Accuracy | AUC | Legible (>0.7)? |
|---|---|---|---|---|
| 1 | D2 output_verbosity | 0.814 | 0.882 | yes |
| 2 | D4 error_handling_stance | 0.775 | 0.950 | yes |
| 3 | D3 persistence_durability | 0.768 | 0.950 | yes |
| 4 | D7 dependency_appetite | 0.766 | 0.897 | yes |
| 5 | D1 validation_strictness | 0.744 | 0.910 | yes |
| 6 | D8 concurrency_taste | 0.725 | 0.861 | yes |
| 7 | D6 api_explicitness | 0.574 | 0.668 | no |
| 8 | D5 schema_rigidity | 0.532 | 0.797 | no |

**6 of 8 dimensions are linguistically legible.** The two that aren't (D5 schema rigidity, D6 API explicitness) are exactly the dimensions Phase 3's per-dim R² flagged as weak — concept consistency across two phases.

### The conceptual finding

**The R² = 0.28 ceiling is a property of the embedding, not the analyzer.** M_joint reproduces Phase 3 (0.238 vs 0.276 — within split noise). Alternative methods don't break the information ceiling. They *redistribute* the fixed signal into more usable forms:

1. **Binarizing to signs** lets us extract usable predictions from a noisy continuous signal. A noisy 8-dim continuous prediction at R² = 0.28 looks like garbage; the same noisy prediction binarized to signs is right ~76% of the time per dimension.

2. **Per-dimension modeling** separates the legible from the invisible. A joint probe averages over both and hides the structure. Per-dim classifiers expose it directly — and tell you which dimensions are worth asking about.

3. **Active learning** then closes the residual cheaply. Most legible dimensions are already correct from a single decision's embedding; AL only needs to ask about the invisible ones (D5, D6) plus occasional repair of classifier errors.

### The thesis survives — in relaxed form

Original orthogonal-questioning thesis: "3-5 questions to identify a user's preference vector in an 8-dim space."

Phase 3 verdict on the original: impossible with off-the-shelf embeddings + cosine geometry. Residual barely moved in 8 questions.

Phase 4 verdict on the **relaxed** thesis: viable. **4.17 questions on average to perfect sign recovery** across all 8 dimensions, using per-dim classifiers + active learning. That's right in the 3-5 range the original thesis predicted, just for sign recovery rather than continuous-value recovery.

The architecture that works:

```
1. Per-dimension binary classifiers (one per latent axis)
2. Predict signs from a single decision embedding (~76% per-dim accuracy)
3. Active learning: ask about whichever dimension the classifier is most uncertain about
4. Stop when all sign predictions are confirmed
```

This is a real, deployable design — not the same as the original GP-with-cosine-kernel thesis, but a member of the same family.

### What's still ceiling-limited

Two dimensions (D5 schema rigidity, D6 API explicitness) sit below the 0.7 legibility threshold. The active-learning protocol asks about these directly — there's no way to infer them from text-rendered decisions because the renderer LLM doesn't have distinct vocabulary for them. This is the linguistic-legibility ceiling: a property of the language and the encoder, not the analyzer.

In a real deployment, two consequences follow:
- You'd ask the user explicitly about D5/D6 (the "non-inferable" dimensions) on every new session. These become your guaranteed minimum question budget.
- You can drop D5/D6 from the framework if they don't matter for your application, recovering the lower question count.

### Mahalanobis vs cosine

Mahalanobis distance roughly doubled cosine's pair-distance Spearman ρ (0.074 vs 0.042). Still very weak — neither geometric distance method is competitive with the supervised approaches. **The signal isn't in pair distances**, period; it's in learned projections onto specific axes. Any architecture that depends on "embedding distance = preference distance" — retrieval-augmented systems, GP kernels over raw embeddings, k-NN session lookup — is making an assumption the data does not support.

## What we now know (across all four phases)

1. The naive geometric framework — embed values, use cosine + PCA — doesn't work, regardless of input format. (Phases 1-3)
2. Information *is* present in embeddings of preference-laden text, at roughly 30% recoverability for continuous values. (Phase 3)
3. That same information becomes ~76% recoverable as binary signs per dimension, via per-dim classifiers. (Phase 4)
4. Active learning over per-dim uncertainty converges in 4-5 questions on linguistically-legible dimensions. (Phase 4)
5. Linguistic legibility varies by dimension — some preferences (persistence, error-handling stance, dependencies) recover cleanly; others (schema rigidity, API explicitness) don't, and no analyzer fixes that. (Phases 3-4)

The original "3-5 questions" thesis was right about the cardinality of the question budget. It was wrong about the mechanism (cosine geometry vs learned per-dim classifiers) and the recovery target (continuous vector vs sign vector with explicit asks for invisible dims).

## Deliverables

- `phase3/phase4_alternatives.py` — main pipeline
- `phase3/phase4_results.json` — full metrics
- `phase3/phase4_perdim_accuracy.png` — per-dim bar chart with legibility threshold
- `phase3/phase4_AL_residual_curves.png` — active learning vs random vs canonical-ordered comparison
- `phase3/phase4_notes.md` — worker synthesis

## Cumulative cost

Phase 1 + 2 + 3 + 4 total OpenAI spend: under $2 of the $30 budget.

## Suggested next experiments (if continuing)

1. **Validate on real data.** Train per-dim classifiers on the Phase 1 mined decisions (with explicit human labels for the latent dimensions, or with the LLM extractor used differently). See if the synthetic-data per-dim accuracy holds up.
2. **Test the AL protocol with realistic uncertainty.** The Phase 4 simulation assumes the user answers truthfully and without ambiguity. A more realistic test injects answer noise and checks robustness.
3. **Reduce the dimension set.** D5/D6 don't recover; maybe they aren't real latent dimensions. Try a 6-dim model and see whether AL converges in 3 questions.
4. **Try a fine-tuned encoder.** The R² = 0.28 ceiling is the off-the-shelf encoder's limit. Fine-tuning a small encoder on synthetic decisions with known ground truth might push it higher and reduce the question budget further.
