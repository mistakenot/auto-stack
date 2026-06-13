---
hash: "38ab7d20"
id: "58fd2f1d"
read_when: "designing session-quality signals or scoring agent traces by feedback quality"
summary: "Distillation of the Effective Feedback Compute (EFC) scaling-law paper into a concrete scoring spec for auto-reflect, mapping the paper's deterministic gate tables to our parquet schema, and naming the success-label gap as the blocker."
title: "Research: Effective Feedback Compute (EFC) as a Session-Quality Signal"
---

# Effective Feedback Compute (EFC) as a Session-Quality Signal

**Source:** Zhang, Wang, Xu, Zhu, Che (Harbin Institute of Technology), *"Scaling Laws for Agent
Harnesses via Effective Feedback Compute."* arXiv:2605.29682v1, 28 May 2026.

**Why this doc exists:** `docs/signals.md` asks "how do we turn raw session data into signals of what
is working well or badly?" This paper answers a sharp, adjacent version of that question with a
formal, *implementable* metric. This note distills it down to (a) what we can build on our existing
data, (b) what blocks us, and (c) how it wires the stack together. It is a design input for
`auto-reflect` and `auto-search`, not a commitment to build.

---

## 0. Problem framing / mental model

Read this before the EFC mechanics — it's the *why*.

**The north star.** To know whether our coding harness is getting *better or worse* (v1 vs v2), we
need to **grade an execution trace**. Then an experiment-agent can A/B two harnesses on the same task,
or sweep a task suite, and optimise against the grade. The whole rest of this doc exists to make that
grade trustworthy and cheap.

**Why naive grading fails.** Tokens, wall time, and tool-call counts barely correlate with success
(the paper: `R² ≈ 0.33–0.42`). A harness that spends more isn't better; one that converts spend into
useful feedback is. So we grade *quality of feedback*, not *quantity of effort*.

**Two scoring layers — keep them distinct, and independent.**

1. **Turn score** — judge each turn (agent thinks → runs a tool → reacts to the result) as **helped /
   did nothing / made it worse**. This is the EFC per-event score and the classifier this doc is
   mostly about.
2. **Trace grade** — aggregate the turn scores into a single "did this trace go well" number.

The trace grade is *built from* turn scores, but **you cannot validate it against itself.** Validate
it **once** against an *independent* success signal (PR merged, tests green at end, user didn't
re-prompt — §6). After that one-time check, use the aggregate as the optimisation target without
hand-grading every trace. Skip it and the eval is circular — you'd just prove v2 is better at emitting
turns the scorer likes.

**Three caveats baked into the design:**

- **"Made it worse" is not in the paper's base score.** The 4-part product `I·V·R·M` floors at zero —
  it expresses "helped a lot" vs "did nothing" but cannot go negative. Regressions (the agent breaks
  what it just fixed) live in the **NRS gates / progress curve** (§7.3), which is *more* than the paper
  gives. Three-way turn scoring requires that layer.
- **The hard part is classification, not arithmetic.** Some turn outcomes are mechanical (`exit 0`);
  the load-bearing ones are *semantic* — "did this bash command actually do what it intended, even
  though it exited 0?" Exit codes are blind to that. The **three-tier labelling pipeline** (§8) exists
  to learn that judgement: exit codes (mechanical 80%) → Haiku (semantic 20% + routing) → human (gold +
  boundary). Explore/exploit on the human pass = uncertainty sampling (exploit) + reserved random
  coverage (explore, catches confident-but-wrong, §8.4).
- **Portable mechanism vs repo-specific calibration is a real fork.** We want a scorer whose *mechanism*
  generalises across languages/repos (§8.7) but whose *calibration* can encode what "good" means in a
  specific codebase. Resolution: **portable base model + per-repo calibration layer** — not one or the
  other.

**Two endgame risks to keep on the radar:** comparing across a *suite* of varying-difficulty tasks
needs `D_task` normalisation (§3.2) or hard tasks read as harness regressions; and once an agent
optimises against the score, **Goodhart** applies — keep the independent success label in the loop as a
periodic ground-truth check so the proxy never fully replaces the real outcome.

**One-line version:** score turns → aggregate into a trace grade → validate that grade *once* against
an independent success signal → optimise the harness against it, with the real outcome spot-checked to
stop gaming.

---

## 1. The one-sentence thesis

> Raw expenditure (tokens, tool calls, wall time, cost) barely predicts whether an agent succeeds.
> What predicts success is how much of that spend converts into **durable, task-sufficient
> feedback** — feedback that is informative, valid, non-redundant, and retained.

Quantitative backbone from the paper:

- Raw tokens / tool calls explain little: `R² = 0.33` / `0.42` in controlled scaling.
- EFC-based coordinates reach `R² = 0.94`; demand-normalized `Oracle-EFC/D_task` reaches `0.99`.
- **Matched-budget intervention (the load-bearing result):** holding token budget, tool-call budget,
  wall time, and cost *identical*, improving only feedback quality raised success from
  **0.27 → 0.90** (§4.2). This rules out "it just spent more compute."
- On mixed **real** traces, raw-compute coordinates have *near-zero or negative* `R²`, while
  `NRS-EFC/D_task` reaches `0.92` and holds at `0.85` on a prospective holdout (§6).

We should treat the exact numbers as approximate (single-source, future-dated preprint). The
*structure* is what we use.

---

## 2. The metric, precisely

### 2.1 Event-level EFC

A trajectory is segmented into feedback events `e_t`. Each gets four bounded factors in `[0,1]`:

| Factor | Meaning | What makes it high |
|---|---|---|
| **I** informativeness | reveals task-relevant info | new constraint, reduced uncertainty, diagnosed failure, subgoal progress |
| **V** validity | grounded in reliable evidence | a deterministic checker / test / execution result fired |
| **R** non-redundant relevance | advances an open subgoal without repeating | novel vs. prior events, avoids repeated error |
| **M** memory retention | changes plan/state/memory so it affects later actions | observation is later referenced / acted on |

Event score is a **product** (Eq. 5): `EFC_t = κ · I_t · V_t · R_t · M_t`, with `κ = 10`. Product
form = **bottleneck behavior**: feedback that is invalid *or* redundant *or* forgotten contributes
≈0 even if the other factors are high. Run-level EFC sums events.

### 2.2 The two derived quantities that matter for us

- **Harness efficiency** `η = EFC / C_raw` — effective feedback per unit raw budget. This is the
  "how good was the agent's *conversion*" number.
- **Task demand** `D_task = L · H_tool · S_state · (1 + N_obs) · (1 − V_oracle)` — how much feedback
  the task *requires*. Normalizing (`EFC / D_task`) is what makes scores comparable across tasks of
  different difficulty.
  - `L` = minimum reasoning/action steps
  - `H_tool` = tool-selection ambiguity
  - `S_state` = state that must be tracked
  - `N_obs` = observation noise/ambiguity
  - `V_oracle` = verifier-signal visibility (test/check coverage). The `(1 − V_oracle)` term means
    **well-tested tasks need less feedback to succeed.**

### 2.3 NRS-EFC — the real-trace variant we'd actually use

Real traces are noisy and repetitive, so the paper down-weights repeated/unstable feedback:

```
EFC_t^nr = ÊFC_t · Q_t · G_t^nr · Λ_t^nr · 1/(1 + 0.35·A_t)
```

where `A_t` is the attempt index for the same target. The `Q/G/Λ` gates (next section) are
**deterministic lookup tables over execution status** — no model training required.

---

## 3. Why this is buildable on data we already have

This is the crux. The paper's hard-to-transfer part is the calibrated regression `ÊFC_t =
max(0, exp(θ₀ + θᵀφ(e_t)) − 1)` (Eq. 8), whose `θ` weights were fit on *their* synthetic tasks. But
the part that does the heavy lifting on **real traces** — Appendix C.4, Eqs. 33–37 — is pure
deterministic lookup over execution outcome, and **our canonical schema already carries its inputs.**

### 3.1 Schema mapping (`auto-etl/internal/model/model.go`)

| Paper quantity | Eq. | Keys on | Our column(s) |
|---|---|---|---|
| Status-quality `Q_t` (validity proxy) | 33 | passed / assertion / runtime / timeout / API error | `BashExitCode`, `Interrupted`, parse `ToolUseResultJSON` |
| Progress gate `G_t` / `G_t^nr` | 34/36 | did severity *improve* vs. last attempt on same target | exit-code severity ordered by `Index` within a `ToolFilePath` / `BashCommand` group |
| Loop gate `Λ_t` / `Λ_t^nr` | 35/37 | repair event vs. generation event | `Edit` on existing file = repair; `Write` new file = generation |
| Redundancy penalty `1/(1+0.35·A_t)` | 11 | attempt index for same target | count of prior `Edit`s to same `ToolFilePath` in session |
| Memory/retention `z_t` (M proxy) | 7 | is a tool result later referenced | `ToolFilePath` + content reuse at a later `Index` |
| Repeated-error avoidance `a_t` (R proxy) | 7 | did the same error recur | repeated nonzero `BashExitCode` on same `BashCommand` |
| Event pairing (`e_t` structure) | §3.1 | tool_use ↔ tool_result linkage | `ToolUseID` (already set on both rows of the pair) |

The status-quality table is concrete enough to copy. From Eq. 33:

```
Q_t = 1.00  passed
      0.42  assertion error
      0.12  runtime error
      0.06  timeout
      0.04  static reject / missing entry point
      0.00  API error
      0.25  other
```

Mapping to our rows: `BashExitCode == 0` → pass bucket; `Interrupted == true` → timeout bucket
(`0.06`); nonzero exit + error class parsed from `ToolUseResultJSON` → assertion/runtime; failed
`Edit`/`Write` (bad `old_string`, file not found) → static-reject. The progress gate then compares
severity across attempts grouped by target, ordered by `Index`.

**Conclusion: session-level NRS-EFC is computable from data we already store**, using gate weights
taken verbatim from the paper, with no training step. The trained base estimator (Eq. 8) is an
*optional* refinement we can add later with our own calibration.

---

## 4. What this gives the stack

### 4.1 `docs/signals.md` gets its first concrete signal

NRS-EFC is the **formal version of the heat-map idea already in CLAUDE.md** ("files edited multiple
times during a workflow usually indicates a problem"). The paper supplies two things our raw
edit-count heat map lacks:

1. A principled discount for the Nth retry: `1/(1 + 0.35·A_t)`.
2. **The distinction raw edit-count cannot make:** 5 edits that *climb the severity ladder toward
   passing* (runtime error → assertion → pass) are **good** feedback (`G_t = 1.35` on the pass
   transition), while 5 edits stuck at the same failure are thrash (`G_t^nr = 0.16`, Eq. 36).
   Edit-count alone conflates these; EFC separates them.

So the first `auto-reflect` signal is not "this file was edited a lot" but "this session accumulated
little effective feedback per edit *while failing to climb severity*" — i.e. a **thrash detector**.

### 4.2 It explains *why* our existing CLAUDE.md disciplines work

The project rule *"run `go build ./...` after writing/modifying each Go file"* is, in EFC terms, a
**validity-injection mechanism**: it forces a deterministic checker to fire so each subsequent edit
is grounded (`V_t` high), instead of stacking unverified edits. The matched-budget result (§4.2) is
the theoretical evidence that this class of rule is high-leverage — it raises success *without*
spending more budget. We can go further and **measure** the EFC lift of build-after-edit across our
own corpus, turning a style rule into a quantified one.

### 4.3 `D_task` wires `auto-reflect` to `auto-graph` and `auto-doc`

`D_task` is not abstract for us:

- `H_tool`, `S_state` ← `auto-graph`'s import graph can estimate fan-out / state complexity of the
  files a session touched.
- `V_oracle` ← presence of tests / checks in the touched area (graph + repo scan).
- A low-EFC session in an **untested, high-fan-out** corner is high `D_task`, not necessarily a bad
  agent. Normalizing by `D_task` is what makes a *fair* cross-session quality signal.

### 4.4 Doc-read quality (`auto-doc`)

`docs/doc-file-usage-findings.md` already found docs are "seen constantly but read rarely." In EFC
terms most doc exposure is **zero-EFC**: cost paid, but neither informative nor retained (`M ≈ 0`). A
doc with high read-count but near-zero downstream retention is a freshness/quality target — a sharper
`autodoc stale` signal than mtime.

---

## 5. Where it does NOT cleanly transfer (read before building)

1. **We have no success label — this is the blocker.** EFC's entire validation is predicting
   `g_x(ŷ) ∈ {0,1}` (failure rate). Our `sessions` schema has **no success/failure label.** Without
   one, EFC is a *descriptive* signal ("this session thrashed"), not the *predictive* coordinate the
   paper validates. To claim the paper's actual result on our data we first need a **success proxy**,
   e.g.:
   - PR merged / branch landed after the session,
   - tests green at session end,
   - user did not immediately re-prompt with a correction.

   **This question should be settled before writing scorer code.** It determines whether we can
   validate the signal or only display it.

2. **Scale mismatch.** The paper studies short, single-goal, auto-checkable benchmark tasks
   (HumanEval / Terminal-Bench / SWE-bench Verified). Our traces are long, multi-goal, heterogeneous
   real sessions. Event segmentation `ℰ(τ)` is far cleaner in their setting; ours needs a
   subgoal-segmentation heuristic (e.g. per file-target, per build cycle).

3. **η is a harness×task interaction, not a global score.** §6.2: H5/H6 deep-closed-loop harnesses
   win on HumanEval (`η ≈ 1.9`) but lose to simpler harnesses on SWE; Terminal stays low-efficiency
   for everyone. **Lesson for `auto-reflect`: never emit a single global "agent quality" number.**
   Always stratify by task type / repo slice, or you punish agents for hard `D_task` and reward them
   for easy verification.

4. **Calibration is not free.** The base estimator's `θ` weights and the fitted `D_task` exponents
   were learned on the paper's tasks. The deterministic gates (§3) port directly; the regression does
   not. Start with gates only.

---

## 6. Manufacturing a success signal (resolves the §5.1 blocker)

The trap is looking for *one* signal: PR-merge is coarse and lagged, tests-green is missing on many
sessions, "user said thanks" is noisy. The right frame is **weak supervision** (Snorkel-style): write
many cheap, independently-wrong **labeling functions (LFs)**, each emitting `{success, fail, abstain}`
plus a confidence, then combine them into a *soft* label `s ∈ [0,1]` with a confidence. Sessions
where enough LFs agree become high-confidence labels; the rest abstain gracefully.

### 6.1 Candidate labeling functions (all computable from our schema)

| LF | Signal | Columns | Strength |
|---|---|---|---|
| `LF_test` | last test command in session exited 0 | `BashCommand` (match `go test`/`pytest`/`npm test`) + `BashExitCode` | **high** — agent's own final checker |
| `LF_build` | last build/vet exited 0 | `BashCommand` + `BashExitCode` | medium |
| `LF_pr` | session's branch maps to a *merged* PR | `GitBranch`, `GitRemote` + `gh` | **high** but lagged/coarse |
| `LF_revert` | a later session reverts/overwrites these edits | `ToolFilePath` cross-session lineage | high-negative |
| `LF_sentiment` | final user turns: approval vs correction / "undo" / "still broken" | `Content`, `Role` | low, always-available |
| `LF_no_reprompt` | user didn't reopen same workspace re-touching same files within N min | `Workspace`, `ToolFilePath`, `Timestamp` | medium-negative |
| `LF_clean_exit` | session ends on a passing check, not an error / `Interrupted` | terminal event `BashExitCode`, `Interrupted` | medium |
| `LF_selfreport` | agent's final message claims done **and** a checker confirms it | `Content` + a checker firing | medium (claim alone → abstain) |

### 6.2 Three rules that keep this non-naive

1. **Independence from EFC is mandatory.** The paper excludes the final success label from event-level
   factors (Appendix C.1) for a reason: if we validate EFC against a label *derived from the same
   severity-trend EFC scores*, the `R²` is circular and meaningless. So the **validation** label must
   lean on signals *outside* the feedback structure — `LF_pr`, `LF_sentiment`, `LF_revert`. The
   trace-internal LFs (`LF_test`, `LF_clean_exit`) are fine for display/ranking but must be
   down-weighted or excluded when measuring whether EFC *predicts* success.
2. **Graded, not binary.** A 90-minute session resolving 3 of 5 subgoals is not 0 or 1. The soft label
   is `s = resolved_subgoal_mass / total_subgoal_mass` — which is exactly what §7's progress curve
   produces at `t = T`. The binary LFs are just anchors.
3. **A small hand-labeled calibration set.** ~50–100 sessions labeled by the user, or auto-anchored
   (`PR merged ∧ tests pass` ≈ certain success; `reverted next session` ≈ certain fail), to (a)
   estimate each LF's precision and (b) learn the combination weights. Mirrors the paper calibrating
   its estimator on a held-out split before trusting it.

### 6.3 Unit caveat

A *session* may be the wrong unit — a **task episode** (sessions sharing `GitBranch` + `Workspace`) is
what actually "succeeds." Define success at the episode level and attribute it down to constituent
sessions.

### 6.4 SWE-bench as a gold calibration anchor

Our problem is that we have *no* clean `g_x(ŷ) ∈ {0,1}` — the §6.1 LFs *manufacture* one. SWE-bench is
the opposite extreme: a curated benchmark whose success label is exact and external. Studying how it
scores tells us both what a gold label looks like and how to validate our manufactured one.

**How SWE-bench scores** (per instance):

1. Each instance = a real GitHub issue + its merged PR, pinned to a `base_commit` in a per-instance
   **Docker image**. It ships two named test sets derived from the PR:
   - **`FAIL_TO_PASS`** — tests that fail at base and pass after the gold fix (verify the bug is fixed).
   - **`PASS_TO_PASS`** — tests that pass before and must still pass after (regression guard).
2. Apply the model's predicted patch (apply-failure → not resolved), then apply the gold `test_patch`
   so the *official* tests are present, then run the tests; a per-framework log parser maps output to
   per-test pass/fail.
3. **`resolved` iff every `FAIL_TO_PASS` test passes AND every `PASS_TO_PASS` test passes** —
   all-or-nothing, no partial credit. Score = `# resolved / total` (the "% Resolved" leaderboard
   number; *Verified* = the 500 human-confirmed-solvable subset).

**Why it matters for our design:**

- **`FAIL_TO_PASS` + `PASS_TO_PASS` is exactly our progress/regression split (§7.3).** "Did you fix it"
  *and* "did you avoid breaking things" — `FAIL_TO_PASS` going green is `P(t)` rising; a `PASS_TO_PASS`
  flipping red is the regression dip / `E(t)`. SWE-bench independently validates that a single
  "passed?" bit is insufficient; you need the regression guard too.
- **It is the `V_oracle ≈ 1`, pre-curated extreme our sessions are not.** Real sessions are multi-goal,
  have no curated test split, and often no checker at all — which is *why* the success label must be
  manufactured (§6.1) rather than read off a harness. `LF_test` is the closest cheap proxy for SWE-
  bench's exact criterion that we can compute on arbitrary sessions.
- **It is the cleanest one-time validation anchor (§0).** Running a handful of our harness's sessions
  *on* SWE-bench Verified instances yields ground-truth `resolved` bits — the independent success
  signal to check whether our aggregate turn-score actually predicts real success, before we trust it
  as an optimisation target. This is the gold end of the §8.2 three-tier hierarchy.

---

## 7. Progress curve from command reuse / near-reuse

EFC told us **repetition is ambiguous** — the same command run five times is either thrash or
convergence, and only the *outcome trend* disambiguates. Success and progress are the same problem at
two time-scales: **progress is the derivative, success (§6) is the integral.**

### 7.1 Define "sameness" — a command-signature normalization ladder

| Level | Definition | Example collapse |
|---|---|---|
| Exact | hash of `BashCommand` | — |
| Normalized | strip volatile args (paths/PIDs/timestamps), keep `(verb, target root)` | `go test ./auto-etl/internal/...` ≡ `go test ./auto-etl/...` |
| Verb-class | bucket verb: `{build, test, lint, run, search, edit, read, git}` | `go build` ≡ `go vet` (both *verify* same module) |
| Target | `ToolFilePath` / package root, regardless of command | repeated edits to one file |
| Embedding | cosine-cluster command strings (`auto-search` already embeds blobs) | fuzzy near-reuse normalization misses |

The workhorse is `sig(event) = (verb_class, normalized_target)`. Group events by signature, order by
`Index` → "the agent's repeated attempts at goal X."

### 7.2 Reuse is meaningless until the outcome trend is attached

For each signature group, read the sequence of status-quality `Q_t` (Eq. 33 over
`BashExitCode` / `Interrupted` / `ToolUseResultJSON`). The *shape* is the signal:

- **Convergence (good):** `Q` climbs `runtime-error → assertion → pass`, then the signature
  *disappears* (resolved, never re-run). Strongest positive: a test red→green that stays gone.
- **Thrash (bad):** same signature, same failing `Q`, attempt count `A_t` rising, contributing nothing
  — the `1/(1+0.35·A_t)` decay.
- **Oscillation (bad):** `pass→fail→pass`, the agent breaks what it fixed (Eq. 34's
  `sev(s_t) < sev(s_{t-1}) → G_t = 0.45` regression case).

### 7.3 Two curves — and their *relationship* is the marker

Do **not** build one monotone score (cumulative tokens/messages rise trivially and mean nothing).
Build two curves by walking events in `Index` order:

- **Progress `P(t)`** — cumulative *resolved-subgoal mass*. Rises when a signature's `Q` improves
  (weighted by novelty), **dips** when a resolved signature regresses. Honest: a session that breaks
  things shows a visible dip.
- **Thrash `E(t)`** — cumulative *redundant-retry mass*: repetition that produced no `Q` gain,
  weighted by `A_t`.

```text
state: bestQ[sig]={}, attempts[sig]={}, P=0, E=0
for event in session ordered by Index:
    sig = signature(event); Q = status_quality(event)
    A = attempts[sig]++; novelty = 1 if A==0 else 1/(1+0.35*A)
    qprev = bestQ.get(sig, 0)
    if   Q > qprev:  P += (Q - qprev) * novelty      # convergence (novel climbs worth more)
    elif Q < qprev:  P += (Q - qprev)                # regression, full penalty, no decay
    if Q <= qprev and A > 0:  E += (1 - novelty)     # thrash mass
    bestQ[sig] = max(qprev, Q)
    emit(index, ts, P, E, sig, A, Q)
```

Normalize the endpoint by `D_task` (or discovered subgoal count) so `P(T)/D_task` ≈ "fraction of
required feedback actually banked" — which **is** the graded success label from §6.2. The loop closes
on itself.

The **shape diagnosis** is what's new (the paper is retrospective/aggregate; this is per-session and
streamable):

| Pattern | Meaning |
|---|---|
| P rises, E flat | smooth progress — healthy |
| **P flat, E rises** | **stuck / thrash — the alert condition** |
| P rises then falls, E rises | regression spiral |
| both flat after early rise | done, or abandoned |

### 7.4 Near-reuse as a *leading* indicator

Track **signature-novelty rate** in a sliding window = `new_signatures / events`. Early session: high
(exploration). Healthy: signatures resolve and don't recur. Stuck: novelty → 0 while the same few
signatures keep firing. Falling novelty + flat `P` predicts thrash *before* `E` blows up — this maps
to EFC's `R` (non-redundant relevance) / novelty term `n_t`, and is the signal **`auto-watch` could
compute streaming to interrupt a stuck agent live.**

### 7.5 Pitfalls to handle up front

- **Reads/exploration have no `Q`.** Score a `Read` only via retention: did a later edit/command touch
  the same `ToolFilePath`? Read→edit = productive (progress credit); read never acted on = wasted.
  (This is EFC's `M` factor doing real work.)
- **Nonzero exit ≠ failure** for some verbs (`grep` exit 1 = no match). Need per-verb-class success
  semantics, not blanket `exit == 0`.
- **Subagents** (`IsSubagent` / `ParentSessionID`): roll their progress into the parent at the dispatch
  point, or you undercount.

### 7.6 The closed loop

Command-signature + outcome-trend gives the **progress curve**; the curve's endpoint, validated
against an *independent* weak-supervision success label (§6), is how we reproduce the paper's central
claim on our own data — and the curve's live *shape* is a thrash detector `auto-watch` can act on.

---

## 8. Portability and the labeling pipeline

§3's status-quality gates are partly **language/tool-specific** — the `go test`/`pytest` verb
matching and the assertion-vs-runtime regex over `ToolUseResultJSON` rot when a new ecosystem appears.
For the mechanism to work across codebases and languages, that one piece must become learned rather
than hand-coded. The other three EFC factors do **not** need this: `R` (signature reuse), `M`
(cross-event reference), and `I` (deferred) are *structural* and already language-agnostic. So the ML
scope is narrow: **classify a single tool output into an ordinal outcome-quality (the `Q_t`/`V_t`
axis); everything else in §3/§7 stays as-is and ports for free.**

### 8.1 The target

An **ordinal** outcome score, not a nominal class: `{hard-fail < soft-fail < no-signal < partial <
success}`. One ordinal gives both `Q_t` (the value) and `G_t` (compare consecutive attempts on a
signature), which is all §7's progress curve needs. Collecting a finer 0–9 scale is fine, but model it
as ~5 **anchored** bands (each with a one-line definition shown to labelers) — humans are unreliable on
fine absolute scales, so the 6-vs-7 distinction is noise.

### 8.2 A three-tier labeling hierarchy

The labels that train the classifier come from three sources, each calibrating the one below it:

| Tier | Source | Volume | Cost | Trust | Role |
|---|---|---|---|---|---|
| 1 | exit codes (distant supervision) | millions | free | noisy | bulk training signal |
| 2 | LLM-as-judge (Haiku) | thousands | cheap | consistent-ish, has biases | label the no-exit-code / ambiguous slice |
| 3 | human (labeling CLI) | hundreds | ~30 min | **ground truth** | gold eval + calibration + boundary |

Tier 1 mines every `tool_result` row with `BashExitCode` set: `exit 0` on a recognized verb →
success, `!= 0` → failure, `Interrupted` → timeout. Our corpus (6 months, many languages) yields
millions of free weak labels — but it is **noisy**: lint warnings exit nonzero without being failures,
some tools always exit 0, flaky tests. The exit code is a *teacher*; the text classifier is the
*student* that generalizes to outputs with no/unreliable exit code (Edit/Read/Write). This mirrors the
paper's own Oracle-EFC → Estimated-EFC calibration structure.

### 8.3 The combined Haiku → human loop

Haiku and the human do **different jobs**: Haiku gives scale and a pre-label; the human adjudicates the
boundary. Route only the cases that matter to the human.

```text
Tier 1  exit-code weak label        → everything (free)
Tier 2  Haiku, self-consistency ×3  → ambiguous / no-exit-code slice (cheap)
route → human  iff  high Haiku variance  OR  Haiku ≠ exit-code  OR  near class boundary  OR  rare class
Tier 3  human corroborate / rescore → only the routed ~hundreds (~30 min)
```

**Getting "uncertain" out of an LLM** (it gives no calibrated probability):

1. **Self-consistency variance** *(primary):* score each output 3–5× at temperature; high score
   variance → route to human.
2. **Disagreement triangulation** *(strongest router):* exit-code label vs Haiku (vs the small model
   later). Where they agree, trust it; where they disagree, route to human. The human is the tiebreaker
   only at genuine three-way uncertainty.
3. **Boundary proximity:** Haiku's score on a class edge → route.
4. Self-reported confidence / token logprobs — weak tiebreaker only; poorly calibrated.

Routing score = **self-consistency variance × disagreement-with-exit-code**.

### 8.4 The one trap: confident-but-wrong

**Do not send the human *only* the uncertain cases.** The dangerous LLM failure mode is
*confident-but-wrong* — judges have systematic blind spots they are sure about. If the human never
sees Haiku's confident outputs, those blind spots go uncaught and the measured accuracy is inflated.
Split the human budget **~70% disagreement/uncertain** (active learning — improve the model) + **~30%
reserved random/confident** (unbiased eval — catch confident-wrong, get an honest accuracy number).
The random slice is non-negotiable.

### 8.5 Labeling CLI — design notes

- **Smart sampling** (the leverage): disagreement sampling (exit-code ≠ model) > uncertainty sampling
  > diversity/coverage (embedding-cluster, stratify by `tool_name`/ecosystem so the gold set supports
  leave-one-ecosystem-out eval) > rare-class oversampling (timeouts, api-errors, partial — rare in
  random draws, important). A human grading confident/easy cases learns nothing; the disagreement
  boundary is worth ~10× per label.
- **Context shown (one screen):** `tool_input`/command, a snippet of the **preceding agent message**
  (intent — `V_t` is meaningless without it), output **head + tail** (truncation eats the verdict),
  `exit_code`/`duration`.
- **Two modes:** *blind* for the random eval slice (hide the machine guess → unbiased); *assisted* for
  refinement (show Haiku's score + reasoning, confirm/override → fast, and every override is a labeled
  "Haiku error").
- **Throughput:** ~6–10 s/label including reading context → ~180–300 labels in 30 min. Enough for gold
  eval + calibration + active-learning seed; **not** enough to train a model from scratch (that's
  Tier 1's job).
- Lives where the embeddings/sampling already are (`auto-search`) or as `autoreflect label`.

### 8.6 The flywheel and runtime story

Each round of human corrections does double duty: (1) **few-shot Haiku** with the corrected boundary
cases (or fit a monotonic calibration map for a consistent bias) → Haiku improves on its weak spots →
fewer cases route to the human next round; (2) **distill** the stack (exit codes + Haiku + human) into
a small classifier for runtime scoring. The human's role asymptotes toward pure audit.

**Runtime model** (replaces §3's hand gates): start **lexical + structural features → logistic
regression or gradient-boosted trees** — token-presence flags as *features not decisions*, exit code,
line count, has-stack-trace shape, `DurationMs`, `tool_name` one-hot. Pure-Go inference, ships as a
small model file, zero runtime deps (consistent with the minimal-deps preference). Escalate to an
embedding + linear head only if lexical underperforms. **Haiku is build-time only** — it labels
training data, never runs in the hot path — so the portability / zero-runtime-dep goal holds.
**Do not hardcode verbs** in the model; it must classify from output *shape*, or the brittleness has
just moved inside the model.

### 8.7 Proving portability

Random-split accuracy lies (dominated by majority languages). The honest test is
**leave-one-ecosystem-out**: train on Go/Python/shell, hold out *all* of Rust/cargo (or JS/npm),
measure transfer. The real axis of variation is **tool/ecosystem diversity, not human language** (tool
output is English regardless), so hold out ecosystems, not natural languages.

---

## 9. Proposed next steps

1. **Resolve the success-proxy question** (§5.1) — decide what "this session succeeded" means in our
   data. Cheapest viable: PR-merged + tests-green-at-end. Everything else depends on this.
2. **Tech-spike a gate-only NRS-EFC scorer** over a handful of real sessions from
   `~/.auto/etl/output`, using only §3's deterministic tables. Question to answer: *does the thrash
   signal visibly separate known-good from known-bad sessions in our corpus?*
3. If the spike separates signal from noise, design the `auto-reflect` surface: per-session NRS-EFC,
   per-file `η`, stratified by `D_task` slice — never a single global score (§5.3).
4. **Build the labeling pipeline** (§8) to replace §3's hand gates with a learned output classifier:
   mine exit-code weak labels from the corpus (Tier 1), add a Haiku pre-labeler with self-consistency
   routing (Tier 2), and ship a labeling CLI for the human boundary slice (Tier 3). Validate with
   leave-one-ecosystem-out (§8.7).
5. Stretch: estimate `D_task` from `auto-graph` (§4.3) and measure the EFC lift of the
   build-after-edit discipline (§4.2) as the first quantified workflow rule.

## 10. Glossary

- **EFC** — Effective Feedback Compute. Sum of `κ·I·V·R·M` over feedback events.
- **NRS-EFC** — Non-Redundant Stable EFC. Real-trace variant with status/progress/loop gates and a
  retry penalty. The variant we'd compute.
- **η (harness efficiency)** — `EFC / C_raw`. Conversion of raw budget into effective feedback.
- **D_task** — task demand; the feedback a task requires. Normalizer for cross-task comparison.
- **V_oracle** — verifier-signal visibility (test/check coverage). Lowers `D_task`.
- **Matched-budget** — controlling token/tool/time/cost identical and varying only feedback quality;
  the experiment that proves spend isn't the driver.

## Related project docs

- `docs/signals.md` — the open question this note feeds into.
- `docs/doc-file-usage-findings.md` — doc-read-as-zero-EFC (§4.4).
- `CLAUDE.md` — heat-map idea (§4.1), build-after-edit discipline (§4.2).
- `auto-etl/internal/model/model.go` — canonical schema mapped in §3.1.
