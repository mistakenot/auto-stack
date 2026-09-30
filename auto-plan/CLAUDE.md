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

```bash
auto plan init                                   # create docs/plans/ (idempotent)
auto plan new demo --kind task                   # docs/plans/001-demo/graph.json, plan node only
auto plan add 001 goal --title "…"               # flags are generated from the registry
auto plan add 001 ac --proves g-k7q2 --title "…" --gwt @ac.md --verify-cmd "go test ./…"
auto plan link 001 d-9t2w constrains g-k7q2      # endpoint types are checked
auto plan lint 001                               # or: auto plan lint all
auto plan show 001 --text                        # the goal ladder
```

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

Two environment variables, read only when set:

- `AUTO_PLAN_SEED` (uint64) seeds ID generation.
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
`docs/plans/*/graph.json` byte for byte at each `# checkpoint <n>`.

The merged `auto` binary is built from the repo root with `make build`.
