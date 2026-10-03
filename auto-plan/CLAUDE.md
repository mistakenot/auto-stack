# auto-plan

Structured plan graphs. Ships as `auto plan` in the unified binary.

A Plan is one deterministic `graph.json` under `.auto/plan/plans/NNN-name/`: typed
nodes and typed edges, validated on every write. It is the only source of
truth for the plan; git is its history.

## Vocabulary

Canonical terms live in `docs/concepts/UBIQUITOUS_LANGUAGE.md` (§ Planning):
Plan, Child Plan, Goal, Acceptance Criterion, Decision, Alternative, Rail,
Defect, Stage, File Change, Journey, Leg, Question, Tree, Stage Brief. Every
registered node type names its term (`NodeType.Term`), and
`internal/schema/registry_test.go` fails if the glossary lacks it.

Don't call a plan a *task* (that is only its `kind`), a Stage a *phase*, or a
Defect a *finding*.

## Command surface

Every command reads `auto plan <verb> <plan> [args…]`. `<plan>` is `NNN`, the
plan ID `NNN-xxxx`, the folder `NNN-name`, a path, or `all` where it makes
sense; a bare `NNN` that two folders share fails with `ambiguous-plan`. Output
is JSON on stdout by default and text with `--text`; diagnostics go to stderr as
`{"errors":[{code,path,field,message,value}],"hint":…}` with exit 1.
`auto plan quickstart` prints the happy path; `auto plan docs` prints the full
reference, generated from the registry.

```bash
# what plans exist?
auto plan list --text                            # one row per plan: ID, name, kind, lifecycle, frozen, epic

# create
auto plan init                                   # create .auto/plan/plans/ + AGENTS.md, CLAUDE.md symlink (idempotent)
auto plan new demo --kind task                   # .auto/plan/plans/001-demo/graph.json, plan ID 001-k7q2
auto plan new walking-skeleton --kind task --epic 001   # a child plan of epic 001

# write (Decode → mutate → Validate → Save; refused on any error)
auto plan add 001 goal --title "…"               # flags are generated from the registry
auto plan add 001 ac --proves g-k7q2 --title "…" --gwt @ac.md --verify-cmd "go test ./…" --layer e2e
auto plan add 001 annex --kind usage --path using-brief.md --title "…" --about g-k7q2   # also writes a create-only stub .md; `file` names it
auto plan link 001 d-9t2w constrains g-k7q2      # endpoint types are checked; returns the edge ID
auto plan unlink 001 e-7k2q                      # by edge ID, or: unlink 001 d-9t2w constrains g-k7q2
auto plan update 001 g-k7q2 --description "…"    # same field flags as add; plan --lifecycle advances
auto plan retire 001 a-4hn8                      # status retired; ID and edges kept
auto plan move 001 g-k7q2 --before g-3m1x        # rewrites one rank, never an ID
auto plan renumber 004-m3x9 [--to NNN]           # after a merge collision: new folder number + every reference

# check
auto plan lint 001                               # or: auto plan lint all (exit 1 on any error)
auto plan fmt all --check                        # canonical bytes? (without --check: rewrite)

# read (resource verbs: cheap → full fidelity)
auto plan list 001 --type ac                     # a plan's nodes: IDs + metadata + titles; `list all` spans plans
auto plan describe 001 ac-3fxm                   # fields cut to ~200 chars + the get command
auto plan get 001 ac-3fxm                        # the node in full, every edge in and out
auto plan search all "ack"                       # case-insensitive over IDs and text

# display lenses
auto plan show 001 --text                        # the goal ladder (epics: journeys, children, rails)
auto plan trace 002 ac-3fxm --up --text          # lineage; --down; crosses plans
auto plan tree 001 t-8p1d --text                 # a tree node in show-me notation
auto plan tree 001 --files [--stage s-2m6c] --text   # the file tree derived from file nodes
auto plan brief 001 s-2m6c --text                # a Stage Brief: everything one stage needs
```

**The plans folder.** `init` and `new` both call `workspace.EnsureScaffold`:
it creates `.auto/plan/plans/`, a short `.auto/plan/plans/AGENTS.md` (the folders are plans
managed by `auto plan`; use the CLI, not hand edits) and `.auto/plan/plans/CLAUDE.md`
as a relative symlink to `AGENTS.md` (a copy, with a stderr note, where
symlinks fail). Each is created only when missing: an existing AGENTS.md is
never overwritten, and an existing CLAUDE.md of any form is left alone (stderr
note when it is not that symlink; exit 0). The result's `scaffolded` lists the
files a run created and is absent when none. The plan scan only reads
`NNN-name` folders, so these files are invisible to it. Plans are tool-owned
data under `.auto/plan/` (the `.auto/<tool>/` convention), outside `docs/`, so
auto doc never scans them and needs no ignore.

**Plan IDs.** `new` gives every plan an ID `NNN-xxxx` (folder number + 4
random Crockford base32 characters, the same generator as node IDs), stored as
the top-level `id` of graph.json. Everything that names another plan stores
that ID: qualified references `004-k7q2:r-8hw3` (edge targets and `[[…]]`
prose), `plan.epic`, `child.plan` and `rail.deferred` (the registry's `PlanRef`
fields). On input the CLI also takes the shorthand `004:r-8hw3` / `--epic 004`
/ `--plan 004` / `--deferred 004` and expands it before saving when exactly one
plan has the number (else `ambiguous-plan`, or the not-found error). Prose may
keep a hand-written `[[004:id]]`; lint resolves it, or reports `ambiguous-ref`.
`PlanSet` indexes plans by their graph.json `id`. Lint reports
`plan-id-mismatch` when the ID's number is not the folder's.

**Merge collisions.** Two branches that each create plan 004 merge cleanly in
git; `lint all` (any lint in the set) then reports `duplicate-plan-number` on
both, naming each plan ID. `auto plan renumber <plan> [--to NNN]` (default: the
next free number) renames the folder, rewrites the ID's number (the suffix is
kept) and rewrites every reference to the old ID in every writable plan. It
refuses with `frozen-ref`, changing nothing, when a frozen plan references it.
Every new graph is encoded and validated first and staged as a temp file;
then the temp files are renamed into place and the folder renamed last, with a
best-effort rollback on a rename failure. Several renames are not one atomic
step: a crash in that window can leave some plans rewritten (`lint all` shows
what remains).

**Epics and cross-plan references.** An epic (`--kind epic`) holds goals,
rails, journeys with legs, decisions and `child` nodes. A child plan is created
with `--epic NNN`, and the epic lists it with `add NNN child --plan <child>`
(both halves are required, or lint reports `child-epic-mismatch`). A qualified
reference names a node in another plan, and only cross-plan edge types accept
one: `honors`, `delivers`, `builds-on` (from the child's `plan` node),
`discharges` (AC → rail) and `wouldBreak`. From `lifecycle: plan` an epic
requires every rail honoured by a child (or `--deferred <child>`) and every leg
delivered by one. Hierarchy is epic → task only; nothing in the ID scheme
depends on that.

**Annexes.** An `annex` (prefix `ax`) is a Markdown file beside `graph.json`,
registered as a node, holding plan-time exposition whose *facts* belong in the
graph (D-5). Its `kind` is `usage`, `structures` or `testing`, and at most one
annex per kind may be active (registry `UniqueBy: kind`, code
`duplicate-annex-kind`). `path` is a flat `<kebab>.md` filename relative to the
plan folder, so a folder rename on `renumber` keeps it valid. `add annex` writes
a **create-only** stub `.md` when the file is absent (never overwriting user
content), reports it in the result (`file`, and a `wrote …` line in `--text`),
and wires any `--about <id>` edge inline — `about` now runs from a tree **or an
annex** to any node. The `testing` stub seeds one `## <layer>` section per
`ac.layer` enum member; `usage` and `structures` carry a fixed skeleton. An AC
carries an optional `layer` (`e2e|integration|golden|unit|manual`) naming the
test layer that proves it. Folder-aware lint (all gated at `solution`, read
through the plan folder, skipped when a plan is linted without its folder):
`annex-missing` (a `path` names no file), `unlinked-file` (a `*.md` in the
folder no annex registers), `annex-ref` (a `[[id]]` in an annex file resolves to
no node — the same code-span-skipping resolution as prose), and
`annex-testing-layers` (one-directional: a `## <layer>` section is missing for a
layer some AC uses — extra sections are fine). `annex-changed` is gated at
`done`.

## The registry is the only extension point

`internal/schema/registry.go` declares the lifecycle sequence
(`Schema.Lifecycle`), every node type (prefix, glossary term, fields,
`MinLifecycle`, `RankScope`) and every edge type (endpoint types, `CrossPlan`).
Validation, lint, the `add` flags and `--help` all read it, so a new type is a
registry entry and nothing else. `Lifecycle.Index`/`AtLeast` read the
registry's sequence, rules and types name steps, and the registry tests check
every `MinLifecycle` is a step — so a future format version can try another
lifecycle without touching old plans.

## Invariants

- **Two gates.** `graph.Decode` is tolerant: only malformed JSON fails it.
  `graph.Validate` returns every structural problem as data. Lint merges both.
  Writes are Decode → mutate → Validate → Save and refuse on any error,
  including errors already in the file.
- **Canonical bytes.** `graph.Save` sorts nodes by (type, id) and edges by
  (from, type, to), uses a fixed key order (`version, id, nodes, edges`; edges
  `id, from, type, to`), a 2-space indent, no HTML escaping and a trailing
  newline, and writes atomically (temp file + rename). There is no lock: one
  agent plans one plan at a time.
- **Opaque IDs.** A type prefix + 4 Crockford base32 characters (`ac-3fxm`),
  regenerated on collision with any existing ID, retired ones included. Edges
  have IDs too (`e-xxxx`) in the same space, so a review can anchor to one; a
  (from, type, to) triple is still unique. The plan node's ID is literally
  `plan` (the plan's own ID is the top-level `id`). Reading order is the
  separate `rank` field.
- **Prose references skip code.** `graph.FindProseRefs` finds `[[…]]` in text
  fields but skips inline code spans and fenced code blocks, so prose can show
  the syntax as an example (`` `[[ac-3fxm]]` ``). Lint and `renumber`'s rewrite
  both read references through it, so what lint checks is what renumber
  rewrites.
- **An AC proves one or more goals.** `show` lists it under each, labelled
  under the first goal in reading order and marked `shared` under the others;
  `brief` lists every goal it proves. `move` treats it as a sibling of the ACs
  under any of its goals. Decision labels (D1…) follow decision rank, not
  where a decision first appears on the ladder.
- **Semver, never migrated.** `version` is a semver string; the tool's is
  `schema.Version` (`1.0.0`). Reads accept any `1.x.y`; a plan of another
  major is read best effort and not validated against this registry (lint:
  `other-version` warning, also for a newer 1.x). Writes — every mutating
  command, `fmt` without `--check` (only when it would rewrite), `renumber` — are
  refused with `frozen` when the plan is another major or newer than the tool,
  or its lifecycle is `done` (setting `done` is the last allowed write).
- **Freeze hashes annexes.** `annex.hash` is a `Computed` field (never a flag):
  the `--lifecycle done` write is the one place it is set, recording the SHA-256
  of each active annex's bytes before the graph is saved. That transition
  **refuses** with `annex-missing` (exit 1, nothing saved, lifecycle not
  advanced) if any annex's file cannot be read, so every annex must have its
  file on disk before the freeze. From `done` on, lint recomputes the hash and
  reports `annex-changed` when a frozen annex's bytes drift from it — a frozen
  plan is history, so the fix is to restore the file or start a new plan.

## Determinism for tests

Two environment variables, read only when set, and meant only for tests (leave
them unset in normal use):

- `AUTO_PLAN_SEED` (uint64) seeds ID generation. Each invocation mixes in the
  plan's node count, so a scripted sequence of commands always yields the same
  IDs — and different plans built by the same sequence get the same IDs. A new
  node's ID is drawn before its edges' IDs, so adding edges never shifts node
  IDs. `new` draws the plan ID from a stream keyed by the plan number.
- `AUTO_PLAN_DATE` (YYYY-MM-DD) pins the date `new` records.

## Build and test

```bash
cd auto-plan
go build ./...
go test ./...            # unit, in-process CLI, and the e2e scenarios
go test -short ./...     # skips e2e (it builds the auto binary)
go test ./e2e/ -update   # regenerate scenario snapshots, then review the diff
go test ./internal/render/ -update   # regenerate text goldens
```

The e2e harness (`e2e/e2e_test.go`) builds `auto` from `../auto-cli`, creates a
temp git workspace with a pinned environment, runs
`e2e/testdata/scenarios/<name>/commands.txt` line by line, and at each
`# checkpoint <n>` compares every `.auto/plan/plans/*/graph.json` **and every
annex `*.md` sidecar beside it** byte for byte (and that
`.auto/plan/plans/AGENTS.md` and its CLAUDE.md symlink exist). A
`# expect exit=<n> stdout=<file>` line checks the next invocation against
`snapshots/stdout/<file>`. A `# preview <folder> <file>` line reassembles AC-14's
preview-data contract — per-layer AC counts, the annex inventory (kind/title/path
+ `about` targets), and the raw usage/structures bodies — from a plan's final
`graph.json` + annex files and byte-compares it with `snapshots/<file>`.

Two scenarios (`go test ./e2e/ -run 'Lifecycle|Dogfood'`):

- **lifecycle** walks one task plan through every mutating command, including a
  rejected write, a retire, a `renumber`, an annex `add` (stub `.md` + `file`
  result) and the freeze after `done` (which records each annex's `hash`).
  `annex-changed` needs a mid-scenario file edit the commands.txt harness cannot
  do, so it is covered in `internal/lint` unit tests, not here.
- **dogfood** rebuilds epic 005, its child task 062 and task 052 as plans
  001–003 from CLI calls alone, lints the family clean, and golden-compares
  `show`, `trace`, `tree --files` and `brief`. The child (002) also gains
  `ac.layer` on its ACs and three real annexes, is frozen to `done`, and feeds
  the `preview-data.json` golden. Its `commands.txt` holds literal generated
  IDs: to change the script, rerun it incrementally, read each `id` from the
  output, and refresh with `-update`.

The merged `auto` binary is built from the repo root with `make build`.
