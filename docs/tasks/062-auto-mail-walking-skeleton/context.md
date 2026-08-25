# Context: Task 062 — auto-mail walking skeleton

Codebase facts gathered for [plan.html](plan.html) (epic 005, T1). Everything
below was read in this repo at the time of writing; paths and line numbers are
repo-root-relative.

## Key Files

### The in-band nudge seam (the riskiest integration)

- `auto-cli/cmd/auto/hookscmd.go:52-127` — `newHooksFireCmd`. Reads the hook
  payload from stdin (bounded to 1 MiB), resolves `cwd`/`project` from the
  registry, calls `hooks.CaptureContext()`, appends the durable envelope, POSTs
  the bus event, then calls `matchAndEmitHint(...)`. **Never returns an error
  after the `--agent` check** — "a hook must not break the agent".
- `auto-cli/cmd/auto/hookscmd.go:124` — the single call site the mail check is
  inserted beside: `matchAndEmitHint(cmd.OutOrStdout(), cmd.ErrOrStderr(), agent, payload, ev, root)`.
- `auto-cli/cmd/auto/hints.go:135-145` — `hookResponse` / `hookSpecificOutput`:
  `{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"…"}}`.
  This is the exact envelope both Claude and Codex parse; it is the mechanism for
  an in-band nudge and it already exists.
- `auto-cli/cmd/auto/hints.go:147-187` — `matchAndEmitHint`. Two behaviours to
  copy: it is scoped to `PostToolUse` only ("every other installed event stays
  silent so we never write unexpected stdout that an agent might reject"), and it
  emits **at most one** hint per event (`return` after the first match).
  Consequence for mail: the mail nudge and a hint rule can collide on one event,
  so their ordering must be decided rather than left to chance.
- `auto-shared/hooks/log.go:29-58` — `CaptureEnv` / `captureEnvFrom`: an
  allowlist of `NTM_*`, `TMUX`, `TMUX_*` only. Herdr is invisible today (G7/T3).
- `auto-shared/hooks/log.go:62-72` — `tmuxFields`: `tmux_session`,
  `tmux_window_index`, `tmux_pane_index`, `tmux_pane_id` (`%1`-style, the
  "stable, rename-proof pane handle for `tmux send-keys -t`").
- `auto-shared/hooks/log.go:74-77` — `tmuxQueryTimeout = 200 * time.Millisecond`,
  with the comment "a hook runs in the agent's critical path… we cap it so a
  wedged tmux server can never stall the hook." This is the precedent G8 names as
  "same discipline as the existing tmux capture timeout".

### SQLite conventions already in the repo

- `auto-watch/internal/store/store.go:65-83` — `Open(path)`: `os.MkdirAll` the
  parent, `sql.Open("sqlite", path)`, then
  `PRAGMA journal_mode = WAL` / `PRAGMA busy_timeout = 5000` /
  `PRAGMA foreign_keys = ON`. This is the pattern to copy verbatim.
- `auto-watch/internal/store/store.go:94-101` — `WALCheckpoint` (`PRAGMA
  wal_checkpoint(TRUNCATE)`), called on the daemon tick to bound WAL growth.
- `auto-watch/internal/store/store.go:102+` — `Migrate(ctx)`: an ordered
  `[]string` of `CREATE TABLE IF NOT EXISTS`, fronted by a `schema_migrations`
  table.
- `auto-search/internal/indexdb/schema.go:243` — the same `PRAGMA journal_mode=WAL`.
- `modernc.org/sqlite` is a **direct** dependency of `auto-watch` (v1.53.0) and
  `auto-search` (v1.53.0), and an indirect one of `auto-cli`. Pure Go, no cgo —
  no new runtime dependency, and `CGO_ENABLED=0` release builds keep working.

### Package mounting

- `auto-cli/cmd/auto/main.go:12-26` — every tool imports its `rootcmd` package
  (`artifactcmd "github.com/mistakenot/auto-artifact/rootcmd"`, …).
- `auto-cli/cmd/auto/main.go:40-52` — `root.AddCommand(...)` takes
  `xcmd.New(stdout, stderr)` per tool. Adding `auto mail` is two lines here.
- `go.work:3-17` — the `use (...)` block; every module is listed.
- `Makefile:16` — `PROJECTS := auto-shared auto-doc auto-env auto-etl auto-watch
  auto-search auto-reflect auto-skill auto-graph auto-ui auto-config
  auto-artifact auto-cli`. All modules participate in fmt/vet/lint/vulncheck/test.
- `Makefile:25` — `RACE_PROJECTS := auto-shared auto-watch` — the modules whose
  concurrency code is exercised under `-race`. Mail's ack race and its concurrent
  writers belong here.
- `Makefile:37-40` — `build:` compiles only `auto-cli/cmd/auto` into `bin/auto`;
  there is no per-tool build target to add.

### Newest package to copy structurally

`auto-artifact/` (task 051) is the most recent build-out of the blueprint:

```
auto-artifact/cmd/autoartifact/main.go
auto-artifact/rootcmd/rootcmd.go
auto-artifact/internal/app/app.go
auto-artifact/internal/cli/{root,init,doctor,quickstart,docs,update,…}.go
auto-artifact/internal/config/settings.go
auto-artifact/internal/{artifact,s3,setupscript}/…
auto-artifact/conformance/conformance_test.go
auto-artifact/CLAUDE.md
```

Note it ships a top-level `conformance/` package — the same shape
`auto-shared/rpc/conformance/` uses (`conformance.go`, `fakes.go`,
`conformance_test.go`): one suite, run against every implementation.

### The harness

- `harness/CLAUDE.md` — the scenario model and the four-step "How to add a
  scenario" recipe: compose dir → `src/harness/scenarios/<name>.py` subclassing
  `Scenario` → one line in `SCENARIOS` → `tests/<name>/conftest.py` + tests.
  "Adding a scenario is purely additive — it touches no existing scenario."
- `harness/src/harness/scenarios/base.py:20-60` — `Scenario`: `name`, `services`,
  `compose_path`, `up(build, timeout)` (runs `check_ready()` after
  `compose up --wait`), `down()`, `status()`, `run(service, cmd)`.
- `harness/src/harness/cli.py:22-27` — the `SCENARIOS` dict; one line per
  scenario.
- `harness/scenarios/event-flow/docker-compose.yaml` — the multi-service shape:
  a YAML anchor (`&agent` / `<<: *agent`) to clone a service, `init: true` for
  PID-1 child reaping, and a per-service `healthcheck` gating on a ready-file.
  Build context is `../../..` (the repo root) with `dockerfile:` given as a
  repo-root-relative path.
- `harness/scenarios/event-flow/scripts/agent-entrypoint.sh` — seeds
  `~/.auto/host.json` with a distinct `HOST_ID`, `git init` + empty commit in
  `/workspace`, `auto init --project` to register the project, starts the daemon,
  then blocks on the ready-file with a `timeout 20 sh -c 'until [ -f … ]'`.
- `harness/src/harness/scenarios/event_flow.py:59-84` — `check_ready()`: explicit
  fail-fast gates with `RuntimeError` and a diagnostic message per failure.
- `harness/src/harness/scenarios/event_flow.py:160-181` — `recent_events` /
  `assert_doc_changed`: bounded-retry polling that **matches on presence**, and
  raises `AssertionError` with the full observed set on timeout. The docstring
  records why presence not counts: "delivery is at-most-once / lossy under
  backpressure (bus-spec §5)". Mail promises at-least-once, so mail's assertions
  can be stronger — but must still tolerate duplicates (G4).
- `harness/src/harness/scenarios/event_flow.py:130-144` — `fire_hook`: loads a
  JSON fixture, patches `cwd`/`tool_input.file_path`, base64-pipes it into
  `auto hooks fire --agent claude` inside the container, asserts exit 0.
  base64 is used throughout to dodge shell-quoting hazards.
- `harness/tests/event_flow/conftest.py` — a session-scoped fixture that `up()`s
  once and `down()`s at teardown.

### The glossary (G16 — must be edited first)

- `docs/concepts/UBIQUITOUS_LANGUAGE.md:1-7` — autodoc frontmatter
  (`hash`, `id`, `read_when`, `summary`, `title`). The `summary` enumerates the
  current term list, so adding terms means updating it, then running
  `auto doc fixed <file>` to refresh the hash (pre-commit checks freshness).
- `docs/concepts/UBIQUITOUS_LANGUAGE.md:12-14` — a generated ER block:
  `<!-- ER-DIAGRAM:START (generated by glossary.py — do not edit by hand) -->`.
- `.claude/skills/domain-modelling/scripts/glossary.py` — the validator/generator:
  `check <file>` (structural lint: every entry has a definition + `_Avoid_`, every
  `_Has_:` target is defined, no word both canonical and avoided, no duplicates;
  exit 1 on error) and `diagram <file> --write` (regenerates the ER block).
- Entry format, from the existing entries: bold term, definition, `_Avoid_:` line,
  optional `_Has_:` line naming related terms.
- **Message is already taken** (`docs/concepts/UBIQUITOUS_LANGUAGE.md`, Session
  Data section): "A single role-tagged exchange within a Session". This is the
  collision D-4 exists to avoid.

### RPC / transport (not used in T1; the seam T3 grows into)

- `auto-shared/rpc/peer.go:96-120` — `Peer`, a symmetric duplex JSON-RPC 2.0
  endpoint; `WithHandler(method, h)` registers a method by dotted name.
- `auto-shared/transport/transport.go:39-70` — `Listen(uri)` / `Dial(ctx, uri)`
  dispatching on scheme: `unix://` and `tcp://` only, with a remediation message
  on an unknown scheme.
- `auto-shared/rpc/conformance/` — `conformance.go`, `fakes.go`,
  `conformance_test.go`: the "one suite, run against every implementation"
  pattern D-062-5 borrows.

## Patterns

- **Every tool is its own Go module** with a `rootcmd` façade, mounted on
  `auto-cli`'s root command. All implementation lives under `internal/`.
  `replace github.com/mistakenot/auto-shared => ../auto-shared` in every `go.mod`.
- **JSON on stdout, diagnostics on stderr**, 2-space indent, `--text` as the
  opt-in human mode (never `--json`).
- **Resource noun + verb triad** (`list` / `describe <id>` / `get <id>`, `search`
  for ID-less discovery) for addressable data — but D-4 deliberately makes `mail`
  itself the noun with flat verbs, to avoid `auto mail message get` colliding
  with `auto search message get`.
- **Config layering**: `~/.auto/host.json` (machine identity),
  `~/.auto/{tool}/settings.json` (global), `.auto/{tool}/settings.json` (project),
  loaded through `auto-shared/config`.
- **Hooks never break the agent**: bounded reads, bounded external calls,
  swallow-and-continue on every runtime error, always exit 0.
- **Harness discipline**: fail-fast readiness gates before any assertion; assert
  the observable outcome with bounded retry, never poll-to-settle; a missing
  product seam is a finding to raise, not something to patch around in the
  scenario.

## Related Tasks

- **Task 053 — auto-hook-hints** built `auto-cli/cmd/auto/hints.go`: the
  trigger registry, `hookSpecificOutput.additionalContext`, and the
  PostToolUse-only, one-emission-per-event discipline. T1's nudge is a second
  producer on that same seam.
- **Task 058 — promote-e2e-harness** established `harness/` and its scenario
  model, including the "missing seams are findings, not patches" rule.
- **Task 047 — hook-retarget-autowatch** and **045/046 (auto-ui event
  aggregation / multi-host SPA)** produced the `event-flow` topology: one daemon
  per agent container, each with a distinct seeded host id. D-062-1 deliberately
  does *not* copy that topology, because distinct host ids are what make it
  multi-host and the MVP is single-host.
- **Task 051 — auto-artifact-cli** is the newest full build-out of
  `docs/auto-package-patterns.md`, including a top-level `conformance/` package.
- **Task 042 — auto-ui-proxy-backends** is the reason `auto-ui` crash-loops
  without a configured backend; it is the documented daemon-fragility that D-11
  cites when it keeps the daemon out of mail's critical path.

## Git history (verified)

- `d783cd8 feat(053): auto-hook-hints — cross-agent hint emission on hook events (#115)`
  — introduced `auto-cli/cmd/auto/hints.go` and the
  `hookSpecificOutput.additionalContext` emission. The mail nudge is the second
  producer on that seam; its single-emission discipline is the constraint
  D-062-9 resolves.
- `6e4b5c8 feat(047): hook-retarget-autowatch — autowatch is the sole hook ingest (#108)`
  — established `auto hooks fire` as the one hook entry point, which is why the
  nudge belongs there and not in a new hook.
- `1b1dada feat(022): hook-event-log — durable JSONL capture + hooks ETL source (#76)`
  — `~/.auto/hooks/raw/*.jsonl`, the append-only log seam 4 calls the bindings'
  real event stream (which is why bindings are excluded from mail's own log).
- `e566f6d feat(058): promote-e2e-harness (#124)` — created `harness/`, its
  scenario model and the "missing seams are findings, not patches" rule.
- `63a3df3 feat(051): auto-artifact-cli (#114)` — the most recent whole-package
  build-out of `docs/auto-package-patterns.md`, including a top-level
  `conformance/` package. The structural template for `auto-mail/`.
- `c86f26f`, `2f4c604`, `33217de` — epic 005 itself: added, then narrowed and
  answered Q-4 (`#parent` scoped to in-process subagents, D-13). Confirms the
  epic in the working tree is current at planning time.

**Path drift check (run at planning time):** every path named in the Solution
tab's file-change tree and in the Key Files section above was verified to exist
(for edits) or to be absent (for adds) on `main` at commit `33217de`. Three
files in the working tree are modified by unrelated work and are **not** part of
this task: `auto-cli/cmd/auto/initcmd.go`, `auto-cli/cmd/auto/initcmd_test.go`,
`e2e/test-install.sh`.
