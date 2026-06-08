# Plan: Task 017

## Summary
Merge the 10 tool binaries into one `auto` binary via a go.work workspace + an `auto-cli`
umbrella module + a thin public `rootcmd` wrapper per tool, then sweep build/CI/install/daemon
plumbing and all user-facing text to the `auto <tool>` form — executed as a **Claude workflow**
(fan-out where work is disjoint, sequential where it has a barrier).

## Changes
| Symbol | File | Description |
|--------|------|-------------|
| + | `go.work` | Workspace listing all 12 modules (11 existing + auto-cli) |
| + | `auto-cli/go.mod`, `auto-cli/cmd/auto/main.go`, `auto-cli/cmd/auto/main_test.go` | Umbrella module: root `auto` cmd mounting 10 tools + `auto update`; integration tests |
| + | `auto-{config,doc,env,etl,graph,reflect,search,skill,ui,watch}/rootcmd/rootcmd.go` | Public seam: `New(stdout,stderr) *cobra.Command` per tool |
| ~ | `auto-etl/cmd/{root,run,zen,update}.go` | `var rootCmd`+`init()` → `NewRootCmd()` builder; `Use:"autoetl"`→`"etl"`; help text |
| + | `auto-doc/internal/cli/root.go` | Extracted `NewRootCmd()` (+ factories moved from `cmd/autodoc/main.go`); `Use:"autodoc"`→`"doc"` |
| ~ | `auto-env`/`auto-config` `internal/cli/root.go` | `newRootCmd`→`NewRootCmd`; `Use`→stem; help text |
| ~ | `auto-{search,graph,reflect,skill,ui,watch}/internal/cli/{root,quickstart,docs,doctor}.go` | `Use`→stem; help strings → `auto <tool> …` |
| ~ | `auto-search/internal/cochange/*.go` | Error hints `autoetl run`→`auto etl run`, etc. |
| ~ | `auto-watch/internal/daemoninstall/{template,resolve,status,install,restart,validate}.go` + `internal/cli/{daemon,ops,task,root}.go` | BinPath `…/auto`; ExecStart + runtime-status gain `watch` infix; remediation strings `autowatch …`→`auto watch …` (manager.go service identity RETAINED) |
| ~ | `Makefile` | One `auto` build/dist/install; add auto-config + auto-cli to PROJECTS loops |
| ~ | `install.sh`, `e2e/test-install.sh`, `e2e/test-curl-install.sh` | Single `auto` asset; `auto <tool>` invocations; assert old names absent; fix autoui gap |
| + | `scripts/check-no-stale-binary-refs.sh` | Scoped CI guard for AC-7 |
| ~ | `README.md`, `CLAUDE.md`, `auto-*/CLAUDE.md`, `docs/user-journey.md`, `docs/autostack-install-daemon.md` | Docs sweep → `auto <tool>` |
| ~ | Skill **sources**: `auto-reflect/skills/reflect-on-agent-sessions/`, `skills/{release,self-improve}/`, `.claude/skills/{skill-reviewer,new-solution}/` → `auto <tool>`; then regenerate `.agents/` | Edit sources, not generated `.agents/` mirror |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test
- [ ] `auto-cli/cmd/auto/main_test.go` — mounted-path integration: `auto etl run --help`
      lists `--input/--output/--full/--only/--repo-path/--since`; `auto etl` has persistent
      `--debug`; `auto doc --help` lists 12 subcommands + `--json`; all 10 stems `--help` exit 0
- [ ] `auto-watch/internal/daemoninstall/*_test.go` — generated unit ExecStart `…/auto watch start`; runtime status args `[…/auto, watch, status, --json]`
- [ ] existing `cd <tool> && go test ./...` (per-tool behaviour, all green under workspace)
- [ ] `scripts/check-no-stale-binary-refs.sh` exits 0 on the swept tree
- [ ] `e2e/test-install.sh` / `e2e/test-curl-install.sh` — single `auto` installs; `auto <tool> --help`/`--version`; old binary names absent
- [ ] `make check build test vulncheck` green

## Execution Sequence
```
Phase 1 (Foundation: go.work + auto-cli skeleton)
   │
   ├──> Phase 2 (Per-tool seam+rename: 10 parallel agents, one per tool dir)
   │        │
   │        └──> Phase 3 (Wire umbrella main + main_test.go)  ──> Phase 4 (Build/install/CI plumbing + guard)
   │                                                                      │
   └──> Phase 5 (Docs + skills sweep: ~4 parallel, depends only on Phase 1 naming)
                                                                          │
   Phase 6 (Verify: make check/build/test/vulncheck + guard + e2e) <─────┴── (after 3,4,5)
```
Parallelism: Phase 2 = up to 10 concurrent agents (disjoint tool dirs, no worktree isolation
needed). Phase 5 runs concurrently with 2–4. Phases 1, 3, 6 are barriers.

## Plan

### Phase 1: Foundation — workspace + umbrella skeleton  *(sequential; blocks all)*
- [x] Step 1.1: Create `auto-cli/go.mod` (`module github.com/mistakenot/auto-cli`, `go 1.26.1`);
      add `require` + `replace … => ../<dir>` for each of the 10 tool modules **using each
      module's real path** (auto-doc = `github.com/datadyne-io/autodoc => ../auto-doc`; the
      other 9 = `github.com/mistakenot/auto-<dir>`) and `auto-shared`. Template: `auto-graph/go.mod:6,16`.
- [x] Step 1.2: Create `go.work` at repo root with `go 1.26.1` and `use (…)` listing all 11
      existing module dirs + `./auto-cli`.
- [x] Step 1.3: Create a minimal compiling `auto-cli/cmd/auto/main.go` (root `auto` cobra cmd,
      `Version = version.Version`, no tool subcommands yet) so the module builds.
- [x] Step 1.4: Verify: `cd auto-cli && go build ./...` succeeds; `go work sync` clean; from
      repo root `go build ./auto-cli/cmd/auto` produces a runnable `auto --help` (exit 0).
- [x] Step 1.5: Verify no regression: `make check` still green for existing modules under the
      new workspace (run `cd auto-search && go vet ./...` as a spot check — workspace transparent).
- [x] Step 1.6: Commit: `feat(017): phase 1 - go.work + auto-cli umbrella skeleton`

### Phase 2: Per-tool seam + rename  *(fan out: one agent per tool dir; depends on Phase 1)*
Each agent owns exactly one tool dir end-to-end (disjoint paths ⇒ no conflicts). Per tool:
- [x] Step 2.x.a: Ensure a callable `*cobra.Command` constructor exists:
  - search/graph/reflect/skill/ui/watch — already `NewRootCmd(app)` (no change).
  - env/config — rename `newRootCmd` → `NewRootCmd` (export); fix internal callers.
  - doc — create `auto-doc/internal/cli/root.go` `NewRootCmd()` and move the 12 `newXCmd()`
    factories + `--json` persistent flag out of `cmd/autodoc/main.go` into that package; leave
    `cmd/autodoc/main.go` as a thin `os.Exit(cli.Execute(...))` (do not delete — keeps module buildable).
  - etl — refactor `cmd/root.go` `var rootCmd`+`init()` and the `init()` blocks in
    `run.go/zen.go/update.go` into one `func NewRootCmd() *cobra.Command` that declares the
    `--debug` persistent flag and all `run` flags locally and `AddCommand`s run/zen/update.
    Keep `Execute()` (delegates to `NewRootCmd()`) and `main.go` working (fixturegen `go run .`).
- [x] Step 2.x.b: Add `<tool>/rootcmd/rootcmd.go` (package `rootcmd`) exposing
      `func New(stdout, stderr io.Writer) *cobra.Command` that builds the app (2-arg
      `app.New(stdout,stderr)`; env/config use 3-arg with `os.Getwd()`) and returns the root cmd.
- [x] Step 2.x.c: Rename the root `Use:` from `auto<tool>` to the bare stem
      (`search`,`etl`,`doc`,…). Rewrite ALL shipped invocation strings in that tool's tree —
      `root.go`/`quickstart.go`/`docs.go`/`doctor.go` **and any other `internal/**/*.go` runtime/
      error/remediation string** — to the `auto <tool> …` form. (auto-search agent also:
      `internal/cochange/{cochange,repo}.go` hints `autoetl run --only git`→`auto etl run --only git`.)
- [x] Step 2.watch.d (auto-watch agent only): apply the daemon changes —
      `daemoninstall/template.go` `ExecStart={{.BinPath}} watch start`; `resolve.go:48` default
      BinPath `…/bin/auto`; `status.go:60` `+ " watch start"` and `:168/:176` insert `"watch"`
      before `"status"`. **Also rewrite all remediation/help invocation strings:**
      `daemoninstall/{install,restart,status,validate}.go` (`rerun … autowatch daemon …` →
      `auto watch daemon …`; `the autowatch binary` → `the auto binary`) and
      `cli/{daemon,ops,task,root}.go` (`autowatch status/init/trigger …` → `auto watch …`;
      `--bin` help → "the auto binary"). **RETAIN** service-identity in `daemoninstall/manager.go`
      (`defaultServiceBase="autowatch"`, `defaultDescription="autowatch daemon"`, `autowatch.service`)
      and the prose `"autowatch systemd service"`. Add/extend `daemoninstall/*_test.go` for the
      ExecStart + runtime-status `watch` infix (AC-5).
- [x] Step 2.x.e: Update that tool's own `*_test.go` expectations changed by the `Use`/help rename.
- [x] Step 2.x.f: Verify per tool: `cd <tool> && go build ./... && go test ./...` green;
      `go run ./rootcmd`-equivalent not needed, but `go vet ./...` clean.
- [x] Step 2.Z: Commit (one per tool or grouped): `feat(017): phase 2 - <tool> seam + rename`

### Phase 3: Wire the umbrella + integration tests  *(sequential; depends on all of Phase 2)*
- [x] Step 3.1: Fill `auto-cli/cmd/auto/main.go` — import the 10 `rootcmd` packages (aliased,
      e.g. `doccmd "github.com/datadyne-io/autodoc/rootcmd"`), `AddCommand` all 10, add a
      top-level `auto update` using `auto-shared/update.Run`. Set root `Short`/`Version`.
- [x] Step 3.2: `go work sync`; `cd auto-cli && go build ./cmd/auto`; run `auto --help` and
      confirm all 10 stems + `update` are listed.
- [x] Step 3.3: Write `auto-cli/cmd/auto/main_test.go` using the `runCLI`/`SetOut(buf)` pattern
      (`auto-search/internal/cli/cli_integration_test.go:90`). **Content assertions on `--help`
      only** (capturable); **executed commands by exit code only** (auto-doc/auto-etl write to the
      real os.Stdout, not the buffer — see solution.md AC-2 "Capture caveat"). Assert:
      (a) `auto etl run --help` lists all 6 run flags (`--input/--output/--full/--only/--repo-path/
      --since`) with defaults + `auto etl --help` shows persistent `--debug`; `auto etl zen --help`
      / `auto etl update --help` exit 0;
      (b) `auto doc --help` lists all 12 subcommands and shows `--json`; `auto doc tree --json`
      and `auto doc tree` **exit 0** (execution sanity — NOT a JSON-content check);
      (c) each of the 10 stems `auto <stem> --help` exits 0 with usage line `auto <stem>`;
      (d) `config` and `ui` subcommands present (AC-6).
<!-- RESOLVED(P2): Step 3.3(b) "auto doc tree --json emits valid JSON" is not capturable via the runCLI buffer pattern
REVIEW: auto-doc hard-codes os.Stdout (cmd/autodoc/main.go, e.g. `commands.TreeOutputJSON(os.Stdout, …)`) and auto-etl uses bare `fmt.Print*`, so the runCLI/SetOut buffer approach captures NOTHING for executed commands (only cobra `--help` text is routed through the command writer). The `auto doc tree --json` content check would assert against an empty buffer. See the matching solution.md AC-2 thread for the three options (exit-code-only, os.Pipe fd capture, or thread writers in the extraction) — resolve there and update this step to match.
AUTHOR: Resolved per solution.md AC-2 (option a). Step 3.3 now: content assertions on `--help` only; `auto doc tree --json`/`auto doc tree` asserted by exit code 0, not JSON content. Added the `--repo-path` flag and moved zen/update to `--help` form. Output correctness for doc/etl stays in their in-module tests.
-->

- [x] Step 3.4: Verify: `cd auto-cli && go test ./...` green (these are the AC-1/AC-2/AC-6 gates).
- [x] Step 3.5: Commit: `feat(017): phase 3 - umbrella main mounts 10 tools + integration tests`

### Phase 4: Build / install / release plumbing + CI guard  *(depends on Phase 3)*
- [ ] Step 4.1: Makefile — add `auto-config` + `auto-cli` to `PROJECTS`; replace per-binary
      `build-*`/`dist-*`/`install-*` with a single `auto` build (`cd auto-cli && go build
      -ldflags=… -o ../bin/auto ./cmd/auto`), dist (`…-o ../dist/auto-$(SUFFIX) ./cmd/auto`),
      and install (`cp bin/auto $(INSTALL_DIR)/`, keep "text file busy" handling for the running
      daemon). Keep `check/test/vet/lint/vulncheck` PROJECTS loops (now incl. config + cli).
- [ ] Step 4.2: `install.sh` — `BINARIES="auto"`; download single `auto-<suffix>` asset; keep
      running-binary replace logic.
- [ ] Step 4.3: e2e — both scripts: `BINARIES="auto"`; per-tool checks become `auto <tool>
      --help`/`--version`; rewrite test-install.sh `init` block to `auto <tool> init`; **add an
      assertion that old names (autoetl, autosearch, …, autoui) are NOT present** (AC-3); this
      also closes the autoui gap.
- [ ] Step 4.4: Create `scripts/check-no-stale-binary-refs.sh` per the (widened) Guard spec in
      solution.md — old-stem + whitespace + subcommand/flag pattern over **all tracked
      `auto-*/**/*.go` (except `*_test.go`/genstats/fixturegen) AND every tracked `**/SKILL.md`**
      plus README/CLAUDE.md/docs; exclude go.mod & `github.com/...` import paths, `[autodoc()]`,
      `~/.auto/<tool>` stems, `docs/tasks/**`, `.claude/worktrees/**`; curated RETAIN allowlist =
      systemd service identity only (`defaultServiceBase`/`defaultDescription`=autowatch,
      `autowatch.service`, `"autowatch systemd service"`, standalone `"the autowatch daemon"`).
      Wire into `make check`.

<!-- RESOLVED(P2): the Guard spec SCAN SCOPE (and the Phase 2/5 sweep file lists) miss files that contain shipped old invocations
REVIEW: See the solution.md threads on the daemoninstall/cochange remediation strings and the duplicate skill copies. Concretely, this step's guard scope must be widened (and the corresponding sweep steps must list the files) to cover: auto-watch/internal/daemoninstall/{install,restart,status,ops}.go (Phase 2.watch), auto-search/internal/cochange/*.go (Phase 2.search — listed in solution Files but outside the current guard scope), auto-watch/internal/cli/{daemon,ops}.go, and the duplicate skill copies under `skills/**` + `auto-reflect/skills/**` (Phase 5.4). As written, a missed `autowatch daemon install` / `autoetl run` / `autosearch` hint in any of these ships with green CI (false AC-8 pass, real AC-7 violation).
AUTHOR: All addressed (mirrors the two solution.md threads). Guard scope (Step 4.4) widened to all tracked `auto-*/**/*.go` runtime strings + every tracked `**/SKILL.md`, with a curated service-identity retain-list. Sweep file lists expanded: Step 2.watch.d now enumerates daemoninstall/{install,restart,status,validate}.go + cli/{daemon,ops,task,root}.go (and the manager.go RETAIN); Step 2.x.c covers cochange/{cochange,repo}.go. Step 5.4 now edits the SOURCE skill copies (`skills/*`, `auto-reflect/skills/*`) + regenerates `.agents/`. So a missed hint now fails CI.
-->

- [ ] Step 4.5: Verify: `make build` produces only `bin/auto`; `make dist GOOS=linux GOARCH=amd64`
      produces only `dist/auto-linux-amd64`; `make check` runs the guard and passes.
- [ ] Step 4.6: Commit: `feat(017): phase 4 - single-binary build/install/release + stale-ref guard`

### Phase 5: Docs + skills sweep  *(fan out ~4; depends only on Phase 1 naming; runs alongside 2–4)*
- [x] Step 5.1: README.md — install/update block, status table, quickstart command blocks,
      mermaid node labels → `auto <tool>`; document `auto update` as canonical (per-tool variants equivalent).
- [x] Step 5.2: Root `CLAUDE.md` (Sub-Projects table, doc-index, quickstart pointers) + the 10
      `auto-*/CLAUDE.md` command docs → `auto <tool> …`.
- [x] Step 5.3: `docs/user-journey.md` and `docs/autostack-install-daemon.md` (daemon now under
      `auto watch`; add the "post-upgrade: re-run `auto watch daemon install`" note).
- [x] Step 5.4: Skills — edit the **SOURCE** copies (NOT the generated `.agents/` mirror):
      `auto-reflect/skills/reflect-on-agent-sessions/SKILL.md` (30 `autosearch`→`auto search`),
      `skills/release/SKILL.md` (assets → single `auto-<suffix>`), `skills/self-improve/SKILL.md`
      (example refs); for `skill-reviewer`/`new-solution` locate their tracked source (under
      `.claude/skills/`) and edit there. Then **regenerate** the `.agents/` copies and stage them:
      `make skills-sync` (covers `skills/*`) **plus** `npx skills install "$(CURDIR)/auto-reflect/skills" -y`
      (skills-sync does NOT cover `auto-reflect/skills`). Confirm source and `.agents/` copies match.
- [x] Step 5.5: Verify: `scripts/check-no-stale-binary-refs.sh` exits 0 over the docs + ALL
      tracked `**/SKILL.md` (source + `.agents/`) scope.
- [x] Step 5.6: Commit: `docs(017): phase 5 - sweep README/CLAUDE.md/docs/skills to auto <tool>`

### Phase 6: Verify end-to-end  *(sequential barrier; depends on 3, 4, 5)*
- [ ] Step 6.1: `make check` (fmt-check + vet + lint + stale-ref guard) green across all PROJECTS.
- [ ] Step 6.2: `make build` (single `auto`) + `make test` (all modules incl. auto-cli/auto-config) green.
- [ ] Step 6.3: `make vulncheck` green.
- [ ] Step 6.4: Run `e2e/test-install.sh` (local build path) — single `auto`, all `auto <tool>`
      invocations succeed, old names absent.
- [ ] Step 6.5: Manual smoke: `bin/auto watch daemon install --dry-run` (or unit-test
      equivalent) shows `ExecStart=…/auto watch start`.
- [ ] Step 6.6: Commit: `feat(017): phase 6 - full verification green` (or fold fixes into prior phases).

## Success Criteria
- [ ] `make build` yields exactly one binary `bin/auto`; `make dist` yields one `auto-<suffix>` per platform (AC-1, AC-3, AC-4)
- [ ] `auto-cli/cmd/auto/main_test.go` green: all 10 stems `--help` exit 0 with `auto <tool>` usage; auto-etl run flags + persistent `--debug` intact; auto-doc 12 subcommands + `--json` intact (AC-1, AC-2)
- [ ] `auto config` and `auto ui` are mounted and covered by e2e (AC-6)
- [ ] daemon tests assert generated unit ExecStart **and** runtime status use `auto … watch …` (AC-5)
- [ ] `scripts/check-no-stale-binary-refs.sh` exits 0; no shipped help/README/CLAUDE.md/skill references an unshipped binary name (`[autodoc()]` exempt) (AC-7)
- [ ] `make check build test vulncheck` + e2e install scripts all green (AC-8)
- [ ] No per-tool binary is built/installed; old names absent after install (AC-3)

## Open Questions
- (none — all resolved in requirements + solution; module strategy = go.work + wrapper pkgs)
