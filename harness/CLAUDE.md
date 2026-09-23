# auto-stack e2e harness

A hermetic, reproducible, Docker-based harness for end-to-end testing, probing,
and fuzzing across **any** auto-stack surface. It builds the real `auto` binary
from monorepo source and drives it inside containers over real transports — no
mocks, no shortcuts.

The harness is a single [`uv`](https://docs.astral.sh/uv/)-managed Python project
rooted here. All invocation is `uv run …` — no bare `python`/`pip`/`venv`.

## Scenario model

The harness is organised around **scenarios**. A scenario is a self-contained
slice of end-to-end coverage:

- **`scenarios/<name>/`** — its own `docker-compose.yaml`, `Dockerfile*`,
  `scripts/`, and `fixtures/`. Fully self-contained: bring one up without the
  others. Each runs under its own Compose project name (`harness-<name>`), so
  scenarios are isolated even when run side by side.
- **`src/harness/scenarios/<name>.py`** — a thin Python module subclassing
  `Scenario` (in `base.py`) that declares the services, adds fail-fast readiness
  gates, and exposes a small command+assert DSL.

Everything shared lives once in the core:

- **`src/harness/core.py`** — `Harness` (Compose lifecycle: `up`/`down`/`status`,
  container `exec` via `run`, health polling) and `Result` (`.stdout`/`.stderr`/
  `.exit_code`/`.ok`/`.json()`). Scenario-agnostic.
- **`src/harness/scenarios/base.py`** — `Scenario`: binds a name to its compose
  stack + a `Harness`, runs `check_ready()` gates after `up`.
- **`src/harness/cli.py`** — `uv run harness <scenario> up|run|down|status`.

```
harness/
├── pyproject.toml / uv.lock         # single uv project (auto-harness)
├── src/harness/
│   ├── core.py                      # Harness + Result (generic)
│   ├── cli.py                       # uv run harness <scenario> ...
│   └── scenarios/
│       ├── base.py                  # Scenario base (compose path, gates, DSL)
│       ├── skill_remote.py          # scenario 1 helpers
│       ├── event_flow.py            # scenario 2 DSL
│       ├── mail_flow.py             # scenario 3 DSL
│       └── lock_flow.py             # scenario 4 DSL
├── scenarios/
│   ├── skill-remote/                # compose + Dockerfiles + scripts + fixtures
│   ├── event-flow/
│   ├── mail-flow/
│   └── lock-flow/
└── tests/
    ├── skill_remote/                # per-scenario tests + session fixture
    ├── event_flow/
    ├── mail_flow/
    └── lock_flow/
```

## Scenarios

### `skill-remote` — auto-skill add/sync/rename over real HTTPS

Two services: a `git-server` (nginx + fcgiwrap + git-http-backend serving a bare
skills repo over self-signed HTTPS) and a `sut` (the `auto` binary). Exercises the
full remote code path: canonicalize URL → blobless clone → realize → git-archive
extraction → render to targets. TLS trust chain via a shared-volume CA cert.

### `event-flow` — agent hooks → autowatch → auto-ui

Multiple `agent` containers, each with a **distinct seeded host id** and a
**co-located `auto watch start` daemon** (loopback hook-ingest + TCP RPC on
`0.0.0.0:7788`), plus one `auto-ui` container (debug ring on) that subscribes to
every agent's RPC backend. The DSL fires `auto hooks fire` inside an agent and
asserts the derived `doc.changed` lands in auto-ui's `/api/debug/recent`, keyed by
`(data.path, host)`.

Why co-located daemons: `HookIngest` rejects non-loopback POSTs and a daemon
overwrites `ev.Host`, so a single central daemon can neither accept cross-container
hooks nor yield distinct host ids. One daemon per agent (fire loopback-locally,
relay over RPC) mirrors the 045/046 multi-host model with zero product change.

### `mail-flow` — two agents on one host trading `auto mail`

**One** `host` container: one seeded host id, one `~/.auto`, and **two registered
project workspaces** (`/workspace/project-a`, `/workspace/project-b`). Each
"agent" is a separate `auto mail …` invocation with its cwd in one of those
workspaces. The DSL drives the epic's C1 loop — subscribe, send, list, ack —
plus the in-band nudge that `auto hooks fire` emits when a working agent has
mail waiting.

Why one container: the mail store is host-global, and this harness's own
convention gives each container a **distinct** `HOST_ID`, so two containers
would be two *hosts* rather than two agents on one (D-062-1). Two workspaces in
one container is genuinely two agents on one host, and it exercises the
concurrent-writer case without inventing a shared-`HOME` volume pattern the
harness does not have. When cross-host mail arrives, `mail-flow` grows a second
container with its own host id — additive, with the single-host case left as
the control.

`check_ready()` gates on the ready-file, both workspaces being registered, an
initialised store, an empty `auto mail list` per workspace, **and** an empty
pending-flag directory. Both halves of the on-disk state are session-scoped and
shared by every test in the module, so a new test wants its own address and
should ack everything it sends; a flag outliving its mail nudges whatever binds
to that pair next, and the hook never opens the store to double-check (G8).

### `lock-flow` — two Workers on one host contending for a lock Group

**One** `host` container: one seeded host id, one `~/.auto`, and **one
registered project workspace** (`/workspace/project`) that both Workers share.
Each "Worker" is a separate `auto` invocation with **`AUTO_LOCK_WORKER`** set to
`worker-a` or `worker-b` — the override rung of the identity chain, which makes
identity deterministic with no tmux and no linked worktrees in-container
(D-11). With it unset, the same workspace is the *bare* main checkout (D-6).
The entrypoint runs `auto hooks install` (the Claude `PreToolUse` entry is what
makes a lock enforceable, D-13) and opts the project in with one Group,
`drizzle-schema`, over `db/schema/**` and `db/migrations/**`.

The DSL drives the full loop through the real binary and the real hook wiring:
`take`/`release`/`status`/`clear`/`doctor` as a Worker, and `fire_edit`, which
pipes a Claude `PreToolUse` Edit payload into `auto hooks fire --agent claude`
as that Worker; `assert_denied` reads exactly one deny object off stdout and
`assert_allowed` requires an empty one. A stub `gh` on PATH
(`scenarios/lock-flow/scripts/gh`) answers `gh pr list` from a file the tests
write via `set_pr_state`, so the merge-verified `clear` is exercised OPEN vs
MERGED without a network; every gh call is logged so a test can assert it was
(or was not) consulted. A worktree-kind holder — the only kind `clear` looks up
— cannot be produced by the CLI in this container, so `seed_worktree_holder`
writes one into the store the way the Go tests do.

Why one container: the lock store is host-global, and this harness's own
convention gives each container a **distinct** `HOST_ID`, so two containers
would be two *hosts* rather than two Workers on one — the same reasoning as
`mail-flow`. Everything is synchronous and local, so the assertions are direct
(no bounded retry). The store is shared by every test in the module; an autouse
fixture empties it and resets the gh stub before each test, so the tests are
independent.

`check_ready()` gates on the ready-file, the project being registered, the lock
config carrying the Group, an empty store, **and** `auto hooks fire --agent
claude` being wired onto `PreToolUse` in `/workspace/project/.claude/settings.json`.

## Usage

```bash
# Run a scenario's test suite (builds images from source on first run)
uv run pytest tests/skill_remote -v
uv run pytest tests/event_flow -v
uv run pytest tests/mail_flow -v
uv run pytest tests/lock_flow -v
uv run pytest -v                    # all four scenarios

# Interactive probing via the CLI
uv run harness skill-remote up
uv run harness skill-remote run sut "auto skill sync --text"
uv run harness skill-remote down

uv run harness event-flow up
uv run harness event-flow run agent-1 "cat /tmp/watch-ready.json"
uv run harness event-flow down
uv run harness event-flow down --keep-images   # iterating: reuse layers next up

uv run harness mail-flow up
uv run harness mail-flow run host "cd /workspace/project-b && auto mail list"
uv run harness mail-flow down

uv run harness lock-flow up
uv run harness lock-flow run host "cd /workspace/project && AUTO_LOCK_WORKER=worker-a auto lock status --text"
uv run harness lock-flow down

# Import in Python (probes / scripted tests share the same DSL)
from harness.scenarios.event_flow import EventFlowScenario
s = EventFlowScenario(); s.up()
s.edit_doc("agent-1", "docs/plan.md", "# hi")
s.fire_hook("agent-1", "/workspace/docs/plan.md")
s.assert_doc_changed(path="docs/plan.md", host="agent-1")
s.down()
```

## How to add a scenario

Adding a scenario is purely additive — it touches no existing scenario.

1. **`scenarios/<name>/docker-compose.yaml`** (+ any `Dockerfile*`, `scripts/`,
   `fixtures/`). Give each service an `init: true` if its PID 1 spawns children,
   and a per-service `healthcheck` so `up --wait` gates on real readiness. Build
   the `auto` binary from monorepo source (build context `../../..`, the repo
   root); reference Dockerfiles by their repo-root-relative path.
2. **`src/harness/scenarios/<name>.py`** — subclass `Scenario`, set `name` (must
   match the folder) and `services`, override `check_ready()` with fail-fast gates,
   and add scenario helpers. Reach for `self.run(service, cmd)` for exec.
3. **Register it** in `src/harness/cli.py`'s `SCENARIOS` dict (one line).
4. **`tests/<name>/conftest.py`** — a session-scoped fixture that `up()`s the
   scenario and `down()`s at teardown; add `tests/<name>/test_*.py`.

## Discipline

- **Flakiness gates are load-bearing.** Every infra stage self-validates before
  the next: `up --wait` + per-service healthchecks, then explicit DSL gates
  (`check_ready`: daemon bound, backend subscribed, project registered) *before*
  any assertion. A poll-to-settle is not an assertion — assert the observable
  outcome, with bounded retry, matching on presence not counts (delivery is
  at-most-once / lossy under backpressure).
- **Real binaries.** The `auto` binary is compiled in-image from source; the first
  `up` is slow. Shared-container isolation is per-workspace / per-registered
  project, not per-container.
- **Teardown removes the images it built.** `down()` passes `--rmi local`, so a
  scenario's images do not survive its own test run. Each one is a full `auto`
  build, and left behind they accumulate per scenario per branch until the host
  fills up — which takes every agent on the box down with it, not just the tests.
  Pass `--keep-images` (or set `HARNESS_KEEP_IMAGES=1`) while iterating on a
  scenario so repeated cycles reuse the layers. This reclaims *images* only: the
  builder cache is bigger and shared with every other project on the host, so it
  is never pruned from here — `docker builder prune` is the manual lever.
- **Missing seams are findings, not patches.** Scenarios use existing product
  seams only; if one is missing, flag it — don't change `auto-*` code here.

## Deferred (follow-up tasks)

- The **pydantic self-describing command-class DSL** (discoverable schemas,
  per-command assertions) — the current thin `Harness`/`Result` DSL is kept.
- **WebSocket-based assertions** — event-flow asserts via `/api/debug/recent`
  HTTP polling, which covers ingest→derive→relay→hub-record but not the final
  `/api/ws` push to a browser.
- **CI wiring** and non-Docker execution backends (SSH/LXC).
