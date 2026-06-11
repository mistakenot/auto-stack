# User Simulation: `auto reflect miner` queue API (task 023)
*2026-06-11 — 7 simulants — product: a deterministic CLI work-queue + priority scorer that hands a mining agent ranked, content-free session IDs*

**Diversity axis:** **Adoption lifecycle × Corpus scale** (user-chosen blend). The cohort runs evaluator → first-run → veteran re-miner → churning, crossed with corpus sizes from 50 to 10k sessions across many workspaces. This was deliberately the lens — it surfaces *where the designed contract creaks under real usage*, not just feature wishes. Personas treated the task-023 API as fully implemented exactly per spec and tried to do real jobs with it, so every wish below is a candidate **retrospective change to the designed API before implementation.**

> **Headline:** 4 of 7 simulants abandoned their session, 2 hit partial walls, and the one "success" (a by-the-book newcomer) finished with two unresolved unknowns. The abandonments cluster on a small number of contract gaps, not scattered nits — which is the strongest possible signal that the API needs revision before it's built.

## Cohort

| # | Name | Lifecycle × Scale | Job | Horizon | Temperament | Perturbation | Outcome |
|---|------|-------------------|-----|---------|-------------|--------------|---------|
| 1 | Dana | Evaluator × ~200 | Decide if `miner` beats her `mined.md` | today | skeptical | 90s before standup | **abandoned** |
| 2 | Sam | First-run × 50 | First end-to-end mining pass | today | by-the-book | first time, shaky model | success* |
| 3 | Priya | Veteran re-miner × 625 | Re-mine after v1→v2 bump | +3mo | tinkerer | daemon committing mid-session | **abandoned** |
| 4 | Marcus | Lead × 10k / 8 repos | Cross-repo coverage dashboard | +3mo | impatient | boss watching live demo | **abandoned** |
| 5 | Lena | Churning × ~300 | "Is mining paying off?" | +3mo | perfectionist | 2am, frustrated | **abandoned** |
| 6 | Wei | Adjacent-domain × 80 | Signals as an analytics dataset | +12mo | explorer | train, flaky wifi | partial |
| 7 | Ravi | Automation × 5k nightly | Headless cron next→mine→ack loop | +12mo | shortcut-seeker | no TTY, fully scripted | partial |

\* Sam reached the goal but only by guessing two undocumented bridges and left not understanding `miner_version`.

---

## Top recommendations

### #1 — Disambiguate empty / error / missing-data states; never report coverage over an absent source
**Need:** A user (human or machine) must be able to tell "0 pending, genuinely all mined" from "the data source is empty/missing/misconfigured." Today both collapse to `[]` / `coverage_pct: 100`.
**Convergence:** 4/7 — Dana, Sam, Ravi, Marcus
**Kano:** table-stakes (its absence directly caused the very first abandonment)
**Horizon:** today
**Evidence:**
- Dana: `miner status` → `"coverage_pct": 100, "total_sessions_in_scope": 0` while the ETL source was empty. *"It told me I was 100% covered while it couldn't read a single session."* The real `no etl output found` error only surfaced behind an invented `--verbose`. She abandoned in <90s.
- Sam: `miner next` → `[]` read as "did I break it?"; only `status` revealed the workspace filter.
- Ravi: had to **break the data source on purpose** (`AUTO_ETL_OUTPUT=/nonexistent`) to confirm errors go to stderr/exit 1 — the three-way (drained / work / broken) outcome works but is **undocumented**, so he couldn't trust it without probing.
- Marcus: repos with no event log report `coverage_pct: 0.0`, indistinguishable from genuinely-unmined repos.

**Designed-API changes:**
- `coverage_pct` must be `null` (or a `state: "no_data"` field) when `total_sessions == 0` — never `100`/`0` by division default.
- `next`/`status` must surface a source-missing/unreadable error on the **default** invocation (stderr, non-zero exit), not behind a verbose flag.
- Empty `[]` from `next` should carry a stderr hint distinguishing "0 for this workspace, try `--all`" from "source empty, run `auto etl run`."
- Document the `next` exit-code contract explicitly: `0`+empty = drained, `0`+items = work, non-zero = error.

### #2 — A failed/skip ack state, distinct from `--observations 0` (mined-but-empty)
**Need:** "I mined it and found nothing" and "I tried to mine it and failed (or it's noise I'm deliberately skipping)" are different facts. Collapsing both into `obs=0` either re-serves failures forever or permanently mislabels them as mined.
**Convergence:** 3/7 — Ravi (blocker), Priya, Lena
**Kano:** table-stakes for unattended use; performance for humans
**Horizon:** +12mo (automation), but the metric corruption bites today
**Evidence:**
- Ravi: `ack --status failed` rejected. With `--limit 1`, a failed mine that he refuses to ack → `next` re-serves the **same** top session → infinite loop. *"'failed' and 'found nothing' are the same word."* Forced an out-of-band sidecar skip-list, defeating the event-sourced single-source-of-truth pitch.
- Priya: acked daemon merge-conflict noise as `--observations 0`; it dragged `observations_per_session_mean` down — honest skips poison the headline quality metric.
- Lena: couldn't tell mined-but-empty from orphaned in any aggregate.

**Designed-API changes:** add `miner skip <id> --reason …` or `ack --status mined|empty|failed|skipped`. `failed` is retryable (reappears next run) but not counted as mined; `empty`/`skipped` are terminal-at-this-version but excluded from quality means. Keep them as distinct event sub-types on the same log (no new store).

### #3 — Re-mine transparency: flag re-mines in `next`, persist score+signals on the ack event, warn on version-bump resurface
**Need:** The v1→v2 baked-version re-mine is completely opaque. Users can't tell a re-mine from a first-timer, can't see what the prior score was (so can't justify re-mining), and automation gets its queue silently exploded by a routine binary upgrade.
**Convergence:** 4/7 — Priya (blocker), Lena, Ravi, Sam
**Kano:** table-stakes (drove Priya's abandonment) + performance
**Horizon:** +3mo
**Evidence:**
- Priya: `next` items have no `prior_ack`/`remined` field — she cross-referenced the event log per session to learn the #1 item was a v1 re-mine. The ack event stores `observation_count` but **not** `priority_score`/`signals`, so a v1↔v2 score diff is *structurally impossible* — the v1 score is gone forever. *"The version bump reopened all 625 sessions, but the one number that would justify re-mining any of them — what the score used to be — was thrown away at ack time."* Abandoned.
- Lena: coverage went **down** 40%→37.7% after the bump (41 of her sessions reset to pending); felt like punishment. No `--by-version` accounting to see if the re-mine was worth it.
- Ravi: a deploy bumping v1→v2 silently re-serves all 5000 sessions at full LLM spend with nothing in the log line to warn him; he had to build a version-guard in cron.
- Sam: `miner_version: 2` appeared with no in-CLI explanation; he feared it.

**Designed-API changes:**
- Add `prior_ack: {version, observations, ts}` (or `remined: true` + `times_mined: N`) to each `next` item.
- **Persist `priority_score` + the `signals` snapshot on the `session_mined` event** (cheap, and the data already exists at ack time). Enables score diffs and outcome analysis later.
- Add a resurface warning to `status` (`pending_due_to_version_bump: N`) and/or `next --warn-on-resurface`.
- Offer `next --remined-only` / `--prior-version N` to scope a re-mine to just the prior cohort instead of draining the whole reopened corpus.

### #4 — Read-only per-session signal access, decoupled from the work-queue filter
**Need:** The scorer computes a valuable per-session friction matrix, but it's *only* ever emitted through the queue lens — filtered by ack-state and subagent-status. Multiple users want signals for **arbitrary or all** sessions without mutating state.
**Convergence:** 4/7 — Wei (blocker), Priya, Marcus, Lena (with Dana wanting to inspect ranking = 5/7 touch it)
**Kano:** performance, tipping to delighter (unlocks a whole analytics use case the product wasn't built for)
**Horizon:** today → +12mo
**Evidence:**
- Wei: `next --limit 1000` returned 47 of 80 sessions — acked + subagent rows silently excluded. The richest sessions (the 15 already-mined = highest friction) are exactly the ones hidden. `miner signals <id>` didn't exist. *"It computes exactly the friction matrix I want — then hands it to me one un-mined column at a time and hides the worst rows behind an ack I'm not allowed to press."*
- Priya: wanted `miner describe <id>` (the repo's own noun+verb convention) for per-session ack-history + score; only a flat queue exists.
- Marcus: `status` knows the full population but emits only aggregates; he hand-rolled jq to get per-session rows.

**Designed-API changes:** add `miner signals <id...>` (or `miner describe <id>`) returning computed signals + ack history for any session regardless of state; and/or a `next --include-acked` / `miner dump` full-corpus read-only mode. This aligns with the repo's documented resource-oriented `list`/`describe`/`get` pattern, which the current 3-verb `next`/`ack`/`status` surface skips.

### #5 — Cross-repo coverage rollup; make `status` scope-consistent with `next`
**Need:** The queue (`next --all`) is org-aware, but coverage (`status`) is stubbornly per-repo and even rejects/ignores `--all`. Anyone managing more than one repo can't get a rollup.
**Convergence:** 3/7 — Marcus (blocker), Ravi, Wei
**Kano:** performance
**Horizon:** +3mo
**Evidence:**
- Marcus: `status --all` silently returned single-repo numbers with his boss watching; `--group-by workspace` errored; he ended up reading a `for`-loop + jq rollup aloud to leadership. *"The queue knows about all 8 repos but the coverage only knows about the one I'm standing in."* `next --all --limit 10000` dumped 148MB with no pagination and a 40s hang. Abandoned.
- Ravi: cron runs from `/`, so "current repo" scope is meaningless — he must remember `--all` every call or silently mine nothing; wanted an `AUTO_MINER_SCOPE=all` default.
- Wei: read `--all` as "all states," but it only means "all workspaces" — the overload confused him.

**Designed-API changes:** `status --all` must aggregate across workspaces **and** return a per-workspace breakdown array + an org total; add `miner coverage` (or `status --group-by workspace`); support a scope env default; add pagination/`--count-only` to `next --all` for scale; resolve the `--all` overload (it's accepted on `next`, rejected on `status` today — inconsistent).

---

## Wildcards
*One-off ideas worth a look, each preserved rather than averaged away.*

- **Outcome-validated scoring / mining-ROI view (Lena).** The deepest critique: `priority_score` predicts *friction*, never *"will this produce a rule that gets promoted."* Lena's tally — 154 mined → 271 observations → 244 orphaned (90%) → 4 rules — is what drove her out. *"Coverage went down and I have 4 rules to show for 154 sessions."* A `miner status` that surfaces orphan-rate and a mining→rules ROI line would be the single biggest **retention** lever. **Note:** outcome-aware scoring is explicitly *out of v1 scope* (spec: deterministic heuristics only) — but the **ROI visibility** (joining mining counts to rules shipped) is cheap and is the churn antidote. Worth a retrospective AC.
- **Source-of-truth unification (Dana).** `miner` reads raw ETL parquet at `~/.auto/etl/output`; `auto search` reads its own index — and they disagreed about whether 200 sessions existed. This maps directly onto task 023's **own open question** ("parquet reader strategy"): if the miner reads a source that the rest of the suite doesn't keep populated, every user hits Dana's wall. Consider a preflight/`doctor` check or reading the same source `auto search` already indexes.
- **Daemon/author noise filtering (Priya).** The autowatch daemon's own merge-conflict churn ranked #1 by score. A `--exclude-author` facet (or surfacing session author) keeps mechanical noise out of the queue — increasingly relevant as the dogfooding daemon generates sessions.
- **Tabular/CSV output + atomic streaming (Wei, Marcus).** No `--format csv` anywhere; a flaky connection mid-dump left a corrupt file (no cursor/atomic `--output`). Cheap to add, unlocks notebook + screenshot use.

## Friction log (designed contract already covers it, but it hurt)
*Quick wins — bugs/sharp edges in behavior the spec already specifies.*

1. **Duplicate entries in `next` output** (Lena: `sess_41a9`, `sess_2d77` each listed twice). The spec promises an ordered array; dupes erode trust instantly. Dedupe.
2. **Length-normalization over-rewards short noisy sessions** (Priya: 47-msg daemon session out-ranked a 412-msg refactor; Lena: 12-msg blip @0.66 outranked a 410-msg, 14-error marathon). A couple of corrections in a tiny session inflate the normalized density. Cap or floor on session length, or expose raw vs normalized score.
3. **Work item → fetch-command bridge missing** (Sam guessed `auto search session get`; tried `miner get` first). Add a `fetch_cmd` field or a hint line on each work item — the skill *and* humans need it.
4. **`--observations N` is self-reported and unvalidated** (Sam: "what N do I pass?"). Consider auto-deriving N from recorded `observe` events for that session, or warn on mismatch.
5. **`--since` accepted but silently ignored on `status`** (Lena) — accepting a flag and doing nothing is worse than rejecting it.
6. **Quickstart never mentions `miner`** (Sam — his entire job). Add `auto reflect miner quickstart` and a mention in the top-level quickstart.
7. **`signals` keys are unlabeled/unitless** (`length_norm`, `correction_density`) — Sam inferred meaning. Document units in `--help`.
8. **`miner_version` surfaces with no in-CLI explanation** (Sam, Lena) — add a one-line note or `--explain`.

## Churn signals
*Every abandonment, with the exact moment and cause.*

- **Dana — abandoned <90s in.** Moment: `status` reported `coverage_pct: 100` over an empty source; real error hidden behind a flag. Cause: the tool lied in the "relax, you're done" direction. → Rec #1.
- **Priya — abandoned mid-re-mine.** Moment: discovered the v1 score was never persisted on the ack, making the v2 re-score unverifiable, with no way to scope to her actual cohort. Cause: re-mine opacity. → Rec #3.
- **Marcus — abandoned during live demo.** Moment: `status --all` returned single-repo numbers; no rollup; 148MB dump hung 40s. Cause: per-repo coverage can't aggregate. → Rec #5.
- **Lena — abandoned (decided to stop using the tool).** Moment: tallied 90% orphaned observations / 4 rules / coverage going *backwards*. Cause: no payoff visibility; mining feels like a graveyard. → Wildcard (ROI) + Rec #3.
- **Wei — partial.** Got 47/80 rows but a sample biased away from the worst sessions; will likely re-derive from parquet directly. → Rec #4.
- **Ravi — partial.** Loop runs but propped up by three sidecar workarounds (flock, skip-list, version-guard). → Recs #1, #2, #3.

---

## Appendix: full traces

### Simulant report: Dana
**Persona:** Solo skeptical evaluator, tracks reviewed sessions in a hand-kept `mined.md`, wants a verdict before standup in 90 seconds.
**Outcome:** abandoned — `miner` reported `coverage_pct: 100` while its data source was empty, and only surfaced the real "no etl output found" error behind an invented `--verbose`; `auto search` (same suite) had all 200 sessions indexed.
Key turns: `miner next --limit 5` → `[]`; `miner status` → `coverage_pct:100, total_sessions_in_scope:0`; `--all` still 0; `next --all --verbose` [INVENTED] → `no etl output found`; `auto search session list` → 3 real sessions; `--source search` [INVENTED] → unknown flag. Gave up at the deadline.
Blocker wishes: distinguish "fully mined" from "source missing" (never 100% over empty); surface the error on default invocation; preflight/fallback to the source `auto search` uses.
Quote: *"It told me I was 100% covered while it couldn't read a single session — I trust my markdown file more than that."*

### Simulant report: Sam
**Persona:** By-the-book junior, first-run adopter, told to "start mining sessions," copies commands from docs, fears corrupting state.
**Outcome:** success* — completed one full `next → search session get → observe → ack` pass verified in `status`, but reached the goal by guessing two undocumented bridges and left not understanding `miner_version`.
Key turns: `quickstart` never mentioned `miner`; found it via `--help`; `next` → `[]` (panic) until `status` showed workspace filter; `miner get <id>` → unknown command; guessed `auto search session get` (worked); `ack --help` gave no guidance on what N is; recorded one `observe`; `ack --observations 1` appended `session_mined` @v2; verified exclusion + status math; `miner --version`/`status --explain` [INVENTED] → unknown.
Blocker/major wishes: quickstart must cover `miner`; work items should print the fetch command; empty `next` should explain the workspace filter; guidance on `--observations N`.
Quote: *"The empty list scared me more than an error would have — and the one number it forced me to type is the one thing nobody explained."*

### Simulant report: Priya
**Persona:** Veteran re-miner dogfooding `auto reflect`, re-opening 625 sessions after a v1→v2 bump while an autowatch daemon mutates the corpus.
**Outcome:** abandoned — reopened all v1 sessions with no way to tell re-mines from first-timers, no v1↔v2 score diff (v1 score never persisted), no way to scope to her cohort; fell back to re-mining 6 known IDs by hand.
Key turns: `status` → coverage cratered to 0.96%; `next` items show no re-mine status; `miner describe` [INVENTED] doesn't exist; `events list --session` confirmed #1 was a v1 re-mine (5 obs); ack event stores obs count but not score → diff impossible; daemon-fresh 47-msg session jumped to #1 via length-norm artifact; acked it `--observations 0` (poisoned the mean); `--remined-only`/`--max-version`/`--ids` [INVENTED] all absent; denominator moved 3× as the daemon ingested. Abandoned.
Blocker wishes: re-mine indicator on `next`; persist score+signals on ack + expose diff; scope queue to prior-version cohort or accept `--ids`.
Quote: *"The version bump reopened all 625 sessions, but the one number that would justify re-mining any of them — what the score used to be — was thrown away at ack time."*

### Simulant report: Marcus
**Persona:** Impatient eng lead over 8 repos / ~10k sessions, wants a cross-repo coverage dashboard live for his boss.
**Outcome:** abandoned — coverage is per-repo by design; `status --all` silently returned single-repo numbers (later rejected `--all` outright); `next --all` dumped 148MB with no pagination; built the rollup with a bash loop + jq instead.
Key turns: `status` (one repo) → 72.4%; `status --all` ignored the flag; `--group-by workspace` [INVENTED] errored; help confirmed "per-repository"; `next --all` IS org-wide (5 cwds) — asymmetric with coverage; `--limit 10000` → 40s hang, 148MB; `--count-only` [INVENTED] absent; `--workspace <path>` [INVENTED] absent (must cd into 8 worktrees); seven repos read 0% (untouched vs no-log ambiguous); `status --all --format table` [INVENTED] rejected. Computed org coverage ~13% on a napkin.
Blocker wishes: `status --all` aggregate + per-workspace breakdown; `--group-by workspace` / `miner coverage`; org-level coverage number.
Quote: *"The queue knows about all 8 repos but the coverage only knows about the one I'm standing in."*

### Simulant report: Lena
**Persona:** Perfectionist, one month in, cooling off — measures by visible ROI, mining at 2am to decide whether to quit.
**Outcome:** abandoned — got her honest answer (154 mined → 271 obs → 90% orphaned → 4 rules, 1 stuck in draft; coverage went *down* after a version bump) and stopped acking. The tool answered only by being fought.
Key turns: `status` → 37.7% (felt lower than last week); `--verbose` revealed 41 v1 sessions reset to pending by the bump; `status --show-rules` [INVENTED] absent (miner doesn't know rules); `rules list` → 4 rules from 14 cited sessions of 154 mined; `observations list --status orphaned --count` → 244/271 orphaned; `next` had duplicate entries + a 12-msg blip outranking a 410-msg marathon; a rule-producing session absent from the queue (no outcome→scorer loop); `--since 30d` ignored; `miner roi` / `status --by-version` [INVENTED] absent.
Blocker wishes: mining→rules ROI view; orphan rate in `status`; coverage that can't silently go backwards on a bump.
Quote: *"Coverage went down and I have 4 rules to show for 154 sessions — at 2am the tool finally told me the truth, but only because I fought it."*

### Simulant report: Wei
**Persona:** ML/data analyst repurposing the miner's per-session friction signals as an analytics dataset, without touching the playbook.
**Outcome:** partial — extracted a clean 47-row CSV without mutating state, but queue semantics structurally exclude the 15 already-mined (highest-friction) sessions and all 18 subagents' signal rows; no per-file or time-series cut.
Key turns: `next --limit 3` gave exactly the signal table he wanted; `--limit 1000` returned 47 of 80 (acked + subagents excluded); `--all` → 59, still filtered; `miner signals <id>` [INVENTED] absent; `auto search session list` → all 80 IDs but no bridge to signals; positional pinning + `miner export`/`--format csv` [INVENTED] absent; wifi drop corrupted the dump (no cursor/atomic output); `reflect stats --per-session` [INVENTED] redirected him back to the queue; `--include-subagents` gives IDs with no signals object.
Blocker wishes: read-only `miner signals <id>`; `--include-acked`/`miner dump` full-corpus pull; per-row subagent signals.
Quote: *"It computes exactly the friction matrix I want — then hands it to me one un-mined column at a time and hides the worst rows behind an ack I'm not allowed to press."*

### Simulant report: Ravi
**Persona:** Platform engineer wiring the miner into a headless nightly cron loop; shortcut-seeker, no TTY, checks `$?`.
**Outcome:** partial — loop runs, but no claim/lease, no failed/skip state, and an ambiguous empty-queue exit code mean it's robust only until something crashes mid-iteration.
Key turns: `next` emits clean JSON / stderr-separated; `--all` needed from cron's `/` cwd; empty queue = `[]`/exit 0 (same as success — must parse length); `AUTO_ETL_OUTPUT=/nonexistent` proved real errors → stderr + exit 1 (a usable but undocumented three-way); re-ack appends a 2nd event (not idempotent at event level, but mined-state stays correct); crash-before-ack = re-served not lost (good, but only single-consumer); `miner claim --ttl` [INVENTED] absent (fell back to flock); `ack --status failed` [INVENTED] absent → failures and empties collapse, with `--limit 1` causing an infinite re-serve loop; version bump silently re-serves 5000 sessions on a deploy → built a version-guard.
Blocker wish: distinct failed/skip ack state. Major: claim/lease; version-bump warning.
Quote: *"The queue's great until something dies mid-loop — and then I find out the hard way that 'failed' and 'found nothing' are the same word."*
