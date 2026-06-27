---
name: new-package
description: >-
    Scaffold a new auto-* package in the auto-stack monorepo. Creates the full directory structure, Go source files, go.mod, CLAUDE.md, and Makefile integration following established patterns. Use when: "create a new package", "add auto-foo", "scaffold a new tool", "new auto package".
metadata:
    short-description: "Scaffold a new auto-* package"
---

# New Package

Scaffold a new `auto-*` package in the auto-stack monorepo following the established patterns documented in `docs/auto-package-patterns.md`.

## Pre-flight

1. Read the patterns doc at `docs/auto-package-patterns.md` to load the full blueprint.
2. Ask the user for the following (if not already provided):
   1. **Package name** — the `{name}` part of `auto-{name}` (lowercase, no hyphens in binary name). Example: `foo` produces directory `auto-foo/`, binary `autofoo`.
   2. **Short description** — one-line description for the root Cobra command and CLAUDE.md.
   3. **Purpose** — a sentence or two about what the tool does and its role in the auto-stack.

## Step 1: Create directory structure

Create all directories:

```
auto-{name}/
├── cmd/auto{name}/
├── internal/
│   ├── app/
│   ├── cli/
│   └── config/
└── docs/
```

## Step 2: Create go.mod

Create `auto-{name}/go.mod`:

```
module github.com/mistakenot/auto-{name}

go 1.26.1

require (
    github.com/mistakenot/auto-shared v0.0.0
    github.com/spf13/cobra v1.10.2
)

replace github.com/mistakenot/auto-shared => ../auto-shared
```

## Step 3: Create source files

Create these files following the exact patterns from `docs/auto-package-patterns.md`:

1. **`cmd/auto{name}/main.go`** — Minimal entry point delegating to `cli.Execute()`
2. **`internal/app/app.go`** — App struct with Stdout, Stderr, CWD
3. **`internal/cli/root.go`** — Root command with ExitError, Execute(), newRootCmd()
4. **`internal/cli/init.go`** — init command with `--project` flag
5. **`internal/cli/doctor.go`** — doctor command returning DoctorCheck JSON
6. **`internal/cli/quickstart.go`** — quickstart with embedded markdown
7. **`internal/cli/docs.go`** — docs with embedded markdown
8. **`internal/cli/update.go`** — update delegating to auto-shared/update
9. **`internal/config/settings.go`** — Settings struct, path helpers, load/save/validate

Use the patterns doc as the source of truth for every code template. Replace `{name}` placeholders with the actual package name.

### Key conventions to follow

- `SilenceErrors: true` and `SilenceUsage: true` on root command
- `rootCmd.Version = version.Version`
- JSON output by default using `json.NewEncoder` with 2-space indent
- Config paths: `~/.auto/{name}/settings.json` (global), `.auto/{name}/settings.json` (project)
- Use `config.EnsureHost()` in the init command
- Include a `writeJSON` helper in root.go or a shared file
- Commands accept `*app.App` parameter
- All commands use `RunE` (not `Run`)

## Step 4: Run go mod tidy and build

```bash
cd auto-{name} && go mod tidy && go build ./cmd/auto{name}/
```

Fix any compilation errors before proceeding.

## Step 5: Create CLAUDE.md

Create `auto-{name}/CLAUDE.md` with:
- Tool name and description
- Build and test commands
- Autodoc documentation index placeholder

## Step 6: Update root Makefile

Add to the root `Makefile`:
- Add `auto-{name}` to the `PROJECTS` list
- Add `auto-{name}_BIN` and `auto-{name}_ENTRY` variables
- Add `build-{name}` target
- Add `dist-{name}` target
- Add `cp` line in the `install` target

## Step 7: Update root CLAUDE.md

Add a row to the sub-projects table in the root `CLAUDE.md`:

```markdown
| `auto-{name}/` | `auto{name}` | Early | {description} |
```

## Step 8: Verify

```bash
# Build the new package
cd auto-{name} && go build ./cmd/auto{name}/

# Run vet
cd auto-{name} && go vet ./...

# Test the binary
./bin/auto{name} --version
./bin/auto{name} quickstart
./bin/auto{name} doctor
```

## Step 9: Build all to confirm no breakage

```bash
make build
```

## Constraints

- Follow patterns exactly as documented — do not invent new conventions.
- Do not add domain-specific commands during scaffolding. The user adds those after.
- The scaffolded package must compile and pass `go vet` before finishing.
- Do not create test files during scaffolding — the user adds tests alongside domain logic.
- Use the same Go and dependency versions as existing packages.
