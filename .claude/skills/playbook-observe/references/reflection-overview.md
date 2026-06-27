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
