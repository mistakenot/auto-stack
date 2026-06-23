---
name: playbook-refine
description: "Distils immutable observations into a small evergreen rule set in docs/reflection/rules.yaml, choosing ADD/UPDATE/MERGE/SPLIT/REMOVE/SKIP per observation. Use when 'refine rules', 'refine playbook', 'curate rules', 'distil observations', or after playbook-observe. Not for mining observations (use playbook-observe) or matching rules to a task (use playbook-search)."
---

# Playbook: Refine

A single dedicated agent reads `observations.yaml` and curates `rules.yaml`. An
observation is a point-in-time record; a rule is the deduplicated, generalized,
still-true instruction distilled from one or more observations. Refine writes
`rules.yaml` only.

## Reflection playbook — shared model

This skill is one stage of a three-stage reflection loop that turns evidence
from completed work into reusable, task-facing guidance. The three stages share
three source-of-truth files under `docs/reflection/`:

```text
docs/reflection/
├── observations.yaml  # immutable lessons mined from completed work + ingestion ledger
├── rules.yaml         # curated, evergreen guidance + refine cursor
└── retrievals.ndjson  # append-only rule-search telemetry
```

| Stage | Skill | Reads | Writes |
|---|---|---|---|
| Observe | `/playbook-observe` | `auto search` transcripts, task `feedback.md`, `observations.yaml` | `observations.yaml` |
| Refine | `/playbook-refine` | `observations.yaml`, `rules.yaml`, DuckDB over `retrievals.ndjson` | `rules.yaml` |
| Search | `/playbook-search` | `rules.yaml` | append-only `retrievals.ndjson` |

Each stage writes exactly one file. Never write a file another stage owns.

**Semantics differ on purpose:**

- An **observation** is evidence from a particular task/session that *may*
  justify changing guidance. Immutable once written.
- A **rule** is the current, deduplicated guidance distilled from observations.
- A **retrieval** is an operational event proving search *returned* a rule — not
  that the caller applied it or that it helped.

### Positioning (do not duplicate other stores)

- `learning-diary` / `docs/learnings.yaml` owns novel techniques and personal
  learning (git/PR/session mining). It does **not** feed this rule set. This
  playbook mines only **task session transcripts + `feedback.md`** — git/PR
  mining stays with `learning-diary`.
- `task-feedback-analyser` / `docs/rules.md` is the **predecessor**. After these
  three skills reach parity, retire it; never dual-write `docs/rules.md` and
  `rules.yaml`.
- `auto reflect` (`.auto/reflect/`) is a separate event store; its miner may help
  discover candidates but is **not** a canonical input here.

### Time-ordered IDs and atomic appends — use the shared helper

All IDs (observation IDs, retrieval `event_id`s) must be immutable and lexically
time-ordered: a microsecond UTC timestamp plus a monotonic per-process counter,
e.g. `20260623T123045.123456Z-0001`. A date or second-resolution timestamp plus
a random suffix is **insufficient** (a later record could sort before an earlier
one).

Do not hand-roll IDs or shell-redirect into the NDJSON log. Use the helper
shipped with this skill:

```bash
# Mint N strictly-increasing, time-ordered IDs (one per line):
python3 "$CLAUDE_SKILL_DIR/scripts/reflect.py" gen-id --count 3

# Atomically append events to retrievals.ndjson under an advisory file lock.
# Reads NDJSON (one object per line) or a single JSON array from stdin; missing
# event_id / occurred_at are stamped from a real clock. Safe under concurrency.
echo '<ndjson-or-json-array>' | python3 "$CLAUDE_SKILL_DIR/scripts/reflect.py" \
  append --file docs/reflection/retrievals.ndjson
```

`$CLAUDE_SKILL_DIR` is this skill's install directory (Codex: substitute the
directory containing this `SKILL.md`).

### Requirements and operating at scale

Runtime tools: `python3` (+ the bundled `reflect.py`), `yq` (mikefarah), `auto
search`, and `sha256sum`. `duckdb` is needed **only** for Refine's retrieval
analytics — if it is absent, skip analytics (it only *nominates* rules, it never
gates correctness). If `yq` is unavailable, `python3` with `pyyaml` is a working
fallback for YAML reads/writes (slower; it loads the whole file).

These stores are built to scale to thousands of records **without ever loading a
whole file into agent context**. Use the streaming tools, never read-all-then-
rewrite:

- **Append** in place (verified: appending to a 5,000-record / 1.3 MB file
  streams in <0.5 s):
  `yq -i '.observations += [ {…}, {…} ]' docs/reflection/observations.yaml`
- **Select** only the records you need by id (e.g. observations after a cursor):
  `yq '.observations[] | select(.id > "<cursor>")' docs/reflection/observations.yaml`
- **Match** rules cheaply by reading only `(id, read_when, lifecycle)` tuples
  (~20× smaller than the full file), then fetch `value`/`why` for the few
  selected ids:
  `yq -r '.rules[] | [.id, .read_when, .lifecycle] | @tsv' docs/reflection/rules.yaml`
- **Analyse** `retrievals.ndjson` with DuckDB, which queries the file directly
  (200k events aggregate in <0.2 s). Write any intermediate JSON to a non-hidden
  working path the tools can read.

Tooling caveat: a snap-packaged `yq` (the strict `home` interface) can only read
files under **non-hidden `$HOME`** paths — operate on the repo's
`docs/reflection/` paths, not `/tmp` or dot-directories like `~/.cache`.

## Inputs and outputs

- Reads `docs/reflection/observations.yaml`.
- Reads `docs/reflection/rules.yaml`.
- Queries `docs/reflection/retrievals.ndjson` (via DuckDB) for supporting
  analytics — never as the source of truth for rule validity.
- Writes `docs/reflection/rules.yaml` only.

Refine is **manually triggered**. Its cursor stores the complete ID of the
newest processed observation:

```yaml
# docs/reflection/rules.yaml
cursors:
  rules:
    last_processed_observation: 20260623T123045.123456Z-0001
rules:
  - id: prefer-get-value-in-react-prop-tests
    lifecycle: confirmed          # draft | confirmed | retired
    read_when: Writing a unit test that reads React props before rendering.
    value: Prefer using X ...      # evergreen, self-contained
    why: Using another method can cause ...
    sources: [20260623T123045.123456Z-0001]
    updated: "2026-06-23"
    predecessor_ids: []            # transitive retired ancestors that roll up here
    successor_ids: []              # on a retired rule: what replaced it
```

Because observation IDs are time-ordered, select every observation whose ID
sorts lexically **after** `last_processed_observation`, process oldest-first,
and advance the cursor only after a successful write. No new observations is a
no-op. At scale, fetch just the new slice with
`yq '.observations[] | select(.id > "<cursor>")'` rather than loading the whole
file, and read existing rules as `(id, read_when, lifecycle)` tuples (see
"operating at scale" above) to decide ADD vs UPDATE/MERGE.

## Per-observation action (pick exactly one)

- **ADD** — no existing rule covers it and the evidence threshold is met; author
  a new rule.
- **UPDATE** — it reinforces/sharpens/partially contradicts an existing rule;
  edit `value`/`why`/`read_when` and append the observation ID to `sources`.
- **MERGE** — several observations/rules describe one lesson; fold into one rule,
  widening `read_when` only enough to cover all valid triggers.
- **SPLIT** — a rule covers unrelated triggers; break into narrower rules so each
  `read_when` stays specific.
- **REMOVE** — evidence shows a rule is wrong/obsolete (e.g. the code it
  referenced is gone); retire it (do not delete) so historical retrievals stay
  interpretable.
- **SKIP** — too one-off / low-signal to generalize; advance the cursor without
  changing a rule. The observation stays in the immutable backlog for later
  clustering.

## Evidence threshold (separates early capture from trusted guidance)

- A normal ADD requires observations from **≥2 distinct sessions** and creates a
  `draft` rule.
- A single directly-evidenced **high-severity incident or explicit user
  correction** may create a `draft`, but it stays provisional.
- A rule becomes `confirmed` only with supporting observations from **≥3 distinct
  task IDs** (retains the predecessor `task-feedback-analyser` bar).
- A normal single observation that cannot UPDATE an existing rule is SKIPped for
  rule creation but remains in the backlog.

`playbook-search` returns `confirmed` rules by default; `draft` rules
are flagged and returned only on explicit request for provisional guidance.

## Rule quality bar

- `read_when` is load-bearing: short enough that reading many doesn't bloat
  context, specific enough to judge relevance without reading the full rule.
- `value` is evergreen and self-contained — must not assume access to the
  originating session.
- `why` explains the failure mode or tradeoff.
- `sources` lists **every** observation ID supporting the rule.
- `id` is immutable and readable. Records are **never deleted** from `rules.yaml`.
- `lifecycle` is `draft`, `confirmed`, or `retired`; search ignores `retired`.
- `predecessor_ids` on an active rule lists every transitive retired ancestor
  whose retrievals should roll up to it. `successor_ids` on a retired rule names
  the active/intermediate rules that replaced it (MERGE → one successor; SPLIT →
  several).

## Decay and analytics — nominate, never auto-remove

A rarely-retrieved rule may still guard a rare but severe failure mode. Age or
low retrieval count alone is **not** evidence a rule is wrong; removal requires
semantic evidence (contradiction or obsolescence). Use analytics only to
*nominate* rules for inspection.

Expose the rule array as JSON without loading it into context, then run DuckDB
over the NDJSON log:

```bash
yq -o=json '.rules' docs/reflection/rules.yaml > /tmp/reflection-rules.json
```

Per-rule retrieval totals and recency:

```sql
SELECT rule_id, count(*) AS retrieval_count,
       count(DISTINCT task_id) AS distinct_tasks,
       max(occurred_at) AS last_retrieved
FROM read_ndjson_auto('docs/reflection/retrievals.ndjson')
WHERE event_type = 'retrieval'
GROUP BY rule_id ORDER BY retrieval_count DESC;
```

Roll historical (merged/split) IDs into their active successors via
`predecessor_ids`:

DuckDB infers `JSON[]` for `predecessor_ids` when every rule's list is still
empty, which breaks `unnest`; cast to `VARCHAR[]` and guard the unnest so the
query is robust both before and after any MERGE/SPLIT lineage exists:

```sql
WITH active_rules AS (
  SELECT id, CAST(predecessor_ids AS VARCHAR[]) AS predecessor_ids
  FROM read_json_auto('/tmp/reflection-rules.json')
  WHERE lifecycle IN ('draft','confirmed')
), aliases AS (
  SELECT id AS active_rule_id, id AS historical_rule_id FROM active_rules
  UNION ALL
  SELECT id, unnest(predecessor_ids) FROM active_rules WHERE length(predecessor_ids) > 0
), retrievals AS (
  SELECT * FROM read_ndjson_auto('docs/reflection/retrievals.ndjson')
  WHERE event_type = 'retrieval'
)
SELECT coalesce(aliases.active_rule_id, retrievals.rule_id) AS active_rule_id,
       count(*) AS retrieval_count, max(retrievals.occurred_at) AS last_retrieved
FROM retrievals LEFT JOIN aliases
  ON aliases.historical_rule_id = retrievals.rule_id
GROUP BY 1 ORDER BY retrieval_count DESC;
```

DuckDB is an analytics layer, not the semantic matcher.

## Rules

- Write `rules.yaml` only. Never touch `observations.yaml` or `retrievals.ndjson`.
- One action per observation; advance the cursor only after a successful write.
- Never delete a rule record — retire it with `successor_ids` set.
- Confirmed needs ≥3 distinct task IDs; draft needs ≥2 distinct sessions (or one
  high-severity incident / user correction).
