# Context: Task 009 — skill-git-cache-trust

Codebase grounding for [plan.html](plan.html) (epic-004 remote-skill-management, task T2). Builds the security-critical fetch substrate: a content-addressed global git cache, transport allow-list, credential hygiene, machine-local `trust.json`, and the `cache`/`trust` CLI sub-resources. Depends on task 032 (T1) for the `.auto/skills/` plural layout and the `Lock`/`LockEntry` schema types.

## Key Files

### auto-skill Env / paths (extend; 032 pluralizes these)
- `auto-skill/internal/skill/skill.go:38-41` — `Env{Root, RootOverride}`; `:43-57` `ResolveRoot`.
- `auto-skill/internal/skill/skill.go:59-76` — path helpers. **TODAY singular**: `ProjectSettingsPath()` → `.auto/skill/settings.json` (`:63-65`); `GlobalSettingsPath()` → `~/.auto/skill/settings.json` (`:67-76`, honors `RootOverride` → `<root>/.auto/skill/...`); `SkillsDir()` → `./skills/` (`:59-61`). **032 (T1) pluralizes to `.auto/skills/` and adds `SkillsConfigDir`/`SkillsYAMLPath`/`LockPath`/`ManifestPath` + global `~/.auto/skills/settings.json`.** T2 adds `UpstreamCacheDir()` → `~/.auto/skills/upstream/` and `TrustPath()` → `~/.auto/skills/trust.json` in the same plural layout (honoring `RootOverride` for test isolation).
- `auto-skill/internal/skill/skill.go:19` — `const settingsFileName = "settings.json"`.

### Existing git invocation to extend (the build-on point)
- `auto-shared/git/detect.go:57-73` — **`runGit(dir string, args ...string) (string, error)`** = `exec.Command("git", args...)` with `-C dir` style usage; `:13-23` `RepoRoot`, `:28-45` `Provenance` (degrades gracefully on non-repo), `:49-55` `OriginRemote` (`git remote get-url origin`). T2 adds cache wrappers (`Clone`/`Fetch`/`Archive`/`RevParse`/`LsRemote`) over this pattern — args after `--`, `GIT_TERMINAL_PROMPT=0`, and `GIT_NO_LAZY_FETCH=1` for offline ops.
- `auto-skill/internal/cli/sync.go:14-22` — current `sync` **shells out to `npx skills add`** (`exec.CommandContext(..., "npx", "skills", "add", ...)`). This is the npx dependency epic-004 replaces; T2 does NOT touch it (T4 deletes it) but it confirms `os/exec` usage is already the norm here.

### auto-shared/git — credential-safe URL helpers (REUSE)
- `auto-shared/git/normalize.go:16-65` — **`NormalizeRemoteURL(raw string) string`**: SSH→HTTPS, strips `.git`, **strips credentials**, lowercases host, returns "" on empty. The credential-stripping backbone for `CanonicalizeURL` + the lock `url`.
- `auto-shared/git/normalize.go:69-72` — `ComputeRepoID(normalizedRemote string) string` → 16-char hex SHA256. Use for the **URL-hash disambiguation suffix** when two distinct remotes would collide at one cache path.
- `auto-shared/git/normalize.go:77-80` — `ComputeRepoIDFromPath(absPath string) string` → fallback id for local sources.

### auto-shared/config — JSON I/O, paths, registry (REUSE)
- `auto-shared/config/validation.go:5-12` — **`ValidationError{Code, Path, Field, Message, Value}`** — canonical structured error (no severity), mandated by CLAUDE.md/package-patterns. `cache`/`trust` validation returns `[]ValidationError`.
- `auto-shared/config/jsonfile.go:27` — `DecodeJSONFileStrict(path, target)` (reject unknown keys — use for `trust.json`); `:66` `WriteJSONFileAtomic(path, value)` (temp+rename, safe for concurrent readers — use for `trust.json` writes).
- `auto-shared/config/paths.go:16` `HomeDir()` (prefers `$HOME`); `:28` `AutoDir()` → `~/.auto`; `:46` `EnsureAutoDir()`.
- `auto-shared/config/projects.go:39` `ProjectsConfigPath()` → `~/.auto/projects.json`; `:78` `LoadProjects(path)`; `:103` `EnsureProjects()`; `:177` `UpsertProject`; `:211` `(c ProjectsConfig) FindProjectByExactPath(dir)`; `:225` `FindProjectByRemote(remote)`. **`cache prune --unreferenced`** reads project paths from this registry to decide what is "known-referenced".

### File-lock pattern to copy (per-repo concurrency)
- `auto-env/internal/registry/registry.go:44-58` — **`withLock(fn func() error) error`**: `os.OpenFile(lockPath, O_CREATE|O_RDWR, 0600)` → `syscall.Flock(fd, LOCK_EX)` → `defer Flock(fd, LOCK_UN)`. Identical pattern at `auto-reflect/internal/store/jsonl.go:37-42`. `syscall` is stdlib — **no new dep**. T2 puts a `<cache-repo>.git/.fetch.lock` lock around fetch/extract.

### CLI command structure (mount cache/trust here)
- `auto-skill/internal/cli/root.go:60-96` — `NewRootCmd(app)`; persistent `--root` flag; `:82` `cmd.AddCommand(...)` mounts subcommands. T2 mounts `newCacheCmd()` (`list`/`path`/`prune`) and `newTrustCmd()` (`list`/`add`/`remove`).
- `auto-skill/internal/cli/root.go:98-181` — `newInitCmd`: the **JSON-default + `--text`** output precedent (`skill.EncodeJSON` to stdout, diagnostics to stderr, exit 1 on error). `skill.EncodeJSON` lives in `auto-skill/internal/skill/skill.go`.

### Tests
- `auto-skill/internal/cli/cli_integration_test.go:18-79` — integration harness: `t.TempDir()` root + `runCLI(t, "--root", root, ...)` returning (stdout, stderr, exit); `assertExists` helper. No git-based fixtures exist in auto-skill yet — T2 introduces a helper that `git init` + commits a small skill tree in a temp dir to clone from (`file://` source — already in the transport allow-list, so it doubles as the local-transport test).

## Patterns
- **JSON-default I/O** (`auto-package-patterns.md` "JSON Output"; `root.go`): `skill.EncodeJSON` (MarshalIndent + trailing newline) → stdout; diagnostics/errors → stderr; `--text`/`--format text` flag (never `--json`); exit 1 on errors even with partial valid results.
- **Shared `validate()`** (`auto-package-patterns.md` "Validation"): `func validate(x) []config.ValidationError` with codes (`invalid_*`/`required`/`duplicate_*`), JSON-path-ish `Path`, every hard error carries a remediation hint in `Message`.
- **Strict decode**: `trust.json` via `DecodeJSONFileStrict`; atomic writes via `WriteJSONFileAtomic`.
- **git exec discipline**: always `git ... -- <args>`; set `GIT_TERMINAL_PROMPT=0` (never prompt for creds), `GIT_NO_LAZY_FETCH=1` for offline checks; never run remote hooks (clone/archive don't).
- **Cobra mounting**: `internal/cli/root.go` `cmd.AddCommand(...)`; sub-resources are noun+verb (`cache list/path/prune`, `trust list/add/remove`) per CLAUDE.md resource convention.
- **No new runtime deps** (CLAUDE.md): `syscall.Flock`, `net/url`, `archive/tar` (for parsing `git archive` tar stream), `crypto/sha256` are all stdlib; `auto-shared` is stdlib-only.

## Design-doc references (`docs/remote-skills-design.md`)
- **§Global git cache** (lines 100-229) — the master spec for everything in T2: layout/identity, safe path encoding (RESOLVED P1 :122), cache key canonical identity + origin verify + hash suffix (RESOLVED P2 :127), bare blobless clone (:132), realize-blobs-online (:135), offline `GIT_NO_LAZY_FETCH=1` guarantee (:138, RESOLVED P1 :143), extract-without-checkout + entry validation + resource limits (:148, RESOLVED P1 :154), transport safety allow-list (:161, RESOLVED P1 :193), machine-local trust + bootstrap fail-closed (:166-181, RESOLVED P1 :183, P2 :188), credential hygiene reject-at-parse (:201, RESOLVED P1 :206), GC/prune two modes (:216, RESOLVED P2 :223), per-repo file-lock concurrency (:228).
- **§CLI surface** (lines 611-657) — `cache list | prune | path`, `trust list | add <endpoint> | remove <endpoint>` (`:651-653`); full options table (`:1247-1249`): `cache prune` flags `--dry-run`/`--unreferenced`/`--max-age <dur>`/`--max-size <size>`; trust endpoint = `scheme://host:port` or local path. Persistent flags `--root`, `--format json|text` (`:1254`).
- **§Developer quickstart → 7. Cache & targets** (lines 1185-1200) — exact command examples (`cache list`, `cache path github.com/acme/agent-skills`, `cache prune [--dry-run|--unreferenced|--max-age|--max-size]`); RESOLVED P2 (:1197) aligns prune to default age/size eviction + `--unreferenced` registry mode.
- **§Source formats** (lines 718-752) — canonical identity = `<host>/<repo-path>` full ordered path (so nested GitLab subgroups nest); HTTPS/SSH forms collapse to one cache. T3 owns source-form parsing; T2 owns the canonical-URL → cache-identity primitive it calls.
- **§lock.json schema** (lines 558-578) — `url` is "sanitized canonical URL (no credentials)"; `source` mirrors the cache path. T2 produces these; 032's `ValidateLock` defends them.

## Related Tasks
- **Task 032 (T1, `docs/tasks/032-skill-project-schemas/`)** — defines `.auto/skills/` plural layout, `Lock`/`LockEntry` types, the `credentials_in_url` schema check, and the `init` wizard + `projects.json` reuse. **T2 depends on it**: reuses `Env` plural paths and `Lock` types; shares one `containsCredentials`/`CanonicalizeURL` helper with `ValidateLock` (one source of truth). The two tasks touch disjoint files.
- **Epic 004** (`docs/epics/epic-004-remote-skill-management.html`) — T2 honors G-transport, G-cred-hygiene, G-extract-safe, G-offline-check. The "Cache identity" + "Trust boundary" load-bearing seams (epic Architecture tab) are delivered here. T3 (`add`) consumes the cache; T4 (`sync`) drives it through a worker pool; T5 owns prune *receipts* (distinct from T2's cache eviction).
- **Pattern precedent — registry plumbing** `66e9c4c` (#72) introduced `auto-shared/config/projects.go`; `cache prune --unreferenced` reuses it (do not reimplement). **`auto-env`** registry (`auto-env/internal/registry/`) is the precedent for `syscall.Flock` file-locked, atomic-write on-disk state — the closest analog to the git cache's concurrency model.
- **Task 020 (`docs/tasks/020-auto-hooks-install/`, completed)** — idempotent config-install, `WriteJSONFileAtomic`, structured-error-with-remediation precedent for `trust add`/`trust remove`.

### Drift check
- All Solution-tab paths confirmed present except the new files T2 adds. `auto-shared/git/{normalize,detect}.go`, `auto-shared/config/{jsonfile,paths,projects,validation}.go`, `auto-env/internal/registry/registry.go` (flock), `auto-skill/internal/cli/{root,sync}.go` all confirmed at the cited lines. `syscall`/`net/url`/`archive/tar`/`crypto/sha256` are stdlib; `auto-skill/go.mod` direct deps = `auto-shared`, `cobra`, `yaml.v3` only (no new dep needed). 032's plural `Env` helpers are **pending merge** — see Open Questions for the standalone-buildability fallback.
