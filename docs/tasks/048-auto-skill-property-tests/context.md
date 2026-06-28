# Context: Task 048

Codebase context for adding property-based tests to auto-skill. See [plan.html](plan.html).

## Key Files

- `auto-skill/internal/transport/transport.go` — URL canonicalization, credential detection, endpoint derivation. 6 URL forms, 4 allowed schemes, credential query key list.
- `auto-skill/internal/skill/lock.go:13-31` — `Lock` / `LockEntry` structs. `ParseLock()` uses strict JSON decoding (`DisallowUnknownFields`). `ValidateLock()` checks name regex, state enum, required fields, credential-free URLs.
- `auto-skill/internal/skill/skillsyaml.go:14-47` — `SkillsYAML` / `SkillConfig` / `ReplacementMap` structs. `ParseSkillsYAML()` uses strict YAML decoding (`KnownFields(true)`). Custom `UnmarshalYAML` on `ReplacementMap` handles legacy empty-sequence form.
- `auto-skill/internal/skill/manifest.go` — `Manifest` / `ManifestSkill` / `ManifestTarget` structs. Strict JSON decode.
- `auto-skill/internal/skill/version.go:27-77` — `ValidateVersionSpec()` — accepts `latest`, `branch:*`, `tag:*`, `commit:<hex>`, bare strings. `commitHexRE = ^[0-9a-f]{7,40}$`.
- `auto-skill/internal/skill/skill.go:24` — `skillNameRE = ^[a-z0-9]+(?:-[a-z0-9]+)*$`
- `auto-skill/internal/render/render.go:120-230` — `Render()` pure function. `splitFrontmatter()`, `normalizePath()`, `composeSkillMD()` are **unexported**.
- `auto-skill/internal/render/hash.go:111` — `ComputeSkillVersion()` exported. `canonicalizeText()`, `sha256Hex()` unexported.

## Patterns

- All test files use **internal package declarations** (`package render`, not `package render_test`) — they access unexported functions directly.
- Test naming: `TestFunctionName_Scenario` (e.g. `TestSkillVersion_DeterministicAcrossRenders`).
- No existing `testdata/` dirs in transport, skill, or render packages.
- No PBT library in the monorepo — `rapid` would be the first.
- Existing render tests use a `baseInput()` helper and `mustRender(t, in)` pattern for test setup.
- No custom `MarshalJSON` / `MarshalYAML` methods — serialization uses standard `json.Marshal` / `yaml.Marshal`.
- `config.ValidationError` (from `auto-shared`) has fields: `Code`, `Path`, `Field`, `Message`, `Value`.

## Validation Regexes

| Name | Pattern | Package |
|------|---------|---------|
| `skillNameRE` | `^[a-z0-9]+(?:-[a-z0-9]+)*$` | skill |
| `commitHexRE` | `^[0-9a-f]{7,40}$` | skill |
| `replacementVarRE` | `^[A-Za-z_][A-Za-z0-9_]*$` | skill |

## Export Status (render package)

| Function | Exported | Implication |
|----------|----------|-------------|
| `Render()` | Yes | Property tests can use from any package |
| `ComputeSkillVersion()` | Yes | Direct testing from any package |
| `splitFrontmatter()` | No | Must test from `package render` |
| `normalizePath()` | No | Must test from `package render` |
| `composeSkillMD()` | No | Must test from `package render` |
| `canonicalizeText()` | No | Must test from `package render` |

## Related Tasks

- Task 037 (`skill-integration-migration`) — the migration from Vercel to native `auto skill`. Introduced the compatibility layers this PBT suite will stress-test.
- Epic 004 (`remote-skill-management`) — the parent epic; all 7 tasks shipped. PBT is a post-ship hardening effort.
