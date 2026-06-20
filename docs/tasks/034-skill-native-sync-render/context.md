# Context: Task 034 — skill-native-sync-render

Codebase + dependency-contract context for epic-004 **T4** (native `sync` + render
engine — the PR where the `npx skills` shell-out dies). See [plan.html](./plan.html).

> **Dependency-API note.** Tasks 032 (schemas), 009 (cache/trust), and 033 (`add`)
> are *planned, not merged*. The Go symbol names below for those layers come from
> their `plan.html` API outlines and are the **planned surface** T4 builds behind;
> if a name differs at execution time, T4 adapts to the merged signature (the
> contract — not the exact identifier — is load-bearing). The design source of
> truth is [`docs/remote-skills-design.md`](../../remote-skills-design.md).

## Key Files (this package)

- `auto-skill/internal/cli/sync.go:10-27` — `newSyncCmd()` **shells out**:
  `exec.CommandContext(ctx, "npx", "skills", "add", "./skills", "--agent", "codex",
  "claude-code", "--full-depth", "-y")`. **T4 deletes this exec** and replaces the
  body with a call into the native `sync` pipeline + the `--check`/`--locked`/
  `--no-update`/`--target`/`--jobs` flags.
- `auto-skill/internal/cli/update.go:11-28` — `newUpdateCmd()` is the **binary
  self-update** (`update.Run` from `auto-shared/update`). Per epic decision **D-3**,
  relocating this to root `auto update` and reclaiming the `auto skill update` name
  for skills is **T6**, not T4 (see Open Questions). T4 builds the skills float-then-
  render *engine* + entrypoint and drives it through `sync`'s `auto_update` mode; it
  does **not** touch this command's registration.
- `auto-skill/internal/cli/root.go:60-96` — `NewRootCmd`; subcommands registered via
  `cmd.AddCommand(newInitCmd(resolveEnv), …, newSyncCmd())`. `resolveEnv()` closure
  (58-69) yields `skill.Env`. T4 edits `newSyncCmd` in place (keeps registration).
- `auto-skill/internal/cli/root.go:29-39` — `type ExitError struct { Code int; Err
  error }`; non-zero exits return `&ExitError{Code: N, Err: …}`.
- `auto-skill/internal/cli/cli_integration_test.go` — `runCLI(t, args...) (stdout,
  stderr string, code int)` builds `app.New` + `cli.NewRootCmd`, isolated buffers,
  decodes `ExitError`. Helpers: `decodeJSONMap`, `decodeDiagnostics`, `validSkill`,
  `writeFile`, `assertExists`. Tests isolate `HOME` + pass `--root`. T4 extends with
  `sync` / `sync --check` / `sync --locked` cases against a `file://` fixture.
- `auto-skill/internal/skill/skill.go:380` — `func EncodeJSON(v any) ([]byte, error)`
  (JSON payload encoder used by every command); `:43` `ResolveRoot`; `:23`
  `skillNameRE = ^[a-z0-9]+(?:-[a-z0-9]+)*$`; `:349` `ValidateSkillName`.
- `auto-skill/internal/skill/skill.go:776-807` — `parseFrontmatterAndBody()` splits a
  `SKILL.md` on `---\n` into frontmatter + body. The render engine reuses this shape
  to read the `customize:` block and to strip leading YAML frontmatter from file-ref
  injections (the `strip_frontmatter` default).
- `auto-skill/go.mod:8` — `gopkg.in/yaml.v3 v3.0.1` already a dep (the `customize:`
  schema and `skills.yaml` parse go through 032's typed parser / yaml.v3; **no new
  runtime deps** — the template engine is stdlib `text/template`/`text/template/parse`).

## Key Files (shared layer)

- `auto-shared/config/jsonfile.go:27` — `DecodeJSONFileStrict(path, target)`; `:66`
  `WriteJSONFileAtomic(path, value)` (temp + atomic rename). `manifest.json` and
  `lock.json` are written via 032's typed structs + this atomic writer; the journaled
  commit reuses the temp-then-rename pattern for same-FS staging.
- `auto-shared/config/validation.go:6` — `type ValidationError struct { Code, Path,
  Field, Message string; Value any }` — the canonical structured error.
- `auto-shared/git/detect.go:57` — `runGit(dir, args...) (string, error)`; 009 extends
  with `Clone`/`Fetch`/`Archive`/`RevParse`/`LsRemote` (args after `--`,
  `GIT_TERMINAL_PROMPT=0`, offline `GIT_NO_LAZY_FETCH=1`). `sync`'s phase-B fetch and
  phase-C `git archive` extraction call these through 009's cache.
- `auto-shared/git/normalize.go` — `NormalizeRemoteURL` / `ComputeRepoID*`; 009's
  cache keys on these for the per-repo dedupe in phase A.

## Dependency contracts T4 consumes

### From 032 (schemas) — `auto-skill/internal/skill/`
- `ParseLock` / `ValidateLock` / `LockEntry{Source,URL,VersionSpec,Ref,Commit,Subpath
  string; Private,Local bool; State string}` — **dependency identity only**; derived
  render fields rejected as `unknown_field`. Locked `sync` rewrites the **manifest**,
  never the lock (the seam).
- `ParseSkillsYAML` / `ValidateSkillsYAML` — `auto_update bool` (default true),
  `targets`, `skills.<name>.{version, replacements}`; replacement value = a literal
  string **or** a file-ref `{file, section?, include_heading?, strip_frontmatter?}`.
- `ParseManifest` / `ValidateManifest` / `Manifest` — the **derived render state**
  T4 *populates*: per skill `template_hash`, resolved replacement literals, file-ref
  `content_hash`es + `matched_heading`, `skill_version`, `render_version`; per target
  the managed set + expected `skill_version`. (If a derived field is absent from
  032's struct, T4 extends the schema — see Open Questions.)
- `ValidateVersionSpec(s) error` — grammar only (`latest | branch:<n> | tag:<n> |
  commit:<hex> | bare`); resolution is the cache's job.

### From 009 (cache + trust) — `auto-skill/internal/{transport,cache,trust}/`
- `cache`: open/resolve a repo by canonical identity; `ResolveRef(ref) (sha, error)`
  (online ref re-resolution — used only when a float is allowed); `Realize(sha)
  (online; materializes a pinned commit's objects on a cache miss — **locked
  materialization**); `CommitPresent(sha) (bool, error)` (offline, `GIT_NO_LAZY_FETCH
  =1`, never fetches — phase-A skip + `--check`); `Extract(sha, subpath, dest)`
  (streams `git archive`, **validates every entry**: symlink/special/gitlink rejected,
  file-count + size limits). `LsRemote`/`Clone`/`Fetch` are network ops behind trust.
- `trust.Authorize(endpoint, requested, io)` / `IsApproved` — phase-B fetch passes
  every endpoint through the gate; non-TTY fails closed unless trust is requested.
- `transport`: `CanonicalizeURL` / `ContainsCredentials` / `Endpoint` (the trust id).

### From 033 (add) — `auto-skill/internal/{source,discovery,add}/`
- `add` writes the `lock.json` entries (`state:resolved`) + `skills.yaml` stubs and
  behaves as `--no-sync` until T4 lands. **T4 wires the post-`add` auto-sync call**
  (the `(unless --no-sync) render into every output target` step the design names).
- `discovery.Discover` + the tree-digest helper are reusable for the on-disk
  target-tree digest comparison (the incremental skip) — though T4's digest is the
  full **rendered** `skill_version`, not discovery's source digest.

## Patterns

- **Pure render seam** — render is `(template, replacements, resolved-files) →
  canonical tree`; `skill_version = sha256(canonical_json(sorted files))`. Same lock +
  same inputs → identical bytes on every machine (G-determinism). No code execution
  in the path (G-no-exec): `text/template` parsed via `text/template/parse` then the
  AST is **walked and rejected** unless every node is a field-access action or the
  literal-brace escape.
- **Three-phase pipeline** — A Plan (no network: dedupe by repo, skip cache-satisfied,
  locked materialization, intent reconciliation) → B Fetch (bounded `--jobs` worker
  pool, isolated per-repo failure, exit non-zero on any failure; `--check` skips B) →
  C Process (extract→render→write, incremental on-disk-digest skip). `--check` is an
  offline CI gate (G-offline-check, G-fast-sync).
- **Journaled crash-consistent commit** (G-crash-consistent) — stage same-FS → swap
  per skill (old → journaled trash) → receipts → manifest → lock → clear journal; a
  non-empty journal triggers roll-forward/back recovery; pruning suppressed when the
  desired set is incomplete.
- **JSON-default** — stdout strictly parseable payload, stderr diagnostics, `--text`
  for humans; data-listing returns valid results even when some invalid, exits
  non-zero if any error. `sync` only **warns** on token budgets (advisory); `lint`
  gates (G-token-budget).

## Related Tasks

- **033 (T3, depends-on)** — native `add`: parse → resolve → discover → write lock +
  `skills.yaml` stub, behaving as `--no-sync`. T4 supplies the render/sync engine
  `add` defers to and wires its post-add auto-sync.
- **032 (T1)** — the three schemas + shared `validate()`; T4 **populates**
  `manifest.json` through the typed `Manifest`, never redefining the schemas.
- **009 (T2)** — the fetch substrate (cache/transport/trust); T4 **drives** its
  `ResolveRef`/`Realize`/`CommitPresent`/`Extract` + trust gate; never reimplements.
- **T5 (prune & adopt, downstream)** — receipts-as-deletion-authority, manifest-orphan
  pruning, foreign-dir conflict handling, `adopt`, `doctor` drift reporting. T4
  **writes** receipts + manifest (the commit protocol needs them) but does **not** run
  the orphan-prune pass (see Open Questions).
- **T6 (deprecations, downstream)** — reclaims `auto skill update` from the binary
  self-update (D-3). T4 builds the skills update engine + entrypoint; T6 wires the
  public command with no temporary alias.
- **T7 (migrate + hooks, downstream)** — `migrate` resolves then calls `sync`;
  pre-commit `sync --check` + post-merge `sync --locked` are wired in T7.
