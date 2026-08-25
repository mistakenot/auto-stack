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
│       └── event_flow.py            # scenario 2 DSL
├── scenarios/
│   ├── skill-remote/                # compose + Dockerfiles + scripts + fixtures
│   └── event-flow/
└── tests/
    ├── skill_remote/                # per-scenario tests + session fixture
    └── event_flow/
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

## Usage

```bash
# Run a scenario's test suite (builds images from source on first run)
uv run pytest tests/skill_remote -v
uv run pytest tests/event_flow -v
uv run pytest -v                    # both scenarios

# Interactive probing via the CLI
uv run harness skill-remote up
uv run harness skill-remote run sut "auto skill sync --text"
uv run harness skill-remote down

uv run harness event-flow up
uv run harness event-flow run agent-1 "cat /tmp/watch-ready.json"
uv run harness event-flow down
uv run harness event-flow down --keep-images   # iterating: reuse layers next up

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
