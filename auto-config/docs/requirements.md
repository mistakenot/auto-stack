---
hash: "4f5c8f38"
id: "f7b21d3a"
read_when: "validating coding agent configuration or setting up development environments"
summary: "Requirements for autoconfig: validate and manage Claude/Codex agent configuration, set session names, and provide utility functions for coding agent environments."
title: "AutoConfig Requirements"
---

# AutoConfig Requirements

## 1. Purpose

`autoconfig` ensures a machine's coding-agent configuration (Claude Code, Codex, etc.) is correct, consistent, and ready for productive sessions. It also provides utility commands for common session-management tasks like naming sessions and toggling settings.

Its main jobs are:

- validate that Claude Code and Codex configuration files exist and contain expected values
- detect misconfigurations, missing permissions, stale hooks, and conflicting settings
- provide quick utility commands for session naming, setting toggling, and config bootstrapping
- report problems as structured JSON with concrete remediation hints

`autoconfig` is primarily a pre-session and troubleshooting tool. Agents or humans run it at session start to verify the environment, or invoke utilities mid-session to adjust settings.

## 2. CLI Principles

- Command output defaults to JSON.
- Human-readable text output is available via `--text` where noted.
- In JSON mode, `stdout` is strictly parseable; diagnostics go to `stderr`.
- JSON responses from `doctor` use a top-level array of check objects.
- `autoconfig` follows the common stack conventions for `init`, `quickstart`, `docs`, `doctor`, and `update`.
- `autoconfig init` must create `~/.auto/settings.json` if it does not already exist, then create `~/.auto/config/settings.json`.

## 3. Configuration Scope

### Files autoconfig knows about

#### Claude Code

| File | Scope | Purpose |
|------|-------|---------|
| `~/.claude/settings.json` | Global | Hooks, MCP servers, permissions, status line |
| `~/.claude/settings.local.json` | Global | Machine-local overrides |
| `~/.claude/CLAUDE.md` | Global | User-wide agent instructions |
| `.claude/settings.json` | Project | Project-scoped permissions and hooks |
| `.claude/settings.local.json` | Project | Machine-local project overrides |
| `CLAUDE.md` | Project | Project-scoped agent instructions |

#### Codex

| File | Scope | Purpose |
|------|-------|---------|
| `~/.codex/config.yaml` | Global | Model, approval mode, providers |
| `~/.codex/instructions.md` | Global | User-wide agent instructions |
| `codex.md` | Project | Project-scoped agent instructions |

#### Auto Stack

| File | Scope | Purpose |
|------|-------|---------|
| `~/.auto/settings.json` | Global | Shared host-level defaults |
| `.auto/*/settings.json` | Project | Per-tool project-local settings |

### Derived storage

```
~/.auto/config/settings.json    # autoconfig's own settings
```

## 4. v1 Scope

### In scope

- `doctor` — validate configuration files exist, parse correctly, and contain expected structure
- `init` — bootstrap autoconfig settings and optionally scaffold missing config files
- `session name` — set or read the current session name for Claude Code
- `quickstart` / `docs` — self-documentation commands
- `update` — check for and install latest release
- `show` — display parsed config for a given tool (claude, codex, auto)
- `diff` — compare two config files or global vs project settings

### Deferred

- Auto-fixing misconfigured files (beyond what `init` scaffolds)
- MCP server health checking (liveness probes for configured servers)
- Hook validation (checking that hook scripts exist and are executable)
- Config migration between Claude Code versions
- Syncing settings across machines

## 5. Success Criteria

- `autoconfig doctor` detects the 10 most common misconfiguration problems (see §7)
- `autoconfig session name "my-session"` sets the session title in under 200ms
- `autoconfig show claude` renders a merged view of global + project Claude settings
- All commands return valid JSON on stdout and exit 0 on success, non-zero on failure
- `autoconfig doctor` returns partial results (passing checks) even when some checks fail

## 6. Functional Requirements

### R1: doctor — Configuration Health Checks

`autoconfig doctor` runs a battery of checks against the local environment and reports results as a JSON array.

Each check object:

```json
{
  "name": "claude-settings-exists",
  "status": "pass|fail|warn|skip",
  "message": "~/.claude/settings.json exists and parses as valid JSON",
  "remediation": "Run: autoconfig init"
}
```

#### Checks (v1)

1. **claude-settings-exists** — `~/.claude/settings.json` exists and is valid JSON
2. **claude-settings-local-exists** — `~/.claude/settings.local.json` exists (warn if missing, not fail)
3. **claude-md-exists** — `~/.claude/CLAUDE.md` exists and is non-empty
4. **claude-project-settings** — `.claude/settings.json` exists in cwd (skip if not in a git repo)
5. **claude-project-md** — `CLAUDE.md` exists in cwd (skip if not in a git repo)
6. **codex-config-exists** — `~/.codex/config.yaml` exists and is valid YAML (skip if codex not installed)
7. **codex-instructions-exists** — `~/.codex/instructions.md` exists (skip if codex not installed)
8. **auto-settings-exists** — `~/.auto/settings.json` exists and is valid JSON
9. **auto-host-id** — `~/.auto/settings.json` contains a non-empty `hostId` field
10. **settings-json-valid** — all discovered `settings.json` files parse without error

Flags:

```bash
autoconfig doctor              # all checks, JSON output
autoconfig doctor --text       # human-readable table
autoconfig doctor --tool claude  # only Claude-related checks
autoconfig doctor --tool codex   # only Codex-related checks
autoconfig doctor --tool auto    # only auto-stack checks
```

Exit code: 0 if all checks pass or warn, 1 if any check fails.

### R2: init — Bootstrap Configuration

```bash
autoconfig init              # create ~/.auto/config/settings.json, ~/.auto/settings.json if missing
autoconfig init --project    # also create .auto/config/settings.json in cwd
autoconfig init --scaffold   # create missing Claude/Codex config files with sensible defaults
```

`--scaffold` creates only files that do not already exist. It never overwrites.

Default scaffolded `~/.claude/settings.json`:

```json
{
  "hooks": {},
  "mcpServers": {}
}
```

Default scaffolded `~/.claude/CLAUDE.md`:

```markdown
# Instructions

Add your global instructions here.
```

### R3: session name — Session Naming Utility

```bash
autoconfig session name "refactor auth middleware"   # set session name
autoconfig session name                              # get current session name (JSON)
autoconfig session name --text                       # get current session name (plain text)
```

Session name is stored in `~/.auto/config/session.json`:

```json
{
  "name": "refactor auth middleware",
  "set_at": "2026-04-17T12:00:00Z"
}
```

Other tools in the auto-stack can read this file to tag ETL records, search results, and watch triggers with the active session context.

### R4: show — Display Parsed Configuration

```bash
autoconfig show claude          # merged global + project Claude settings
autoconfig show codex           # parsed Codex config
autoconfig show auto            # merged auto-stack settings
autoconfig show claude --global # global only
autoconfig show claude --project # project only
```

Output is the parsed, merged JSON (or YAML for codex). When both global and project settings exist, project values override global values with a shallow merge.

### R5: diff — Compare Configurations

```bash
autoconfig diff claude                           # diff global vs project Claude settings
autoconfig diff file1.json file2.json            # diff two arbitrary config files
```

Output is a JSON object showing keys that differ, with `global` and `project` (or `left` and `right`) values.

### R6: quickstart / docs / update

Standard stack commands:

- `autoconfig quickstart` — prints a happy-path walkthrough to stdout
- `autoconfig docs` — prints full command reference to stdout
- `autoconfig update` — checks for and installs latest release via auto-shared

## 7. Non-Functional Requirements

### Testing

- Unit tests for config parsing, merging, and validation logic
- Integration test for `doctor` with a temp home directory
- Integration test for `init --scaffold` verifying no-overwrite behavior

### Validation

- One shared `validate()` function for settings.json schema checks
- Structured error objects: `{code, path, field, message}`
- Every `fail` result includes a `remediation` string

## 8. Downstream Requirements

- **auto-etl**: can read `~/.auto/config/session.json` to tag session records with the active session name
- **auto-watch**: can invoke `autoconfig doctor --tool auto` as a pre-flight check before starting the daemon
- **auto-search**: no direct dependency, but session names from autoconfig enrich search metadata

## 9. Explicit Non-Goals

- `autoconfig` does not manage MCP server lifecycle (start/stop/restart)
- `autoconfig` does not edit `CLAUDE.md` content beyond initial scaffolding
- `autoconfig` does not replace `claude config` or `codex config` — it complements them with cross-tool validation
- `autoconfig` does not store secrets or credentials
- `autoconfig` does not depend on any other auto-stack binary at runtime — it reads shared file formats only
