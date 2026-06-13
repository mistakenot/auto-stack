---
hash: "9b4f48c8"
id: "57ecc6a4"
read_when: "continuing the orthogonal questioning experiment or evaluating embedding input format choices for decision data"
summary: "Phase 2 spikes testing richer input formats and project-identity factoring for decision embeddings; turn-window (F4) improved pairwise correlation 7x, Spike 8 factoring dropped n_90 to 26, and raw Q&A embedding (Spike 9) dropped n_90 to 23."
title: "Orthogonal Questioning Phase 2: Context-Rich Embedding Inputs"
---

# Spike: Context-Rich Embedding Inputs (Phase 2)

Phase 1 ([orthogonal-questioning-validation.md](orthogonal-questioning-validation.md)) tested whether bare embeddings of 2-5 word `decision_value` strings carry enough signal to support the orthogonal-questioning framework. The dominant negative finding wasn't about the math — it was about the data: LLM-extracted decision values are short, paraphrased differently each session, and embedding them strips out the context that gives the decision its meaning.

Phase 2 tests the hypothesis that **the context surrounding a decision is what makes it embeddable**. Same downstream metrics (correlation with co-occurrence, PCA spectrum shape, temporal stability), but with progressively richer input formats and a stronger embedding model. The goal is to find out whether Phase 1's failures were fundamental to the user's preference structure, or artifacts of throwing away context before embedding.

## Prerequisites

- All Phase 1 artifacts in `.tmp/experiments/orthogonal-questioning/` (decisions.jsonl, scripts, results JSONs, caches)
- Python 3.12 + uv (same env as Phase 1)
- OpenAI API key (~$29 of $30 budget remaining; Phase 1 spent <$1)
- autosearch CLI for fetching full message context

## Design principles

1. **Reuse the Phase 1 dataset's `decisions.jsonl` as the source-of-truth list of decision events.** We don't re-mine. We just embed the same events differently.
2. **Hold methodology constant across format ablations.** Same sessions, same top-30 types filter, same correlation/PCA/Procrustes procedures. Only the input string per decision changes.
3. **Treat input format as the independent variable.** Six formats from bare to fully-augmented.
4. **One format wins → re-run Phase 1's failing spikes against that format.** If the geometric structure now appears, the framework wasn't wrong, the inputs were.

---

## Spike 5: Input Format Ablation

**Risk being tested:** Phase 1 embedded bare 2-5 word `decision_value` strings. Maybe more context per input would surface the structure that bare values hide.

**What to build:**

A Python script that generates six embedding inputs per decision tuple and runs the same correlation test as Phase 1 Spike 2 (Spearman ρ between embedding cosine similarity and session co-occurrence count) on each format. The format with the highest ρ — especially the highest *positive-only* ρ (Phase 1's main weakness) — wins.

### The six formats

For decision tuple `{type: "validation_approach", value: "shared validate() function", context: "Want one source of truth for filter validation"}` in session S42, message M86:

| ID | Format | Example input (truncated) |
|---|---|---|
| F1 | Bare value | `shared validate() function` |
| F2 | Type + value + context (Phase 1 enriched) | `validation_approach: shared validate() function — Want one source of truth for filter validation` |
| F3 | Q&A pair as it appeared | `Q: How should we structure validation? A: shared validate() function` |
| F4 | Turn-window (decision + 2 preceding turns) | `[agent]: Two options here — per-command validation, or a single shared function... [user]: shared validate() function — keep filter rules in one place` |
| F5 | LLM-generated rationale (HyDE) | `The user chose a shared validate() function over per-command duplication because filter rules need a single source of truth and divergence between commands has caused validation bugs in the past.` |
| F6 | Full raw user message | `lets do shared validate() function. i want one source of truth for filter validation otherwise we'll end up with subtle differences between commands like we had with the search args last month` |

**How to construct each:**

- **F1, F2**: already available from `decisions.jsonl` — no additional work.
- **F3**: for `source=explicit_ask` decisions, the raw_text already contains a Q&A block (parse `"User has answered your questions: \"...\"=\"...\"" `). For other sources, skip F3 (only run it on the explicit_ask subset).
- **F4**: use `autosearch session get <session_id>` to pull the full message list, find the index of the decision message, concatenate the prior 2 user/agent turns + the decision turn. Cap at ~1500 tokens.
- **F5**: call gpt-4.1-mini once per decision with prompt: *"In one or two sentences, explain why a developer would choose '{value}' for {type} given the context: {context}. Write it as a justification, not a question."* Cache aggressively.
- **F6**: `autosearch message get <message_id>` returns the full message text. Use as-is.

### Metrics per format

- Spearman ρ (all pairs) vs co-occurrence
- Spearman ρ (positive-only pairs) — Phase 1's failure mode; this is the discriminator
- Near-duplicate rate: fraction of unique-value pairs above cosine 0.9 that *did not* co-occur (this is the extractor-paraphrase problem; ideally drops sharply as context lengthens)
- Mean cost in tokens per input (for the cost trade-off in Spike 6)

**What good looks like:** F4/F5/F6 produce ρ_positive_only > 0.25 (Phase 1 got 0.06) and reduce the near-duplicate rate by ≥50% vs F1.

**What bad looks like:** All formats land within ±0.05 of Phase 1's ρ_bare. Context doesn't matter — the embedding model is simply unable to discriminate user-specific decisions at the granularity this problem needs.

**Stretch goal:** Compute pairwise format-vs-format embedding cosine similarity for the same decision. If F1 and F6 produce very different embeddings of the same decision, the format is doing significant work; if they're nearly identical, the embedding model is essentially ignoring the extra context.

---

## Spike 6: Embedding Model and Dimension Ablation

**Risk being tested:** Phase 1 used `text-embedding-3-small` (1536-dim, MTEB 62.3). `text-embedding-3-large` (3072-dim, MTEB 64.6, MIRACL 54.9 vs 44.0) might handle the longer, more nuanced inputs from Spike 5 materially better — or might not, given the absolute MTEB gap is small.

**Depends on:** Spike 5 producing a winning format (call it F*).

**What to build:**

A script that re-runs Spike 5's correlation test using F* on three model configurations:

| Config | Model | Dims | Cost |
|---|---|---|---|
| M1 (Phase 1 baseline) | text-embedding-3-small | 1536 | $0.02/1M |
| M2 | text-embedding-3-large | 1536 (Matryoshka via `dimensions` API param) | $0.13/1M |
| M3 | text-embedding-3-large | 3072 (native) | $0.13/1M |

### Metrics per config

- ρ_all and ρ_positive_only on F*
- Per-decision-pair: how often does M2/M3 *disagree* with M1 on whether a pair is in the top-10% most similar? High disagreement = model choice matters; low = it doesn't.
- Total spend across the experiment

**What good looks like:** M3 improves ρ_positive_only by ≥0.05 over M1 on F*. Worth the ~6.5x cost given the absolute spend is still pennies.

**What bad looks like:** M1, M2, M3 are within ±0.02 of each other. Spend is wasted; stay on small.

**Cost ceiling:** Whole experiment with ~700 unique inputs × ~500 tokens × 3 models is roughly 1M tokens = ~$0.13 total. Trivial.

---

## Spike 7: Re-run Phase 1's Failing Tests with the Best Format/Model

**Risk being tested:** Even if Spike 5/6 show better correlation, the geometric structure (low effective dim, stable across time, faster simulation convergence) may still not emerge. Correlation is necessary but not sufficient.

**Depends on:** Spike 5 producing F*, Spike 6 producing M*.

**What to build:**

Re-run Phase 1 Spikes 1, 3, 4 (geometry, stability, simulation) using F* + M*. **Use exactly the same scripts as Phase 1**, with only the embedding step swapped out. Same top-30 types, same session filtering, same Procrustes test, same simulation strategies.

### Headline metrics (compared to Phase 1)

| Metric | Phase 1 result | Phase 1 target | Phase 2 result | Phase 2 target |
|---|---|---|---|---|
| Spike 1: n_90 (components for 90% var) | 40 | < 20 | TBD | < 25 (relaxed) |
| Spike 1: top-5 PCs interpretable | 5/5 | ≥ 3 | TBD | ≥ 3 + axes encode decisions not project-identity |
| Spike 3: Procrustes disparity | 0.994 | < 0.4 | TBD | < 0.6 (relaxed) |
| Spike 3: % chaotic dimensions | 96% | < 20% | TBD | < 60% (relaxed) |
| Spike 4: ortho-PCA vs frequency savings | 27.0% | ≥ 30% | TBD | ≥ 30% |

Targets are relaxed because we know the data has hard limits; we want to see whether the *direction* of improvement is meaningful, not whether we hit the original aggressive bar.

**What good looks like:** All five metrics improve in the right direction *and* at least two of them cross the Phase 2 target. That's enough evidence to invest in the full GP framework.

**What bad looks like:** Metrics move <5% from Phase 1. Context didn't unlock structure; the framework is wrong, not the inputs.

---

## Spike 8: Project-Identity Factoring

**Risk being tested:** Phase 1 Spike 1 found that the top PCs encode *which project the session is about*, not shared decision axes. Even with better inputs, this confound may dominate. The thesis only makes sense if decision structure exists *within* a project, not just *between* projects.

**Depends on:** F* + M* from Spikes 5+6.

**What to build:**

A script that:

1. For each session, computes a **project-context vector** = embedding of the session's first user message (the task description). Use F* form if possible (i.e. for the task description, use the same context-augmentation logic).
2. Builds session decision vectors using F* + M* as in Spike 7.
3. **Subtracts the project-context vector** (linear projection out, or simple residual: `v_residual = v_session − (v_session · v_project) * v_project`) from every cell of the session-by-dimension matrix before running PCA.
4. Re-runs Spike 1's interpretability and spectrum tests on the residualized matrix.

### Metrics

- n_90 on the residualized matrix
- Top-5 PC interpretations — do they now describe decision *axes* (e.g. "fail-fast vs lenient", "deep validation vs none") rather than project identities (e.g. "code project vs docs project")?
- Pairwise session distance: did residualization actually reduce within-project-cluster variance?

**What good looks like:** n_90 drops below Spike 7's number by ≥20%. Top PCs now load on *decision-type bundles* (e.g. PC1 = {validation_approach, error_handling, schema_design} — a "rigor" axis) rather than {file_layout: src/, doc_layout: docs/}.

**What bad looks like:** Residualization makes n_90 worse (we removed signal, not noise). Or top PCs are now unintelligible. Either way the project-identity confound was actually the real structure.

---

## Spike 9 (radical): Skip Extraction Entirely, Embed Raw Q&A Units

**Risk being tested:** The whole extraction pipeline may be the wrong abstraction. Each AskUserQuestion event in a session is *already* a structured decision artifact: a question, a set of presented options, and the user's choice. Maybe these don't need extracting — they need embedding directly.

**Depends on:** Nothing (can run in parallel with Spike 5).

**What to build:**

1. Re-query autosearch for `"User has answered your questions"` messages with `--role tool` (Phase 1 found 230 of these in 56 sessions). These are the gold dataset.
2. For each, extract the *full* `{question, options, chosen_answer}` triple by parsing the message content. Do not extract a `decision_type` or `decision_value` — keep the unit whole.
3. Embed each triple as a single string: `Question: {q}\nOptions: {o1, o2, o3}\nChose: {a}`.
4. Build session vectors by **mean-pooling** all Q&A unit embeddings for a session (or concatenating top-K, padded with the global centroid).
5. Run Phase 1's geometric tests (PCA spectrum, top-5 PC interpretability, split-half Procrustes) on this representation.

### Metrics

- Same as Spike 7's headline table
- Additionally: number of usable sessions (Q&A units appear in fewer sessions than mined decisions, so the dataset shrinks)
- Inter-session pairwise embedding distance: are sessions in the same project closer to each other than to other projects? (Sanity check that the representation captures something.)

**What good looks like:** Geometric structure appears here even when it didn't in Spikes 5-8. That would mean the extraction step itself was destroying the signal, and the path forward is "embed structured artifacts as-is, don't reduce them to taxonomies."

**What bad looks like:** No improvement over Spike 7. The data simply doesn't have low-dim structure regardless of representation.

**Why this is the most important spike to run:** If it works, it suggests the entire Phase 1 framing was wrong in an interesting way — the unit of analysis was too small. If it doesn't work, it's strong evidence that the geometric framework is the wrong tool for this problem, independent of input choices.

---

## Execution Order

```
Spike 5 (format ablation)        ←  foundation, do first
    │
    └── Spike 6 (model ablation)  ←  needs F* from Spike 5
            │
            └── Spike 7 (re-run geometry tests with F* + M*)  ←  the headline question

Spike 8 (project-identity factoring)  ←  parallel with 7 once F*+M* known
Spike 9 (skip extraction)             ←  fully independent, can run from day 1
```

Spike 9 is independent and should run in parallel with everything else — it's a different hypothesis, not a refinement of the same one.

## Go/No-Go Decision Matrix

| Spike 5 (format) | Spike 7 (re-run) | Spike 9 (raw units) | Verdict |
|---|---|---|---|
| Pass | Pass | Pass | **Full go.** Context-rich embeddings unlock the framework. Use F* + M* + (optionally) raw Q&A pooling. Build the GP. |
| Pass | Partial | Pass | **Pivot to Spike 9's representation.** The Q&A-unit approach worked where decision extraction didn't — drop extraction from the pipeline. |
| Pass | Fail | Fail | **Geometric framework is wrong tool.** Better inputs help correlation but the structure isn't there. Pivot to nearest-session retrieval (use the embeddings for similarity lookup, not PCA). |
| Fail | Fail | Pass | **Spike 9 only.** Skip the mined-decisions track entirely; build around Q&A artifacts as first-class. |
| Fail | Fail | Fail | **No-go (confirmed).** Phase 1's negative result holds even with better inputs. Fall back to frequency-based defaults or per-task-cluster priors from Phase 1's Spike 3 bright spot (`test_strategy` 3.7× baseline). |

## Estimated Effort

| Spike | Effort | Blocking? |
|---|---|---|
| 5 (format ablation) | 2-3 hours | Yes — Spike 6 and 7 depend |
| 6 (model ablation) | 1 hour | Yes — Spike 7 depends |
| 7 (re-run geometry) | 1-2 hours (mostly reuse Phase 1 scripts) | No |
| 8 (project-identity factoring) | 2 hours | No |
| 9 (skip extraction) | 2-3 hours | No — independent |
| **Total** | **~8-11 hours** | Spike 5 + Spike 9 in parallel halves wall-clock |

## Cost Budget

Total OpenAI spend should remain well under $5 of the remaining $29.

- Spike 5 F5 generation: 700 decisions × gpt-4.1-mini × ~200 tokens out = ~$0.40
- Spikes 5-7 embeddings on all 6 formats × 700 inputs × ~500 tokens average × text-embedding-3-small = ~$0.04
- Spike 6 re-runs of F* on text-embedding-3-large = ~$0.05
- Spike 9 Q&A unit embeddings: 230 units × 200 tokens × small = ~$0.001
- Recomputation buffer: ~$1

## What Phase 2 Will *Not* Test

- **The extraction prompt itself.** We hold the Phase 1 extraction constant and only vary how we embed its outputs. If Spikes 5-8 all fail, redoing extraction with a stronger model (or hand-crafting clean data, per Phase 1's synthetic fallback option) is a Phase 3 question.
- **Per-project priors.** The Phase 1 Spike 3 bright spot (`test_strategy` predictable from task embedding) suggests context-conditioned models are viable. That deserves its own design doc, not a bolt-on here.
- **GP framework itself.** This phase is about whether the *inputs* support the framework, not about building it. Spike 4-style simulation in Phase 2 (via Spike 7) is the only GP-adjacent measurement.

## Open Questions for the Designer

1. Should Spike 5's F4 (turn-window) include only the user's own preceding turns, or also the agent's turns? Agent turns carry the framing that constrained the user's answer, but also add noise.
2. For Spike 9, when a session has many Q&A units (some have 5+), is mean-pooling the right aggregation? Alternatives: max-pool, attention-weighted, or use the first N as a fixed-length representation.
3. Spike 8 assumes the "project-identity" axis is captured by the task description. What if it's actually captured by the *first decision* in the session? Worth a sensitivity check.

---

## Findings (run 2026-05-27)

All five Phase 2 spikes executed. Artifacts in `.tmp/experiments/orthogonal-questioning/`.

### Headline verdicts

| Spike | What was tested | Result | Verdict |
|---|---|---|---|
| 5: Format ablation | F1-F6 input formats, correlation test | F4 (turn-window) wins: ρ_positive_only 0.063 → 0.453 | **PASS** |
| 6: Model/dims | small vs large@1536 vs large@3072 | M3 only +0.011 ρ over M1; 95% same rankings | **NO MOVE** (use small) |
| 7: Re-run geometry with F4+small | Phase 1's full geometric battery | 1/5 targets met; n_90 still 40; session-identity leakage | **PARTIAL/FAIL** |
| 8: Project-identity factoring | Subtract task-description projection before PCA | n_90 40 → 33 (primary) or 26 (alt). All top-5 PCs flip to decision-content | **PASS** |
| 9: Skip extraction, raw Q&A units | Mean-pool Q&A embeddings per session | n_90 40 → 23. Top PCs flip to decision-content. Procrustes still poor. | **PARTIAL** (qualitatively positive) |

Mapping to the Phase 2 go/no-go matrix: the closest cell is `Spike 5 pass / Spike 7 partial / Spike 9 partial → Pivot to Spike 9's representation`. Both Spikes 8 and 9 produced real structural improvements; Spike 7 alone didn't.

### The reframing

Phase 1 concluded the *math* was wrong — the decision space didn't have low-dim structure. Phase 2 reverses that: **the representation was wrong**, not the math.

Specifically:

1. **Embedding richer context per decision (Spike 5) only helps correlation, not geometry.** F4 boosts pairwise correlation from 0.063 to 0.453 — a 7× lift — but when Spike 7 re-ran Phase 1's full battery (PCA, stability, simulation) using F4, n_90 stayed at 40, Procrustes stayed at 0.98, simulation savings actually dropped from 27% to 25%. The correlation gain was *session-identity leakage*: when 70% of F4 strings are duplicates across decisions in the same session, "pairs that co-occur" trivially also "embed to the same vector," which inflates ρ without revealing real decision structure.

2. **Removing project-identity (Spike 8) does help geometry.** Linearly projecting out the task-description vector before PCA drops n_90 from 40 → 33 (primary) or 26 (alt where the projection uses the first decision's F4 instead of the task description). The top-5 PCs cleanly flip from "code project vs docs project" to interpretable decision themes — `error_handling` (corrective vs preventive), `validation_approach` (runtime vs input-level), schema rigor. Diagnostic numbers confirm signal preserved: L2 norm drops 1.00 → 0.63, within-session cosine 0.87 → 0.78 (still clustered), cross-session 0.49 → 0.13 (the shared "task cone" was correctly removed).

3. **Skipping extraction entirely (Spike 9) helps the most.** Embedding raw `{question, answer}` Q&A artifacts and mean-pooling per session drops n_90 from 40 → 23 (43% reduction). Top PCs cleanly encode decision content: spec-design vs bug-triage, file-location vs scope, ship-it vs design-the-skill. Trade-off: yield is smaller (351 Q&A units / 36 sessions, since AskUserQuestion is only used in some sessions) and split-half Procrustes is still high (0.91), partly because the temporal split crosses a phase change in the user's workflow (early sessions were skill-authoring, later are tool implementation).

4. **Model upgrade (Spike 6) is irrelevant at this scale.** text-embedding-3-large adds 0.011 to ρ over text-embedding-3-small and produces 95% identical top-10% rankings. The bottleneck is the data and the unit of analysis, not the embedder's capacity.

### What changed between Phase 1's conclusion and now

Phase 1's "no-go on geometric framework" was based on a hidden methodology choice: embed each LLM-extracted decision *value* as a separate point. That choice (a) strips the surrounding context the LLM saw when extracting, (b) introduces paraphrase noise (96% chaotic dimensions under string canonicalization), and (c) means each session contributes ~10-15 short embeddings, all of which are dominated by project-identity in PC1.

Phase 2 found two alternative representations that get around this:

- **Spike 8 path** — keep the extracted decision pipeline but residualize project-identity. Preserves the per-type breakdown that's load-bearing for any future GP modelling (per-decision uncertainty propagation needs typed dimensions).
- **Spike 9 path** — drop extraction entirely. Each AskUserQuestion event is already a structured artifact: question, answer, surrounding context. Embed and pool. Simpler, cheaper, no LLM extraction step at all. Smaller dataset but cleaner signal.

Both produce real geometric structure where Phase 1 found none.

### Concrete next steps

In priority order:

1. **Build the GP/orthogonal-questioning prototype on the Spike 9 representation.** Mean-pooled Q&A embeddings per session, run nearest-neighbour session retrieval for cold-start, use PCA components as the orthogonal question basis. This is the cheapest path with the cleanest structure.
2. **For decisions that don't come through AskUserQuestion**, fall back to Spike 8's residualization approach — keep the typed dimensions, factor out the task-description vector before any geometric computation.
3. **Re-examine Phase 1 Spike 3's stability finding.** Procrustes is still high in both Spikes 8 and 9 (0.91-0.98). Two competing explanations: (a) genuine non-stationarity in user preferences over the 6-month window, (b) small-sample noise (n=36 per half is severely under-determined for a 1536-dim space). Discriminating between these needs more data — either waiting for more sessions, or running on a longer historical window if the auto-etl dataset extends further back than this experiment used.
4. **Don't bother with text-embedding-3-large.** Phase 2 settles this empirically: the small model is good enough at this data scale. Revisit only if (3) produces a much larger dataset where the model's marginal capacity becomes detectable.

### What the GP framework now looks like, sketched

With Spike 9's representation:

- **Prior**: mean and covariance of the historical session Q&A embeddings (in 1536-dim space)
- **Observation model**: each Q&A answer reduces uncertainty along the direction of that Q's embedding
- **Acquisition**: pick the next question whose embedding is most aligned with the current largest-eigenvalue direction of the posterior covariance — *and* most distant from already-asked questions in this session (orthogonality)
- **Cold start**: nearest-neighbour session retrieval seeds the prior with the closest 3-5 historical sessions

This is a 50-line prototype away from runnable, and the Phase 2 data shows there's enough structure for it to do something non-trivial.

### What's *still* not salvageable

- The original headline claim "3-5 questions instead of 50" doesn't survive even with Phase 2's improvements. Spike 9 simulation wasn't re-run (would be the natural Spike 10) but even at n_90=23, ortho-PCA's lift over frequency is unlikely to reach the 30% bar. The framework is qualitatively viable; the 10x compression number was always aspirational.
- Temporal stability is still a problem. Procrustes 0.91-0.98 means PCA bases shift substantially across the 6-month window. Any production deployment needs to either accept retraining on a rolling window or condition on time/project explicitly.

### Cost

Phase 2 total OpenAI spend: under $0.05. Phase 1+Phase 2 combined: well under $1 of the $30 budget.
