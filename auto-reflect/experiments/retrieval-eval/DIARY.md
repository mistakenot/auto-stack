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
3. **Oracle — DONE.** 20-query pilot, then the remaining 80, same oracle (one
   Sonnet agent grades all 120 rules per query, graded 1–3). Merged into the
   frozen gold standard `data/qrels/qrels.jsonl` (the pilot-only `pilot.*` files
   were consolidated away). 100 queries, 89% coverage, 585 labels.
4. **Variant comparison — DONE (Phase 4).** 6 variants (hard-gate baseline,
   no-filter, domain-boost, idf-tag, bm25, bm25+idf-tag) scored on the frozen
   qrels with session-clustered Wilcoxon + cluster-bootstrap CIs + Holm. New
   scaffold (`variants.rank`) pinned to reproduce `match.go` exactly (4/4 baseline
   tests). See "Phase 4 results" below. **It overturned the predicted outcome.**

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

## The four high-leverage changes (intersection of all three reads) — status

The minimal set (resisting textbook gold-plating). Status as of this checkpoint:

1. ✅ **Measure precision, not just recall** — done in `metrics.py` +
   `conditions.py` (precision@k, full-set precision, surfaced-count).
2. ✅ **Wrong/empty-domain condition** — done in `conditions.py` ({guess, none,
   wrong}). This already answered the core question (see findings), so we ran the
   full oracle without waiting on #3–#4.
3. ⬜ **Second-judge kappa spot-check** — PENDING. Single oracle so far; a second
   judge (different model / human audit on ~20 decision-changing queries) with
   randomized rule order would quantify the self-preference risk. Do before
   treating any Phase-4 result as more than directional.
4. ✅ **Cluster by session + clean-64 primary** — done in `stats.py` +
   `evaluate.py`. Wilcoxon runs on session-mean deltas (37 clean sessions),
   bootstrap resamples sessions, Holm corrects the family; clean 64 is the
   pre-registered primary, all-100 + flagged are sensitivity.

## Pilot extension — precision + domain conditions (free, no new oracle)

`retrieval_eval.conditions` over the (then 20-query) pilot qrels, three domain
conditions (`guess` / `none` / `wrong` — wrong = a real tag disjoint from the
query's relevant-rule domains). _(This pilot table is kept for the record; the
full-100 numbers below supersede it. The pilot-only derived file was later
consolidated into `qrels.conditions.json`.)_

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

## Full golden set (100 queries) — the bench is now built

Labeled all 100 queries (pilot 20 + remaining 80, same oracle). `data/qrels/qrels.jsonl`
is the frozen gold standard; any variant now scores against it with **zero new
oracle calls**. Coverage holds: **89/100 have a relevant rule**, mean **5.85**
relevant/query (median 5, max 17), 585 labels (grades 1:115 / 2:303 / 3:167).

Conditions on the full 100 (`qrels.conditions.json`) — *sharper* than the pilot:

| condition | recall | recall@5 | recall@10 | precision@5 | precision(full) | surfaced/120 |
|---|---|---|---|---|---|---|
| guess | 0.90 | **0.27** | **0.41** | **0.30** | 0.119 | 64.5 |
| none  | 0.96 | 0.19 | 0.30 | 0.23 | 0.054 | 107 |
| wrong | 0.00 | 0.00 | 0.00 | 0.00 | 0.000 | 33.4 |

The bigger N **strengthens** the nuanced verdict:

1. **Surfacing problem confirmed, large:** no-filter surfaces **107 of 120** rules;
   even with a guess, **64.5**. Full precision 5–12%. recall@10 ≈ 0.41 at best —
   the scorer still surfaces most of the playbook and ranks weakly.
2. **The domain gate genuinely helps top-k when the guess is right** — clearer now:
   precision@5 **0.30 vs 0.23** (+30% rel.), recall@5 **0.27 vs 0.19**, recall@10
   **0.41 vs 0.30**. Removing off-domain rules ranks the relevant ones higher in
   the slice an agent actually reads. So the gate is doing real work.
3. **…and is catastrophic on a wrong guess: recall 0.0.** Full recall cost vs
   no-filter is only 6pts (0.90 vs 0.96), but the *tail* is total failure.

> **Decision shape (now data-backed on 100):** the gate is a high-variance
> precision/ranking aid. **domain-as-boost** is the clear win — it keeps the +7pt
> precision@5 / +10pt recall@10 benefit (rank up in-domain rules) without the
> wrong-guess catastrophe (never exclude). Separately, the dominant lever is the
> **scorer** (surfaces 54–89% of the playbook, ranks poorly) — bigger than the
> filter. The full qrels now let us test scorer variants for free.

## Validity boundary: the query *interface* is still undefined

Surfaced mid-Phase-4 (worth stating plainly because it bounds what every number
here means). We have been evaluating a **matcher** — `(intent text, domain tags)
→ ranked rules` — but we have **not** defined how auto-reflect is *triggered* or
what produces the query. Separate the three layers we'd been collapsing:

- **Matcher** — the ranking function. *This is the only thing Phase 4 measured.*
  Shared core, reused by every interface.
- **Query** — the `(intent, domain)` tuple. Its *shape/quality* is set by the
  interface.
- **Trigger** — what issues a query, and from what context.

Three candidate interfaces, and they are **not the same retrieval problem**:

| interface | trigger | query shape | same regime as this eval? |
|---|---|---|---|
| keyword/intent (current) | agent/skill calls `retrieve` | NL intent + domain guess | ✅ exactly what we mined |
| bash-command hook | a command fires a hook | terse command + file paths | ⚠️ point-in-time, but tags/paths carry more signal than prose |
| live sidecar | hooks + transcript stream | rolling, noisy, *continuous* window | ❌ different problem (streaming, stateful) |

Decision (with Charlie, 2026-06-29): **keep the matcher interface-agnostic**; all
three interfaces are wanted long-term, user picks. So:

- **Phase 4 transfers to the keyword + bash interfaces** (both call a point-in-time
  matcher; a better matcher helps both) — but every conclusion here is
  **conditioned on intent-shaped queries**. BM25's edge comes from many query
  terms; on a 3-word keyword string or a bare bash command it would shrink and the
  domain signal would matter more. The `idf-tag` win is the most interface-robust
  finding; the `bm25` win is the most interface-sensitive.
- **The live sidecar is a separate experiment**, not a variant in this bench:
  streaming relevance (which rules are *becoming* relevant), dedup against
  already-surfaced rules, transition detection. Relates to the parked live-tier
  arch + `self-improving-playbook-retrieval.md`.
- **The assets are interface-agnostic.** Corpus, qrels, metrics, stats, and the
  variant scaffold all survive an interface change; only `queries.jsonl` is
  interface-specific. A second interface = mine a query set in *its* shape and
  re-run the bench for free — which also tests whether the ranking conclusions
  hold under a different query distribution.

## Future bench variants (parked — testable in this harness)

Ideas that slot into the Phase-4 framework once the inputs exist; not built yet.

- **Faceted (verb/noun) tags** (Charlie, 2026-06-29). Split the flat `domain`
  taxonomy into two orthogonal *facets* — an **action** facet (verb, e.g.
  `Create`/`Update`/`Delete`/`refactor`/`test`/`merge`) and an **entity** facet
  (noun, e.g. `UnitTest`/`AuthModule`), à la `Create(UnitTest)`. Prior art:
  frame semantics / FrameNet, intent+slots (Alexa/Dialogflow), faceted
  classification (Ranganathan), and — in-house — Conventional Commits `type(scope)`
  + the `contextual-commit` action lines. Design constraints if pursued: **additive,
  not a replacement** for flat tags; **controlled vocabularies** (entity facet
  governed by `docs/concepts/UBIQUITOUS_LANGUAGE.md`, verb facet a closed set);
  IDF weighted **per facet**; facet-to-facet matching is far more selective than
  substring overlap, so it's really a *structured-scorer* bet (attacks finding #1).
  Caveats: many rules are invariants/gotchas with no clean verb (so the facet is
  optional, ~half the corpus may lack it); matching needs the query parsed into
  verb(noun) too; re-tagging the 120-rule corpus is the only real cost (cheaper
  angle: derive the action facet from the commits/sessions the rules were mined
  from). Couples tightly with the **observation-time trigger capture** idea below
  and the **bash-command query interface** — the verb facet *is* the operation
  that triggers the rule.

## Trigger signatures — the observation-side representation question

(Charlie, 2026-06-29.) The query-interface thread has an observation-side twin:
today a rule's applicability is a prose `use_when` matched by lexical keyword
overlap — the **lowest-precision** option, and exactly the source of finding #1
(the matcher surfaces most of the playbook). A **trigger signature** is anything
observable *both* when the lesson is learned *and* when the agent is later working;
the good ones are high-precision and cheap to match. The space, tiered by
payoff-vs-cost (most of Tier 1 already flows through hooks/ETL/autowatch):

- **Tier 1 (high precision, cheap, on the wire):**
  - **Error / failure strings** — exit codes, stderr/compiler/lint regex, panic &
    stack patterns. Highest signal; ~half of `MEMORY.md` *is* error strings
    ("main is already used by worktree", "216/GROUP"). Exact error → exact rule.
  - **Tool-use signature** — which tool + input shape (`Edit`/`Write`/`Task`/an MCP
    tool), independent of prose.
  - **File path / glob** — `**/match.go`, `go.mod`, `*_test.go`, CI yaml. autowatch
    already speaks glob Triggers.
  - **Slash-command / workflow phase** — `/new-task`, commit-time, release-time,
    session-start (the `<command-name>` tag = where in the lifecycle).
- **Tier 2 (valuable, machinery partly exists):** git/repo state (branch,
  worktree-vs-primary, dirty, ahead/behind, open PR, merge-in-progress — many
  "worktree rules" are really git-state triggers); code/AST patterns (via ast-grep,
  which auto-graph uses); diff shape (files-touched, signature changed, rework
  churn — already a session-quality signal in `todo.md`); environment/config
  (OS, `systemd --user`, CI-vs-local, dep version).
- **Tier 3 (costly/speculative):** sequence/temporal ("after merge, before rebase";
  co-edit adjacency — the sidecar's turf); semantic/embedding intent match; data/
  schema shape (parquet columns, JSON/API shape).

Three framing points that matter more than the list: (1) it's a **precision/recall
spectrum** — error-string = high-P/low-R, keyword = high-R/low-P (today's flaw);
capturing several lets you trade along the curve. (2) Signatures **compose** — a
trigger is a Boolean/weighted combination ("in a worktree AND running `gh pr
merge`"), far more expressive than one string. (3) Each signature maps to a natural
query interface (error/cmd/tool → bash-hook + sidecar; glob → autowatch; git-state/
phase/sequence → sidecar; keyword/semantic → pull), so this taxonomy also fills out
the interface menu. Start-two pick: **error strings** (highest precision, already
collected informally) + **file globs** (cheap, autowatch can already act). See
`todo.md` → auto-reflect trigger-capture entry.

## Open questions / risks

- **Does the ranking verdict survive a non-intent query distribution?** Untested.
  Mine a keyword-shaped and a bash-command-shaped query set and re-run the bench;
  if BM25's edge collapses on short queries, the recommendation narrows to
  `idf-tag` only. (See validity boundary above.)
- Will the **full clean-64 set** show a larger gate effect than the pilot's 2/20,
  or confirm "hard gate isn't the dominant problem"? If the latter, the
  recommendation may be "drop the hard exclusion for a cheap safety win" rather
  than "this fixes a big leak."
- Oracle self-preference magnitude — unknown until a second judge runs.
- Precision cost of removing the gate — entirely unmeasured; could be the deciding
  factor.
- The eval ignores the **`select` stage** and **draft-vs-confirmed** surfacing that
  the live loop uses.

## Secrets / public-repo review (done before pushing)

Scanned all committed reflect-eval + `.auto/reflect/events` files. **No live
secrets**: the only `github_pat_…` strings are *truncated* illustrative examples
(`github_pat_11AB35...`) inside the playbook's own defensive credential-stripping
rules — not usable tokens. No AWS/private keys, no personal email/PII. Fixed one
hardcoded `/home/vscode` absolute path in `workflows/land_queries.py` (now
file-relative). Content caveat: the reflect data is a candid internal mistake-log
(security incidents, post-mortems) — not secret, and the source `feedback.md`
files were already in the repo, so nothing fundamentally new is exposed.

## Reproducibility / artifact map

Everything lives under `auto-reflect/experiments/retrieval-eval/`.

- **Code:** `src/retrieval_eval/baseline.py` (faithful port of match.go),
  `metrics.py` (recall@k, ndcg@k, mrr, excluded-relevant-rate, precision@k),
  `conditions.py` (per-condition guess/none/wrong analysis), `analyze_pilot.py`
  (coverage + domain-gate preview), `gocli.py`, `corpus.py`. **Phase 4:**
  `variants.py` (filter×scorer registry + IDF/BM25; `hard-gate` reproduces
  match.go), `stats.py` (cluster bootstrap + clustered Wilcoxon + Holm),
  `evaluate.py` (the bench runner; `--write` → `data/results/phase4.json`).
- **Conformance:** `conformance/` — `run_conformance.py`, `harness.py`,
  `fixtures.py`. Run `python conformance/run_conformance.py` or `pytest -m baseline`.
- **Method scripts (Claude Code Workflow `.mjs`, run with data injected):**
  `workflows/mine_queries.mjs`, `workflows/oracle.mjs`, `workflows/land_queries.py`
  (+ `workflows/README.md`).
- **Data (pinned, committed):** `data/corpus/rules.snapshot.json` (120 rules,
  commit cc5db4d) · `data/queries/queries.jsonl` (100 held-out) ·
  `data/qrels/qrels.jsonl` (the gold standard) + `qrels.conditions.json`. Each has
  a sibling `SNAPSHOT.md` / `QUERIES.md` / `QRELS.md` provenance file.
- **Re-run analyses:** `PYTHONPATH=src python -m retrieval_eval.conditions data/qrels/qrels.jsonl --write`
  and `... analyze_pilot data/qrels/qrels.jsonl`. **Phase-4 bench** (needs the
  `analysis` extra — numpy/scipy): `PYTHONPATH=src python -m retrieval_eval.evaluate --write`.

## Next steps (Phase 5 — confirm, then port)

The bench is built and frozen; variants now re-score for free
(`PYTHONPATH=src python -m retrieval_eval.evaluate --write`). What's left, in
order:

1. **Second-judge kappa pass (change #3 — still PENDING, now gating).** Phase 4
   found *no significant good-guess beat* of the gate; the recommendation rests on
   the wrong-guess robustness margin + the `bm25+idf-tag` point-estimate lead. A
   second judge (different model / human audit on the decision-changing queries,
   randomized rule order) is now the blocker before porting anything — it quantifies
   the single-oracle self-preference risk that could be inflating either side.
2. **Power for the scorer claim.** `bm25+idf-tag` is best on every slice but
   n.s. after Holm (53 clean queries / 31 sessions). Either accept it as
   *directional* and lean on robustness, or expand the held-out query set to get
   the power to confirm/deny the scorer lever.
3. **Port the winner into `match.go` (Phase 5).** Recommended minimal change:
   **`idf-tag`** — replace the hard domain-exclusion with an IDF-weighted,
   non-excluding in-domain boost (ties the gate on good guesses, kills the
   wrong-guess recall-0 cliff). Optionally also swap the substring scorer for BM25
   (`bm25+idf-tag`) if step 1–2 confirm it. Re-run conformance after.
4. **Validate via the live reflect feedback loop** — the real A/B (offline nDCG is
   a proxy for utility, IIR §8.6).

**Predicted outcome from the data so far:** `domain-as-boost` ≥ `hard-gate`
(keeps +7pt precision@5 / +10pt recall@10 on good guesses, removes the wrong-guess
recall-0 catastrophe); the larger win is a better scorer. _(Phase 4 only
**partly** confirmed this — see below: flat domain-boost is actually **worse** on
good guesses; only the IDF-weighted boost ties.)_

## Phase 4 results — the bench, and a prediction overturned

Six variants, factored as **filter strategy × scorer** so each result is
attributable to the one axis it moved (`src/retrieval_eval/variants.py`):

| variant | filter (the `domain_guess`) | scorer |
|---|---|---|
| `hard-gate` (baseline) | hard exclusion gate | lexical (+3 use_when, +1 tag) |
| `no-filter` | ignored | lexical |
| `domain-boost` | flat +3 in-domain boost, never excludes | lexical |
| `idf-tag` | IDF-weighted in-domain boost (down-weights `go`), never excludes | lexical |
| `bm25` | ignored | BM25 over use_when tokens |
| `bm25+idf-tag` | IDF-weighted boost | BM25 |

**Pre-registered** (fixed before looking): primary metric **nDCG@10** (graded,
and recall is already saturated so it can't discriminate); primary slice **clean
64**; primary condition **guess** (the realistic case); each variant vs
`hard-gate`; inference = **Wilcoxon on session-mean deltas** (clustered by
`source_session`, 31 sessions / 53 queries-with-relevant) + **cluster-bootstrap**
95% CI + **Holm** across the family. `data/results/phase4.json`.

**Primary (clean64 / guess / nDCG@10) — Δ vs hard-gate:**

| variant | Δ nDCG@10 | 95% CI | p (Holm) | rank-biserial | verdict |
|---|---|---|---|---|---|
| no-filter | **−0.064** | [−0.105,−0.032] | **0.000** | −1.00 | sig. **worse** |
| domain-boost | **−0.029** | [−0.052,−0.012] | **0.008** | −1.00 | sig. **worse** |
| idf-tag | +0.015 | [−0.030,+0.060] | 1.00 | +0.12 | ties |
| bm25 | −0.008 | [−0.072,+0.057] | 1.00 | −0.13 | ties |
| bm25+idf-tag | **+0.064** | [−0.006,+0.138] | 0.36 | +0.32 | best pt-est, **n.s.** |

**Robustness (clean64 / WRONG guess / nDCG@10) — Δ vs hard-gate:** every
non-gate variant beats the gate by **+0.19 to +0.31, all reject Holm** (gate
collapses to nDCG@10 = **0.000**; idf-tag 0.230, bm25+idf-tag 0.278, bm25 0.305).

Three findings, in order of importance:

1. **The hard gate is *better than the diary assumed* in the good-guess case, and
   the naive replacements are significantly worse.** This is the surprise that
   overturns the predicted outcome. `no-filter` (−0.064) and flat `domain-boost`
   (−0.029) are **significantly worse** than the gate on nDCG@10 (both survive
   Holm). The gate's *exclusion* does real ranking work that a non-excluding boost
   does not replicate: removing off-domain rules from the pool entirely is what
   lifts relevant rules into the top-10, and a flat boost that lifts *all*
   in-domain rules (relevant or not) just dilutes the slice. So "domain-as-boost ≥
   hard-gate" is **false for the flat boost**.
2. **The gate's *only* defect is the wrong-guess catastrophe — and the
   IDF-weighted boost fixes it for free.** `idf-tag` **ties** the gate under good
   guesses (Δ+0.015 / +0.003 on all100, n.s.) while turning the wrong-guess
   nDCG@10 from **0.000 → 0.230** (sig.). That is the actual, defensible win: a
   non-excluding IDF boost matches the gate's good-guess ranking and removes the
   recall-0 cliff. The *flavor* matters — IDF-weighted, not flat.
3. **The scorer is a real but conditional lever.** BM25 only clearly helps when
   there is *no* usable domain signal: in the `none` condition it lifts nDCG@10
   0.250→0.305 and cuts surfacing 107→85. Under a good guess, gate+lexical (0.314)
   already edges bm25-no-filter (0.305). The combined **`bm25+idf-tag` has the best
   point estimate in every slice** (clean 0.378, all100 0.433, flagged 0.516) and
   is wrong-guess-robust (0.278) — but is **not significant** vs the gate after
   Holm at this N (p_holm 0.36 / 0.24). It is the candidate to pursue, not yet a
   proven winner.

> **Decision shape (revised, data-backed on the full bench):** *Do not* drop the
> domain filter and *do not* use a flat boost — both lose good-guess ranking
> quality. The safe, recommendable change is **`idf-tag`**: an IDF-weighted,
> non-excluding domain boost. It ties the shipped gate when the guess is right and
> eliminates the wrong-guess recall-0 catastrophe. The larger prize is
> **`bm25+idf-tag`** (better scorer + idf boost), best on every slice and robust,
> but it needs more statistical power and a second judge before porting to
> `match.go`. The pre-registered primary did **not** find a significant beat of
> the gate on good guesses; the case for change rests on **robustness**, which is
> decisive and significant.

Caveats: single oracle (self-preference unquantified — second-judge pass still
pending); the `wrong` condition is disjoint-by-construction (worst case); BM25
params are stock (k1=1.5, b=0.75), deliberately untuned to avoid overfitting the
eval set; N is modest (53 clean queries / 31 sessions) so the directional bm25
results lack power. The scaffold applies hard-rule injection uniformly across all
variants, so it is held constant (not a confound).

## Status (current — all pushed to origin/main)

- **Phases 1–4 DONE.** Baseline conformance green (now 4/4: + the variant
  scaffold pinned to `match.go` on all 100 queries). 100 held-out queries. Full
  100-query golden qrels (89% coverage, mean 5.85 relevant/query, 585 labels).
  Variant bench run with session-clustered Wilcoxon + bootstrap CIs + Holm
  (`data/results/phase4.json`).
- **Headline:** no variant *significantly* beats the hard gate on good guesses
  (two are sig. worse); the gate's only real flaw is the wrong-guess recall-0
  collapse, which a **non-excluding IDF-weighted domain boost (`idf-tag`)** fixes
  while tying good-guess quality. `bm25+idf-tag` is best on every slice but n.s.
- **Phase 5 next:** second-judge kappa (now the gating blocker), then port
  `idf-tag` into `match.go`, then live-loop validation.
- **Session-clustered significance: DONE** (`stats.py`). Second-judge kappa: still
  PENDING.
- **Commit trail (session):** `cc5db4d` consolidate 266 obs→120 rules · `0d7db62`
  harness+conformance · `7aba6b5` phases 2-3 checkpoint+diary · `ef1f939` pilot
  extension · `a991e63` full golden qrels · `2161559` path fix (HEAD). Earlier:
  `60faf31` seed 266 observations. All on `origin/main`.
