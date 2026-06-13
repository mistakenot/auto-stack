---
hash: "fbb41128"
id: "53639ef8"
read_when: "reviewing structured compiler assumption validation results or understanding why the v5 schema surgery was abandoned"
summary: "v5 schema surgery experiment results for the structured compiler: removing decision_candidates/blast_radius and adding verbatim qualifiers/axis_priorities — verdict ABANDON, as NRS dropped 0.25 and CDR dropped 0.118 against v3 baseline."
title: "Structured Compiler Assumption 1 — v5 Schema Surgery Report"
---

# Assumption 1 Validation Report — v5 (Schema Surgery)

**Spike:** Structured Compiler — Phase 6.2.
**Date:** 2026-05-27
**Cases:** 40 (all usable; v3-input regime reuses pre-existing elicited Q&A).
**Models:** extraction + generation + most judges = `gpt-4o-mini`. NRS judge = `gpt-4o`.
**Hypothesis under test:** schema surgery (drop `decision_candidates`, retire
`blast_radius`, require verbatim qualifiers, add `axis_priorities`) will lift NRS
toward 4.0 without breaking CDR.

---

## Verdict: **ABANDON** (the surgery as proposed)

The three-part pass criterion required:
- **NRS lifts ≥ 0.5 (paired) on task_folder cases under v5_v3input.**
  Observed paired Δ = **−0.25** (worse). FAIL.
- **CDR holds within ±0.05 of v3.**
  Observed paired Δ (v5_v3input vs v3, full corpus) = **−0.118**, CI [−0.184, −0.058], p(>0)=0.0.
  Task_folder paired Δ = **−0.32**, p(>0)=0.0004. FAIL by a wide margin.
- **`axis_priorities` populated and non-uniform.**
  35/40 cases under v5_v3input had a non-uniform axis_priorities (max−min > 0.05);
  35/40 had at least one axis with priority > 0.6. PASS.

Two of three thresholds fail. The qualifier-rendering pillar of the surgery DID
work — verbatim audit is 100% clean — but verbatim qualifiers did not move the
nuance score, and removing the decision_candidates / blast_radius buckets cost
CDR on exactly the input regime where the schema previously held up best.

---

## Four-arm summary (n=40)

| Metric | baseline | v3 | v5_base | v5_v3input |
|---|---|---|---|---|
| **CDR** | 0.359 [0.262, 0.461] | 0.413 [0.318, 0.519] | 0.308 [0.213, 0.410] | **0.296** [0.216, 0.383] |
| **HAR** | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.025 [0.000, 0.075] | 0.025 [0.000, 0.075] |
| **NRS** (1-5) | 2.43 [2.28, 2.58] | 2.58 [2.40, 2.78] | 2.38 [2.23, 2.53] | **2.45** [2.30, 2.60] |
| **CPR** (n=36) | 0.669 [0.537, 0.801] | 0.725 [0.611, 0.833] | 0.611 [0.482, 0.738] | **0.581** [0.440, 0.720] |

Bracketed numbers are 95% bootstrap CIs.

### Paired Δ within each input regime (paired bootstrap, 5000 iters)

| Metric | v5_base − baseline | v5_v3input − v3 | v5_v3input − baseline |
|---|---|---|---|
| CDR | −0.051 [−0.109, +0.006], p=0.04 | **−0.118 [−0.184, −0.058], p<0.001** | −0.063 [−0.148, +0.026], p=0.08 |
| HAR | +0.025 [0.000, +0.075] | +0.025 [0.000, +0.075] | +0.025 [0.000, +0.075] |
| NRS | −0.05 [−0.18, +0.08] | −0.125 [−0.325, +0.05] | +0.025 [−0.10, +0.15] |
| CPR | −0.058 [−0.178, +0.056] | **−0.144 [−0.259, −0.037], p=0.003** | −0.088 [−0.278, +0.097] |

The two large, statistically significant moves both point against the surgery.

### Task_folder strata (n=8, the bucket where v3 already worked)

| Metric | baseline | v3 | v5_v3input | Δ (v5_v3input − v3) |
|---|---|---|---|---|
| CDR | 0.72 | 0.84 | 0.52 | **−0.32**, CI [−0.50, −0.13] |
| NRS | 3.00 | 3.25 | 3.00 | −0.25 |
| CPR | 0.64 | 0.70 | 0.55 | −0.15 |

The CDR collapse on task_folder is the single most important number in this
report. v3's headline lift (0.72 → 0.84) was the strongest case for keeping the
rich-input + Q&A path alive. Surgery erased two-thirds of that lift and then
some.

---

## Field utilization audit (v5_v3input, n=40)

| Field | Mean | Median | Cases with 0 |
|---|---|---|---|
| hard_constraints | 1.6 | 1.0 | 14 / 40 |
| soft_preferences | **5.1** | 5.0 | 0 / 40 |
| └ soft_preferences with "TBD"/"candidates" | 4.6 | — | 0 / 40 |
| qualifiers | 0.8 | 1.0 | 19 / 40 |
| └ qualifiers with source_quote populated | 0.8 | — | 19 / 40 |
| assumptions | 2.0 | 2.0 | 9 / 40 |
| **axis_priorities** | 6.55 axes | 6 | 0 / 40 |

axis_priorities stats (v5_v3input): per-case mean priority 0.64; per-case max
mean 0.91; per-case min mean 0.49; non-uniform in 35/40 cases; 35/40 had at
least one priority > 0.6. The new field carries meaningful signal.

Key shifts vs the original schema:

- **soft_preferences mean jumped from 1.4 → 5.1.** The "TBD-bucket" instruction
  fired in every case (40/40 had ≥1 TBD soft_preference). The decision_candidates
  bucket has not been lost — it has been redistributed into soft_preferences.
  But the items it now carries are far softer in voice ("TBD; candidates: …"),
  and the CDR judge appears to read them as un-committed rather than as decisions
  worth crediting. This is the most plausible cause of the CDR drop.
- **qualifiers held steady around ~0.8 per case (vs ~1.0 in baseline).** Slightly
  fewer because the "must be verbatim" instruction made the extractor drop
  paraphrased ones — which is the desired behaviour. Every qualifier in v5 has a
  populated source_quote.
- **hard_constraints unchanged (1.6 vs 1.9 baseline, ~half cases at zero).**
  The bottleneck the original audit named is still there.

### Verbatim qualifier audit (spot-check, 5 cases)

| Case | Regime | # qualifiers | # strict-verbatim | # loose-verbatim |
|---|---|---|---|---|
| sc_001 | v5_v3input | 4 | 4 | 4 |
| sc_002 | v5_v3input | 1 | 1 | 1 |
| sc_003 | v5_v3input | 1 | 1 | 1 |
| sc_004 | v5_v3input | 2 | 2 | 2 |
| sc_005 | v5_v3input | 1 | 1 | 1 |
| sc_001 | v5_base | 3 | 3 | 3 |
| sc_002 | v5_base | 1 | 1 | 1 |
| sc_003 | v5_base | 1 | 1 | 1 |
| sc_004 | v5_base | 2 | 2 | 2 |
| sc_005 | v5_base | 1 | 1 | 1 |

**100% clean across both regimes.** Every emitted `source_quote` is a
character-for-character substring of the source material (initial prompt +
planning docs + qa_pairs as applicable). The verbatim instruction WORKED — the
extractor really did stop paraphrasing.

---

## Why didn't NRS move?

The verbatim guarantee landed (audit-clean), generation rendered qualifiers as
direct block quotes, and yet NRS sits at 2.45 (vs v3's 2.58 — slightly *worse*).
Three observations from the artifacts:

1. **NRS is judging against ground-truth materials richer than what the
   extractor sees.** The NRS judge reads `gt = initial_prompt + planning_docs +
   feedback.md + corrections + final_artifacts`. Many of the "nuances" in that
   bundle live in feedback.md and corrections — material the extractor never
   sees. Quoting verbatim from what the extractor *does* see can't move that
   needle if the relevant nuance lives downstream.

2. **The score-3 ceiling on task_folder is sticky.** Of 8 task_folder cases,
   every single one scored NRS=3 in v5_v3input (CI is [3.0, 3.0]). The judge
   reads ≥1 preserved exception (the qualifier block-quote is hard to miss) but
   declines to award 4 or 5 because the draft still summarises rather than
   preserves the source's overall conditional voice. A handful of verbatim
   carve-outs do not turn a paraphrased spec into a verbatim one.

3. **Removing decision_candidates pulled NRS down on autosearch cases.** Where
   the extractor used to flag "storage TBD: sqlite/postgres" as a candidate set
   the generator would dutifully list, the same content is now buried inside a
   soft_preference with "TBD" prefix. The generator's preferences-and-tradeoffs
   section reads softer; the NRS judge counts that as nuance lost.

**Honest verdict on the bottleneck:** the generator is not the limit. It
faithfully reflects the state, and the verbatim-rendering pillar of the surgery
proves it can carry source language. The structured state itself doesn't carry
the relevant nuance — and importantly, **the source the extractor reads doesn't
contain it either.** NRS measures alignment with corrections and feedback, which
no ex-ante artifact can predict. Asking the schema to lift NRS toward 4.0 was
asking it to anticipate post-hoc corrections.

---

## Why did CDR drop?

CDR fell most on task_folder (−0.32, p<0.001). Two mechanisms, both traceable to
the surgery:

- **Decision_candidates removal hurt more than helped.** In the original audit,
  decision_candidates was used in 13/40 cases — but disproportionately on the
  task_folder ones with explicit fork enumeration. Folding them into
  soft_preferences with "TBD" sentinels reshapes a decision-with-options into a
  preference-with-question-mark. The downstream generator renders them as open
  bullets in `## Preferences and Trade-offs`, and the CDR judge reads them as
  un-decided rather than as positions the team committed to. So decisions that
  used to be scored as "present" in the draft now read as "still TBD".
- **Verbatim qualifier discipline trimmed paraphrased carve-outs that had been
  carrying decision content.** When the extractor reads "use sqlite (with
  postgres as a fallback if scale demands it)" and previously emitted a paraphrase
  qualifier *and* a hard_constraint, it now only emits the verbatim qualifier
  (or drops it). The "and-then-some" content is gone.

The combination produces a softer, more provisional-sounding draft on exactly
the cases where v3 had the firmest grip.

---

## Cost

| Step | Calls | Model | Cost |
|---|---|---|---|
| extract (both regimes, 2 × 40) | 84 | gpt-4o-mini | $0.043 |
| generate (both regimes, 2 × 40) | 84 | gpt-4o-mini | $0.032 |
| score (judges across both regimes) | 1228 | mixed | $0.552 |
| **Total** | | | **$0.627** |

Within the $0.80 budget. Cached judge calls saved ≈30% of scoring spend through
cross-cache reuse on GT-only judges.

---

## Recommendation

**ABANDON the surgery as currently proposed.** The schema problems the original
audit named are real, but the proposed fixes do not address them at the level
that matters:

- `decision_candidates` was indeed inert in the base regime — but in
  task_folder cases it was carrying useful structure. Collapsing it into
  soft_preferences erased decision-grade content from drafts. **Reverse this
  change.** If the bucket is inert on autosearch cases, accept the empty bucket
  there rather than reshape it.
- `blast_radius` → `axis_priorities` is a strict win in form (the new field is
  non-uniform on 35/40 cases and carries real signal). But downstream metrics
  do not yet read it, so it shows up here as zero-impact. **Keep this change
  for any future regret-aware question policy** (Phase 6.3 territory).
- Verbatim qualifiers are a strict win in form (100% audit-clean) and a
  near-zero-impact change for the metrics in this experiment. **Keep this
  change** — it costs nothing and it makes the artifact citable. But it is not
  the lever that lifts NRS.

If a v6 is built, it should:
1. Restore `decision_candidates` as a sibling of `soft_preferences` (not a
   replacement target).
2. Keep verbatim qualifiers and `axis_priorities`.
3. Stop pretending the extractor can lift NRS without seeing feedback/corrections.
   The honest path to NRS ≥ 4 is changing the input contract (feed the extractor
   recent correction-rich sessions or similar prior tasks), not changing the
   schema.

For Phase 6 synthesis: **v3 (rich-input + Q&A, original schema) remains the
strongest contender**. The schema does not appear to be the next lever; the
input contract does.

---

## Files

- Scripts:
  - `scripts/build_structured_state_v5.py`
  - `scripts/generate_requirements_v5.py`
  - `scripts/score_assumption_1_v5.py`
  - `scripts/run_a1_v5.py`
- Artifacts:
  - `artifacts/states_v5_base/` (40)  `artifacts/states_v5_v3input/` (40)
  - `artifacts/drafts_v5_base/` (40)  `artifacts/drafts_v5_v3input/` (40)
  - `artifacts/scores_v5_base/` (40)  `artifacts/scores_v5_v3input/` (40 + summary.json)
- Caches: `artifacts/extraction_v5_cache.sqlite`, `generation_v5_cache.sqlite`,
  `scoring_v5_cache.sqlite`.
