# auto-plan

Structured plan graphs. Ships as `auto plan` in the unified binary.

A Plan is one deterministic `graph.json` under `docs/plans/NNN-name/`: typed
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

Every command reads `auto plan <verb> <plan> [args…]`. `<plan>` is `NNN`,
`NNN-name`, a path, or `all` where it makes sense. Output is JSON on stdout by
default and text with `--text`; diagnostics go to stderr as
`{"errors":[{code,path,field,message,value}],"hint":…}` with exit 1.
`auto plan quickstart` prints the happy path; `auto plan docs` prints the full
reference, generated from the registry.

```bash
# create
auto plan init                                   # create docs/plans/ (idempotent)
auto plan new demo --kind task                   # docs/plans/001-demo/graph.json, plan node only
auto plan new walking-skeleton --kind task --epic 001   # a child plan of epic 001

# write (Decode → mutate → Validate → Save; refused on any error)
auto plan add 001 goal --title "…"               # flags are generated from the registry
auto plan add 001 ac --proves g-k7q2 --title "…" --gwt @ac.md --verify-cmd "go test ./…"
auto plan link 001 d-9t2w constrains g-k7q2      # endpoint types are checked
auto plan unlink 001 d-9t2w constrains g-k7q2
auto plan update 001 g-k7q2 --description "…"    # same field flags as add; plan --lifecycle advances
auto plan retire 001 a-4hn8                      # status retired; ID and edges kept
auto plan move 001 g-k7q2 --before g-3m1x        # rewrites one rank, never an ID

# check
auto plan lint 001                               # or: auto plan lint all (exit 1 on any error)
auto plan fmt all --check                        # canonical bytes? (without --check: rewrite)

# read (resource verbs: cheap → full fidelity)
auto plan list 001 --type ac                     # IDs + metadata + titles; `list all` spans plans
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

**Epics and cross-plan references.** An epic (`--kind epic`) holds goals,
rails, journeys with legs, decisions and `child` nodes. A child plan is created
with `--epic NNN`, and the epic lists it with `add NNN child --plan <child>`
(both halves are required, or lint reports `child-epic-mismatch`). A qualified
reference `NNN:ID` names a node in another plan, and only cross-plan edge types
accept one: `honors`, `delivers`, `builds-on` (from the child's `plan` node),
`discharges` (AC → rail) and `wouldBreak`. From `lifecycle: plan` an epic
requires every rail honoured by a child (or `--deferred <NNN>`) and every leg
delivered by one.

## The registry is the only extension point

`internal/schema/registry.go` declares every node type (prefix, glossary term,
fields, `MinLifecycle`, `RankScope`) and every edge type (endpoint types,
`CrossPlan`). Validation, lint, the `add` flags and `--help` all read it, so a
new type is a registry entry and nothing else.

## Invariants

- **Two gates.** `graph.Decode` is tolerant: only malformed JSON fails it.
  `graph.Validate` returns every structural problem as data. Lint merges both.
  Writes are Decode → mutate → Validate → Save and refuse on any error,
  including errors already in the file.
- **Canonical bytes.** `graph.Save` sorts nodes by (type, id) and edges by
  (from, type, to), uses a fixed key order, a 2-space indent, no HTML escaping
  and a trailing newline, and writes atomically (temp file + rename). There is
  no lock: one agent plans one plan at a time.
- **Opaque IDs.** A type prefix + 4 Crockford base32 characters (`ac-3fxm`),
  regenerated on collision with any existing ID, retired ones included. The plan
  node's ID is literally `plan`. Reading order is the separate `rank` field.

## Determinism for tests

Two environment variables, read only when set, and meant only for tests (leave
them unset in normal use):

- `AUTO_PLAN_SEED` (uint64) seeds ID generation. Each invocation mixes in the
  plan's node count, so a scripted sequence of commands always yields the same
  IDs — and different plans built by the same sequence get the same IDs.
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
`e2e/testdata/scenarios/<name>/commands.txt` line by line, and compares every
`docs/plans/*/graph.json` byte for byte at each `# checkpoint <n>`. A
`# expect exit=<n> stdout=<file>` line checks the next invocation against
`snapshots/stdout/<file>`.

Two scenarios (`go test ./e2e/ -run 'Lifecycle|Dogfood'`):

- **lifecycle** walks one task plan through every mutating command, including a
  rejected write and a retire.
- **dogfood** rebuilds epic 005, its child task 062 and task 052 as plans
  001–003 from CLI calls alone, lints the family clean, and golden-compares
  `show`, `trace`, `tree --files` and `brief`. Its `commands.txt` holds literal
  generated IDs: to change the script, rerun it incrementally, read each `id`
  from the output, and refresh with `-update`.

The merged `auto` binary is built from the repo root with `make build`.
