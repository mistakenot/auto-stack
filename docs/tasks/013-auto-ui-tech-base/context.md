---
hash: "33f447e4"
id: "9e1a14a0"
read_when: "implementing auto-ui or scaffolding a new auto-* package following the auto-graph conventions"
summary: "Verified codebase context for scaffolding auto-ui: exact signatures for auto-shared dependencies, reference package patterns from auto-graph, embed precedent, and server/SPA integration points."
title: "Context: Task 013 — Auto UI Tech Base"
---

# Context: Task 013

Verified codebase facts grounding the implementation of `auto-ui/` (a new auto-* package serving a
no-build Preact+htm SPA from a single Go binary). See [solution.md](./solution.md) for the design.

## Key Files

### auto-shared dependencies (exact signatures)
- `auto-shared/version/version.go:3` — `var Version = "dev"` (import `github.com/mistakenot/auto-shared/version`)
- `auto-shared/update/update.go:34` — `func Run(stdout, stderr io.Writer) (*Result, error)`; `Result` fields: `CurrentVersion, LatestVersion, Updated bool, Message` (JSON-tagged)
- `auto-shared/config/paths.go:28` — `func AutoDir() (string, error)`
- `auto-shared/config/paths.go:46` — `func EnsureAutoDir() error`
- `auto-shared/config/jsonfile.go:13` — `func DecodeJSONFile(path string, target any) error` (permissive)
- `auto-shared/config/jsonfile.go:27` — `func DecodeJSONFileStrict(...)` (rejects unknown fields — use for tool config)
- `auto-shared/config/jsonfile.go:42` — `func WriteJSONFile(path string, value any) error` (creates parents, indented, trailing newline)
- `auto-shared/config/validation.go:6-12` — `ValidationError{Code, Path, Field, Message string; Value any}`

### Reference package to copy: auto-graph (closest, newest)
- `auto-graph/cmd/autograph/main.go:1-12` — entry point: `os.Exit(cli.Execute(context.Background(), os.Stdout, os.Stderr))`
- `auto-graph/internal/app/app.go:1-24` — `App{Stdout, Stderr io.Writer; CWD string}`; `New(stdout, stderr)` computes cwd internally
- `auto-graph/internal/cli/root.go:14-24` — `ExitError{Code int; Err error}` + `Error()` (returns "" when Err nil)
- `auto-graph/internal/cli/root.go:26-41` — `Execute()` unwraps `*ExitError`, prints to stderr, returns code
- `auto-graph/internal/cli/root.go:43-64` — `NewRootCmd(app)`: `SilenceErrors/SilenceUsage: true`, `Version = version.Version`, `SetOut/SetErr`, `AddCommand(init, doctor, quickstart, docs, update, <domain>)`
- `auto-graph/internal/cli/init.go:10-40` — calls `config.EnsureSharedSettings()` + `config.EnsureGraphSettings()`, prints paths/created status, returns nil
- `auto-graph/internal/cli/doctor.go:19-111` — builds `[]doctorCheck{Check, Status, Message, Hint string}`, JSON to stdout, `&ExitError{Code:1}` if any fail
- `auto-graph/internal/cli/update.go:11-28` — calls `update.Run(cmd.OutOrStdout(), cmd.ErrOrStderr())`, JSON-marshals result
- `auto-graph/internal/config/settings.go:127-173` — `EnsureSharedSettings() (string, SharedSettings, bool, error)` and `EnsureGraphSettings()` pattern: `(path, settings, wasCreated, error)`; creates `~/.auto/{name}/settings.json` if missing, validates
- `auto-graph/go.mod:3` — `go 1.26.1`; `:8` — `github.com/spf13/cobra v1.10.2`; `:18` — `replace github.com/mistakenot/auto-shared => ../auto-shared`

### Embed precedent (only existing usage — single file, no build tags)
- `auto-config/internal/hooks/install.go:10-11` — `//go:embed prepare-commit-msg` / `var hookScript []byte`; written out with 0755. **No directory embed and no build-tag split exists yet in the repo** — this task introduces both (`//go:embed all:static` + `//go:build dev`/`!dev`).

### Monorepo wiring (exact insertion points)
- `Makefile:15` — `PROJECTS := auto-doc auto-env auto-etl auto-watch auto-search auto-reflect auto-skill auto-graph` (append `auto-ui`)
- `Makefile:18-33` — per-package `<pkg>_BIN` / `<pkg>_ENTRY` block; add `auto-ui_BIN := autoui`, `auto-ui_ENTRY := ./cmd/autoui`
- `Makefile` build/dist/install targets (~line 45+) — add `build-ui`, `dist-ui`, `install-ui` + a `cp $(BUILD_DIR)/autoui $(INSTALL_DIR)/` line in the aggregate install
- `Makefile` LDFLAGS — `-X github.com/mistakenot/auto-shared/version.Version=$(VERSION)`, reused by all build targets
- `CLAUDE.md:54-66` — sub-projects table is **alphabetical by directory**; `auto-ui/` row goes between `auto-skill/` and `auto-watch/`
- **Confirmed**: `auto-ui/` does not exist yet.

## Patterns

- **Package layout** (`docs/auto-package-patterns.md:13-39`): everything under `internal/` (no public exports); one file per command in `cli/`; domain logic separate from CLI wiring.
- **CLI output** (root `CLAUDE.md:21-41`, patterns `:250-256`): JSON-default to **stdout** (parseable only, 2-space indent); diagnostics/errors/progress to **stderr**; `--text` flag for human mode; exit 1 on error even with partial results. (So the `serve` startup log line goes to stderr — matches solution.)
- **Standard subcommands** (`docs/auto-package-patterns.md:173-237`): `init` (+`--project`), `doctor` (JSON checks), `quickstart` (embedded md), `docs` (embedded md), `update` (delegates to `update.Run`).
- **Config locations** (`docs/auto-package-patterns.md:261-266`): host `~/.auto/host.json`; global `~/.auto/{name}/settings.json`; project `.auto/{name}/settings.json`.
- **Error handling** (`:397-411`): `&ExitError{Code, Err}` with a remediation hint in the message (e.g. "run `autoui init`").
- **Go build discipline** (root `CLAUDE.md`): run `go build ./...` after each Go file; `go mod tidy` to generate go.sum.
- **Git worktree discipline** (root `CLAUDE.md`): `git fetch origin && git checkout main && git pull origin main` before branching.
- **Testing** (`docs/auto-package-patterns.md:433-441`, root `CLAUDE.md:42-46`): unit `*_test.go` alongside code; e2e in `e2e/` under `//go:build e2e`; `testdata/` for fixtures; `t.TempDir()` / `t.Setenv()` for isolation; golden files with `-update`.
- **Ports**: autoenv (`auto-env/docs/requirements.md:35-76`) allocates per-worktree ports via templates/SQLite, but per solution this task uses a fixed `--port` (default 8080); autoenv wiring deferred.

## Related Tasks

- **auto-graph scaffold** — initial scaffold commit `78d2616` ("feat(autograph): phase 1 - scaffold package with standard subcommands") created the 13-file template (cmd/main.go, internal/app, internal/cli/{root,init,doctor,quickstart,docs,update,<domain>}, internal/config/settings.go, go.mod, go.sum, CLAUDE.md). Makefile + CLAUDE.md registration landed in `e6b7b44` ("phase 6 - e2e tests and Makefile integration"). This is the exact template/sequence for auto-ui.
- **`new-package` skill** (`.claude/skills/new-package/SKILL.md`) — fully functional; scaffolds the standard layout (main.go, app.go, root/init/doctor/quickstart/docs/update, config/settings.go) with the enforced patterns above. Phase 1 should use it rather than hand-writing boilerplate.
- **auto-config git-hook embed** (commit `964289b`) — the only `//go:embed` precedent; pattern reference for asset delivery (single file; this task extends it to a directory + build tags).
