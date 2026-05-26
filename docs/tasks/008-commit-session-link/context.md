# Context: Task 008

Codebase context for the commit-to-session link implementation. See [solution.md](./solution.md) for the design.

## Key Files

### Commit model and trailer extraction

- `auto-etl/internal/model/git.go:39-82` — `Commit` struct. The `SessionID` field will be added after `TrailersJSON` (line 74). All fields have `parquet:"..."` tags. Schema version is 2 (`auto-etl/internal/model/model.go:5`).

- `auto-etl/internal/git/extract.go:415-460` — Where each `Commit` is assembled. Line 417 calls `parseTrailers(messageBody)` and stores the result in `TrailersJSON`. The `SessionID` extraction goes right after this, before the struct literal at line 435.

- `auto-etl/internal/git/extract.go:740-788` — `parseTrailers()` returns JSON like `{"Session-Id":["uuid"],"Co-Authored-By":["name <email>"]}`. To extract session ID: unmarshal as `map[string][]string`, take first element of `"Session-Id"` key.

### Messages parquet (for fallback)

- `auto-etl/internal/model/model.go:23-63` — `AgentMessage` struct. Key fields:
  - `SessionID string` with tag `parquet:"session_id,dict"` (line 26)
  - `BashCommand string` with tag `parquet:"bash_command"` (line 42)
  - `Content string` with tag `parquet:"content"` (line 31)

- `auto-etl/internal/writer/github.go:143-164` — `readExistingParquet[T]()` generic function. Returns `nil, nil` if file doesn't exist. Uses `parquet.NewGenericReader[T]`. This is the pattern for reading messages parquet in the fallback.

### Git ETL orchestration

- `auto-etl/cmd/run.go:367-451` — `runGitETL()` function. The loop at line 396 iterates repos, calls `ExtractRepo` (line 414), then `WriteGit` (line 420). `LinkSessionIDs` inserts between these two calls — after line 414, before line 420. The `outputDir` variable is available in scope and points to `~/.auto/etl/output`.

### Auto-config reference patterns

- `auto-doc/cmd/autodoc/main.go:63-81` — Reference `init` command with `--project` flag pattern:
  ```go
  func newInitCmd() *cobra.Command {
      var project bool
      cmd := &cobra.Command{
          Use:   "init",
          RunE: func(cmd *cobra.Command, args []string) error {
              if project {
                  return commands.InitProject(os.Stdout, cwd)
              }
              return commands.InitGlobal(os.Stdout)
          },
      }
      cmd.Flags().BoolVar(&project, "project", false, "...")
      return cmd
  }
  ```

### Existing hook

- `hooks/prepare-commit-msg` — 24-line bash script. Reads `CLAUDE_CODE_SESSION_ID` env var, skips merges/squashes, uses `git interpret-trailers --in-place` to append `Session-Id: $SID`. Currently untracked in git. This file will be embedded via `go:embed` in auto-config.

## Patterns

### Parquet struct convention
All parquet-mapped structs use `parquet:"field_name"` tags with optional `,dict` for dictionary encoding on high-cardinality string fields. New fields are added to the struct — the parquet writer picks them up automatically via reflection.

### Generic parquet reader
`readExistingParquet[T]()` in `writer/github.go` is the reusable pattern. For fallback, we need a lightweight struct that reads only the 3 columns we need (`session_id`, `bash_command`, `content`) rather than the full 36-field `AgentMessage`. Parquet-go handles partial reads — missing struct fields are silently skipped.

### Auto-package conventions (from `docs/auto-package-patterns.md`)
- Directory: `auto-config/cmd/autoconfig/main.go`, `auto-config/internal/cli/`, `auto-config/internal/hooks/`
- Module: `github.com/mistakenot/auto-config` with `replace ../auto-shared`
- Go version: `1.26.1`
- All implementation under `internal/`
- One file per command in `cli/`

### No existing go:embed usage
No `//go:embed` directives exist in the monorepo yet. This will be the first use.

## Related Tasks

- **Task 002** (`docs/tasks/002-git-history-etl/`): Established the git ETL pipeline, `Commit` struct, `parseTrailers()`, and parquet writer. Direct foundation for Task 008.

## Design Tension

The doc `auto-etl/docs/git-history-etl.md` (line 94) says commits should not store derived interpretations like session links — those belong in derived datasets. It proposes a separate `commit_session_links` table with confidence scores. Task 008 requirements deliberately chose the simpler approach: a `session_id` field directly on the `Commit` row. The requirements are authoritative here.
