# Context: Task 032 — skill-project-schemas

Codebase grounding for [plan.html](plan.html) (epic-004 remote-skill-management, task T1). Defines the `.auto/skills/` layout, three strict schemas (`skills.yaml` / `lock.json` / `manifest.json`) with one shared `validate()` each, the `init --project` wizard, and folds `agents` into `init`.

## Key Files

### Current `init` / Env / settings (to be reworked)
- `auto-skill/internal/skill/skill.go:38-41` — `Env{Root, RootOverride}`; `ResolveRoot` at `:43-57`.
- `auto-skill/internal/skill/skill.go:59-76` — path methods. **`ProjectSettingsPath()` uses the singular `.auto/skill/`** (`:63-65`); `GlobalSettingsPath()` → `~/.auto/skill/settings.json` (`:67-76`); `SkillsDir()` → `./skills/` (`:59-61`, keep). The singular `.auto/skill/` path must become **`.auto/skills/`** (plural) per the design's Path layout.
- `auto-skill/internal/skill/skill.go:87-129` — `Init(env, project bool)`; `:1153-1175` `ensureSettingsFile` writes `{"schemaVersion": 1}` (the *entire* current settings payload); `:1177-1192` `ensureDir`.
- `auto-skill/internal/skill/skill.go:18-20` — `const settingsFileName = "settings.json"`.
- `auto-skill/internal/cli/root.go:98-181` — `newInitCmd`: JSON-default with `--text`; this is the wizard's mounting point. Registered at `root.go:82-93` alongside the other commands.

### `agents` command (to be removed, folded into init)
- `auto-skill/internal/cli/agents.go:12` — `autoskillSnippet` const; `:14-15` `agentFiles = [CLAUDE.md, AGENTS.md, GEMINI.md]`; `:63-83` `ensureSkillSnippet` (idempotent append, symlink-safe). Registered as `newAgentsCmd()` at `root.go:91` — remove that line.

### Reusable shared infrastructure (`auto-shared/config`) — the big win
- `auto-shared/config/validation.go:5-12` — **`ValidationError{Code, Path, Field, Message, Value}`** — the canonical structured error the design + CLAUDE.md mandate. `:14-33` `ValidationErrorsError` wraps `[]ValidationError` as one `error`.
- `auto-shared/config/projects.go:22-36` — `ProjectRef{ID, Path, Remote, Name, Tools, RegisteredAt}` + `ProjectsConfig{Projects []ProjectRef}`.
- `auto-shared/config/projects.go` — full registry API to **reuse, not rebuild**: `ProjectsConfigPath()` (`:38`), `LoadProjects`/`SaveProjects` (`:78`,`:91`), `EnsureProjects()` (`:103`), `UpsertProject` (`:177-188`, path-keyed replace/append, stamps `RegisteredAt`), `FindProjectByExactPath` (`:211`), `FindProjectByRemote`, `FindProjectByID`, `ValidateProjects` (`:254-300`), `NormalizeID`/`SlugifyID` (`:50-73`, regex `^[a-z0-9]+(?:-[a-z0-9]+)*$`).
- `auto-shared/git/normalize.go:16-65` — `NormalizeRemoteURL()`: SSH→HTTPS, strips `.git`, **strips credentials**, lowercases host. Use for the `lock.url` credential check and project `remote`.
- `auto-shared/config/jsonfile.go:13-98` — `DecodeJSONFile`/`DecodeJSONFileStrict` (strict = reject unknown keys), `WriteJSONFile`/`WriteJSONFileAtomic` (temp+rename).
- `auto-shared/config/paths.go:16-52` — `HomeDir()` (prefers `$HOME`), `AutoDir()` (`~/.auto`), `EnsureAutoDir()`. Add a `SkillsGlobalDir()` → `~/.auto/skills/` helper here or locally.

### Existing validation patterns in auto-skill
- `auto-skill/internal/skill/skill.go:274-289` — `Severity` enum + `Diagnostic{Severity, Code, Path, Field, Message, Value}` (lint-time, severity-tiered). Schema `validate()` should return the **shared `config.ValidationError`** (no severity) per package patterns, not `Diagnostic`.
- `auto-skill/internal/skill/skill.go:349-360` — `ValidateSkillName` + `skillNameRE = ^[a-z0-9]+(?:-[a-z0-9]+)*$` (`:24`). Reuse this exact regex for skill keys in all three schemas.

### Dependencies
- `auto-skill/go.mod` — **`gopkg.in/yaml.v3 v3.0.1` is already a direct dep** (used in `skill.go:15,802,1129`). No new runtime dep needed for `skills.yaml` parsing (honors minimal-deps rule). `auto-shared` is stdlib-only.

## Patterns
- **JSON-default I/O** (`auto-package-patterns.md` "JSON Output"; `root.go` lint/init): `json.Encoder` with 2-space indent to **stdout**; diagnostics/errors to **stderr**; `--text` flag (never `--json`, JSON is default); exit 1 on errors even with partial valid results. `skill.EncodeJSON` (`skill.go:380-386`) = MarshalIndent + trailing newline.
- **Shared `validate()`** (`auto-package-patterns.md` "Validation"): `func validate(x) []config.ValidationError`, codes like `required`/`invalid_*`/`duplicate_*`, `Path` as JSON-path-ish string, every error gets a remediation hint in `Message`.
- **Strict schemas**: reject unknown keys. JSON via `DecodeJSONFileStrict`; YAML via `yaml.Decoder.KnownFields(true)`.
- **Cobra mounting**: `internal/cli/root.go` `cmd.AddCommand(...)`; standalone binary via `rootcmd/`, mounted as `auto skill`.

## Schema field reference (from `docs/remote-skills-design.md`)
- **skills.yaml** (`§skills.yaml schema`, lines 518-556): `auto_update bool`, `targets []string`, `commit_targets bool`, `trusted_hosts []string`, `shared.{version, replacements}`, `skills.<name>.{version, replacements}`. Replacement **literals are strings** (non-string scalars must be quoted); mapping with `file:` ⇒ file-ref `{file, section?, include_heading?, strip_frontmatter?}`. Skill keys match the name regex.
- **lock.json** (`§lock.json schema`, lines 558-609): `{version int, skills.<name>.{source, url, version_spec, ref, commit, subpath, private, local, state}}`; `state ∈ {resolved, unresolved}`; alphabetized, **no timestamps**; `url` must be credential-free. **Dependency identity only** — no derived render fields (the load-bearing seam).
- **manifest.json** (`§Managed vs. ad-hoc skills` prose, lines 958-973): derived render state — per skill `{template_hash, replacements, file-ref content_hashes + matched_heading, skill_version, render_version}`; per target the managed set + expected `skill_version`. T1 defines + validates the schema only; **no hashing/render** (that is T4).
- **version-spec grammar** (`§Versioning convention`, lines 255-268): `latest` | `branch:<n>` | `tag:<n>` | `commit:<sha>` | bare (best-effort tag→branch→commit-prefix). T1 validates the *grammar*; resolution is later.
- **init wizard** (`§CLI surface` init note, lines 659-669): targets/auto_update/default-version/commit_targets prompts; `-y` + flag overrides `--target`/`--auto-update`/`--no-auto-update`/`--default-version`/`--commit-targets`/`--no-commit-targets`; bare `init` writes `~/.auto/skills/settings.json`; idempotent.

## Related Tasks
- **Epic 004** (`docs/epics/epic-004-remote-skill-management.html`) — T1 here; T2 cache+trust, T3 add, T4 sync+render (writes the manifest/hashes), T5 prune/adopt, T6 inspection+deprecations (`ls` removal, root `auto update`), T7 migrate+hooks. T1's lock-schema + manifest/derived-state split is the seam every later task builds behind.
- **Design doc** `docs/remote-skills-design.md` — master spec; inherited defaults (`auto_update:true`, `commit_targets:true`, machine-local trust, copy-only) are settled (epic D-2), not reopened.
- Pattern precedent: `auto-watch`/`auto-ui` already consume `auto-shared/config` projects registry (`auto-ui/internal/cli/serve_test.go:25-26` writes a `projects.json` fixture).

### Git history (enrichment)
- **`66e9c4c`** (Jun 11) "feat: project registry plumbing — shared registry, auto init, auto hooks fire (#72)" — introduced the whole `auto-shared/config/projects.go` API (`ProjectRef`, `EnsureProjects`, `UpsertProject`, `FindProjectByExactPath`, `ValidateProjects`). **`ebfbc3b`** (#73) fixed legacy-registry migration inside `EnsureProjects`. This API is stable and is exactly what AC-7 reuses — do not reimplement.
- **`c2ec1c9`** "init --project flag, propagate exit codes, targeted feedback remediation" — the **root CLI** (`auto init --project`) wizard/flag pattern; mirror it for `auto skill init --project`.
- **`cd80ea9`** "feat(017): unify binaries into a single 'auto' command" — moved `init`/`agents` under `auto skill`; `init` is already JSON-default (matches this task's I/O contract). **`a90a2cc`** added the original `init/create/lint/ls/doctor` CLI.
- **Task 020 (`docs/tasks/020-auto-hooks-install/`, completed)** — established idempotent config-install + atomic writes (`WriteJSONFileAtomic`) + structured-error-with-remediation. Note: 020 used a *lenient* `map[string]any` merge to **preserve** unknown keys (it edits a file another tool owns). T1 is the opposite — **strict** decoding that **rejects** unknown keys (G-schema-strict), because `auto skill` owns these three files. Don't copy 020's lenient merge here.
- **Task 017 (`docs/tasks/017-unify-binaries-into-auto/`, completed)** — the multi-phase fan-out execution precedent.
- **Path drift check:** all Solution-tab paths confirmed present except `auto-skill/internal/cli/init.go` (does not exist yet — current `init` lives in `root.go:98-181` as `newInitCmd`; the Solution tab correctly marks `cli/init.go` as `add` and `root.go` as `edit`). `gopkg.in/yaml.v3 v3.0.1` confirmed direct in `auto-skill/go.mod`. No drift.
