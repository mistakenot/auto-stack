# Task 017: Unify binaries into a single `auto` command

## Problem
The stack ships 10 separate binaries (`autodoc`, `autoenv`, `autoetl`, `autograph`,
`autoreflect`, `autosearch`, `autoskill`, `autoui`, `autowatch`, plus `autoconfig`).
They are increasingly used together, but every tool is built, released, installed, and
documented independently — making deployment, updates, and onboarding needlessly
complex. We want one binary, `auto`, with each tool exposed as a subcommand
(`auto etl`, `auto search`, …), behaviour otherwise unchanged.

## Goals
- Produce a single `auto` binary that mounts every tool's existing command tree as a
  subcommand (`auto <tool> <...>`), preserving all current flags, output, and behaviour.
- One build, one release artifact per platform, one install/update path.
- Hard cutover: the old per-tool binaries are no longer built or shipped.
- Fold in the two anomalies: include `autoconfig` as `auto config`, and fix the
  `autoui` install/e2e gap so all 10 subcommands are consistent.
- Update all user-facing references (in-tool help, README, CLAUDE.md, docs, skills) to
  the new `auto <tool>` form in the same change, so nothing tells users to run a name
  that no longer exists.

## Context (from deep search)
- **No internal cross-binary calls.** No tool execs another auto-* tool; integration is
  entirely via shared on-disk parquet/files. So there are **no internal call sites to
  rewrite** — a hard cutover is structurally safe.
- **Command trees are mostly mountable already.** 8 tools expose a reusable
  `NewRootCmd(app)`; `auto-env` only needs its `newRootCmd` exported; `auto-doc` builds
  its root inline in `main.go`; `auto-etl` uses package-global `rootCmd` + `func init()`
  registration and needs a small refactor to a function-based builder.
- **Each tool is a separate Go module** (own `go.mod`, all depending on `auto-shared`).
  The merged `auto` main must import every tool's command tree. *Module strategy
  (go.work workspace vs single merged module) is deferred to solution.md.*
- **`autowatch` daemon** generates a systemd unit with
  `ExecStart={{.BinPath}} start`, default BinPath `~/.local/bin/autowatch`. Post-merge
  this must become the `auto` binary invoking `auto watch start`.
- **Reference surface:** ~600–900 actionable references (hard-coded `Use:` fields,
  `quickstart`/`docs`/`doctor` help strings, README, root CLAUDE.md Sub-Projects table,
  per-project CLAUDE.md, `docs/`, and the `reflect-on-agent-sessions` + `release` skills).
  Note: `[autodoc()]` source tags are a data format, **not** a CLI invocation — do not rename.

## Acceptance Criteria

**AC-1: Single binary with all subcommands**
- Given a clean build of the repo
- When `make build` runs
- Then exactly one binary `auto` is produced, and for each of `config|doc|env|etl|graph|reflect|search|skill|ui|watch`, `auto <tool> --help` exits 0 and its usage line shows the `auto <tool> …` form (the same subcommands/flags as the pre-merge tool, re-rooted under `auto`).

<!-- RESOLVED(P3): "matches the pre-merge tool's help" is not mechanically testable
REVIEW: solution.md step 4 renames each tool's root `Use:` from `autosearch` → `search`, and mounting under `auto` changes every usage line to `auto search …`. So the post-merge help deliberately does NOT match the pre-merge help even "sans binary name" (usage paths, examples, and the Use stem all change). The smoke test (`main_test.go`) can only assert subcommands are present, not that help "matches". Suggest restating the criterion as "each subcommand's `--help` exits 0 and shows the `auto <tool> …` usage form" so it's verifiable.
AUTHOR: Restated AC-1 to the verifiable form: `auto <tool> --help` exits 0 and shows the `auto <tool> …` usage line, with the same subcommands/flags re-rooted under `auto` (rather than "matches pre-merge help"). The deeper flag/output-parity check now lives in AC-2, whose test strategy was strengthened (see solution.md AC-2 thread).
-->


**AC-2: Behaviour preserved**
- Given any previously valid invocation `auto<tool> <args>`
- When run as `auto <tool> <args>`
- Then flags, JSON/text output, exit codes, and side effects are unchanged.

**AC-3: Old binaries removed**
- Given the merged build
- When `make build`/`make dist`/`make install` run
- Then no per-tool binaries (`autoetl`, `autosearch`, …, `autoconfig`) are built, distributed, or installed; only `auto`.

**AC-4: Install & update simplified**
- Given a release tag
- When CI release runs and a user runs `install.sh` (curl installer)
- Then a single `auto` asset per supported platform (`linux-amd64`, `darwin-arm64`) is published and installed, and the in-tool self-update path updates `auto`.

**AC-5: Daemon points at merged binary**
- Given `auto watch` daemon install on a Linux host
- When the systemd unit is generated
- Then `ExecStart` invokes the `auto` binary with `watch start` (default path `~/.local/bin/auto`), and status/restart logic resolves the same binary.

**AC-6: autoconfig folded in + autoui gap fixed**
- Given the merged build
- When listing subcommands
- Then `auto config` is present, and `auto ui` is covered by install.sh and the e2e install checks alongside the other tools.

**AC-7: No stale references in shipped surface**
- Given the merged build
- When running each tool's `quickstart`/`docs`/`doctor`, triggering its **runtime error/remediation/help output** (e.g. daemon install/restart/status hints, cochange "run …" hints), and reading README, root + per-project CLAUDE.md, and the `reflect-on-agent-sessions` / `release` / `self-improve` skills (source copies, not the generated `.agents/` mirror)
- Then every command example or remediation instruction uses `auto <tool> …`; no shipped string tells the user to run a binary name that is no longer shipped. Exempt: `[autodoc()]` data tags, and retained systemd **service-identity** strings (service base/description `autowatch`, `autowatch.service` unit name) which name the service, not a binary invocation.

**AC-8: CI green**
- Given the merged repo
- When CI runs (`make check`, `make build`, `make test`, `make vulncheck`) and the e2e install tests run
- Then all pass.

## Out of Scope
- Renaming subcommands within tools or changing any tool's own behaviour/flags.
- `auto-eval` and `auto-web` (docs-only, no Go binary).
- Backwards-compat alias shims for old binary names (explicit hard cutover; users update their own scripts/aliases).
- Choosing the module-merge mechanism (decided in solution.md).
- Archival/historical `docs/tasks/*` planning docs that reference old names for past work (update only where still user-facing/current).

## Open Questions
- [x] Old binaries: aliases or hard cutover? → **Hard cutover, only `auto`.**
- [x] Scope of rename: build-only vs full sweep? → **Everything in one task** (build + help strings + README + CLAUDE.md + docs + skills).
- [x] Which dirs become subcommands? → **The 9 built tools + fold in `autoconfig`; fix the `autoui` install/e2e gap.** `auto-eval`/`auto-web` excluded.
- [x] Module strategy (go.work vs merged module)? → **Deferred to solution.md** (explore tradeoffs there).
