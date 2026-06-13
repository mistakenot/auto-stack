---
hash: "b8ee1555"
id: "716c9303"
read_when: "implementing the unified auto binary, adding a new rootcmd seam, or normalizing a tool's command structure"
summary: "Solution for merging 10 tool binaries into one auto binary via go.work workspace and thin rootcmd public seam per tool, with normalization of three non-standard tools and rename of root Use: values."
title: "Solution: Task 017 — Unify Binaries into Auto"
---

# Solution: Task 017

## Approach

Merge the 10 tool binaries into one `auto` binary using a **go.work workspace + a thin
public wrapper package per tool**, then sweep all build/CI/install/daemon plumbing and
user-facing text to the new `auto <tool>` form. No import-path rewrites.

1. **Add the workspace + umbrella module.** Create `go.work` at repo root listing all 11
   existing modules plus a new `auto-cli` umbrella module (`github.com/mistakenot/auto-cli`)
   whose `cmd/auto/main.go` builds the root `auto` command and mounts every tool.

2. **Expose each tool's command tree through a public seam.** Go's `internal/` rule
   forbids `cmd/auto` (a different module) from importing `<tool>/internal/cli`. Add a tiny
   public package `<tool>/rootcmd` (within each tool module, so it *can* see `internal/...`)
   exposing `func New(stdout, stderr io.Writer) *cobra.Command`. The umbrella imports the 10
   wrappers and `AddCommand`s them.

3. **Normalize the 3 non-standard tools** so each has a callable `NewRootCmd(app)`:
   - `auto-env`, `auto-config`: export `newRootCmd` → `NewRootCmd`.
   - `auto-doc`: extract the inline `rootCmd` from `cmd/autodoc/main.go` into a reusable
     `NewRootCmd()` in `internal/cli`.
   - `auto-etl`: refactor the package-global `var rootCmd` + `func init()` registrations
     (`run.go`, `zen.go`, `update.go`, `root.go`) into a single `NewRootCmd()` builder.

4. **Rename each tool's root `Use:` to its bare stem** (`autosearch` → `search`, etc.) and
   rewrite all embedded help text (`quickstart`/`docs`/`doctor`/`root.go` + error
   remediation hints) to the `auto <tool> …` form. `[autodoc()]` source tags are a data
   format — left untouched.

5. **Collapse the build/install/release plumbing** to one binary: Makefile, `install.sh`,
   both e2e scripts, and (implicitly) `release.yml` via `make dist`.

6. **Point the autowatch daemon at the merged binary**: default `BinPath` → `~/.local/bin/auto`,
   `ExecStart={{.BinPath}} watch start`. Update **both** ExecStart paths in
   `daemoninstall`: (a) unit generation/parse (`template.go`, `status.go` ExecStart
   reconstruct), AND (b) the runtime `status --json` shell-out, which must gain the `watch`
   infix (see status.go thread). Document the one-time post-upgrade step for existing
   deployments (re-run `auto watch daemon install` to regenerate the orphaned unit).

<!-- RESOLVED(P3): hard cutover orphans any already-installed systemd unit pointing at the removed `autowatch` binary
REVIEW: An existing deployment has a generated unit with `ExecStart=~/.local/bin/autowatch start`. After this change, install.sh installs only `auto` and the old `autowatch` binary is gone, so the live unit's ExecStart dangles and the daemon fails to (re)start on next boot/restart — silently, until someone checks. The new template only affects units generated *after* upgrade. Is re-running `auto watch daemon install` to regenerate the unit an expected manual step, or should the upgrade path handle it? Given the single-user journey this may be acceptable, but it should be a documented post-upgrade step (e.g. in docs/autostack-install-daemon.md) rather than an implicit break.
AUTHOR: Accepted as a documented manual step (consistent with the single-user journey and the hard-cutover decision; auto-detecting/rewriting a foreign systemd unit during install is out of proportion). Step 6 now states the post-upgrade action explicitly, and the `docs/autostack-install-daemon.md` sweep (step 7 / Files) will add a "Post-upgrade: regenerate the unit" note: after upgrading to the merged binary, run `auto watch daemon install` once to replace the stale `ExecStart=…/autowatch start` unit with `…/auto watch start`. Added an AC-5 sub-check that a freshly generated unit + the runtime status path both use the `auto … watch …` form.
-->


7. **Sweep docs + skills**: README, root + per-project CLAUDE.md, `docs/user-journey.md`,
   `docs/autostack-install-daemon.md`, and the `reflect-on-agent-sessions` / `release` skills.

### Subcommand map (stem = tool dir minus `auto-`)
`auto config | doc | env | etl | graph | reflect | search | skill | ui | watch`
Config/data paths stay tool-named (`~/.auto/search/…`, not binary-derived) — **no migration**,
since no tool derives paths from `os.Args[0]`/`os.Executable()`.

### Execution strategy: drive implementation with a Claude workflow
This work is large but highly parallelizable (10 mostly-independent tool dirs + a docs/skills
sweep over ~600–900 refs), so **implement it via a Claude workflow** (multi-agent
orchestration) rather than one linear pass. `/new-plan` should structure the phases so each
maps to a workflow stage:

1. **Foundation (sequential, 1 agent):** add `go.work` + the `auto-cli` umbrella module
   skeleton (`go.mod` + empty `cmd/auto/main.go`). Must land first — everything imports it.
2. **Per-tool seam + rename (fan out, 1 agent per tool, ~10 parallel):** each agent owns one
   tool dir end-to-end — add its `rootcmd` wrapper, normalize its constructor (the auto-etl
   refactor, auto-doc extract, env/config export), rename root `Use:`→stem, and sweep that
   tool's `internal/cli` help strings. Disjoint dirs ⇒ no file conflicts; worktree isolation
   not required (one tool subtree per agent).
3. **Wire the umbrella (sequential, 1 agent):** fill in `cmd/auto/main.go` mounting all 10
   wrappers + `auto update`; add `main_test.go` (AC-1/2/6 assertions). Depends on phase 2.
4. **Plumbing + daemon (fan out, ~3 parallel):** (a) Makefile/install.sh/e2e, (b)
   auto-watch daemoninstall ExecStart + runtime-status fix, (c) `scripts/check-no-stale-binary-refs.sh`.
5. **Docs/skills sweep (fan out, ~4 parallel):** README, CLAUDE.md set, `docs/*`, skills —
   independent files, one agent per area.
6. **Verify (sequential, 1 agent):** `make check build test vulncheck`, run the CI guard and
   e2e install scripts, fix fallout.

Adversarially verify the two high-risk refactors (auto-etl init()→builder, auto-doc extract)
in phase 3's tests before declaring done.

### Umbrella main (outline)
```go
// auto-cli/cmd/auto/main.go
func main() {
    root := &cobra.Command{Use: "auto", Short: "Autonomous coding stack", Version: version.Version}
    so, se := os.Stdout, os.Stderr
    root.AddCommand(
        configcmd.New(so, se), doccmd.New(so, se), envcmd.New(so, se),
        etlcmd.New(so, se),    graphcmd.New(so, se), reflectcmd.New(so, se),
        searchcmd.New(so, se), skillcmd.New(so, se), uicmd.New(so, se),
        watchcmd.New(so, se),
    )
    root.AddCommand(newUpdateCmd(so, se)) // canonical top-level `auto update` via auto-shared/update
```

`auto update` is the canonical, documented update path for the single binary. The per-tool
`update` subcommands (`auto search update`, `auto etl update`, …) are **intentionally
retained** as equivalent aliases — they already delegate to the same `auto-shared/update.Run`
(re-run `install.sh`), and removing them would mean editing each tool's command tree, which
is out of scope (requirements "Out of Scope": no subcommand changes). README/CLAUDE.md will
document `auto update` as the one to use and note the per-tool variants are equivalents.

<!-- RESOLVED(P3): top-level `auto update` is redundant with the per-tool `update` subcommands that stay mounted
REVIEW: Every tool keeps its own `update` subcommand (verified e.g. auto-search/internal/cli/update.go, auto-etl/cmd/update.go), and they all delegate to auto-shared/update.Run which just re-runs install.sh (context.md L36-39). After the merge there will be a top-level `auto update` PLUS `auto search update`, `auto etl update`, … — ~11 commands that do the identical thing (re-install the single `auto` binary). That's confusing surface. Either drop the per-tool `update` subcommands from the mounted trees (cleaner, but technically "changing a tool's subcommands" — out of scope as written) or explicitly document that the per-tool ones are retained aliases. At minimum, note the decision so it's intentional rather than accidental.
AUTHOR: Made the decision explicit (option 2): keep per-tool `update` as retained equivalents and add `auto update` as the canonical, documented path. Dropping the per-tool ones is out of scope (it changes each tool's subcommand set). Added a sentence below the outline stating the decision and that docs will steer users to `auto update`.
-->

```go
    if err := root.ExecuteContext(context.Background()); err != nil {
        fmt.Fprintln(se, err); os.Exit(1)
    }
}
```

### Wrapper (outline, one per tool; aliased on import in main)
```go
// auto-search/rootcmd/rootcmd.go
package rootcmd
func New(stdout, stderr io.Writer) *cobra.Command {
    return cli.NewRootCmd(app.New(stdout, stderr)) // config/env also pass cwd
}
```

### go.work (outline)
```
go 1.26.1
use (
    ./auto-shared ./auto-config ./auto-doc ./auto-env ./auto-etl ./auto-graph
    ./auto-reflect ./auto-search ./auto-skill ./auto-ui ./auto-watch ./auto-cli
)
```
Existing `replace … => ../auto-shared` directives stay (harmless; keep modules
standalone-testable). `auto-cli/go.mod` `require`s each tool's **module path** (not its dir
name) + `replace`s it to `../<dir>`, with go.work resolving the workspace build.

> **auto-doc is the outlier:** its module path is `github.com/datadyne-io/autodoc`
> (auto-doc/go.mod:1), NOT `github.com/mistakenot/auto-doc`. So the umbrella imports
> `github.com/datadyne-io/autodoc/rootcmd` (aliased `doccmd`), and auto-cli/go.mod needs
> `require github.com/datadyne-io/autodoc …` + `replace github.com/datadyne-io/autodoc => ../auto-doc`.
> Use auto-graph/go.mod:6,16 (which already requires+replaces autodoc this way) as the template.
> All other 9 tools follow `github.com/mistakenot/auto-<dir>`.

<!-- RESOLVED(P3): auto-doc's module path is an outlier — "require each tool module + replace to ../<tool>" is wrong for it
REVIEW: I verified auto-doc/go.mod:1 is `github.com/datadyne-io/autodoc`, not `github.com/mistakenot/auto-doc` (context.md L11 notes this). So the umbrella's import of its wrapper is `github.com/datadyne-io/autodoc/rootcmd` (aliased `doccmd`), and auto-cli/go.mod must `require github.com/datadyne-io/autodoc` + `replace github.com/datadyne-io/autodoc => ../auto-doc` — the module path ≠ the directory name for this one tool. The generic "require each tool module + replace to ../<tool>" rule silently breaks here. Worth calling out so the implementer doesn't write `github.com/mistakenot/auto-doc`. (auto-graph already does this correctly at auto-graph/go.mod:6,16 — use it as the template.)
AUTHOR: Corrected the generic rule to "module path, not dir name" and added an explicit callout block: auto-doc's module is `github.com/datadyne-io/autodoc`, so its require/replace/import use that path (wrapper import aliased `doccmd`), with auto-graph/go.mod:6,16 as the working template. Prevents the implementer writing `github.com/mistakenot/auto-doc`.
-->


## Files

```
+ go.work                                         # workspace listing all 12 modules
+ auto-cli/go.mod                                 # module github.com/mistakenot/auto-cli
+ auto-cli/cmd/auto/main.go                       # root `auto` cmd, mounts 10 tools + update
+ auto-cli/cmd/auto/main_test.go                  # smoke: all 10 subcommands present (AC-1,6)
+ auto-config/rootcmd/rootcmd.go                  # public seam → cli.NewRootCmd(app.New(...,cwd))
+ auto-doc/rootcmd/rootcmd.go                     # public seam
+ auto-env/rootcmd/rootcmd.go                     # public seam
+ auto-etl/rootcmd/rootcmd.go                     # public seam → cmd.NewRootCmd()
+ auto-graph/rootcmd/rootcmd.go                   # public seam
+ auto-reflect/rootcmd/rootcmd.go                 # public seam
+ auto-search/rootcmd/rootcmd.go                  # public seam
+ auto-skill/rootcmd/rootcmd.go                   # public seam
+ auto-ui/rootcmd/rootcmd.go                      # public seam
+ auto-watch/rootcmd/rootcmd.go                   # public seam
+ scripts/check-no-stale-binary-refs.sh           # CI guard for AC-7 (scoped scan + allowlist, see below)

# Guard spec (scripts/check-no-stale-binary-refs.sh):
#   Pattern: word-boundary OLD INVOCATION forms only — `\b(autodoc|autoenv|autoetl|
#     autograph|autoreflect|autosearch|autoskill|autoui|autowatch|autoconfig)\b`
#     followed by whitespace + a subcommand/flag token (e.g. `autosearch search`,
#     `autoetl run`, `autodoc --json`, `autowatch daemon install`, `autowatch status`).
#     Bare-token matches inside larger identifiers/paths do NOT count.
#   SCAN SCOPE = git-tracked files only (use `git ls-files`), covering the full shipped
#   surface — runtime strings included:
#     - README.md, CLAUDE.md, auto-*/CLAUDE.md, docs/user-journey.md, docs/autostack-install-daemon.md
#     - `auto-*/**/*.go` EXCEPT `*_test.go` and `**/cmd/genstats/**` — i.e. ALL runtime/help/
#       error/remediation strings (internal/cli/**, internal/daemoninstall/**, internal/cochange/**, etc.),
#       not just quickstart/docs/doctor/root.go
#     - every tracked `**/SKILL.md` (sources AND generated copies: skills/**, auto-reflect/skills/**,
#       .agents/skills/**) — see the skill source-of-truth note below
#   EXCLUDE (out of scope / legitimately retained):
#     - docs/tasks/** (archival planning docs, incl. THIS folder)
#     - **/go.mod, **/go.sum and any `github.com/...` import path (datadyne-io/autodoc survives)
#     - `[autodoc()]` data tags (regex-excluded)
#     - .claude/worktrees/** ; *_test.go fixtures ; auto-search/internal/cochange/fixturegen/** (dev-only `go run .` comments)
#     - config/data dir stems `~/.auto/<tool>/` and `.auto/<tool>/` (tool-named, not binary)
#     - CURATED RETAIN allowlist for systemd SERVICE IDENTITY (not binary invocations), exact strings:
#       `defaultServiceBase = "autowatch"`, `defaultDescription = "autowatch daemon"`,
#       `autowatch.service`, the prose phrases `"autowatch systemd service"` and
#       (standalone, not followed by a subcommand) `"the autowatch daemon"`.
#       NOTE: `"the autowatch binary"` is NOT retained — the binary is now `auto` (rewrite to `auto`).
#   The stem+subcommand pattern + the small curated retain-list (service identity only) is what
#   keeps the guard tractable while now covering runtime error/remediation output (AC-7 surface).

<!-- RESOLVED(P2): the stale-ref CI guard needs a much broader allowlist than just [autodoc()], or it will fail CI on legitimately-retained names
REVIEW: A grep for old binary stems will hit many references that MUST stay and are not CLI invocations:
  - Module path `github.com/datadyne-io/autodoc` (auto-doc/go.mod:1) and auto-graph's `require`/`replace` of it (auto-graph/go.mod:6,16) — the import-path "autodoc" survives the merge.
  - Service identifiers kept on purpose (context.md L58): `defaultServiceBase="autowatch"`, `defaultDescription="autowatch daemon"`, the `autowatch.service` unit name.
  - Config/data dirs `~/.auto/{search,etl,…}` and tool-named dirs — these contain stems but are not binary names.
  - Archival `docs/tasks/*` planning docs (explicitly out of scope per requirements L88 / this doc's Out of Scope) and this very task folder, which is saturated with old names.
The guard's allowlist/scoping needs to enumerate these or it'll either false-positive (red CI on valid refs) or be so broad it misses real stale refs. Please specify the exact scan scope (which paths) and the allowlist set in the solution, since this script gates AC-7/AC-8.
AUTHOR: Replaced the vague "allowlist [autodoc()]" with a full guard spec (added above the Files block). Two key moves: (1) the pattern matches only OLD-STEM + whitespace + subcommand/flag (i.e. command *invocations*), so bare tokens inside import paths, dir names, and service labels don't trip it; (2) an explicit scan scope (shipped surface) + exclude list covering all four cases you flagged — go.mod module paths/datadyne-io, daemon service identifiers, ~/.auto/<tool> config stems, and docs/tasks/** archival docs (incl. this folder). This makes the guard tractable and precisely targets what AC-7 forbids.
-->


~ auto-config/internal/cli/root.go                # newRootCmd → NewRootCmd; Use "autoconfig"→"config"; help text
~ auto-env/internal/cli/root.go                   # newRootCmd → NewRootCmd; Use "autoenv"→"env"; help text
~ auto-env/internal/cli/quickstart.go             # help strings → `auto env …`
~ auto-doc/cmd/autodoc/main.go                    # delegate to cli.NewRootCmd (thin main, or delete entry)
+ auto-doc/internal/cli/root.go                   # extracted NewRootCmd(); Use "autodoc"→"doc"
~ auto-doc/cmd/autodoc/*                           # move inline subcmd factories into internal/cli
~ auto-etl/cmd/root.go                            # var rootCmd+init → NewRootCmd(); Use "autoetl"→"etl"
~ auto-etl/cmd/run.go, zen.go, update.go          # move init() flag/AddCommand into NewRootCmd()
~ auto-etl/main.go                                # call cmd.NewRootCmd().Execute() (or delete entry)
~ auto-search|graph|reflect|skill|ui|watch/internal/cli/root.go   # Use stem; help text
~ */internal/cli/quickstart.go, docs.go, doctor.go               # embedded help → `auto <tool> …`
~ auto-search/internal/cochange/{cochange,repo}.go # error hints `autoetl run --only git`→`auto etl run --only git` (cochange.go:50,65,68; repo.go:89)
~ auto-watch/internal/daemoninstall/resolve.go    # default BinPath …/bin/autowatch → …/bin/auto
~ auto-watch/internal/daemoninstall/template.go   # ExecStart={{.BinPath}} watch start
~ auto-watch/internal/daemoninstall/status.go     # (1) ExecStart reconstruct `+ " watch start"` (≈:60); (2) runtime shell-out args `[BinPath,"watch","status","--json"]` (≈:168,:176); (3) remediation strings `rerun … autowatch daemon status/install` (≈:40,47,157) → `auto watch daemon …`
~ auto-watch/internal/daemoninstall/install.go    # remediation strings `rerun with sudo autowatch daemon install[/--enable/--start]` (≈:43,56,59,65,71) → `auto watch daemon …`
~ auto-watch/internal/daemoninstall/restart.go    # remediation strings `… autowatch daemon install/restart` (≈:21,26,41) → `auto watch daemon …`
~ auto-watch/internal/daemoninstall/validate.go   # `the autowatch binary` (≈:143) → `the auto binary`
~ auto-watch/internal/daemoninstall/manager.go    # RETAIN service identity (`defaultServiceBase="autowatch"`, `defaultDescription="autowatch daemon"`) — do NOT rewrite
~ auto-watch/internal/cli/{daemon,ops,task,root}.go # invocation strings → `auto watch …` (daemon.go:72,73 printed `autowatch daemon status`/`autowatch status`; ops.go:41 `inspect autowatch status`; task.go:214 `run autowatch trigger …`; root.go:102,117 `run autowatch init`). RETAIN service prose `"autowatch systemd service"`. `--bin` help (daemon.go:81) → "the auto binary"

<!-- RESOLVED(P2): status.go runtime status invocation also needs the `watch` infix, not just ExecStart parsing
REVIEW: I checked auto-watch/internal/daemoninstall/status.go. Beyond ExecStart parse/reconstruct, the daemon shells out to the binary to read live status at status.go:168 and :176 — it builds args `[spec.BinPath, "status", "--json"]` (and remediation strings at :183/:190 print the same). Post-merge BinPath is `~/.local/bin/auto`, so this becomes `auto status --json`, which is wrong — there is no top-level `auto status`; it must be `auto watch status --json`. Also status.go:60 reconstructs `ExecStart: parsed.BinPath + " start"` which must become `+ " watch start"`. The file annotation only says "parse ExecStart"; the runtime `status` invocation (the part that actually breaks `auto watch daemon status`) is not called out. Add this to the status.go scope and AC-5 should assert the runtime status command includes `watch`.
AUTHOR: Confirmed and expanded the status.go scope to BOTH paths: (1) the ExecStart reconstruct `+ " start"` → `+ " watch start"` (≈:60), and (2) the runtime status shell-out `[BinPath,"status","--json"]` → `[BinPath,"watch","status","--json"]` (≈:168,:176) plus the remediation strings (≈:183,:190). Also folded into Approach step 6 ("both ExecStart paths"). AC-5 now asserts the runtime status command (not just the generated unit) includes the `watch` infix — see the AC-5 Test Coverage row.
-->

<!-- RESOLVED(P2): user-facing remediation/error strings with old invocations live in files NOT in the sweep list NOR the guard scan scope
REVIEW: I grepped auto-watch/internal for literal `autowatch <subcmd>` invocations printed to users. Beyond status.go (which is scoped), there are stale CLI invocations the Files block does not list and the guard will not scan:
  - auto-watch/internal/daemoninstall/install.go:43,56,59,65,71 — "rerun with sudo autowatch daemon install[/--enable/--start]"
  - auto-watch/internal/daemoninstall/restart.go:21,26,41 — "run sudo autowatch daemon install first", "rerun with sudo autowatch daemon restart"
  - auto-watch/internal/daemoninstall/status.go:40,47,157 — "rerun with sudo autowatch daemon status", "rerun sudo autowatch daemon install to rewrite the unit" (these are LITERAL remediation strings, separate from the :60/:168/:176/:183/:190 paths already scoped)
  - auto-watch/internal/cli/ops.go:41 — error "...inspect autowatch status or stop the existing daemon"
  - auto-search/internal/cochange/cochange.go:50,65,68 + repo.go:89 — error hints "run `autoetl run --only git`" (this file IS in the Files list, good — but see scope point below)
These are SHIPPED output (printed on the error/remediation path), so they fall under AC-7's "no instruction references a binary name that is no longer shipped". Two concrete gaps:
  (1) Sweep coverage: install.go, restart.go, ops.go, and the status.go literal strings are not enumerated in the Files block, so a phase-2 agent owning auto-watch may miss them (the listed status.go scope is only the ExecStart/shell-out paths).
  (2) Guard coverage: the Guard spec SCAN SCOPE is `**/internal/cli/{quickstart,docs,doctor,root}.go` + auto-etl/cmd help strings only. It does NOT include `internal/daemoninstall/**`, `internal/cli/{daemon,ops}.go`, or `internal/cochange/**` — exactly the files above. So even the cochange/daemon strings the solution DOES intend to fix are not verified by the guard, and missed ones leave CI green (AC-8) while shipping a dangling `autowatch …`/`autoetl …` hint (violating AC-7). Recommend: (a) add these files to the sweep list, and (b) widen the guard SCAN SCOPE to cover `auto-*/internal/**/*.go` runtime strings (the old-stem+subcommand pattern is already narrow enough to avoid false positives on import paths/service labels). Also note AC-7's enumerated verification surface (quickstart/docs/doctor + README/CLAUDE.md/skills) omits runtime error/remediation hints entirely — worth adding "runtime error/remediation strings" to AC-7's surface so this class is in-scope by criterion, not just by guard.
AUTHOR: All three fixed. (a) Sweep: enumerated install.go, restart.go, validate.go, and cli/{daemon,ops,task,root}.go in the Files block (with exact line refs) and split out manager.go as RETAIN-only — confirmed the strings via grep (install.go:43,56,59,65,71; restart.go:21,26,41; status.go:40,47,157; ops.go:41; daemon.go:72,73; task.go:214; root.go:102,117; cochange.go:50,65,68; repo.go:89). (b) Guard: SCAN SCOPE widened to all tracked `auto-*/**/*.go` except `*_test.go`/genstats/fixturegen, with a curated RETAIN allowlist for the systemd service-identity strings only (`defaultServiceBase`/`defaultDescription`=autowatch, `autowatch.service`, `"autowatch systemd service"`, standalone `"the autowatch daemon"`) — and explicitly NOT retaining `"the autowatch binary"` (now `auto`). (c) AC-7 surface: updated in requirements.md to add "runtime error/remediation/help output strings" to the verified surface, so this class is in-scope by criterion. The daemon sweep is owned by the Phase-2 auto-watch agent (plan Step 2.watch.d expanded); cochange by the Phase-2 auto-search agent (Step 2.x.c).
-->


~ Makefile                                        # one `auto` build/dist/install; add auto-config,auto-cli to loops
~ install.sh                                      # BINARIES → "auto"; single asset download
~ e2e/test-install.sh                             # build/invoke `auto <tool>`; assert old names absent
~ e2e/test-curl-install.sh                        # single `auto` asset; `auto <tool> --version`
~ README.md                                       # install, status table, quickstart, mermaid → `auto <tool>`
~ CLAUDE.md                                       # Sub-Projects table; doc-index; quickstart pointers
~ auto-*/CLAUDE.md (10 files)                      # per-tool command docs → `auto <tool> …`
~ docs/user-journey.md                            # commands → `auto <tool>`
~ docs/autostack-install-daemon.md               # daemon under `auto watch`; ExecStart examples
# SKILLS — edit the SOURCE copies (NOT .agents/, which is generated by `npx skills install`):
~ auto-reflect/skills/reflect-on-agent-sessions/SKILL.md  # SOURCE; 30 `autosearch` → `auto search`
~ skills/release/SKILL.md                          # SOURCE; expected assets → single `auto-<suffix>`
~ skills/self-improve/SKILL.md                     # SOURCE; example refs `autosearch`/`autoetl`/`auto-doc`
~ .claude/skills/{skill-reviewer,new-solution}/… or wherever their SOURCE lives  # example refs (locate the tracked non-.agents source; see note)
# Then regenerate the generated copies and stage them:
~ .agents/skills/**                                # REGENERATED: `make skills-sync` (covers skills/*) + `npx skills install "$(CURDIR)/auto-reflect/skills" -y` (reflect skill; skills-sync does NOT cover auto-reflect/skills)

<!-- RESOLVED(P2): there are duplicate, separately git-tracked copies of these skills under `skills/` and `auto-reflect/skills/` that the sweep and guard both miss
REVIEW: `git ls-files` shows the reflect/release/self-improve skills exist as MULTIPLE distinct git-tracked files (verified via `ls -i` — different inodes, not symlinks):
  - .agents/skills/release/SKILL.md  AND  skills/release/SKILL.md (top-level)
  - .agents/skills/self-improve/SKILL.md  AND  skills/self-improve/SKILL.md
  - .agents/skills/reflect-on-agent-sessions/SKILL.md  AND  auto-reflect/skills/reflect-on-agent-sessions/SKILL.md
All copies under `skills/**` and `auto-reflect/skills/**` contain the same old `autosearch`/`autoetl` invocations. The Files list and the Guard spec SCAN SCOPE only target `.agents/skills/**/SKILL.md`, so the parallel copies are neither swept nor verified — stale refs survive in shipped, git-tracked files (AC-7). Worse: if `auto-reflect/skills/reflect-on-agent-sessions/` is the canonical SOURCE that gets synced into `.agents/skills/` (it lives inside the tool that owns the skill), editing only `.agents/skills/` could be overwritten by a later sync. Please (a) clarify the source-of-truth relationship between `auto-reflect/skills/`, top-level `skills/`, and `.agents/skills/`, (b) add the other tracked copies to the sweep, and (c) widen the guard scope to `**/SKILL.md` (excluding `.claude/worktrees/**`) so all copies are checked.
AUTHOR: Verified the relationship (you were right — I had it backwards). `.agents/skills/**` is GENERATED: `Makefile:355` skills-sync runs `npx skills install "$(CURDIR)/skills" -y` then stages `.agents/`. So the SOURCES are the non-.agents tracked copies — `git ls-files` confirms exactly three for the binary-ref skills: `skills/release/SKILL.md`, `skills/self-improve/SKILL.md`, and `auto-reflect/skills/reflect-on-agent-sessions/SKILL.md` (skill-reviewer/new-solution have no `skills/`/`*/skills/` source — they live under `.claude/skills/`; the implementer locates the tracked source and edits that). Fixes: (a) Files block now edits SOURCES, not `.agents/`, and adds a regenerate step — `make skills-sync` covers `skills/*`, but the reflect skill needs an explicit `npx skills install "$(CURDIR)/auto-reflect/skills" -y` because skills-sync only syncs `skills/`. (b) Guard SCAN SCOPE widened to every tracked `**/SKILL.md` (sources + .agents generated copies), excluding `.claude/worktrees/**` — so an un-regenerated `.agents/` copy fails CI. Plan Step 5.4 updated to match.
-->

- (no standalone per-tool binaries built/installed; old cmd/auto<tool>/main.go entries may be deleted)
```

## Test Coverage

| AC  | Test Type | File |
|-----|-----------|------|
| AC-1 | smoke (Go) + e2e | `auto-cli/cmd/auto/main_test.go`; `e2e/test-install.sh` (`auto <tool> --help`) |
| AC-2 | unit + umbrella integration | existing `cd <tool> && go test ./...` (per-tool behaviour) **plus** `auto-cli/cmd/auto/main_test.go` exercising the mounted path — see assertions below |

AC-2 umbrella integration assertions (`main_test.go`, building the root cmd in-process via
`root.SetOut(buf)/SetErr(buf)` + `SetArgs` + `ExecuteContext`).
**Capture caveat:** cobra routes `--help`/usage through the command's writer (capturable via
`SetOut`), but auto-doc writes results to a hard-coded `os.Stdout` and auto-etl uses bare
`fmt.Print*` — so **executed** (non-help) doc/etl commands do NOT write to the buffer. The
wrapper's `(stdout,stderr)` params are honored by tools that thread writers (search/graph/…)
and ignored by doc/etl (they hit the real `os.Stdout` — identical to production, where that IS
stdout). Therefore: **content** assertions go on `--help` output only; **executed** commands are
asserted by **exit code** only (output correctness for doc/etl stays covered by their own
in-module tests, which still run). [Option (a) chosen over an `os.Pipe` fd-swap (b) or
threading writers through doc/etl (c) — keeps the umbrella test simple and avoids scope creep.]
- **auto-etl (highest risk — init()→NewRootCmd refactor):** `auto etl run --help` lists all
  pre-merge flags (`--input`, `--output`, `--full`, `--only`, `--repo-path`, `--since`) with
  identical defaults; `auto etl --help` shows persistent `--debug`; `auto etl zen --help` and
  `auto etl update --help` exit 0 (mounted). This guards that the package-global `debug` var and
  the AddCommand calls spread across `run.go`/`zen.go`/`update.go` all moved into the builder
  without dropping wiring — all via `--help` (capturable).
- **auto-doc (inline main→extracted NewRootCmd):** `auto doc --help` lists all 12 subcommands
  (init/tree/stale/agents/fix/fixed/graph/search/quickstart/docs/doctor/update) and shows the
  persistent `--json` flag (content, on --help); `auto doc tree --json` and `auto doc tree`
  **exit 0** (execution sanity — NOT a stdout-content check, since the JSON goes to the real
  os.Stdout, not the buffer).
- **Mount sanity (all 10 stems):** `auto <tool> --help` exits 0 and the usage line reads
  `auto <tool>`; one representative real invocation per modern-pattern tool (e.g. `auto search
  --help`) exits 0.

<!-- RESOLVED(P2): the `auto doc tree --json emits valid JSON` assertion can't capture output via the runCLI buffer pattern — auto-doc (and auto-etl) hard-code os.Stdout
REVIEW: The cited test template (auto-search/internal/cli/cli_integration_test.go:90 runCLI) captures output via `app.New(&out, &errOut)` — it works ONLY because auto-search threads writers through `app`. I checked the two high-risk tools and neither does:
  - auto-doc: every command writes to a hard-coded `os.Stdout` (cmd/autodoc/main.go:74,97,99,135,163,173,197,218,242,255,268,278,292,339,361,363 — e.g. `commands.TreeOutputJSON(os.Stdout, entries)`).
  - auto-etl: run.go uses `fmt.Printf`/`fmt.Println` (run.go:74,80,81,114,158,184), i.e. the package-global os.Stdout — no writer threading.
Consequence for main_test.go: the `--help`-based assertions are FINE (cobra routes usage through the command's writer, so root.SetOut(buf) captures `auto etl run --help` / `auto doc --help` output). But the one assertion that EXECUTES a command and inspects emitted content — "auto doc tree --json emits valid JSON" (and any future "representative real invocation" that checks stdout text) — cannot capture via a buffer; the JSON goes to the process's real os.Stdout, so the test would assert against an empty buffer (false pass or panic on json.Unmarshal). Also note the wrapper signature `New(stdout, stderr io.Writer)` is misleading for doc/etl: those params are ignored (the commands ignore the writers), so passing buffers to the wrapper changes nothing. Options: (a) downgrade the doc/etl "real invocation" checks to exit-code-only and keep content assertions on --help; (b) capture the real os.Stdout fd in the test (os.Pipe swap) for those cases; or (c) thread writers through doc/etl as part of the extraction (behaviour-neutral, but more work and arguably scope). Pick one and make main_test.go's doc/etl content assertions match what's actually capturable.
AUTHOR: Chose option (a). Rewrote the AC-2 assertion spec above: added an explicit "Capture caveat" paragraph (cobra --help is capturable via SetOut; doc/etl executed commands write to the real os.Stdout and the wrapper writers are ignored by those two tools — same as production). Content assertions now sit only on `--help`; `auto doc tree --json` / `auto doc tree` are asserted by **exit code 0** only (no buffer-content check), and doc/etl output correctness remains covered by their in-module tests. Also corrected the auto-etl flag list to include `--repo-path` and moved zen/update checks to `--help` form. plan Step 3.3 updated to match. (Rejected (b)/(c) as scope creep for no added coverage — the high-risk thing is flag/subcommand *wiring*, which --help fully captures.)
-->

<!-- RESOLVED(P2): AC-2 (behaviour preserved) test strategy is the thinnest part of the plan, yet it's the highest-risk AC
REVIEW: "existing `go test ./...` (unchanged)" exercises each tool's NewRootCmd in isolation — it does NOT exercise the mounted `auto <tool>` path, so it can't catch regressions introduced by mounting. "umbrella integration invocation" is undefined (which invocations? what's asserted?). The two changes with real behaviour risk are (a) the auto-etl `var rootCmd` + `func init()` → `NewRootCmd()` refactor (package-global flag vars like `debug` at cmd/root.go:20 and AddCommand registrations spread across run.go/zen.go/update.go must all move into the builder without changing flag wiring), and (b) the auto-doc inline `main.go` → extracted `NewRootCmd()`. Please specify concrete integration assertions for at least these two (e.g. run `auto etl run --help` and a representative `auto doc …` through the umbrella and diff flags/output against the pre-merge tool), rather than relying on per-module tests that bypass the mount.
AUTHOR: Replaced the vague "umbrella integration invocation" with concrete, mounted-path assertions in main_test.go, targeting exactly the two high-risk refactors: auto-etl (assert all run flags + defaults, persistent --debug, zen/update mounted — proving the init()→builder move kept flag wiring) and auto-doc (assert all 12 subcommands + persistent --json works), plus a per-stem mount-sanity check. These run against the root `auto` command, not per-tool isolation, so they catch mount regressions. AC-2 row updated.
-->

| AC-3 | e2e | `e2e/test-install.sh` asserts old binary names are NOT installed |
| AC-4 | e2e + CI | `e2e/test-curl-install.sh` (single asset); `release.yml` verify-install job |
| AC-5 | unit | `auto-watch/internal/daemoninstall/*_test.go` assert (a) a generated unit's `ExecStart` is `…/bin/auto watch start` (default BinPath `…/bin/auto`), **and (b) the runtime status shell-out builds `[…/auto, watch, status, --json]`** (the `watch` infix on the live-status path, not just the unit) |
| AC-6 | smoke | `auto-cli/cmd/auto/main_test.go` asserts `config` + `ui` subcommands present; e2e covers `auto ui` |
| AC-7 | CI guard | `scripts/check-no-stale-binary-refs.sh` (run in `make check`) — scoped scan of the shipped surface, old-stem+subcommand pattern, with the exclude/allowlist set specified in the Guard spec above |
| AC-8 | CI | existing `make check build test vulncheck` + e2e jobs |

## Out of Scope
- Backwards-compat alias shims for old binary names (hard cutover, per requirements).
- `auto-eval` / `auto-web` (docs-only, no Go binary).
- Single-merged-module restructure / import-path rewrites (rejected — see below).
- Fully implementing the `autostack install-daemon` design doc; only the ExecStart/BinPath
  wiring needed for `auto watch` is changed here.
- Changing any tool's own subcommands, flags, output formats, config paths, or data schema.
- Updating archival `docs/tasks/*` planning docs that reference old names for past work.

## Rejected Alternatives
- **B: single merged module** — same binary outcome but rewrites every import path and
  relocates each tool under one shared `internal/`; massive high-risk diff for no extra value.
- **Import `internal/cli` directly from the umbrella** — blocked by Go's `internal/` rule;
  the wrapper package is the minimal public seam.
- **Override `cmd.Use` in the umbrella instead of editing each tool's `Use`** — leaves help
  text saying `autosearch`, violating AC-7; editing in-tool keeps help consistent.
- **Keep separate binaries + a launcher shim** — doesn't deliver the single-binary build or
  simpler install the task requires.
