---
hash: "f1eae3f9"
id: "26cfe816"
read_when: "designing question-selection or requirement-collapse systems, or reviewing orthogonal questioning experiment results"
summary: "Four-phase experiment testing whether a coding agent can compress question-asking from ~15 to ~3 by treating user requirements as a vector space; cosine geometry failed but per-dimension classifiers with active learning achieved sign recovery in ~4 questions."
title: "Orthogonal Questioning — Experiment Synthesis"
---

# Orthogonal Questioning — Experiment Synthesis

*Started 2026-05-26. Concluded 2026-05-27. Total OpenAI spend: under $2 of a $30 budget.*

## The question we were asking

Can a coding agent compress the upfront question-asking phase from ~15 questions to ~3, by treating user requirements as a vector in a high-dimensional space and asking questions along orthogonal directions to collapse uncertainty fast?

This is the orthogonal-questioning thesis from `docs/claude-decision-intelligence-deep-dive.md` (Section 6). It promised a 10× compression in question budget by exploiting the geometric structure of decision embeddings. The four-phase experiment program tested whether that geometric structure actually exists in real data, and — when it didn't — whether the framework could be salvaged.

## The four phases

| Phase | What it tested | Verdict |
|---|---|---|
| [Phase 1](phase1-validation.md) | Does the geometric framework work on real mined decision data? PCA, cosine similarity, simulation. | **Mostly no.** Spectrum flat (n_90=40), temporal stability poor (Procrustes=0.99), simulation savings only 27%. |
| [Phase 2](phase2-context.md) | Does richer input context (turn-windows, structured composites, raw Q&A artifacts) unlock the structure? | **Partial yes.** Spike 8 (residualize project-id) and Spike 9 (skip extraction, embed raw Q&A) produced real structural improvements. n_90 dropped from 40 to 23. But pair-distance correlation still wobbly. |
| [Phase 3](phase3-synthetic.md) | On *synthetic data with known ground truth*, can any input format make off-the-shelf embeddings + cosine geometry work? | **No.** Best format hits R²=0.28 via linear probe; cosine similarity ρ≈0 across all formats. The information is in the embedding but not in the geometry. |
| [Phase 4](phase4-alternatives.md) | Can alternative methods (per-dim classifiers, bilinear models, Mahalanobis) extract what cosine missed? Does the relaxed thesis (sign recovery, linguistically-legible dimensions only) hold? | **Yes.** Per-dim binary classifiers + active learning reach perfect sign recovery in 4.17 questions on 8 dimensions. The relaxed thesis is deployable. |

## The arc, as a single story

**Phase 1** said the framework was wrong. The geometric structure the thesis required was nowhere visible in real session data.

**Phase 2** said the representation was wrong, not the framework. Two specific representation changes — explicitly factoring out project identity, or skipping the LLM extraction step and embedding raw Q&A artifacts directly — produced clear structural improvements that Phase 1's bare-value embeddings missed.

**Phase 3** killed the lingering ambiguity. With synthetic data where we knew the ground truth, no input format made cosine geometry recover the latent structure. The framework as proposed asked the embedding model to do something it cannot do.

**Phase 4** rescued the thesis in a modified form. The information *is* present in the embedding (~30% of variance recoverable by a linear probe). It just isn't present in the *geometry* of the embedding. Switch from cosine similarity to per-dimension classifiers + active learning, and the original "3-5 question" claim is back on the table — for sign recovery on linguistically-legible dimensions.

## The conceptual lesson

**The 0.28 R² ceiling is a property of the embedding, not the analyzer.** Alternative methods don't break the information ceiling — they redistribute the fixed signal into more usable forms:

- **Binarizing to signs** lets us extract usable predictions from a noisy continuous signal. R²=0.28 looks like garbage; the same noisy prediction binarized to signs is right ~76% of the time per dimension.
- **Per-dimension modeling** separates the legible from the invisible. A joint probe averages over both and hides the structure. Per-dim classifiers expose it directly.
- **Active learning** closes the residual cheaply because most legible dimensions are already correct from a single decision; AL only needs to ask about the invisible ones.

**Cosine similarity is unsupervised. A learned probe is supervised.** Cosine similarity assumes the embedding space is isotropic with respect to your task — every direction contributes equally. That's almost never true for a generic text encoder. A learned probe says "of the 1536 dimensions, I care about these specific ones for this specific task." This is the classic geometric-vs-discriminative distinction in ML, and the orthogonal-questioning thesis as originally framed implicitly bet on geometric.

## What's deployable

A real, deployable architecture exists. See [deployable-architecture.md](deployable-architecture.md) for the full bootstrap process and usage walkthrough.

The headline shape:

```
1. Define 6-10 latent preference dimensions
2. Train one binary sign classifier per dimension (LogisticRegression on text-embedding-3-small)
3. For each new task:
   a. Predict signs on all dimensions from the user's history
   b. Identify the most uncertain dimensions
   c. Ask the user about those (bundled in one AskUserQuestion)
   d. Generate the requirements.md
4. Continuously refresh classifiers from accumulated session data
```

Expected gain over the current `/new-task` flow: ~3 questions per task instead of ~15. Not the original 10× thesis, but real.

## What's *not* deployable (and never will be)

- **Cosine-distance-based nearest-neighbour session retrieval** for cold-start prior estimation. Pair distance Spearman ρ across all four phases stayed near zero. Don't use cosine distance as a "similar sessions" signal.
- **PCA-basis acquisition functions.** Phase 1+2's results showed the PCA basis doesn't predict useful question ordering.
- **Continuous-value preference recovery.** The R²=0.28 ceiling on off-the-shelf embeddings is too low for continuous recovery. Stick to sign recovery, which is robust to that ceiling.
- **The 10× compression promise.** Realistic gain is ~3-5×, not 10×.

## Related artifacts

- [embedding-model-research.md](embedding-model-research.md) — notes from researching OpenAI's text-embedding-3 family and what their docs do/don't say. Background to the model-selection decisions.
- [deployable-architecture.md](deployable-architecture.md) — practical bootstrap + usage + comparison to a 20-template baseline.
- Phase docs (`phase1-validation.md` through `phase4-alternatives.md`) — full hypothesis, design, and findings per phase.

## Code and data

All experimental code, datasets, embeddings, and plots live at:
`/home/vscode/src/auto-stack/.tmp/experiments/orthogonal-questioning/` (Phase 1+2)
`/home/vscode/src/auto-stack/.tmp/experiments/orthogonal-questioning/phase3/` (Phase 3+4)

Not checked into git. Reproducible from the scripts in those directories.

## If you were going to continue

The Phase 4 doc lists four next experiments worth running. Top priority: validate per-dim classifiers on real labeled data instead of synthetic, to confirm the 4.17-questions number holds outside the controlled setting.
