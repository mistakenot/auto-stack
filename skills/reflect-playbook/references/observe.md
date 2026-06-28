# Stage 1 · Observe — capture evidence-linked findings

An **observation** is point-in-time evidence from one session that *may* justify
changing guidance later. Cheap, append-only, immutable. Capture first; generalize
in Stage 2. Every observation needs **at least one evidence session id**.

## Add an observation

```bash
auto reflect observation add \
  --kind gap \
  --subject "no guidance on scoping go build to a module" \
  --evidence-session "$SID" \
  --evidence-message "$SID-98" \
  --evidence-quote "go build ./... rebuilt everything and was slow" \
  --domain go \
  --severity normal
```

Flags:

- `--kind` — `correction` | `pattern` | `gap` | `incident` (required). An
  observation of `--kind gap` is a **mined finding with a domain** (missing
  guidance you spotted in a transcript) — *not* the same thing as a **feedback
  gap** (the `gap` field on a feedback event, read via `auto reflect gap list`,
  which carries no domain). The usual flow is: read a feedback gap, then file it
  here as an `--kind gap` observation with the right `--domain`.
- `--subject` — what it's about (required).
- `--severity` — `normal` | `high` (default `normal`). **`high` = an incident
  that auto-bypasses the 2-session consolidation gate** — use sparingly.
- `--domain` — repeatable or comma-separated tags (`--domain go,cli`).
- `--context` — situational background.
- `--suggested-generalization` — a candidate rule this might become (optional;
  hint for Stage 2).
- `--task-id` — originating task, must match `^[0-9]{3}-[a-z0-9]+(?:-[a-z0-9]+)*$`
  (e.g. `049-reflect-audit-lineage-lint`). Empty is fine; a bad format is rejected.

## Evidence is positional — arrays pair by index

`--evidence-session` is required and repeatable. The other evidence flags pair to
it **by position** (1st session ↔ 1st file ↔ 1st commit …):

```bash
auto reflect observation add \
  --kind correction \
  --subject "swallowed error hid a real bug" \
  --evidence-session "$SID"   --evidence-file internal/cli/rule.go \
  --evidence-line-range 12-20 --evidence-commit "$(git rev-parse --short HEAD)" \
  --task-id 049-reflect-audit-lineage-lint \
  --domain go
```

Per-evidence provenance flags (all optional, all positional, all format-checked
**only when present** — empty stays valid):

- `--evidence-message` — transcript message id, format `{sessionId}-{index}` (paste
  from an `auto search` hit). Pins the observation to the exact moment.
- `--evidence-quote` — the supporting quote.
- `--evidence-file` — source file path.
- `--evidence-line-range` — a single line (`12`) or ascending span (`12-34`).
  `nonsense` or a descending span is rejected.
- `--evidence-commit` — a 7–40 char **lowercase-hex** git hash. `not-a-hash` is
  rejected. Use `git rev-parse --short HEAD`.

To cite several moments, repeat the paired flags in lockstep.

`observation add` returns the new id at top-level `.id` (== `.observation.observation_id`):
`OB=$(auto reflect observation add ... | jq -r '.id')` — thread it into `consolidate`'s
`observation_ids`.

## List / filter the backlog

```bash
auto reflect observation list --kind gap --domain go --since 14d --unconsolidated
#   --unconsolidated  only observations not yet folded into a rule (the Stage-2 backlog)
#   --kind / --domain / --since / --limit  filters
```

`auto reflect stats` reports `unconsolidated_observations` (the size of that
backlog) — when it's healthy, move to Stage 2.

## Good observations

- **Specific and reusable** — state the lesson, not what the task did.
- **Quote the evidence** — `--evidence-quote` / `--evidence-message` so a reviewer
  can verify without re-reading the whole session.
- **One finding per record.** Don't bundle three lessons into one subject.
- **Count evidence honestly.** A transcript and its `feedback.md` from the *same*
  task are **one** instance, not two — consolidation gates on *distinct sessions*.

## Rules

- Observations are immutable once written; never rewrite — add a new one.
- Always include ≥1 `--evidence-session`.
- Don't generalize here. A draft rule is Stage 2's job (`consolidate`), and it's
  gated on evidence you can only earn by observing widely first.
- After mining a session, record `miner ack` (Stage 0) so the cursor advances.

Next: **`references/consolidate.md`** to turn clusters into rules.
