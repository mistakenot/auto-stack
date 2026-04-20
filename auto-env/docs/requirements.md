---
hash: "f259711f"
id: "8c2f59e1"
read_when: "implementing autoenv or managing multi-worktree development environments"
summary: "Requirements for autoenv: manage isolated development environments for worktree-based coding agents, handling port allocation, databases, logs, and service lifecycle to avoid conflicts across concurrent instances."
title: "AutoEnv Requirements"
---

# autoenv — Requirements

A CLI tool for managing per-worktree development environments. Spins up isolated stacks of services (web, db, background workers, etc.) with auto-allocated ports, driven by a Process Compose template.

## Goals

- Enable parallel worktree-based development without port conflicts
- Support autonomous coding agents (Claude Code, Codex, opencode) working in isolated environments
- Keep the tool focused — orchestrate existing tools (Process Compose, Docker, Git), don't reinvent them

## Assumptions

- Runs on a single VPS (not distributed)
- No more than 5–8 active worktrees at a time; server has 32GB RAM
- Should support both Docker and non-Docker services uniformly
- Mostly operated by coding agents or via scripts, not interactively by humans
- Agent-agnostic by default — works with any coding agent (Claude Code, Codex, opencode) — but can offer optional integrations for specific ones (e.g. Claude Code hooks)

## Non-goals (for now)

- Multi-user / multi-machine support
- Replacing Process Compose or Docker Compose
- Production deployment workflows

---

## v1 Scope (MVP)

Minimum viable product. Three commands, blocking behavior, enough to be useful.

### Commands

#### `autoenv up [--name X]`

- Default `--name` to basename of current git worktree
- Read template from `./.auto/env/process-compose.template.yaml`
- Discover `${PORT_*}` tokens in template via regex
- Allocate a free port per discovered token, persisted in SQLite
- Render concrete Process Compose config via `envsubst`-style replacement
- Launch Process Compose detached, using unix socket at `/tmp/pc-<name>.sock`
- Block until all services report healthy (via Process Compose readiness probes)
- On success: emit JSON port map + socket path to stdout
- On failure (timeout, process-compose error): report which service(s) failed, dump last N lines of their logs, exit non-zero, leave partial state intact

#### `autoenv down [--name X]`

- Stop the Process Compose instance for this env
- Remove Docker Compose resources (`docker compose down -v` — remove volumes)
- Clean up SQLite state for this env
- Idempotent: no-op if nothing running

#### `autoenv status [--name X]`

- Print current state as JSON: services + readiness + allocated ports
- Works whether env is running or not

### State

- SQLite at `~/.auto/env/state.db`
- Tables:
  - `allocations` — name → port map, unique constraint on port
  - `events` — append-only audit log
- Locking: SQLite transactions handle concurrency

### Template

- Location: `<project>/.auto/env/process-compose.template.yaml`
- Variables: `${PORT_*}` (discovered, auto-allocated), `${BRANCH}`, `${BRANCH_SLUG}`, `${NAME}` (substituted from runtime context)
- Unresolved variables after substitution = hard error

### Directory layout

```
<project>/.auto/env/
  process-compose.template.yaml    # version-controlled

  logs/                             # per-service log files, gitignored

~/.auto/env/
  state.db                          # SQLite, per-machine
```

---

## Backlog

Deferred until the pain justifies the work. In no particular order within sections.

### Command surface

- `autoenv init` — scaffold template, create `.auto/env/` structure, install Claude Code hooks, check deps (process-compose, docker)
- `autoenv ls` — list all known envs with state
- `autoenv restart <service>` — restart a single service within an env
- `autoenv logs <service> [-f]` — tail a service's logs (via LogQL against Loki, fall back to file)
- `autoenv prune` — reap orphaned envs whose git branches are gone
- `autoenv doctor` — diagnose common problems
- `autoenv reload` — re-render template and apply without full teardown

### Multi-agent integration

- Claude Code `WorktreeCreate`/`WorktreeRemove` hook integration (ships as part of `init`)
- Claude Code `SessionStart` hook for context injection (port map, URLs)
- Claude Code `UserPromptSubmit` hook for cwd-delta detection (when agent switches worktrees mid-session)
- MCP server (`autoenv mcp`) exposing status/logs/restart as tools for portable cross-agent integration
- `AGENTS.md` scaffolding for portable agent conventions (Codex, opencode)

### Observability

- Loki instance per worktree (docker compose alongside pc services), with Alloy or Promtail shipping `./logs/*.log`
  - Alternative: shared Loki on main, worktrees only ship via Alloy with `branch=X` labels (lower RAM)
- Cross-worktree log query support
- Structured JSON log extraction (level, trace_id) as Loki labels where cardinality allows

### Failure modes to specify

- **Timeout behavior on `up`:** current plan is "leave partial state up, report failures, let user decide." Needs validation in practice.
- **`up` when already running + template changed:** detect drift via config hash, warn but noop; `reload` command handles actual re-apply.
- **Stale state recovery:** Process Compose died, state says running. Detect via socket reachability check. `up` treats as fresh launch; `status` reports stale honestly.
- **Bad template:** syntax error or unresolved `${PORT_*}` reference → fail fast with clear error, don't partially launch.
- **Concurrent `up` invocations:** SQLite transaction handles allocation race; verify behavior under stress.

### State / storage concerns

- Event log compaction strategy (squash alloc/release pairs for torn-down envs) — only matters after months of use
- State migration story if schema changes between autoenv versions

### Routing / ingress

- Caddy integration — append/remove per-branch mappings to a config snippet, `caddy reload` on `up`/`down`. Out of scope for autoenv; handle via shell hook that calls a separate script.
- Cloudflare Tunnel integration for public URLs (webhook delivery) — orthogonal, not autoenv's job.

### Nice-to-haves

- `--timeout` flag on `up` (currently hardcoded)
- `--purge` flag on `down` for explicit "also drop DB data" (currently always purges via `docker compose down -v`)
- `--output-format=human` for non-JSON output
- Template discovery walking up from cwd (currently assumes `./.auto/env/`)
- Per-service port ranges by naming convention (currently one pool)
- Health restart counts / uptime in `status` output for detecting flaky services

### Known trade-offs

- One Loki per worktree is wasteful RAM-wise (~200MB each); shared is leaner but couples worktrees to main. Revisit if RAM becomes a constraint.
- Blocking `up` can be slow (30s+ for Firebase-like services). Async mode could help but adds complexity.
- `docker compose down -v` is destructive by default — documented but worth flagging.

---

## Dependencies

- Go 1.22+ (for the CLI binary)
- `process-compose` (runtime)
- `docker` + `docker compose` (runtime, for stateful services)
- `git` (runtime, for worktree detection)
- SQLite (embedded via Go library)
