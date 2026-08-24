# Context: Task 060 — auto-skill file-ref defaults + origin-scoped roots

Codebase grounding for [plan.html](plan.html): where skill "replacements" are
parsed, resolved, rendered, persisted, validated, and surfaced — and exactly
what must change to add (1) file-ref `customize` defaults, (2) origin-scoped
resolver roots, (3) a `root:` enum, (4) manifest/provenance introspection
fields, (5) author-side lint + self-documentation. All paths repo-relative;
module root is `auto-skill/`.

## Key Files — render pipeline (`auto-skill/internal/render/`)

- `render/render.go:14-26` — **`FileRef`** struct: `File`, `Section []string`,
  `IncludeHeading`, `StripFrontmatter *bool`. **No `Root` field** → cap 3 adds one.
- `render/render.go:28-36` — **`ReplacementValue`**: exactly one of `Literal` /
  `FileRef *FileRef`. Flat carrier from skills.yaml into `Render`.
- `render/render.go:51-56` — **`FileRefResolver`** interface, single method
  `Resolve(ref FileRef)`. Root is baked into the concrete resolver at
  construction → the central thing origin-scoping breaks.
- `render/render.go:58-73` — **`RenderInput`** has a **single `Resolver`** field.
  Cap 2 must supply two roots (bundle + project) or resolve author defaults
  separately.
- `render/render.go:85-91` — **`ResolvedFileRefInfo`** (`Var`, `Path`,
  `ContentHash`, `MatchedHeading`) — provenance capture point. Cap 4 adds
  `Form`, `Root`. Populated for file-refs only, not literals.
- `render/render.go:120-230` — **`Render`**: sorted deterministic iteration
  (`:143-147`); file-refs resolved via `in.Resolver.Resolve` (`:158`), appended
  to `fileRefs` (`:164-169`); literals → `supplied[name]` (`:171`); then
  `ResolveValues(schema, tmpl.Vars(), supplied)` (`:175`). **Defaults are applied
  inside `ResolveValues` AFTER file-ref resolution, as scalar strings only** —
  the structural gap for cap 1.
- `render/customize.go:34-41` — **`CustomizeVar`**: `Required`, **`Default string`
  (scalar only)**, `Description`, unexported `hasDefault`. Cap 1 must let
  `default` be a file-ref mapping.
- `render/customize.go:49-74` — **`ParseCustomize`** decodes `customize:` via
  `node.Decode(&cv)`. Must detect `default:` mapping-vs-scalar and retain a
  `*FileRef`.
- `render/customize.go:103-142` — **`ResolveValues`**: supplied wins; else
  `cv.Default` if `hasDefault` (`:128-130`, scalar assignment — the injection
  point for a resolved default file-ref); required-no-default → hard error; else
  `""`. Takes `supplied map[string]string` (already flattened) → a default
  file-ref must be resolved before/within here.
- `render/customize.go:11-31` — **`CustomizeError`** (`ErrCode`/`Var`/`Message`,
  `Code()`); codes `undeclared_placeholder`, `required_value_missing`; plus
  ad-hoc `missing_resolver` at `render.go:154`.
- `render/fileref.go:42-52` — **`fileRefResolver{root string}`**,
  `NewFileRefResolver(root)`; root `EvalSymlinks`-resolved per call.
- `render/fileref.go:60-170` — **`Resolve`**: validation order empty→glob→`{{}}`
  interpolation→absolute (all `invalid_ref`); `EvalSymlinks` on root+target;
  containment via `withinRoot` → `symlink_escape`; section/frontmatter handling;
  `content_hash` over canonical inlined bytes. Codes at `:11-24`
  (`symlink_escape`, `invalid_ref`, `ref_not_found`, `section_not_found`).
  **Containment on the real resolved path is a security invariant — each root
  (bundle, project) must enforce its own independently; `root: project` must not
  become an escape hatch.**

## Key Files — sync wiring (`auto-skill/internal/sync/`)

- `sync/process.go:77-84` — **`skillSource`** has `rootDir` (**bundle root**:
  temp extract dir for vendored `:340`, `./skills/<name>` for authored `:363`).
  **No project-root field** → cap 2 must thread `env.Root` in.
- `sync/process.go:372-412` — **`renderSource(syaml, s)`** builds `RenderInput`
  with `Resolver: render.NewFileRefResolver(s.rootDir)` (`:390`). **Does not
  receive `env`** — must accept project root so consumer refs use `env.Root`
  while author/default refs use `s.rootDir`.
- `sync/process.go:463-494` — **`replacementValues`** merges
  `syaml.Shared.Replacements` (low) then `syaml.Skills[name].Replacements`,
  sorted (`:471-474`). **These are the consumer-side refs → project root (both
  shared and per-skill).**
- `sync/process.go:498-511` — **`nodeToReplacement`** (scalar→Literal,
  mapping→`nodeToFileRef`).
- `sync/process.go:514-544` — **`nodeToFileRef`** decodes
  file/section/include_heading/strip_frontmatter. **Cap 3 adds `root` decode.**
- `sync/manifest.go:22-69` — **`buildManifest`** maps `render.ResolvedFileRefInfo`
  → `skill.ManifestFileRef{Path, ContentHash, MatchedHeading}` (`:33-39`).
  **`Var` (and new `Form`/`Root`) are DROPPED here** → cap 4 widens both structs
  + this mapping. Refuses to write on any `ValidateManifest` error
  (`process.go:199-201`).

## Key Files — schema types (`auto-skill/internal/skill/`)

- `skill/skillsyaml.go:50` — **`ReplacementMap`** (`map[string]yaml.Node`, custom
  `UnmarshalYAML`); `SharedConfig.Replacements` `:33`, `SkillConfig.Replacements`
  `:41`.
- `skill/skillsyaml.go:87-92` — **`knownFileRefKeys`** = file/section/
  include_heading/strip_frontmatter. **Cap 3 must add `root`** or
  `validateFileRefNode` rejects it as `unknown_field`.
- `skill/skillsyaml.go:200-240` — **`validateReplacements`** (sorted; var-name
  regex `^[A-Za-z_][A-Za-z0-9_]*$`; literal/file-ref branching).
- `skill/skillsyaml.go:244-272` — **`validateFileRefNode`** (requires `file`,
  rejects unknown keys). **Cap 3 `root:` enum needs a value branch (project|
  bundle).**
- `skill/manifest.go:31-35` — **`ManifestFileRef`** (`Path`, `ContentHash`,
  `MatchedHeading`). **Cap 4 adds `Var`, `Form`, `Root`.** `ParseManifest` uses
  `DisallowUnknownFields` (`:50`) → new fields safe for new manifests; old
  manifests read with zero values (backward-compatible per D-5).
- `skill/manifest.go:58-89` — **`ValidateManifest`** (`checkHash` on hashes;
  managed-skill refs). New per-ref fields want validation (root enum).
- `skill/version.go:12-25` — **error-code constants** (`CodeRequired`,
  `CodeUnknownField`, `CodeInvalidFileRef`, …) built on
  `github.com/mistakenot/auto-shared/config` `ValidationError{Code,Path,Field,
  Message,Value}`. **New codes (e.g. `invalid_root`) belong here.**

## Key Files — CLI + introspection

- `cli/list.go:163-191` — **`writeDescribeText`** prints a `replacements:` block
  from `prov.Replacements` (a flat `map[string]string`, `:185-190`). Cap 4 needs
  a richer per-replacement shape (var/form/root/content_hash).
- `cli/root.go:191-243` — **`newLintCmd`** → `skill.Lint(env, target)`, JSON
  `[]Diagnostic`, exit 1 on errors.
- `cli/root.go:286-370` — **`newQuickstartCmd`**; `:372-404` **`newDocsCmd`**
  (describe bullet mentions replacements at `:385`). **Cap 5 self-doc goes here.**
- `skill/skill.go:343-396` **`Lint`**, `:652-~870` **`lintSkill`** — validates
  frontmatter/body/links/token-budget/side-files. **It never calls
  `ParseCustomize` — customize validation is entirely absent today.** Cap 5 adds
  it here: load SKILL.md, `render.ParseCustomize`, resolve each default file-ref
  against the **bundle root**, emit `Diagnostic`s.
- `skill/skill.go:325-341` — **`Diagnostic`** (`Severity`, `Code`, `Path`,
  `Field`, `Message`, `Value`); `HasErrors`.
- `inspect/view.go:50-62` — **`Provenance`** with `Replacements map[string]string`
  (`:61`, flat). `inspect/inspect.go:94-157` **`Describe`** sets
  `prov.Replacements = ms.Replacements` (`:151-152`) but **does NOT surface
  `ms.FileRefs`** → cap 4 joins them in.

## Patterns / conventions the design must follow

- **Determinism**: every value/var/error iteration is sorted before output
  (`render.go:143-147`, `customize.go:116-120`, `process.go:471-474`,
  `skillsyaml.go:202-206`). Preserve.
- **Containment on `EvalSymlinks`-resolved real paths** is a hard security
  invariant (`fileref.go:91-127`; design `remote-skills-design.md:380-383`
  RESOLVED(P1): "Containment is enforced on the fully symlink-resolved real
  path… Lexical cleaning alone is no longer trusted"). Both roots enforce
  independently.
- **Structured errors** = `config.ValidationError{Code,Path,Field,Message,Value}`
  (schema layer, codes in `version.go`) / `CustomizeError`+`FileRefError` with
  stable `Code()` (render layer). Every hard error carries a remediation hint
  (root `CLAUDE.md`). Lint emits `Diagnostic` objects, gates at `error`.
- **Manifest ≠ lock**: derived render state (hashes, new provenance fields) lives
  in `manifest.json`; `lock.json` is identity-only
  (`remote-skills-design.md:596-609`). New fields must not enter the
  `skill_version`-hashed tree (`:483-484`).
- **`customize` per-var default** (`remote-skills-design.md:337-354`) and the
  `skills.yaml` file-ref map shape (`:520-551`, type-by-shape: scalar=literal,
  `file:` map=file-ref, literals string-only) are the extension points for caps
  1 & 3.
- **Ubiquitous language** (`docs/concepts/UBIQUITOUS_LANGUAGE.md`): use
  **`project`** for the project root (Avoid `repo`/`workspace`); **`template`**
  is an Avoid word for Skill, so prefer `bundle`/`skill` for the skill-local
  root value. → `root:` enum values `project` | `bundle`.
- **Resource triad** (`docs/auto-package-patterns.md` §Resource Subcommands):
  introspection surfaces through `describe <name>` (not a new command; there is
  **no** `auto skill inspect` command — `inspect` is an internal package only).
- **CLI conventions** (root `CLAUDE.md`): JSON default; stdout = payload only,
  diagnostics → stderr; text mode results-first.

## Verification assets

- **Unit/edge tests**: `render/customize_test.go` (`TestResolveValues_*`,
  `TestRender_CustomizeEndToEnd`) and `render/fileref_test.go`
  (`TestResolveSymlinkEscapeRejected`, `TestResolveDirectSymlinkFileRejected`,
  `TestResolveInvalidRefs`, hash-equals-inlined-bytes) — the symlink/containment
  tests are the template for per-root scoping.
- **Property tests**: `render/render_prop_test.go` uses `pgregory.net/rapid`
  (idempotence/determinism invariants). `assurance-strategy.md` prescribes the
  T1 (model-based `syncStateMachine`) / T2 (property) / T3 (`TestEdgeCase_*`
  pinned) rungs and a **diagnostic-coverage contract** (every silent
  override/shadow must emit a structured warning).
- **Sync tests**: `sync/process_test.go:302` `TestReplacementValuesNamedBinding`
  is the direct home for shared-vs-per-skill precedence + root-scoping.
- **CLI integration**: `cli/cli_integration_test.go` drives commands in-process
  with `t.TempDir()` (`TestLint*`) — the pattern for author-side lint + describe
  introspection tests.
- **E2E harness**: `auto-skill/harness/` is **stale/untracked (empty)** — do NOT
  target it. The live harness is the top-level `harness/scenarios/skill-remote/`
  (SUT + git-server Dockerfiles + `fixtures/skills/*/SKILL.md`), driven by
  `uv run harness skill-remote up|run|down` / `uv run pytest` (root
  `CLAUDE.md` → `harness/CLAUDE.md`). Extend this scenario for the end-to-end
  bundled-default + consumer-override flow.

## Related Tasks

- **036** — established the read triad `list` / `describe <name>` / `get`
  (introspection surfaces via `describe`).
- **048 auto-skill-property-tests**, **050 model-based-sync-testing**,
  **057 auto-skill-edge-case-assurance** — distilled into
  `auto-skill/docs/assurance-strategy.md` (the T1/T2/T3 rung model + diagnostic
  contract this task's verification follows).
- **`docs/remote-skills-design.md`** — the master design (Replacement, file-ref,
  customize, section extraction §393-453, containment §371-383, skills.yaml
  §518-556, manifest §954-973, skill_version §454-516, CLI triad §611-657).
