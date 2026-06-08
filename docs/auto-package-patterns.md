---
hash: "46dc24a4"
id: "a7c3e1f0"
read_when: "creating a new package in the auto-stack monorepo"
summary: "Reference patterns and conventions shared across all auto-* packages in the auto-stack monorepo. Used as the blueprint when creating new packages."
title: "Auto Package Patterns"
---

# Auto Package Patterns

This document captures the shared patterns, conventions, and structure used across all `auto-*` packages in the auto-stack monorepo. It serves as the authoritative blueprint for creating new packages.

## Directory Structure

Every package follows this layout:

```
auto-{name}/
├── cmd/auto{name}/
│   └── main.go              # Minimal entry point
├── internal/
│   ├── app/
│   │   └── app.go           # Runtime context (stdout, stderr, cwd)
│   ├── cli/
│   │   ├── root.go          # Root cobra command + Execute()
│   │   ├── init.go          # init subcommand
│   │   ├── doctor.go        # doctor subcommand
│   │   ├── quickstart.go    # quickstart subcommand
│   │   ├── docs.go          # docs subcommand
│   │   ├── update.go        # update subcommand
│   │   └── {feature}.go     # One file per domain command
│   ├── config/
│   │   └── settings.go      # Settings loading, validation, paths
│   └── {domain}/            # Domain-specific packages
├── docs/                    # Design docs, requirements
├── CLAUDE.md                # Tool description + build instructions
├── go.mod
└── go.sum
```

Key rules:
- All implementation lives under `internal/` (no public API exports)
- One file per command in `cli/`
- Domain logic is separate from CLI wiring
- Tests live alongside the code they test (`*_test.go`)

## Entry Point (main.go)

The entry point is minimal — it delegates immediately to `cli.Execute()`:

```go
package main

import (
    "context"
    "os"

    "github.com/mistakenot/auto-{name}/internal/cli"
)

func main() {
    os.Exit(cli.Execute(context.Background(), os.Stdout, os.Stderr))
}
```

Located at: `cmd/auto{name}/main.go`

## App Context (app.go)

A lightweight DI container passed to all commands:

```go
package app

import "io"

// App holds runtime dependencies injected at the CLI boundary.
type App struct {
    Stdout io.Writer
    Stderr io.Writer
    CWD    string
}

// New creates an App from the given writers and working directory.
func New(stdout, stderr io.Writer, cwd string) *App {
    return &App{Stdout: stdout, Stderr: stderr, CWD: cwd}
}
```

Located at: `internal/app/app.go`

## Root Command (root.go)

The root command wires up Cobra, version, error handling, and subcommands:

```go
package cli

import (
    "errors"
    "fmt"
    "io"
    "context"
    "os"

    "github.com/mistakenot/auto-{name}/internal/app"
    "github.com/mistakenot/auto-shared/version"
    "github.com/spf13/cobra"
)

// ExitError wraps an error with a specific exit code.
type ExitError struct {
    Code int
    Err  error
}

func (e *ExitError) Error() string {
    if e.Err != nil {
        return e.Err.Error()
    }
    return fmt.Sprintf("exit %d", e.Code)
}

// Execute runs the CLI and returns the exit code.
func Execute(ctx context.Context, stdout, stderr io.Writer) int {
    cwd, err := os.Getwd()
    if err != nil {
        fmt.Fprintln(stderr, err)
        return 1
    }

    application := app.New(stdout, stderr, cwd)
    rootCmd := newRootCmd(application)

    if err := rootCmd.ExecuteContext(ctx); err != nil {
        var exitErr *ExitError
        if errors.As(err, &exitErr) {
            if exitErr.Err != nil && exitErr.Err.Error() != "" {
                fmt.Fprintln(stderr, exitErr.Err)
            }
            return exitErr.Code
        }
        fmt.Fprintln(stderr, err)
        return 1
    }
    return 0
}

func newRootCmd(application *app.App) *cobra.Command {
    rootCmd := &cobra.Command{
        Use:           "auto{name}",
        Short:         "Brief description of what the tool does",
        SilenceErrors: true,
        SilenceUsage:  true,
    }
    rootCmd.Version = version.Version
    rootCmd.SetOut(application.Stdout)
    rootCmd.SetErr(application.Stderr)

    rootCmd.AddCommand(
        newInitCmd(application),
        newDoctorCmd(application),
        newQuickstartCmd(application),
        newDocsCmd(application),
        newUpdateCmd(application),
        // domain commands here
    )

    return rootCmd
}
```

## Standard Subcommands

Every package should implement these baseline commands:

### init

Initializes settings. Supports `--project` for project-local config.

```go
func newInitCmd(application *app.App) *cobra.Command {
    var projectFlag bool
    cmd := &cobra.Command{
        Use:   "init",
        Short: "Initialize settings",
        RunE: func(cmd *cobra.Command, args []string) error {
            // 1. EnsureHost() from auto-shared
            // 2. Create ~/.auto/{name}/settings.json (global)
            // 3. If --project: create .auto/{name}/settings.json (project-local)
            return nil
        },
    }
    cmd.Flags().BoolVar(&projectFlag, "project", false, "Initialize project-local settings")
    return cmd
}
```

### doctor

Checks configuration health, returning structured diagnostics:

```go
type DoctorCheck struct {
    Check   string `json:"check"`
    Status  string `json:"status"` // "pass", "fail", "warn"
    Message string `json:"message"`
}
```

### quickstart

Returns an embedded markdown string showing the happy-path workflow.

### docs

Returns an embedded markdown string with the full command reference.

### update

Delegates to `auto-shared/update.Run()`:

```go
func newUpdateCmd(application *app.App) *cobra.Command {
    return &cobra.Command{
        Use:   "update",
        Short: "Check for and install the latest release",
        RunE: func(cmd *cobra.Command, args []string) error {
            result, err := update.Run(application.Stdout, application.Stderr)
            if err != nil {
                return err
            }
            return writeJSON(cmd.OutOrStdout(), result)
        },
    }
}
```

## Resource Subcommands (noun + verb)

Beyond the baseline commands, model a package's domain data as **resources**, and
give each resource the same small, predictable set of verbs. The command is a
noun (the resource), the subcommand is a verb (the action):

```
<tool> <resource> list        # enumerate / discover (cheap, no bodies)
<tool> <resource> describe <id>  # metadata + summary for one item (cheap)
<tool> <resource> get <id>       # full content of one item
```

This is the pattern validated in `autosearch` (`session list|describe|get`,
`message get|describe`) and it generalizes to any package with addressable data
(e.g. `autodoc doc list|get`, `autoetl session list`).

### The verbs

| Verb | Arg | Returns | Cost |
|------|-----|---------|------|
| `list` | filters (flags) | many items, **IDs + metadata only, no bodies** | cheap — safe to call broadly |
| `describe <id>` | one ID | metadata, counts, and a short head+tail **summary** of one item | cheap |
| `get <id>` | one ID | the **full** content of one item | scoped to one item |

Plus a cross-cutting `search` for content-based discovery when you don't yet know
the ID (returns IDs + snippets, never full bodies).

### Design rules (progressive disclosure)

These make the surface token-efficient for agents — start broad and cheap, drill
to full detail only when needed. See
`auto-search/docs/progressive-disclosure-audit.md` for the audit that established them.

- **Stable, composable IDs.** A child ID embeds its parent: `message get <sessionId>-<index>`.
  IDs returned by `list`/`search` are directly usable as `get`/`describe` args.
- **Cheap rungs return no bodies.** `list` and `search` return identifiers and
  metadata only. Bodies cost tokens — defer them to `get`.
- **Truncate with a breadcrumb.** When a rung truncates content, print the exact
  command to recover the full version, e.g.
  `…[truncated — run: <tool> message get <id>]…`. The agent should never have to
  guess the next rung.
- **`get` is full-fidelity by default.** No flag required to get complete content;
  `get` is the bottom of the ladder. If an even-more-raw form exists (e.g. the
  source file on disk), expose its path in `describe`.
- **`describe` summarizes; `get` reproduces.** `describe` is for "is this the
  right item?" (metadata + a head+tail peek); `get` is for "give me everything."
- **No silent fidelity loss.** If the full form of a resource isn't reachable
  through any verb, that's a bug — document it and provide an escape hatch
  (a `--full`/`--raw` flag or a `source_path` in `describe`).

### Conventions carried over from the baseline

- Default to JSON on stdout; offer `--text` for skim-friendly human output.
- `list`/`search` return all items when no filters are given (see project CLAUDE.md).
- Filters are flags (`--since`, `--cwd`, `--tool-name`, …), normalized and validated
  against the same schema used for stored data.

## JSON Output

Default output is JSON to stdout. Use 2-space indentation:

```go
func writeJSON(w io.Writer, v any) error {
    enc := json.NewEncoder(w)
    enc.SetIndent("", "  ")
    return enc.Encode(v)
}
```

Rules:
- **stdout**: Strictly parseable JSON payload only
- **stderr**: Diagnostics, progress, errors, human-readable messages
- Commands that also support text mode use a `--text` flag (not `--json` — JSON is default)
- Exit code 1 on errors, even when partial valid results are returned

## Configuration

### File Locations

| Level | Path | Purpose |
|-------|------|---------|
| Host | `~/.auto/host.json` | Machine identity (shared) |
| Global | `~/.auto/{name}/settings.json` | User-wide tool defaults |
| Project | `.auto/{name}/settings.json` | Per-repo overrides |

### Loading Pattern

Use `auto-shared/config` utilities:

```go
import "github.com/mistakenot/auto-shared/config"

// Paths
func globalSettingsPath() (string, error) {
    autoDir, err := config.AutoDir()
    if err != nil { return "", err }
    return filepath.Join(autoDir, "{name}", "settings.json"), nil
}

func projectSettingsPath(repoRoot string) string {
    return filepath.Join(repoRoot, ".auto", "{name}", "settings.json")
}

// Loading
func loadSettings(path string) (*Settings, error) {
    var s Settings
    if err := config.DecodeJSONFile(path, &s); err != nil {
        return nil, err
    }
    return &s, nil
}

// Saving
func saveSettings(path string, s *Settings) error {
    return config.WriteJSONFile(path, s)
}
```

### Validation

Use the shared `ValidationError` type from auto-shared:

```go
import "github.com/mistakenot/auto-shared/config"

func validate(s *Settings) []config.ValidationError {
    var errs []config.ValidationError
    if s.SomeField == "" {
        errs = append(errs, config.ValidationError{
            Code:    "required",
            Path:    "settings.json",
            Field:   "someField",
            Message: "someField is required",
        })
    }
    return errs
}
```

## go.mod

```
module github.com/mistakenot/auto-{name}

go 1.26.1

require (
    github.com/mistakenot/auto-shared v0.0.0
    github.com/spf13/cobra v1.10.2
)

replace github.com/mistakenot/auto-shared => ../auto-shared
```

Key points:
- Module path: `github.com/mistakenot/auto-{name}`
- Always use local replace directive for `auto-shared`
- Go version matches the rest of the monorepo (currently `1.26.1`)
- Add domain-specific deps as needed (sqlite, parquet-go, etc.)

## Makefile Integration

Add entries to the root `Makefile`:

```makefile
# In PROJECTS list
PROJECTS := auto-doc auto-etl auto-watch auto-search auto-skill auto-{name}

# Binary and entry point
auto-{name}_BIN   := auto{name}
auto-{name}_ENTRY := ./cmd/auto{name}

# Build target
build-{name}:
    cd auto-{name} && go build -ldflags="$(LDFLAGS)" -o ../$(BUILD_DIR)/auto{name} $(auto-{name}_ENTRY)

# Dist target
dist-{name}:
    @mkdir -p $(DIST_DIR)
    cd auto-{name} && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
        go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/auto{name}-$(SUFFIX) $(auto-{name}_ENTRY)

# Install target (add cp line)
# cp $(BUILD_DIR)/auto{name} $(INSTALL_DIR)/
```

## CLAUDE.md

Every package needs a `CLAUDE.md` at its root. Minimum content:

```markdown
# auto{name}

Brief description of what the tool does and its role in the auto-stack.

## Build & Test

After any code change:

\```bash
cd auto-{name} && go build ./cmd/auto{name}/
cd auto-{name} && go test ./...
\```

## Documentation Index

*Auto-generated by `autodoc`. Do not edit manually.*
```

More mature packages should add:
- Configuration schema and defaults
- Key design decisions or principles
- Data format specifications
- Links to docs/ for detailed requirements

## Error Handling

Use `ExitError` for controlled exits with specific codes:

```go
return &ExitError{Code: 1, Err: fmt.Errorf("config not found: run auto{name} init")}
```

For domain errors, wrap with context:

```go
return fmt.Errorf("load settings: %w", err)
```

Every hard error should include a remediation hint (e.g., "run `auto{name} init`").

## Version Injection

Version is set at build time via ldflags:

```
-X github.com/mistakenot/auto-shared/version.Version=$(VERSION)
```

The `version.Version` variable defaults to `"dev"` during development.

## Date Filtering (when applicable)

Commands that filter by time should support:

```bash
--since 5m             # relative: minutes, hours, days, weeks
--after 2026-01-01     # absolute ISO 8601 start (inclusive)
--before 2026-02-01    # absolute ISO 8601 end (exclusive)
```

## Testing Patterns

- Unit tests alongside code: `*_test.go`
- E2E tests in `e2e/` directory
- Test helpers in `internal/testutil/`
- Use `t.TempDir()` for isolated workspaces
- Use `t.Setenv()` for environment overrides
- Test commands by calling `Execute()` with captured stdout/stderr

## CLAUDE.md Table Entry

When adding a new package, update the sub-projects table in the root `CLAUDE.md`:

```markdown
| `auto-{name}/` | `auto{name}` | Early | Description of the tool |
```

## Checklist for a New Package

1. Create directory: `auto-{name}/`
2. Create `cmd/auto{name}/main.go` (minimal entry point)
3. Create `internal/app/app.go` (runtime context)
4. Create `internal/cli/root.go` (root command + Execute + ExitError)
5. Create `internal/cli/init.go` (init command)
6. Create `internal/cli/doctor.go` (doctor command)
7. Create `internal/cli/quickstart.go` (quickstart command)
8. Create `internal/cli/docs.go` (docs command)
9. Create `internal/cli/update.go` (update command)
10. Create `internal/config/settings.go` (config loading + validation)
11. Create `go.mod` with auto-shared replace directive
12. Run `go mod tidy` to generate `go.sum`
13. Create `CLAUDE.md` with build instructions
14. Add build/dist/install entries to root `Makefile`
15. Add row to sub-projects table in root `CLAUDE.md`
16. Run `go build ./cmd/auto{name}/` to verify compilation
17. Add domain-specific commands and packages
