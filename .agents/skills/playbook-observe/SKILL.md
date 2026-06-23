---
name: playbook-observe
description: "Mines completed task runs (session transcripts + feedback.md) for immutable reflection observations into docs/reflection/observations.yaml. Use when 'observe', 'mine observations', 'reflect on tasks', 'gather lessons', or to feed the reflection rule set. Not for git/PR mining (use learning-diary) or distilling rules (use playbook-refine)."
---

# Playbook: Observe

Mine previous task runs for new **observations** — point-in-time evidence that
may justify changing guidance later. Observe writes `observations.yaml` only; it
never edits rules or retrievals.

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

## What an observation is

An ID-keyed, immutable record:

- `description`: a short sentence or two stating what was learned.
- `context`: background needed to understand when it applies.
- `git_commit`: the repository commit checked out when observed.
- `files`: optional relevant repository paths.
- `session_ids`: the Claude/Codex sessions providing raw process evidence.
- `task_id`: the task folder or other stable task identifier, when available.
- `sources`: concrete transcript messages / feedback artifacts supporting it.

Observation IDs come from the shared helper (`reflect.py gen-id`) so they are
immutable and lexically time-ordered.

## Sources of truth

Observe uses **both** when a task has them — they are complementary, not
independent corroboration:

1. **Session transcripts** via `auto search` — raw process evidence, dead ends,
   failures, corrections, sub-agent work.
2. **`docs/tasks/*/feedback.md`** — the curated retrospective.

Evidence from both that came from the *same task* counts as **one** task-level
instance when judging whether a lesson recurs.

Git/PR mining is out of scope — that stays with `learning-diary`.

### Prepare the index

```bash
auto search index            # refresh the index before querying
```

If expected sessions are absent, run `auto etl run` then `auto search index`
again. Scope discovery to this repository.

## Procedure

### 1. Read the ingestion ledger

Read `docs/reflection/observations.yaml`. If absent, this is the first run —
treat the ledger as empty and seed the structure below. The ledger prevents
re-processing:

```yaml
# docs/reflection/observations.yaml
cursors:
  sessions:
    last_scan_started_at: "2026-06-23T12:30:45Z"
    processed:
      - 6c71f534-8a37-4157-9ae2-cabe1ab541c9
  feedback:
    docs/tasks/004-ac-progressive-disclosure/feedback.md:
      content_sha256: 41b3d8...
      processed_at: "2026-06-23T12:30:45Z"
observations:
  - id: 20260623T123045.123456Z-0001
    description: >
      Prefer JSDOM getValue() to read values instead of ...
    context: >
      Writing a unit test that needs to read React prop state ...
    git_commit: 23e7a7b...
    files: [src/components/example.test.tsx]
    session_ids: [6c71f534-8a37-4157-9ae2-cabe1ab541c9]
    task_id: 004-ac-progressive-disclosure
    sources:
      - {kind: session_message, ref: 6c71f534-8a37-4157-9ae2-cabe1ab541c9-98}
      - {kind: feedback, ref: docs/tasks/004-ac-progressive-disclosure/feedback.md}
```

Do **not** collapse the cursor to a single `lastProcessed` name — a task has
many parent/sub-agent sessions, feedback can be edited, and follow-up work
arrives after newer tasks.

### 2. Candidate discovery

Capture `scan_started_at` now (UTC). Then:

**Sessions** — paginate oldest-first:

```bash
auto search session list --cwd <repo-root> --limit 50 --offset <offset>     # first run
# later runs add an overlap window so late ETL arrivals reappear safely:
auto search session list --cwd <repo-root> --limit 50 --offset <offset> \
  --after <last_scan_started_at minus 24h>
```

Increase `--offset` until a page is empty. Discard IDs already in
`cursors.sessions.processed`. Process parent and sub-agent sessions oldest-first
(`auto search session get <id>`; add `--subagent` / `--no-subagent` to scope).

**Feedback** — enumerate `docs/tasks/*/feedback.md` in lexical order, hash each
file, and select files whose path is absent from `cursors.feedback` **or** whose
`content_sha256` changed:

```bash
sha256sum docs/tasks/*/feedback.md
```

### 3. Mine with a team of agents

Dispatch a team of sub-agents over the candidate sessions/feedback (one agent
per task or small batch) to extract observations. Each agent returns candidate
observations as structured data; the coordinator does not need full transcripts
in its own context. Instruct each agent to:

- quote the concrete supporting message/feedback ref in `sources`;
- record the `git_commit`, `files`, `session_ids`, and `task_id`;
- prefer specific, reusable lessons over restating what the task did.

### 4. Write observations

For each accepted observation, mint an ID:

```bash
python3 "$CLAUDE_SKILL_DIR/scripts/reflect.py" gen-id --count <N>
```

Append the new immutable `observations[]` records **in place** with
`yq -i '.observations += [ … ]'` (see "operating at scale" above) — do not read
the whole file into context and rewrite it. **Then** update the ledger:
add every processed session ID to `cursors.sessions.processed`; for each
feedback file write its `content_sha256` + `processed_at`; advance
`cursors.sessions.last_scan_started_at` to this run's `scan_started_at`
**only after** every selected session and feedback file has been processed.

A session ID is processed once. A feedback file is re-processed only when its
content hash changes. Observation records are immutable after insertion — only
the ledger is mutable during observe.

Re-running with no new candidates is a no-op.

## Rules

- Write `observations.yaml` only. Never touch `rules.yaml` or `retrievals.ndjson`.
- Observations are immutable; never rewrite or renumber an existing one.
- IDs come from `reflect.py gen-id` — never hand-roll them.
- One task = one instance even when both a transcript and feedback support it.
- Hand off to `playbook-refine` to distil rules from observations.
