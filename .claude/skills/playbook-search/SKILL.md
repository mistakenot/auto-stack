---
name: playbook-search
description: "Retrieves task-relevant rules from docs/reflection/rules.yaml via a search sub-agent and logs each return to retrievals.ndjson. Use when 'search rules', 'playbook search', 'relevant rules for this task', or at the start of a planning/execution phase. Not for distilling rules (use playbook-refine) or mining observations (use playbook-observe)."
---

# Playbook: Search

A planning or executing agent delegates to a search **sub-agent** at the start of
a task or phase. The sub-agent returns only rules relevant to the current task,
so the parent's context is never polluted by the full rule set. Search appends to
`retrievals.ndjson` and reads `rules.yaml`; it never edits the other two files.

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

## Workflow hooks

The planning workflow invokes Search at these boundaries. After migration, Search
**replaces** direct reads of `docs/rules.md` (the `task-feedback-analyser`
pipeline) — callers must not query both rule stores.

| Caller | Phase | Intent sent to Search |
|---|---|---|
| `new-task` | `requirements` | user request plus repository context |
| `new-solution` | `solution` | approved requirements plus discovered constraints |
| `new-plan` | `planning` | approved solution and proposed file/phase structure |
| `execute-task` | `execution` | task plan, then each phase objective before dispatch |
| `review-task` / `code-review` | `review` | review target, acceptance criteria, changed surface |

`complete-task` keeps producing `feedback.md`; a later
`playbook-observe` run mines it.

## Procedure (three steps, no context bloat)

Capture a `search_id` for this invocation:

```bash
python3 "$CLAUDE_SKILL_DIR/scripts/reflect.py" gen-id   # -> search_id
```

1. **Read lightweight tuples only.** From `rules.yaml`, read just
   `(id, read_when, updated, lifecycle)` for each rule — never the full bodies
   (these tuples are ~20× smaller; see "operating at scale" above):
   `yq -r '.rules[] | [.id, .read_when, .updated, .lifecycle] | @tsv' docs/reflection/rules.yaml`
   If the file is large, shard those tuples across matcher sub-agents so no single
   matcher needs the whole file. Skip `retired` rules; skip `draft` rules unless
   the caller explicitly asked for provisional guidance.
2. **Match `read_when` against the current task.** Each matcher returns candidate
   rule IDs to the coordinator. Candidate rules examined during sharding are
   **not** retrievals.
3. **Fetch selected rules.** Read `value` and `why` only for the finally selected
   IDs and return a compact `(id, value, why)` list to the parent:
   `yq '.rules[] | select(.id == "<id>") | {"id": .id, "value": .value, "why": .why}' docs/reflection/rules.yaml`
   Use explicit `"key": .field` pairs. The bare-shorthand `{value, why}` is jq
   syntax and **fails** on mikefarah yq (`lexer: invalid input text`); the comma
   form `.value, .why` is also wrong — `,` is a top-level union, so `.why`
   evaluates against the document root and silently returns `null`.

If nothing matches, return an **explicit empty result** so the parent can
distinguish "no relevant guidance" from search failure.

## Telemetry — single serialized writer

Sharded matchers **never** write telemetry. The single coordinator for a
`search_id` collects results, builds all complete events, and appends them in one
locked call via the shared helper (advisory `flock`, complete lines, fsync):

- Write one `retrieval` event per rule **actually returned** to the parent.
- Write one `no_match` event when the search returned no rules — this is the
  denominator for measuring search coverage.

```bash
# Build events (one JSON object per line) and append atomically. event_id and
# occurred_at are stamped by the helper if omitted.
cat <<'NDJSON' | python3 "$CLAUDE_SKILL_DIR/scripts/reflect.py" \
    append --file docs/reflection/retrievals.ndjson
{"event_type":"retrieval","search_id":"<search_id>","rule_id":"prefer-get-value-in-react-prop-tests","rule_updated":"2026-06-01","task_id":"004-ac-progressive-disclosure","session_id":"abc123","phase":"planning"}
NDJSON
```

A `no_match` event sets `rule_id` and `rule_updated` to `null`.

### Event fields

- `event_id` — immutable, time-ordered (stamped by the helper).
- `event_type` — `retrieval` or `no_match`.
- `occurred_at` — UTC timestamp (stamped by the helper).
- `search_id` — groups every event from one search invocation.
- `rule_id` — stable rule ID, or `null` for `no_match`.
- `rule_updated` — revision date of the rule content returned.
- `task_id` — stable task identifier when available.
- `session_id` — the parent session that requested the search.
- `phase` — `requirements`, `solution`, `planning`, `execution`, or `review`.

Use `retrieval` / `retrieval_count` / `last_retrieved` terminology only. A
retrieval does **not** claim the rule was applied or helpful. If those states
later become reliably observable, add **new** event types rather than redefining
retrieval.

## Repository setup

`retrievals.ndjson` is durable telemetry — **track it in git**. Its sibling
`retrievals.ndjson.lock` is a transient advisory `flock` file recreated on every
write — **add it to `.gitignore`** so it never gets committed:

```gitignore
docs/reflection/retrievals.ndjson.lock
```

## Rules

- Append to `retrievals.ndjson` only (via `reflect.py append`). Never edit
  `rules.yaml` or `observations.yaml`.
- Only the coordinator writes; matchers return candidates, never log.
- One `retrieval` event per rule returned to the parent; one `no_match` per empty
  search. Never log candidates that weren't returned.
- Return an explicit empty result on no match — never silently return nothing.
