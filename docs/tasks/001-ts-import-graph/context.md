---
hash: "ff9d9e0d"
id: "7a5073c1"
read_when: "implementing auto-graph TypeScript scanner or scaffolding a new auto-* package following the task 001 pattern"
summary: "Verified codebase context for implementing the TypeScript import graph tool in auto-graph, covering key files, patterns, and scaffolding sequence."
title: "Context: Task 001 — TypeScript Import Graph"
---

# Context: Task 001

Codebase context for implementing the TypeScript import graph tool in `auto-graph`. See [solution.md](./solution.md) for the full design.

## Key Files

- `docs/auto-package-patterns.md` -- canonical blueprint for scaffolding new auto-* packages (directory layout, standard subcommands, go.mod, Makefile integration, 17-step checklist)
- `auto-search/cmd/autosearch/main.go:1-12` -- minimal entry point pattern: `os.Exit(cli.Execute(ctx, os.Stdout, os.Stderr))`
- `auto-search/internal/app/app.go:8-24` -- lightweight DI container: `App{Stdout, Stderr, CWD}` with `New(stdout, stderr)` constructor
- `auto-search/internal/cli/root.go:14-68` -- Cobra root command pattern: `ExitError` type, `Execute()` returning int exit code, `NewRootCmd()` wiring subcommands
- `auto-search/go.mod:1-12` -- module naming (`github.com/mistakenot/auto-search`), Go 1.26.1, `replace github.com/mistakenot/auto-shared => ../auto-shared`
- `Makefile:9` -- PROJECTS list: `auto-doc auto-env auto-etl auto-watch auto-search auto-reflect auto-skill` (auto-graph not yet listed)
- `Makefile:12-25` -- per-project `_BIN` and `_ENTRY` vars (e.g. `auto-search_BIN := autosearch`, `auto-search_ENTRY := ./cmd/autosearch`)
- `Makefile:53-54` -- build target pattern: `cd auto-search && go build -ldflags="$(LDFLAGS)" -o ../$(BUILD_DIR)/autosearch $(auto-search_ENTRY)`
- `Makefile:157-176` -- install target: `cp $(BUILD_DIR)/auto{name} $(INSTALL_DIR)/`
- `.gitignore:9` -- `**/testdata/` ignores all testdata dirs (needs `!auto-graph/testdata/` negation for checked-in fixtures)
- `auto-graph/CLAUDE.md:1-20` -- existing brainstorming notes only, no implementation

## Patterns

### Package scaffold sequence (from auto-package-patterns.md checklist)
1. Create `cmd/auto{name}/main.go` → `internal/app/app.go` → `internal/cli/root.go`
2. Add standard commands: init, doctor, quickstart, docs, update
3. Add `internal/config/settings.go`
4. Create `go.mod` with auto-shared replace, run `go mod tidy`
5. Write `CLAUDE.md` with build instructions
6. Add Makefile targets (build, dist, install)
7. Update root CLAUDE.md sub-projects table
8. Add domain-specific packages

### CLI output conventions
- JSON to stdout by default (2-space indent via `json.NewEncoder` + `SetIndent`)
- Diagnostics/errors to stderr
- Text mode via `--text` flag (not `--json`)
- Exit 1 on errors with remediation hints

### Doctor command pattern
- Returns `[]DoctorCheck` JSON with `check`, `status` ("pass"/"fail"/"warn"), `message`
- For auto-graph: check ast-grep installed, check settings

### External tool dependency
- ast-grep v0.41.1 is installed at `/home/vscode/.nvm/versions/node/v22.22.1/bin/ast-grep`
- Scanner shells out via `ast-grep run --lang ts -p '<pattern>' --json=stream <dir>`

## Related Tasks

- No prior tasks exist in `docs/tasks/` — this is the first task (001)
- auto-skill was the most recently scaffolded package (commit `a90a2cc`): single commit with full CLI + Makefile integration
