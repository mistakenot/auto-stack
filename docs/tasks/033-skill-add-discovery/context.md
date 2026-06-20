# Context: Task 033 — skill-add-discovery

Codebase + dependency-contract context for epic-004 T3 (`auto skill add`: source parsing & discovery). See [plan.html](./plan.html).

> **Dependency-API note.** Tasks 032 (schemas) and 009 (cache/trust) are *planned, not merged*. The Go symbol names below for those layers are taken from their `plan.html` API outlines and are the **planned surface** T3 builds behind; if a name differs at execution time, T3 adapts to the merged signature (the contract — not the exact identifier — is load-bearing).

## Key Files (this package)

- `auto-skill/internal/cli/root.go:60` — `func NewRootCmd(application *app.App) *cobra.Command`; subcommands registered via `cmd.AddCommand(newInitCmd(resolveEnv), …, newSyncCmd())` (lines 71–96). `add` is registered here as `newAddCmd(resolveEnv)`.
- `auto-skill/internal/cli/root.go:29-39` — `type ExitError struct { Code int; Err error }`; non-zero exits return `&ExitError{Code: N, Err: …}`. `resolveEnv()` closure (lines 58–69) yields `skill.Env`.
- `auto-skill/internal/cli/sync.go:14-22` — current `sync` **shells out**: `exec.CommandContext(ctx, "npx", "skills", "add", "./skills", "--agent", "codex", "claude-code", "--full-depth", "-y")`. T3 does **not** delete this (T4 does); it adds the native `add` alongside.
- `auto-skill/internal/cli/cli_integration_test.go:372-397` — `runCLI(t, args...) (stdout, stderr string, code int)` builds `app.New(&out,&errOut)` + `cli.NewRootCmd`, runs with isolated buffers, decodes `ExitError`. Helpers: `decodeJSONMap`, `decodeDiagnostics`, `validSkill`, `writeFile`, `assertExists`. Tests isolate `HOME` + pass `--root`.
- `auto-skill/internal/skill/skill.go:23` — `var skillNameRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)`; `:349` — `func ValidateSkillName(name string) error` (≤64 chars + regex). One source of truth for the name grammar (032 may relocate into the schema validators; reuse it).
- `auto-skill/internal/skill/skill.go:37-76` — `type Env`; `SkillsDir() = <root>/skills`, `ProjectSettingsPath()`, `GlobalSettingsPath()`. 032 adds `.auto/skills/` layout helpers; 009 adds `UpstreamCacheDir()` (`~/.auto/skills/upstream/`) + `TrustPath()`.
- `auto-skill/internal/skill/skill.go:776-807` — `parseFrontmatterAndBody()` splits on `---\n`; reads `SKILL.md` frontmatter (`name`, `description`) — reusable to read a discovered skill's declared name during discovery.
- `auto-skill/go.mod:8` — `gopkg.in/yaml.v3 v3.0.1` already a dep (skills.yaml read/write goes through 032's typed parser, not raw yaml here).

## Key Files (shared layer)

- `auto-shared/git/normalize.go:16` — `func NormalizeRemoteURL(raw string) string` (SSH→HTTPS, strip `.git`, lowercase host, **strip credentials**); `:69` `ComputeRepoID(normalizedRemote string) string` (16-hex SHA-256); `:77` `ComputeRepoIDFromPath(absPath string)`. 009's transport/cache layer reuses these for the collision hash-suffix.
- `auto-shared/git/detect.go:57` — `runGit(dir string, args ...string) (string, error)` exec wrapper; 009 extends with `Clone`/`Fetch`/`Archive`/`RevParse`/`LsRemote` (args after `--`, `GIT_TERMINAL_PROMPT=0`, offline `GIT_NO_LAZY_FETCH=1`).
- `auto-shared/config/validation.go:6` — `type ValidationError struct { Code, Path, Field, Message string; Value any }` — the canonical structured error every `validate()` returns.
- `auto-shared/config/jsonfile.go:27` — `DecodeJSONFileStrict(path, target)` (`DisallowUnknownFields`); `:66` `WriteJSONFileAtomic(path, value)` (temp + atomic rename). lock.json is written via 032's typed `Lock` + this atomic writer.
- `auto-shared/config/projects.go:103` — `EnsureProjects() (path, ProjectsConfig, created, err)`; `:177` `UpsertProject(cfg, ProjectRef)` — the `~/.auto/projects.json` registry (used to key the project; `add` reads, does not create).

## Dependency contracts T3 consumes

### From 032 (schemas) — `auto-skill/internal/skill/`
- `ParseLock(b []byte) (*Lock, error)` / `ValidateLock(*Lock) []config.ValidationError` — strict; **derived render fields rejected** as `unknown_field` (the seam). `LockEntry{Source,URL,VersionSpec,Ref,Commit,Subpath string; Private,Local bool; State string}`, `State ∈ {resolved,unresolved}`.
- `ParseSkillsYAML(b []byte) (*SkillsYAML, error)` / `ValidateSkillsYAML(*SkillsYAML) []config.ValidationError` — `skills.<name>.{version, replacements}`; replacement literals are strings; file-refs are `{file:,section?,include_heading?,strip_frontmatter?}`.
- `ValidateVersionSpec(s string) error` — grammar only: `latest | branch:<n> | tag:<n> | commit:<hex> | bare`. **No resolution** (that's T3's job, using the cache).
- Empty scaffold shape: `lock.json` = `{"version":1,"skills":{}}`, alphabetized, no timestamps.

### From 009 (cache + trust) — `auto-skill/internal/{transport,cache,trust}/`
- `transport.CanonicalizeURL(raw) (canonical string, id CacheIdentity, err error)` — rejects creds (`credentials_in_url`) + bad/helper/option-like schemes (`unsupported_transport`); returns sanitized URL + canonical identity (lowercased host + full ordered path). `transport.ContainsCredentials(raw) bool` (shared with `ValidateLock`). `transport.Endpoint(raw) (string, error)` → canonical `scheme://host:port` (or canonical abs path) = the trust identity.
- `cache` API: open/resolve a repo by canonical identity (clone bare+blobless if absent, verify origin on reuse); `ResolveRef(ref) (sha, error)`; `Realize(sha) error` (objects fully present, online); `CommitPresent(sha) (bool, error)` (offline, `GIT_NO_LAZY_FETCH=1`, never fetches); `Extract(sha, subpath, dest) error` (streams `git archive`, **validates every entry before write** — symlink/special/gitlink rejected, file-count + size limits: 2000 files / 64 MiB tree / 8 MiB file). `LsRemote`/`Clone` are network ops behind the trust gate.
- `trust` gate: `Authorize(endpoint, requested, io)` — machine-local approval; TTY prompts + records; non-TTY **fails closed** unless `--trust-requested`/`AUTO_SKILL_TRUST_REQUESTED=1` opts into `skills.yaml trusted_hosts`. `IsApproved(endpoint) bool` (exact canonical match; HTTPS ≠ git:// ≠ other port ≠ local path).
- New error codes available: `unsupported_transport`, `credentials_in_url`, `path_escapes_cache_root`, `symlink_entry`, `special_entry`, `too_many_files`, `size_limit_exceeded`.

## Patterns

- **Cobra command** = `&cobra.Command{Use,Short,RunE}`; flags via `cmd.Flags().StringSliceVar`/`BoolVar`/`StringVar`; resolve env via the `resolveEnv()` closure; JSON payload to `cmd.OutOrStdout()` (via `skill.EncodeJSON`), diagnostics to `cmd.ErrOrStderr()`; return `&ExitError{Code,Err}` for non-zero.
- **JSON-default** (CLAUDE.md + `docs/auto-package-patterns.md:296`): stdout strictly parseable payload, stderr diagnostics, `--text` for humans; data-listing returns valid results even when some invalid, exits non-zero if any error.
- **Shared validate()** returns `[]config.ValidationError`; every hard error carries a remediation hint (`docs/auto-package-patterns.md:357`).
- **Fail-fast on invalid usage** (flag conflicts e.g. `--as` + multi-skill, `--skill` matching nothing) via standard cobra errors.
- **Resource triad** (noun + verb) is T6; `add --list` is a *discovery preview*, not the resource `list` command.

## Related Tasks

- **032 (T1, depends-on)** — defines the three schemas + shared `validate()`; `add` writes lock entries + skills.yaml stubs through its typed parsers, never redefining them.
- **009 (T2, depends-on)** — the security-critical fetch substrate (canonical cache, transport gate, credential rejection, trust). `add` calls its `Resolve/Realize/Extract`/`CanonicalizeURL`/trust-gate; never reimplements them.
- **T4 (sync + render, downstream)** — owns rendering into output targets, `manifest.json`, the `update` verb, `auto_update` floating. `add` stops at lock + stub and behaves as `--no-sync` until T4 lands.
- **T7 (migrate, downstream)** — `migrate` calls `add`'s parse/resolve/local-split primitives; out of scope here.
