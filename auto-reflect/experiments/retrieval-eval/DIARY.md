# Research diary — playbook retrieval evaluation

A running log of thinking, decisions, and findings for the retrieval-eval
experiment. Newest sections appended. This is the "why", the README is the "how",
and `data/*/SNAPSHOT.md|QUERIES.md` are the provenance.

---

## The question

`auto reflect` retrieves playbook **rules** for a coding task. The shipped matcher
(`auto-reflect/internal/rules/match.go`) is **pure lexical**:

- keyword in a rule's `use_when` = +3, keyword in a `domain` tag = +1/keyword,
  normalized by `4·len(keywords)`;
- `--domain` is a **hard ANY-of exclusion** — pass the wrong/absent domain and
  good rules are dropped before scoring;
- any rule with a zero keyword score is dropped;
- `hard` rules inject on domain match alone.

Worry: the tag `go` is on **78% of rules**, so domain filtering is a near-useless
axis, and a bad domain guess can silently exclude relevant rules. **Should we
change the filter** (hard gate → soft boost, IDF tag weighting, or drop it)? We
wanted data before touching `match.go`, not intuition.

## Design decisions (and rejected alternatives)

- **Offline IR eval, not a live A/B (yet).** Build a test collection (rules +
  queries + oracle relevance judgments) and compare retrieval variants on metrics.
  Cheap, deterministic, repeatable. The live feedback loop is the eventual A/B;
  offline is the screen.
- **Python harness, port the winner to Go later.** Variants (IDF, boost,
  semantic), stats, and the LLM oracle are all Python-native; each variant is ~20
  lines here vs a Go rebuild + new subcommand. _Rejected:_ building variants into
  the tool under a `beta` group — it doesn't escape Python (Go has no Wilcoxon/
  nDCG), taxes every idea with a rebuild, and makes semantic variants painful.
- **Baseline fidelity asserted, not assumed.** `baseline.py` is a faithful port of
  `match.go`, pinned by a **conformance harness** that A/Bs Go-CLI vs Python
  ranking. _Rejected:_ trusting the port — a drifted baseline invalidates
  everything. `match.go` is ~50 deterministic lines, so the port is verifiable.
- **Durable in the module, not a throwaway `.tmp` spike.** Lives at
  `auto-reflect/experiments/retrieval-eval/` with its own **pinned** corpus
  snapshot, decoupled from the live event store so runs reproduce.
- **Held-out queries.** Mine retrieval intents from sessions _not_ among the 95
  that produced the rules, to reduce leakage.

## Phase log

1. **Baseline conformance — DONE.** Faithful port + two hermetic conformance
   layers (real-120-rule rebuilt from snapshot + synthetic edge cases). 30/30 +
   14/14 parity vs the Go CLI; ~8s; `pytest -m baseline`. Tests named/marked
   `baseline` to separate regression pins from future variant evaluation.
2. **Query mining (held-out) — DONE.** 48 agents over held-out sessions →
   **100 queries** (64 clean, 36 leakage-flagged via `overlaps_mined_task`), each
   with a (possibly imperfect/empty) `domain_guess`. See `data/queries/QUERIES.md`.
3. **Oracle coverage pilot — DONE.** 20 agents grade all 120 rules per query
   (graded 1–3). `data/qrels/pilot.qrels.jsonl`.
4. **Full oracle — PENDING** (gated on the design changes below).
5. **Variant comparison — PENDING.** hard-gate vs domain-as-boost vs IDF vs
   no-filter; paired stats.

## Findings

### Pilot: coverage is healthy, the gate effect is small (the surprise)

- **Coverage / label density (go/no-go gate): GO.** 90% of queries (18/20) have a
  relevant rule, **mean 4.6 relevant/query** (median 4, max 10). The fear was a
  pile-up at 0–1 relevant (which makes recall@k all-or-nothing and kills power);
  it didn't happen. "Dense" here means *per-query* count, not matrix density — the
  20×120 matrix is 3.8% full, which is normal; what matters is that the typical
  query has a *handful* of relevant rules so metrics can discriminate.
- **Domain-gate effect preview: smaller than hypothesized.** mean recall **with**
  the domain filter = **0.93**, **no** filter = **0.96** — a 3-point gap; the gate
  dropped a relevant rule in only **2/20** queries. Because the mined `domain_guess`
  values were _competent_, the hard gate rarely excludes. **The failure mode we
  suspected requires bad/absent guesses, which the query set under-represents.**

> Reframing: the experiment's payload shifted. "Hard gate excludes good rules" is
> **not** the dominant problem in the common (good-guess) case. The live questions
> become: (a) what **precision** does the gate buy (recall is already ~0.93 with
> it), and (b) how does it behave under **wrong/absent** domain guesses.

### Gotcha: `auto reflect retrieve` is NOT read-only

It appends a `retrieval` event to the canonical log on every call. An early
conformance design ran retrieve against the live store and leaked **127** junk
events (stripped before commit; re-folded). Both conformance layers now build
hermetic throwaway stores. Logged as observation `ob-a6a1b327` in the live
playbook. (A temp `auto reflect init --project` needs an initial git commit first,
else git warns.)

## Second reads (independent, and they converge)

Three independent reads — the **pilot data**, a **Codex** methodology review, and
**IIR (Manning/Raghavan/Schütze) Ch 8** — landed on the same handful of issues.

**Codex verdict: GO-WITH-CHANGES.** "Sound for a *local* decision about
`match.go`, but not yet strong enough to treat as objective IR truth." Its top
threats: (1) **oracle circularity** — one Sonnet judge grading Claude-authored
rules and Sonnet-mined queries → "Claude-judged relevance," not ground truth;
mitigate with blinded/randomized rule order, a strict rubric, negative controls, a
second judge / human audit on decision-changing cases. (2) **precision/noise** is
the main blind spot — removing the gate may raise recall while flooding the agent;
add precision@k, surfaced-count/token-cost, "irrelevant-before-first-relevant".
(3) **leakage**: the "affects every variant equally" claim is too strong — lexical/
tag leakage can favor keyword vs domain variants differently; use the clean 64 as
primary, flagged-overlap as sensitivity only, and **cluster by source session**
(2–3 queries from one session aren't independent). (4) stats guardrails:
pre-register one primary metric/k/comparison, report effect sizes + bootstrap CIs,
handle zero-relevant queries, correct/label multiple comparisons. (5) lifecycle
sensitivity — the snapshot is all-draft rules.

**IIR Ch 8 (Evaluation in IR) crossover — what's useful vs ceremony to skip:**

- _Useful — adopt:_ **precision *and* recall** (§8.3–8.4, the P/R tradeoff — the
  single most valuable import, and exactly what the pilot shows we're missing
  since recall is already saturated); **kappa / second assessor** (§8.5 — the fix
  for the oracle risk); **relevance is to the information *need*, not the query
  string** (§8.1 — the reason `domain_guess` *quality* must be a factor); nDCG for
  graded relevance (already using).
- _Skip — not useful at our scale:_ 11-point interpolated AP / full PR curves,
  ROC, MAP (redundant with nDCG here); **pooling machinery doesn't apply** — with
  120 rules we judge *all* of them per query (complete judgments), so we dodge the
  pooling bias that distorts large-collection IR (a genuine small-corpus win).
- _Reassurance:_ TREC ad-hoc standardized on **~50 topics**; our 64 clean queries
  clears that bar.
- _Framing:_ system effectiveness ≠ user utility (§8.6) — offline metrics are a
  proxy; the reflect **feedback loop** is the real A/B test of utility.

## What changes before the full oracle (the intersection of all three reads)

The minimal high-leverage set (resisting textbook gold-plating):

1. **Measure precision, not just recall** — add `precision@k` + surfaced-count to
   `metrics.py`. The gate's *job* is precision; we never measured its benefit.
2. **Add a wrong/empty-domain condition** — run each query under {agent guess, no
   domain, deliberately-wrong domain} to isolate where the gate actually hurts.
3. **Second-judge kappa spot-check** (~20 queries, different model or a human
   audit) — validate the gold standard isn't one model's idiosyncrasy; randomize
   rule order to debias.
4. **Cluster by session + report on the clean 64 as primary**; flagged-overlap as
   sensitivity only.

## Pilot extension — precision + domain conditions (free, no new oracle)

`retrieval_eval.conditions` over the 20 pilot qrels, three domain conditions
(`guess` / `none` / `wrong` — wrong = a real tag disjoint from the query's
relevant-rule domains). `data/qrels/pilot.conditions.json`.

| condition | mean recall | recall@5 | recall@10 | precision@5 | precision(full) | **surfaced (of 120)** |
|---|---|---|---|---|---|---|
| guess | 0.93 | 0.20 | 0.31 | 0.16 | 0.079 | **78** |
| none  | 0.96 | 0.13 | 0.21 | 0.11 | 0.044 | **108** |
| wrong | 0.00 | 0.00 | 0.00 | 0.00 | 0.000 | 38 |

**Three findings, in order of importance:**

1. **The real problem isn't the domain gate — it's that the matcher surfaces
   ~65–90% of the playbook.** No-filter surfaces **108 of 120** rules; even with a
   domain guess, **78**. Full-set precision is **4–8%**; precision@5 is **11–16%**.
   The lexical score (use_when/domain substring) is so permissive that almost any
   keyword overlap surfaces a rule, and the "ranking" barely separates relevant
   from irrelevant (**recall@10 ≈ 0.31 at best** — only a third of relevant rules
   reach the top 10). This is a *ranking/precision* problem larger than the domain
   question.

2. **The domain gate is a precision crutch that helps in the good-guess case** more
   than the pilot's full-recall number implied: it cuts surfaced 108→78, ~doubles
   full precision (0.044→0.079), and **raises early recall** (recall@5 0.13→0.20)
   by pushing off-domain rules out of the top-k. So domain matching *is* doing
   useful ranking work.

3. **…but the gate is catastrophic under a wrong guess: recall → 0.0** across every
   query. And it bites on *realistic near-misses* too, not just constructed-wrong:
   q-2597d789 guess `[ci,go,monorepo]` → recall 0.67 vs 1.0; q-ab0f5e74
   `[etl,search]` → 0.40 vs 0.60. Two of 18 real queries already lost recall to an
   imperfect (not even fully wrong) guess.

> **Synthesis:** the domain filter is a **high-variance** mechanism — it helps
> precision/early-recall when the guess is right and fails totally when wrong.
> That is a clean, *data-backed* case for **domain-as-boost** (keep the ranking
> benefit, drop the catastrophic exclusion). It is NOT a case that the gate is the
> biggest issue — finding #1 (the matcher surfaces most of the playbook and ranks
> poorly) is the bigger fish, and points at the *scorer* (better lexical or
> semantic), not the filter.

Caveats: 20 queries, single oracle (relevance may be generous); `wrong` is
disjoint-by-construction so its recall=0 is the mechanism's worst case, but the
realistic near-miss losses above are not constructed.

## Open questions / risks

- Will the **full clean-64 set** show a larger gate effect than the pilot's 2/20,
  or confirm "hard gate isn't the dominant problem"? If the latter, the
  recommendation may be "drop the hard exclusion for a cheap safety win" rather
  than "this fixes a big leak."
- Oracle self-preference magnitude — unknown until a second judge runs.
- Precision cost of removing the gate — entirely unmeasured; could be the deciding
  factor.
- The eval ignores the **`select` stage** and **draft-vs-confirmed** surfacing that
  the live loop uses.

## Status

- Phases 1–3 done. Baseline conformance green. 100 held-out queries + 20-query
  oracle pilot landed. Full oracle gated on the four changes above.
