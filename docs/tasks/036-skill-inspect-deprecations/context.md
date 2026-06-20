# Context: Task 036 — skill-inspect-deprecations

Codebase + dependency-contract context for epic-004 **T6** (the read-only
inspection triad — `list` / `describe` / `get` + the `source` and `target`
sub-resources — plus the decisive CLI deprecations: remove `ls`, and reclaim
`auto skill update` for the skills update verb by making root `auto update` the
sole binary self-update). See [plan.html](./plan.html).

> **Dependency-API note.** T6 builds **behind T4 (034)** and on T1 (032), T3
> (033) — all *planned, not merged*. None of the lock/manifest/render code exists
> on disk yet (greps for `manifest`, `lock`, `render`, `skill_version`,
> `LockEntry`, `Manifest` over `auto-skill/**/*.go` return **zero** hits today).
> The Go symbol names below come from those tasks' `plan.html`/`context.md` API
> outlines and are the **planned surface** T6 consumes; if a name differs at
> execution time, T6 adapts to the merged signature — the *contract* is
> load-bearing, not the exact identifier. Design source of truth:
> [`docs/remote-skills-design.md`](../../remote-skills-design.md) → **"CLI
> surface"** (lines 611–716, the resource triad + sub-resources) and
> **"Deprecations / renames from today's surface"** (lines 1306–1315).

## Key Files (this package — exist today)

- `auto-skill/internal/cli/root.go:82-96` — `NewRootCmd(application *app.App)
  *cobra.Command`; subcommands register via `cmd.AddCommand(newInitCmd(resolveEnv),
  newCreateCmd(…), newLintCmd(…), newLsCmd(…), newDoctorCmd(…), newQuickstartCmd(),
  newDocsCmd(), newUpdateCmd(), newAgentsCmd(), newSyncCmd())`. **T6 swaps**
  `newLsCmd` → `newListCmd` and **adds** `newDescribeCmd`, `newGetCmd`,
  `newSourceCmd`, `newTargetCmd`; **removes** the `newUpdateCmd` (self-update) and
  `newAgentsCmd` registrations.
- `auto-skill/internal/cli/root.go:308-358` — `newLsCmd(resolveEnv)`: `Use: "ls"`,
  calls `skill.List(env)`, JSON-default (`--text` alt), partial-success
  (`parseErrors` → stderr, exit 1 if any). **T6 replaces this** with `list`
  (default authored ∪ vendored; `--local`/`--vendored`; stale flag).
- `auto-skill/internal/cli/update.go:11-28` — `newUpdateCmd()`: `Use: "update"`,
  calls `auto-shared/update.Run(...)` — **today `auto skill update` is the BINARY
  self-update**. T6's deprecation deletes this self-update binding so the name is
  free for T4's skills `update [name]` verb (see "The `update` name reclaim" below).
- `auto-skill/internal/cli/agents.go:16-…` — `newAgentsCmd()`: `Use: "agents"`,
  appends an `auto skill` snippet into `CLAUDE.md`/`AGENTS.md`/`GEMINI.md`. The
  deprecation table folds this into `init` (**owned by T1/032**). T6 **removes the
  standalone `agents` command/registration** if T1 left it behind.
- `auto-skill/internal/cli/sync.go:9-25` — `newSyncCmd()`: shells `npx skills add …`.
  **T4 (034) replaces the body** with native sync. T6 does **not** touch `sync`;
  it only confirms the npx exec is gone (deprecation table row 2).
- `auto-skill/internal/cli/root.go:390-433` — `newQuickstartCmd()` / `newDocsCmd()`
  emit hard-coded command lists that still say `ls`. **T6 updates both** (drop `ls`,
  add the triad + sub-resources; point self-update at root `auto update`).
- `auto-skill/internal/cli/cli_integration_test.go` — `runCLI(t, args...) (stdout,
  stderr string, code int)` builds `app.New(&out,&errOut)` + `NewRootCmd`, isolated
  buffers, decodes `ExitError`; helpers `decodeJSONMap`, `decodeDiagnostics`,
  `validSkill`, `writeFile`, `assertExists`; `t.TempDir()` as `--root`. **T6 extends**
  with triad / sub-resource / deprecation cases.
- `auto-skill/internal/cli/root.go:29-39` — `type ExitError struct { Code int; Err
    error }`; the JSON-default exit convention every new command follows.
- `auto-skill/internal/skill/skill.go:210-272` — `type SkillSummary struct {Name,
  Description, Path string}` + `List(env) ([]SkillSummary, []string, error)` — walks
  `Env.SkillsDir()` (= `Root/skills`), partial-success. T6's `list` reuses this for
  the **authored** half and joins the **vendored** half from the lock + manifest.
- `auto-skill/internal/skill/skill.go:380` — `EncodeJSON(v any) ([]byte, error)`;
  `:43` `ResolveRoot`; `:23` `skillNameRE = ^[a-z0-9]+(?:-[a-z0-9]+)*$`; `:349`
  `ValidateSkillName(name)`; `:776` `parseFrontmatterAndBody(content)` — `get` reuses
  this to surface the rendered `SKILL.md` frontmatter/body.

## The `auto update` / `auto skill update` name reclaim (D-3 — owned here)

The single place T6 reaches **outside** `auto-skill`:

- `auto-cli/cmd/auto/main.go:51-78` — the unified `auto` binary (task 017,
  **MERGED** in `cd80ea9`) **already registers a root `auto update`** command
  (`newUpdateCmd()` → `auto-shared/update.Run`). The "relocate self-update to root
  `auto update`" destination from D-3 **exists today** — there is nothing to *add*
  at root. The comment at `:58-59` ("The per-tool `auto <tool> update` subcommands
  are retained equivalents") documents that today **both** `auto update` and `auto
  skill update` run the binary self-update.
- So the reclaim is a **deletion**, not a move: drop `auto skill update`'s
  self-update binding so the name means "update skills." T6 also tightens the
  `main.go` comment so auto-skill is no longer described as a self-update equivalent.

**Composition with T4 (the depends-on=034 sequencing).** The epic's T4 line itself
says T4 "Deletes the npx shell-out; reclaims `auto skill update`" and ships "the
`update [name]` verb" — i.e. T4 cannot register `auto skill update` as the skills
verb without first removing the self-update binding. Because T6 **depends-on 034**
(builds after T4 merges), the functional reclaim is physically performed by
whichever task lands first, and T4 lands first. **Default adopted in this plan:**
T6's net deliverable is the *deprecation end-state*, independent of who flips the
last byte — `auto skill update` == the skills update verb; binary self-update
reachable **only** at root `auto update`; no temporary alias. If T4 already deleted
`update.go` and wired its verb, T6 **asserts** that end-state (e2e) and finishes
the doc/comment cleanup; if T4 parked its verb under a temporary name/alias, T6
performs the actual swap. See Open Questions on the D-3-vs-depends-on ordering
inversion.

## Dependency contracts T6 consumes (planned — from 032 / 033 / 034)

### Lock — `.auto/skills/lock.json` (033 writes, read-only to T6)
`LockEntry{Source, URL, VersionSpec, Ref, Commit, Subpath string; Private, Local
bool; State string}` keyed by skill name — **dependency identity only**. Feeds:
the *vendored* half of `list`; `describe`'s `source`/`url`/`ref`/`commit`/
`version_spec`; and `source list`/`source describe <id>` (deduped by `Source`
repo). Typed reader from 032 (`ParseLock`/`Lock`); T6 never writes it.

### Manifest — `.auto/skills/manifest.json` (032 schema, 034 populates; read-only)
Per skill: `template_hash`, resolved replacement literals, file-ref `content_hash`es,
`skill_version`, `render_version`. **Per target: the managed-skill set + each skill's
expected `skill_version`.** Feeds: `describe`'s `skill_version` + resolved
`replacements`; and the **stale flag** for `list` — compare the on-disk rendered tree
digest against the manifest's expected `skill_version`. Go types
`ParseManifest`/`Manifest` (032).

### Render / on-disk digest — `internal/render` (034, pure leaf; read-only)
`skill_version = sha256(canonical_json(sorted files))` over exact emitted bytes; the
in-file `metadata.auto_skill` stamp is **excluded** from the digest. T6 reuses T4's
**on-disk-tree-digest helper** to compute the stale flag (digest ≠ manifest expected)
**offline** — no fetch, honoring the design's "freshness checks are offline and
honest" (`G-offline-check` spirit; the actual `--check` gate is T4's). `get` reads the
rendered `SKILL.md` bytes from the resolved target tree (authored-only skills served
from `./skills/<name>/SKILL.md`).

### skills.yaml `targets` — `.auto/skills/skills.yaml` (032 schema; read-only)
`targets: [claude, agents]` (default) + `commit_targets`. Feeds `target list`
(configured output target styles + each one's resolved on-disk path —
`.claude/skills`, `.agents/skills` — and managed-skill count from the manifest's
per-target set). Typed reader from 032 (`ParseProjectConfig`/`ProjectConfig`).

## Proposed package layout (T6's new code)

- `auto-skill/internal/inspect/` — a **read-only** package that joins authored
  (`skill.List`), vendored (lock), derived (manifest), and digest (render) state
  into the triad views. No writes, no fetch. Mirrors the `internal/render` +
  `internal/sync` + `internal/ownership` (035) package split T4/T5 establish.
  - `inspect.go` — `Inspect(env, filter)` → combined `[]SkillView`; `Describe(env,
    name)` → provenance; `Get(env, name, target)` → rendered `SKILL.md` bytes.
  - `view.go` — `SkillView{Name, Origin (local|vendored), Description, Path, Source,
    Ref, Commit, SkillVersion string; Stale, Shadowed bool}` + `Provenance`.
  - `loaders.go` — thin wrappers over 032/033/034 typed readers (lock, manifest,
    project config, on-disk digest) so the triad + sub-resources share one load path.
  - `source.go` / `target.go` — `SourceList`/`SourceDescribe`; `TargetList`.
- `auto-skill/internal/cli/list.go` — `newListCmd` / `newDescribeCmd` / `newGetCmd`.
- `auto-skill/internal/cli/resources.go` — `newSourceCmd` (`list`/`describe <id>`),
  `newTargetCmd` (`list`).

## Patterns / conventions (from CLAUDE.md + auto-package-patterns.md)

- **Resource triad** (CLAUDE.md → "noun + verb", `docs/auto-package-patterns.md` →
  "Resource Subcommands"): cheap rungs (`list`, `describe`) return ids + metadata;
  `get` is full-fidelity by default; **truncated output prints the exact command to
  recover the full version**. `search` is the ID-less rung (out of scope here).
- **JSON-default** (`G-json-default`): stdout = strictly parseable payload, stderr =
  diagnostics; `--format text` (the design's spelling at line 644) for humans. Note
  today's `ls` uses `--text`/`--json`; T6 standardizes the new commands on
  `--format text` per the design's CLI-surface header — flag-style reconciliation
  recorded in Open Questions.
- **Decisive CLI** (`G-decisive-cli`): deprecated commands are **removed**, not
  aliased — `ls` is gone (not hidden), the self-update binding is deleted.
- **Fail-fast** invalid usage: `--local` + `--vendored` together is a flag conflict
  (standard Cobra error); unknown `<name>` is a hard error with a remediation hint.
- **Partial success**: data-listing returns valid results even when some items are
  invalid, then exits non-zero (the existing `List` `parseErrors` pattern).
- **Go build discipline**: `go build ./...` in `auto-skill` (and `auto-cli` for the
  `main.go` reach) after each file.

## Guard rails this task is bound by

- **G-decisive-cli** — explicit CLI surface, no long-term aliases: `ls`→`list`
  (removed), `agents`→folded into `init` (T1; standalone removed here), npx
  `sync`→native (T4; confirmed), binary self-update→root `auto update` so `auto skill
  update` means "update skills." Resource triad `list`/`describe`/`get` + `source`/
  `target`.
- **G-json-default** — JSON-default output; payload on stdout, diagnostics on stderr;
  data-listing returns valid results even when some are invalid, then exits non-zero;
  `--format text` for humans.

## Related Tasks

- **034 (T4) — native sync + render** *(direct dependency, 036 depends-on 034)*.
  Populates the manifest, establishes `internal/render` (+ the on-disk-tree-digest
  helper) and `internal/sync`, deletes the npx shell-out, and ships the skills
  `update [name]` verb — the consumer of the `auto skill update` name T6's
  deprecation frees. T6 reads T4's manifest/digest; it adds **no** write path.
- **032 (T1) — schemas/init**. Defines `Lock`/`Manifest`/`ProjectConfig` typed
  readers T6's `loaders.go` wraps, and **folds `agents` into `init`** (T6 removes the
  standalone command).
- **033 (T3) — add**. Writes the lock entries `list`/`describe`/`source` surface.
- **017 — unify binaries** *(MERGED, `cd80ea9`)*. Provides the root `auto` command
  tree (`auto-cli/cmd/auto/main.go`) where root `auto update` already lives — so D-3's
  relocation destination exists today and T6's light reach into it is safe.
- **035 (T5) — prune/adopt/doctor** and **T7 (migrate + hooks)** are siblings;
  `doctor`'s drift/ownership *report* is T5's, distinct from T6's read-only triad.
