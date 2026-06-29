# Second-judge experiment — inter-rater reliability of the golden qrels

**Question (Phase-5 gate).** The Phase-4 verdict rests on qrels graded by a *single*
oracle (Claude/Sonnet) over Claude-authored rules and Sonnet-mined queries. Is that
gold standard trustworthy, or is it "Claude-judged relevance" (self-preference)?
If the labels are biased/noisy, every Phase-4 number — and every future
trigger/facet experiment — is built on sand.

**Design — three-judge triangulation.** gemini is unavailable (ineligible tier), but
two *independent, non-Claude* judges are: `codex` (GPT) and `grok` (xAI). This is
stronger than one second judge: the **codex↔grok** agreement is a *reference
ceiling* for "how much do independent competent judges agree at all," so we can ask
whether Claude is an **outlier** (self-preference) or just shows the normal level of
judge disagreement.

- **Judges.** J1 = Claude/Sonnet (the frozen `qrels.jsonl`). J2 = codex. J3 = grok.
- **Rubric.** Byte-identical to the original oracle (`workflows/oracle.mjs`): strict
  relevance over the rule's *full content* (not keyword overlap), graded
  **3 = directly on-point / 2 = relevant / 1 = marginal / 0 = irrelevant**. Blind
  (judges never see J1's labels) and rule order randomized (seeded).
- **Slice.** The 53 clean-64 queries with ≥1 relevant label — the pre-registered
  Phase-4 primary slice.
- **Item pool per query (the universe kappa is computed over).** The
  *decision-relevant* pool: `(J1-relevant rules) ∪ (top-10 union across all 6
  variants, guess condition) ∪ (random J1-grade-0 fill)`, deterministic/seeded,
  ~25–45 rules. This is exactly the set that determines every Phase-4 metric, and
  it carries both relevant and surfaced-but-irrelevant rules — so kappa is honest
  (not inflated by the ~14k trivial true-negatives a full 120-rule re-judgment
  would add). J2/J3 grade every rule in the pool (explicit 0s).
- **Cost.** 53 queries × 2 judges = 106 CLI calls, one per (query, judge), each
  grading ~30 rules. Resumable (skip already-graded), parallel.

**Measurements.**
1. **Agreement.** Cohen's κ — binary (relevant iff grade ≥ 1) and quadratic-weighted
   (graded 0–3) — pooled over all (query, rule) judgments, for J1–J2, J1–J3, J2–J3.
   Plus confusion matrices and per-judge mean grade (generosity/bias direction).
2. **Decision robustness.** Rebuild qrels from J2, J3, and a **consensus** (median
   grade), re-run the Phase-4 metric panel (nDCG@10, p@5, recall@10) on the 53
   queries for all 6 variants, and check whether the **variant ordering** and the
   **sign of the key deltas** (no-filter & domain-boost worse; idf-tag ≈ gate;
   bm25+idf-tag ≥ gate) survive a judge swap. Internal check: J1-on-pool must
   reproduce the published Phase-4 clean64/guess numbers.

**Pre-registered go/no-go** (fixed before seeing results; Landis–Koch bands):
- **PASS (qrels trustworthy, conclusions robust):** J1–J2 *and* J1–J3 binary κ ≥ 0.40
  (moderate+), J1 is **not** a low outlier vs the codex↔grok κ, **and** the Phase-4
  variant ordering + key delta signs are preserved under J2/J3/consensus.
- **CONDITIONAL (directional only):** κ in 0.20–0.40 (fair) or one J1 pair weak, but
  ordering preserved → act only after a human adjudication pass on the
  decision-changing queries.
- **FAIL (re-label before acting):** κ < 0.20, OR J1 is a clear outlier (J1 pairs ≪
  codex↔grok), OR the variant ordering flips under a judge swap.

Artifacts: `data/results/judge_raw/{codex,grok}.jsonl` (raw grades),
`data/results/second_judge.json` (kappa + robustness), findings in `DIARY.md`.
