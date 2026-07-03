# Context: Task 058 — promote-e2e-harness

Grounded codebase facts for promoting the harness to `./harness/` and adding the
event-flow scenario. See [plan.html](plan.html) for requirements/verification/solution.

## Key Files — existing harness (to move)

- `auto-skill/harness/docker-compose.yaml:17-19` — SUT `context: ../..` (monorepo root), `dockerfile: auto-skill/harness/Dockerfile.sut`. **Breaks on move:** becomes `context: ..` under `./harness/scenarios/skill-remote/`.
- `auto-skill/harness/Dockerfile.sut:1-25` — two-stage `golang:1.26-alpine` → `alpine:3.21`; `COPY . .` then `cd auto-cli && go build -o /usr/local/bin/auto ./cmd/auto`. Line 21 `COPY auto-skill/harness/scripts/sut-entrypoint.sh` is a **root-relative path** → must become `harness/scenarios/skill-remote/scripts/...`.
- `auto-skill/harness/Dockerfile.git-server` + `scripts/{gen-certs,init-repos,nginx.conf}.sh` — Alpine nginx/fcgiwrap/git-http-backend HTTPS git server; context-relative COPYs (survive move as a unit).
- `auto-skill/harness/src/harness/core.py:11-13` — `HARNESS_DIR = parent.parent.parent`, `COMPOSE_FILE = HARNESS_DIR / "docker-compose.yaml"`, `GIT_REMOTE_URL` hardcoded. File-relative → survives move **if `src/harness/` layout preserved**; but a single fixed `COMPOSE_FILE` must become scenario-parameterized.
- `auto-skill/harness/src/harness/core.py:94-106` — `run()` = `docker compose exec -T <svc> sh -c <cmd>`; `Result` (`.stdout/.stderr/.exit_code/.json()/.ok`).
- `auto-skill/harness/tests/conftest.py:10-16` — **session-scoped** `harness` fixture: `h.up(build=True, timeout=600)` → `h.down()`. Tests share the SUT, isolate per `/workspace/<name>` via `fresh_workspace` + `auto skill init --project -y` + `trust_source`.
- `auto-skill/harness/pyproject.toml` — `name = auto-skill-harness`; `[project.scripts] harness = "harness.cli:main"`; hatchling packages `["src/harness"]`; deps `click`, `pytest`. `testpaths = ["tests"]`. Uses `uv` (`uv run pytest`, `uv run harness`).

## Key Files — event-flow surfaces (scenario 2)

- `auto-watch/internal/cli/ops.go:218-224` — `auto watch start` flags: `--rpc-addr` (default → `unix://<watchDir>/rpc.sock`; accepts `tcp://host:port`), `--hook-addr` (**default `127.0.0.1:7787`**), `--ready-file`, `--ctl-events`. Binds `net.Listen("tcp", hookAddr)` (`:130`) and `transport.Listen(rpcAddr)` (`:124`) — pass `0.0.0.0` to be network-reachable.
- `auto-watch/internal/rpcserver/ingest.go:32-52,82,89` — HookIngest: **403 for non-loopback RemoteAddr** (`isLoopback` accepts only 127.0.0.1/::1/localhost), 403 if `Origin` header present, 415 if not `application/json`, 405 if not POST; stamps `ev.Host = hostID` (**overwrite-always**); derives via `bus.DeriveDocChanged(ev, regProvider())`.
- `auto-watch/internal/rpcserver/subscribe.go:46-53` — parameterless `bus.subscribe` → relays all hub broadcasts as JSON-RPC notifications; data-plane events (incl. `doc.changed`) **always relay**; slow subscribers dropped.
- `auto-cli/cmd/auto/hookscmd.go:56-63,160-242,350-393` — `auto hooks fire --agent <claude|codex>`; reads Claude hook JSON on stdin; `PostToolUse`+`tool_name` → `agent.tool.post`; `AUTO_WATCH_HOOK_ADDR` env > `daemon.pid.json .hookAddr` > `127.0.0.1:7787`; POSTs `ev.AsNotification()` to `http://<addr>/`, 150ms timeout, **all runtime errors swallowed (exit 0)**. Does **not** derive `doc.changed` (daemon does).
- `auto-shared/bus/derive.go:16-74` — `DeriveDocChanged` fires only for `agent.tool.post` **with a registered project** (`ev.Project != "" && reg.FindProjectByID(ev.Project) != nil`) and path matching `docs/**/*.{md,html}`. Emits `DocChanged{project,path,...}`; **changed path is at `data.path`** (not top-level). Deterministic derived id = `sha256(srcID:type:path)[:8]`.
- `auto-ui/internal/cli/serve.go:79-82,128,131,180-182` — `auto ui serve` flags `--port`/`--ready-file`/`--projects`; **debug gated by env `AUTO_UI_DEBUG=1`** (`WithDebug`); **binds `127.0.0.1` only**; **refuses to start with zero backends** (exit 2).
- `auto-ui/internal/server/debug.go:33-61` — `GET /api/debug/recent` → JSON **array** of last ≤100 `bus.Event` (oldest first); records **all** relayed backend events; 405 non-GET, 404 if debug off.
- `auto-ui/internal/cli/backends.go:12,61-124` — `auto ui backends add <unix://|tcp://uri>` (`--name`, `--no-verify`); config at `~/.auto/ui/backends.json`. auto-ui dials the **RPC** listener (`tcp://autowatch:<rpcport>`), not the hook port.
- `auto-shared/config/host.go:13-101` — `~/.auto/host.json` `{"hostId","hostname"}`; `hostId` matches `^[a-z0-9][a-z0-9._-]*$`. **Pre-seed by writing the file** before any auto command (no env var); `EnsureHost` won't overwrite a valid file.
- `auto-cli/cmd/auto/initcmd.go:27,30,33,97-101` — `auto init --project` registers cwd repo into shared `~/.auto/projects.json` (the registry `DeriveDocChanged` reads). Seeds `host.json` + `projects.json`; won't clobber a pre-seeded `host.json`.

## Patterns

- **Harness discipline** (`auto-skill/harness/CLAUDE.md`): CLI-first + importable `Harness`; `uv`/pytest; session-scoped up/down; per-workspace isolation on a shared container; build SUT from monorepo source (real binaries); **granular fail-fast validation at every infra stage** before asserting.
- **Package patterns** (`docs/auto-package-patterns.md`): E2E tests in `e2e/`; `init`/`doctor`/`quickstart` CLI shape; JSON-default output.
- **Event delivery** (`docs/auto-bus-spec.md` §5): at-most-once, **lossy under backpressure** (16-slot buffers), in-order per-producer, **no cross-producer ordering**, no dedup guarantee → assert **presence by id/`data.path` with bounded retry**, not exact counts.

## CRITICAL constraint — forces the topology

`HookIngest` rejects **non-loopback** POSTs (403). A separate `agent` container POSTing across the Docker bridge to a central autowatch is refused. Combined with **`ev.Host` overwrite** (a single daemon stamps ITS hostId on every event, so one daemon cannot yield the "distinct host IDs" the requirements chose), the only no-product-change design that satisfies *multiple agent containers with distinct host IDs* is: **each agent container runs its own co-located `auto watch start` daemon** (loopback hook-ingest local to the container) and exposes its **RPC** listener on `tcp://0.0.0.0:<port>`; the single `auto-ui` container `backends add`s each and subscribes via `bus.subscribe`. This is exactly the multi-host aggregation model (tasks 045/046). Firing `auto hooks fire` *inside* each agent container keeps the POST loopback-legal. Changing the loopback gate is a product code change → out of scope.

## Related Tasks

- **040** — autowatch event ingest + relay plumbing (HookIngest, derive, relay).
- **045** — auto-ui event aggregation (BackendManager subscribes to each backend via `bus.subscribe`; `(hostId, projectId)` identity).
- **047** — hook retarget to autowatch-only; auto-ui local ingest removed; `/api/debug/recent` now fed solely by the relay sink.
- **046** — multi-host SPA (aggregation across hosts — the model scenario 2 mirrors).
- **030** — transport-conformance-harness (prior harness-shaped task).

## History enrichment (CB3)

- **Harness build lineage:** `743df45` (foundational HTTPS git-server harness), `4807244` (rename-flow gaps), `9519338` (P0/P1 fuzz-fix + hardening). All 5 event-relay CLI seams (`--hook-addr`/`--rpc-addr`/`--ready-file`, HookIngest 403 non-loopback, `AUTO_UI_DEBUG=1`, `backends add tcp://`, `AUTO_WATCH_HOOK_ADDR`) **CONFIRMED on HEAD — no drift/renames**.
- **Carry-forward lessons:** `init: true` zombie-reaping is load-bearing on the SUT (73+ zombies observed under load, `docker-compose.yaml:24`) → **apply `init: true` to every new agent/SUT service**. TLS/CA machinery is git-transport-specific → the `event-flow` scenario (no git remote) skips it. `http.receivepack=false` + `init.defaultBranch=main` hardening lives in the skill-remote image.
- **Move gotcha (CB3):** `.venv/` and `.pytest_cache/` are **committed** under `auto-skill/harness/` — a raw `git mv` drags them. Phase 1 must exclude/clean them (and rely on `uv sync` to rebuild `.venv`).
- **Reference blast radius is small + self-contained:** only 3 in-tree code/config refs — all *inside* the moving subtree (`docker-compose.yaml:19`, `Dockerfile.sut:21`, `CLAUDE.md:26`) — plus root `CLAUDE.md:223` which links `docs/research/auto-skill-harness-fuzz-test.md` (a research-doc filename, **not** a path into the harness → no change). **No Makefile, no `.github/` CI, no `.dockerignore` harness entry.**
- **030 assurance lesson (quotable):** *"a poll-to-settle is not an assertion"* — assert the observable outcome (the event present in `/api/debug/recent`), not that time elapsed. Our `assert_doc_changed` matches on `data.path`+`host`, satisfying this.
