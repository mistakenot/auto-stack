---
hash: "393facbf"
id: "206c93d2"
read_when: "implementing the unified auto binary or adding a new tool to the auto-cli umbrella module"
summary: "Verified codebase context for merging 10 tool binaries into a single auto binary: go.work feasibility, public command-tree constructors per tool, and the three non-standard tools that need normalization."
title: "Context: Task 017 — Unify Binaries into Auto"
---

# Context: Task 017

Verified codebase facts grounding the binary-merge design. See [solution.md](solution.md).

## Key Files

### Module wiring (determines go.work feasibility)
- No `go.work` exists at repo root today. Each tool module uses a relative
  `replace github.com/mistakenot/auto-shared => ../auto-shared` directive.
- All 11 modules + `auto-shared` are on `go 1.26.1` (consistent — no version conflict).
- Module paths: 9 tools under `github.com/mistakenot/auto-*`; **`auto-doc` is the outlier**:
  `auto-doc/go.mod:1` → `github.com/datadyne-io/autodoc`.
- **Only inter-tool dep:** `auto-graph/go.mod:6` requires `github.com/datadyne-io/autodoc`,
  `auto-graph/go.mod:16` `replace … => ../auto-doc`. All other tools require only auto-shared.
- **No `os.Executable()` anywhere; no tool execs another auto-* tool** — integration is via
  shared on-disk parquet/files only. Hard cutover needs no internal call-site rewrites.

### Command-tree constructors (the public seam needs these)
- Modern `Execute(ctx, stdout, stderr io.Writer) int` → `NewRootCmd(app)` pattern, **public** `NewRootCmd`:
  - `auto-search/internal/cli/root.go:43`, `auto-graph/internal/cli/root.go:43`,
    `auto-reflect/internal/cli/root.go:45`, `auto-skill/internal/cli/root.go:60`,
    `auto-ui/internal/cli/root.go:43`, `auto-watch/internal/cli/root.go:49`.
  - App built via `app.New(stdout, stderr)` (search/graph/reflect/skill/ui/watch).
- **Private** `newRootCmd(app)` (needs export): `auto-env/internal/cli/root.go:52`,
  `auto-config/internal/cli/root.go:51`. Both build app via `app.New(stdout, stderr, cwd)`.
- **Inline** (no constructor): `auto-doc/cmd/autodoc/main.go:19-48` builds `rootCmd` directly
  and `AddCommand`s `newInitCmd()…newUpdateCmd()`. No `app.App`. Needs extraction.
- **Global + init()** (`auto-etl`): `auto-etl/cmd/root.go:13` `var rootCmd`; registrations in
  `func init()` across `auto-etl/cmd/run.go:43`, `zen.go:45`, `update.go:29`, flags in
  `root.go:18`. `auto-etl/main.go` just calls `cmd.Execute()`. Needs init()→function refactor.

### Version / update
- Shared version pkg: `auto-shared/version/version.go:3` `var Version = "dev"`, injected by
  `Makefile:43` LDFLAGS `-X …/auto-shared/version.Version=$(VERSION)`. Every root sets
  `rootCmd.Version = version.Version`. One LDFLAGS line still works for the merged binary.
- Each tool has an `update` subcommand delegating to `auto-shared/update.Run(...)`, which
  re-runs `install.sh`. Since `install.sh` will install only `auto`, update needs no
  binary-name logic — it just re-runs the (updated) installer.

### Build / install / release plumbing
- `Makefile:15` `PROJECTS := auto-doc auto-env auto-etl auto-watch auto-search auto-reflect
  auto-skill auto-graph auto-ui` — **omits `auto-config`** (not built/tested in CI) and `auto-cli`
  (new). Per-binary `build-*`/`dist-*`/`install-*` targets at `:50-264`; `install` copies 9
  binaries individually with "text file busy" handling for autowatch (`:208-217`).
- `install.sh:6` `BINARIES="autodoc autoenv autoetl autograph autosearch autoreflect
  autoskill autowatch"` — **omits `autoui`** (built by Makefile but never installed/e2e'd).
- `.github/workflows/release.yml`: matrix `linux/amd64` + `darwin/arm64`; `make dist GOOS… GOARCH…`;
  uploads all `dist/*` (~18 assets today → 1 binary ×2 after merge); `verify-install` runs
  `e2e/test-curl-install.sh`. `ci.yml` delegates to `make check/build/test/vulncheck`.
- `e2e/test-install.sh:25` and `e2e/test-curl-install.sh:17` hardcode the same 8-binary list
  (no autoui); per-binary `--help`/`--version`/`init` checks.

### Daemon (autowatch)
- `auto-watch/internal/daemoninstall/template.go:21` `ExecStart={{.BinPath}} start`.
- `auto-watch/internal/daemoninstall/resolve.go:48` default BinPath
  `filepath.Join(home, ".local","bin","autowatch")`. `status.go` parses ExecStart back into BinPath.
- `manager.go:13-14` `defaultDescription="autowatch daemon"`, `defaultServiceBase="autowatch"`
  (service *name* can stay `autowatch.service`; only the binary path + `watch` subcommand change).

## Patterns
- **Config paths are tool-named, not binary-named** (`CLAUDE.md` global-config section;
  `docs/auto-package-patterns.md` config section): `~/.auto/{tool}/settings.json` and
  `.auto/{tool}/settings.json`. The merge must NOT change these — verified no tool derives
  paths from the binary name.
- **Standard subcommands every tool keeps** (`docs/auto-package-patterns.md`): `init`,
  `init --project`, `quickstart`, `docs`, `doctor`, `update`. These become `auto <tool> <std>`.
- **Resource noun+verb** (`CLAUDE.md` CLI conventions; auto-package-patterns "Resource
  Subcommands"): `<tool> <resource> list|describe|get`, `search` for discovery — becomes
  three levels deep, `auto <tool> <resource> <verb>`; cobra handles arbitrary depth.
- **`[autodoc()]` tags are a data format, not a CLI invocation** — many `autodoc` matches in
  source are these tags and must NOT be rewritten (AC-7 allowlist).

## Reference-surface scale (from task 017 deep search)
- ~600–900 actionable references concentrated in: README + install.sh + Makefile, root
  CLAUDE.md Sub-Projects table, 10 per-project CLAUDE.md, the `*/internal/cli`
  `quickstart.go`/`root.go`/`docs.go`/`doctor.go` help strings, `docs/user-journey.md`,
  `docs/autostack-install-daemon.md`, and the `reflect-on-agent-sessions` (30 refs) + `release` skills.
- `docs/autostack-install-daemon.md` anticipated a separate `autostack` umbrella command for
  host-level daemon ops — now subsumed by `auto` (daemon lives under `auto watch`).

## Plan-grounding facts (verified current — for plan.md steps)

### Exact refactor targets
- **auto-etl** (`module github.com/mistakenot/auto-etl`, entry `.`): state to move into a
  `NewRootCmd()` builder — `auto-etl/cmd/root.go:11` `var debug bool`; `:13` `var rootCmd`
  (`Use:"autoetl"`); `:18` `func init()` sets Version + persistent `--debug`; `:23`
  `func Execute()`. Subcommands registered via `func init()` in: `cmd/run.go` (`var runCmd`
  + 6 flags `--input/--output/--full/--only/--repo-path/--since` + package vars
  `inputDir,outputDir,fullRun,onlyFlag,repoPathFlag,sinceFlag`), `cmd/zen.go` (`var zenCmd`,
  `Hidden:true`), `cmd/update.go` (`var updateCmd` → `update.Run`). `auto-etl/main.go` is just
  `cmd.Execute()`. `cmd/genstats/main.go` is a separate standalone util (leave alone).
  **Note:** keep `auto-etl/main.go` working — `auto-search/internal/cochange/fixturegen`
  runs auto-etl from source via `go run .`.
- **auto-doc** (`module github.com/datadyne-io/autodoc`): everything is inline in
  `auto-doc/cmd/autodoc/main.go` — `var jsonOutput`; `main()` builds `rootCmd`
  (`Use:"autodoc"`, persistent `--json`) and `AddCommand`s 12 `newXCmd()` factories
  (init/tree/stale/agents/fix/fixed/graph/search/quickstart/docs/doctor/update), **all defined
  in the same file (package main)**, lines ~63–367. Extraction = create
  `auto-doc/internal/cli/root.go` `func NewRootCmd() *cobra.Command` and move the factories
  into that package (or have package main expose them); the wrapper imports it.
- **auto-env / auto-config**: only need `newRootCmd` → `NewRootCmd` exported.
  `auto-env/internal/cli/root.go:52` `func newRootCmd(application *app.App) *cobra.Command`
  (`Use:"autoenv"`); `auto-config/internal/cli/root.go:51` (`Use:"autoconfig"`). Both apps:
  `app.New(stdout, stderr, cwd)` (3-arg, cwd from `os.Getwd()` in their `Execute`).
- **Modern pattern (template for wrappers)**: `auto-search/internal/cli/root.go:43`
  `func NewRootCmd(application *app.App) *cobra.Command` (`Use:"autosearch"`); `:26`
  `func Execute(ctx, stdout, stderr) int` does `app.New(stdout,stderr)` (2-arg) →
  `NewRootCmd` → `ExecuteContext`. `auto-search/internal/app/app.go:` `New(stdout,stderr)`
  computes cwd internally. Same shape for graph/reflect/skill/ui/watch.

### Daemon (auto-watch) — both ExecStart paths
- `auto-watch/internal/daemoninstall/template.go` unit template line `ExecStart={{.BinPath}} start`.
- `auto-watch/internal/daemoninstall/resolve.go:48` default BinPath
  `filepath.Join(homeDir,".local","bin","autowatch")` → `…,"auto")`.
- `auto-watch/internal/daemoninstall/status.go:60` `ExecStart: parsed.BinPath + " start"`
  → `+ " watch start"`; runtime status shell-out `:168` `args := []string{"HOME="+…,
  spec.BinPath, "status", "--json"}` and `:176` sudo variant → insert `"watch"` before
  `"status"` in both.

### Test helper template (for auto-cli/cmd/auto/main_test.go)
- Reuse the pattern at `auto-search/internal/cli/cli_integration_test.go:90` `runCLI(t, args...)`
  → builds `cli.NewRootCmd(app.New(&out,&err))`, `SetArgs`, `ExecuteContext`, returns
  `(stdout,stderr,code)`; and the help-flag assertion at
  `auto-search/internal/cli/cochange_integration_test.go:75` (asserts `--help` exit 0 +
  `strings.Contains(out, flag)` for each expected flag). The umbrella test builds the root
  `auto` cmd instead and runs `auto etl run --help`, `auto doc --help`, etc.

### Build / CI / workspace safety
- CI (`.github/workflows/ci.yml`) uses Go 1.26 and **only Makefile targets** (`make
  install-tools/check/build/test/vulncheck`); no per-module matrix, no e2e. `release.yml`
  matrix linux/amd64 + darwin/arm64, `make dist`, uploads `dist/*`, verify-install runs
  `e2e/test-curl-install.sh`. **No workflow/Makefile references `go.work`/`GOWORK`** — adding a
  committed `go.work` is transparent; per-module `cd $$d && go …` loops still work; cache key
  is `**/go.sum`. No CI YAML changes required.
- Makefile loops (`fmt`/`fmt-check`/`lint`/`vet`/`vulncheck`/`test`) all iterate `PROJECTS`
  (`Makefile:15`, currently 9 dirs — **omits auto-config**). Adding `auto-config` + `auto-cli`
  to PROJECTS pulls them into all quality/test loops. `build`/`dist` derive targets from
  PROJECTS via `$(subst auto-,,…)`; `install` (`:202`) is explicit `cp` lines, not a loop.
- e2e `BINARIES` lists (`test-install.sh:25`, `test-curl-install.sh:17`) **omit autoui**
  (the known gap); both do per-binary `--help` (+`--version` in curl), and test-install.sh
  has a per-tool `init` block (`:159-182`).

### Conventions
- Commits: `feat(017): phase N - <desc>`; multi-phase tasks commit per phase on a feature
  branch, then squash-merge via PR (`feat(017): … (#PR)`). Feature work goes via **branch + PR**,
  not direct to main (CLAUDE.md + memory). Task 016/013 are the phasing templates.
- `feedback.md` convention exists (Problems faced / Reflections / Useful context) — written at
  completion, not now.

## Related Tasks
- Task 016 (etl-preserve-session-signal): most recent shipped ETL change; confirms auto-etl
  `cmd/` is actively evolving — the init()→NewRootCmd refactor should preserve its current flags.
  Strictly-linear 4-phase model with per-phase commits.
- Task 013 (auto-ui-tech-base): established the auto-ui `cmd/autoui` + `internal/cli` layout
  the wrapper-package approach reuses; its plan added auto-ui to PROJECTS + build/dist/install
  targets — the template for wiring auto-cli into the Makefile.
