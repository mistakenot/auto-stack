# Context: Task 035 — skill-prune-adopt

Codebase + dependency-contract context for epic-004 **T5** (the ownership/cleanup
layer — receipt-gated pruning, `adopt`, `remove`, foreign-conflict handling, renamed
handling, and `doctor` orphan/foreign reporting). See [plan.html](./plan.html).

> **Dependency-API note.** T5 builds **behind T4 (034)** and on T1 (032), T2 (009),
> T3 (033) — all *planned, not merged*. None of the prune/adopt/manifest/receipt code
> exists on disk yet (greps for `manifest`, `receipt`, `prune`, `adopt`, `foreign`,
> `orphan`, `skill_version`, `digest` over `auto-skill/**/*.go` return **zero** hits
> today). The Go symbol names below come from those tasks' `plan.html`/`context.md`
> API outlines and are the **planned surface** T5 consumes; if a name differs at
> execution time, T5 adapts to the merged signature — the *contract* is load-bearing,
> not the exact identifier. Design source of truth:
> [`docs/remote-skills-design.md`](../../remote-skills-design.md), the
> **"Managed vs. ad-hoc skills (pruning, renames, adoption)"** section (lines 946–1068).

## Key Files (this package — exist today)

- `auto-skill/internal/cli/root.go:60-96` — `NewRootCmd(application *app.App)
  *cobra.Command`; subcommands register via `cmd.AddCommand(newInitCmd(resolveEnv), …)`.
  T5 adds `newAdoptCmd` + `newRemoveCmd` here and extends `newDoctorCmd`.
- `auto-skill/internal/cli/root.go:58-69` — `resolveEnv()` closure → `skill.Env{Root,
  RootOverride}` via `skill.ResolveRoot(cwd, rootFlag)`. `--root` persistent flag at `:80`.
- `auto-skill/internal/cli/root.go:29-39` — `type ExitError struct { Code int; Err
  error }`; non-zero exits return `&ExitError{Code: N, Err: …}` (the JSON-default exit
  convention every new command follows).
- `auto-skill/internal/cli/root.go:360-388` — `newDoctorCmd()` → `doctorReport(env)`,
  JSON by default. **T5 extends** this report with managed-orphan + foreign/adoptable
  lists (drift compare itself is T4's).
- `auto-skill/internal/cli/sync.go:10-27` — `newSyncCmd()` currently shells out to
  `npx`; **T4 replaces the body**. T5 does not touch `sync`'s entrypoint — it adds the
  receipt-gated prune *pass* inside T4's `internal/sync` commit (the reserved `prunes`
  journal slot).
- `auto-skill/internal/cli/cli_integration_test.go:372-397` — `runCLI(t, args...)
  (stdout, stderr string, code int)`: builds `app.New(&out,&errOut)` + `NewRootCmd`,
  isolated buffers, decodes `ExitError`. Helpers: `decodeJSONMap` (`:408`),
  `decodeDiagnostics` (`:399`), `validSkill(name,desc,body)` (`:444`),
  `writeFile(t,path,content)` (`:461`), `assertExists` (`:467`). Tests use
  `t.TempDir()` as `--root`. T5 extends with `adopt`/`remove`/`doctor` + prune cases.
- `auto-skill/internal/skill/skill.go:380` — `EncodeJSON(v any) ([]byte, error)`
  (marshal-indent + newline); `:43` `ResolveRoot(cwd,rootFlag) (root string, overridden
  bool, err error)`; `:23` `skillNameRE = ^[a-z0-9]+(?:-[a-z0-9]+)*$`; `:349`
  `ValidateSkillName(name) error` (len ≤64 + regex).
- `auto-skill/internal/skill/skill.go:59-76` — `Env.SkillsDir()` →
  `filepath.Join(Root,"skills")`. **Path inconsistency to resolve:**
  `ProjectSettingsPath()` currently uses `.auto/skill/` (**singular**), while the design
  + T1 specify `.auto/skills/` (**plural**) for `skills.yaml`/`lock.json`/`manifest.json`.
  T5 must consume whatever T4/T1 settle on (see Open Questions).
- `auto-skill/internal/skill/skill.go:776-807` — `parseFrontmatterAndBody(content)
  (map[string]any, string, error)` — splits `SKILL.md` on `---\n`. Reused by `adopt`'s
  verify step (read the moved skill's `name`) and `doctor` reporting.
- `auto-skill/internal/skill/skill.go:216-272` — `List(env) ([]SkillSummary, []string,
  error)` — walks `SkillsDir()`, partial-success (returns parse-error strings). The
  pattern `adopt`/`doctor` follow for "return valid results, report invalid".

## Key Files (shared layer — exist today)

- `auto-shared/config/jsonfile.go:66-98` — `WriteJSONFileAtomic(path string, value
  any) error` (temp in same dir → chmod 0644 → `os.Rename`). Receipts + manifest
  rewrites after a prune use this. `:27` `DecodeJSONFileStrict(path, target)`
  (`DisallowUnknownFields`) reads them back.
- `auto-shared/config/validation.go:6-12` — `type ValidationError struct { Code, Path,
  Field, Message string; Value any }` + `ValidationErrorsError{Path, Errors}` — the
  canonical structured error every hard error returns (with a remediation hint).
- `auto-shared/config/projects.go:18,22-31` — `ProjectRef{ID, Path, Remote, Name,
  Tools[], RegisteredAt}` in `~/.auto/projects.json`; project `ID` (regex
  `^[a-z0-9]+(?:-[a-z0-9]+)*$`) is the **receipts filename key**
  (`~/.auto/skills/receipts/<project-id>.json`).
- `auto-shared/git/detect.go:57` — `runGit(dir, args...) (string, error)`; `:13`
  `RepoRoot`, `:28` `Provenance`, `:49` `OriginRemote`. `adopt`'s `git add` goes
  through `runGit`. (009 extends git/ with clone/archive/fetch; T5 doesn't fetch.)

## Dependency contracts T5 consumes (planned — from 032 / 034)

### Manifest — `.auto/skills/manifest.json` (T1/032 schema, T4/034 populates)
Per skill: `template_hash`, resolved replacement literals, file-ref `content_hash`es +
`matched_heading`, `skill_version`, `render_version`. **Per target: the managed-skill
set + each skill's expected `skill_version`.** T5 reads the per-target managed set to
find orphans (managed name ∉ desired set) and the expected `skill_version` to compare
against the on-disk tree digest. The manifest is **desired/managed state only — not a
deletion authority** (a branch/merge can add an entry for a foreign dir).
*Design: lines 958-963.* Go types: `ParseManifest`/`ValidateManifest`/`Manifest`
(032). If a derived field is missing from 032's struct, that's T4's concern, not T5's.

### Receipts — `~/.auto/skills/receipts/<project-id>.json` (T4/034 writes)
The **deletion authority**: records what **this machine actually wrote** as
`target → name → digest`. **Worktrees share the project `id` but receipt entries key
on each target's absolute path** (design lines 82-84), so distinct worktrees never
collide — T5's prune comparison must key on absolute target path. Exact struct shape is
**not formally specified** in the design; T4 writes it inside the journaled commit
(design line 911-913, step 3). Inferred shape (confirm against T4's merged writer):
`{ version, targets: { "<abs-target-path>": { "<skill-name>": "<skill_version-digest>" } } }`.

### Journal `prunes` slot — `.auto/skills/.sync-journal` (T4/034 reserves, T5 fills)
T4's commit protocol journals intended writes/prunes (design lines 907-913): stage
same-FS → swap per skill (old → journaled trash) → **receipts → manifest → lock →
clear journal**. **T4 reserves an empty `prunes` array** "so T5 adds the pass without a
journal-format change" (034 plan, Open Questions). T5 populates it with the orphans it
is about to delete, so the deletion rides T4's crash-consistent recovery
(G-crash-consistent). Pruning is suppressed whenever the desired set is incomplete (a
failed fetch never deletes).

### Lock — `.auto/skills/lock.json` (033 writes, read-only to T5)
`LockEntry{Source,URL,VersionSpec,Ref,Commit,Subpath string; Private,Local bool; State
string}` — dependency identity only. T5 reads it to compute the *vendored* half of the
desired set; `remove --vendored` deletes an entry (via 032's typed writer), but the
normal prune pass never rewrites the lock.

### Render / digest — `internal/render` (T4/034, pure leaf)
`skill_version = sha256(canonical_json(sorted files))` over exact emitted bytes; the
`metadata.auto_skill` in-file stamp is **excluded** from the digest. T5 reuses T4's
on-disk-tree-digest helper to (a) verify a receipt digest still matches before pruning
and (b) compute foreign/adoptable digests for `doctor` + `adopt` divergence checks. The
in-file stamp is informational only — a forged `managed: true` authorizes nothing.

## Package layout (established by T4/034)

- `auto-skill/internal/render/` — pure deterministic leaf (template AST allow-list,
  customize, file-refs, `hash.go` = `skill_version`). No `internal/skill` import.
- `auto-skill/internal/sync/` — phases A/B/C + journal + recovery + manifest population
  + targets union/shadow + **receipts write**. The receipt-gated **prune pass** T5 adds
  lives here (it must be transactional with T4's journaled commit).
- **T5's new code:** a small shared **ownership/prune helper** (the receipt + on-disk
  digest comparison: classify each target dir as *managed-current* / *managed-orphan
  (prune-eligible)* / *managed-but-unestablished* / *modified* / *foreign*) reused by
  `sync`'s prune pass, `doctor`, `adopt`, and `remove`. New CLI commands
  `adopt`/`remove` in `internal/cli`; `doctor` extension in `internal/cli` +
  `internal/skill` (or the new helper).

## Patterns / conventions (from CLAUDE.md + auto-package-patterns.md)

- **JSON-default**: stdout = strictly parseable payload, stderr = diagnostics; `--text`
  (never `--json` — JSON is the default). Data-listing returns valid results even when
  some invalid, then exits non-zero. Every hard error carries a remediation hint.
- **Resource triad** (`list`/`describe`/`get` + `source`/`target`) is T6; `doctor`
  here is the drift/ownership **report**, not the triad.
- **Destructive ops**: prefer explicit flags over ambiguous aliases; `--force`
  overrides a refusal (existing `./skills/<name>` for `adopt`; foreign-collision for
  `sync`); selector required when ambiguous (`remove --local|--vendored`).
- **Go build discipline**: `go build ./...` in `auto-skill` after each file.
- **Guard rails** this task is bound by: **G-no-foreign-delete** (deletion needs a
  manifest orphan AND a machine-local receipt AND a matching on-disk digest; foreign /
  modified / unestablished dirs are reported, never deleted/mutated) and
  **G-crash-consistent** (the prune pass rides T4's journaled commit; a crash leaves a
  recoverable state; pruning suppressed on an incomplete desired set).

## Related Tasks

- **034 (T4) — native sync + render** *(direct dependency)*. Writes manifest +
  receipts, reserves the journal `prunes` slot, establishes `internal/render` +
  `internal/sync`, and explicitly defers the **receipt-gated deletion pass**,
  **foreign-dir conflict handling**, `adopt`/`remove`, and `doctor` orphan/foreign
  reporting to T5 (034 plan, "Prune / adopt / doctor (T5)" + Open Questions).
- **032 (T1) — schemas/init**. Defines `Manifest`/`ParseManifest`/`ValidateManifest`
  and the `projects.json` id reuse that keys receipts.
- **009 (T2) — cache + trust**, **033 (T3) — add**. Provide the lock T5 reads and the
  tree-digest helper lineage; T5 does no fetching/trust work itself.
- **T6 (inspection triad + deprecations)** and **T7 (migrate + hooks)** consume T5's
  ownership classifications but are out of scope here.

### Git-history confirmation

`git log -- auto-skill/` shows the package is young: `a90a2cc` (v1 CLI: init, create,
lint, ls, doctor, quickstart, docs) and `25a9c88` (lint `--text` output) are the only
substantive commits; `doctor` is `doctorReport` in `cli/root.go` and `Lint` lives in
`skill/skill.go` (no `lint.go`/`doctor.go` split yet). No prune/adopt/manifest/receipt
code has ever landed — T5 is greenfield on top of T4's (planned) `internal/render` +
`internal/sync`. The unification commit `cd80ea9` (017) folded the tools into the single
`auto` binary, so all surfaces ship as `auto skill <verb>`.
