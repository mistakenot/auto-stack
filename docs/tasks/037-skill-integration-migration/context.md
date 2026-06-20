# Context: Task 037 — skill-integration-migration

Codebase facts grounding the Solution/Plan for epic-004 T7 (migrate vercel + git
hooks). See [plan.html](plan.html). This task is **glue**: a `migrate vercel`
command and hook wiring on top of the native sync/render engine (034) and
prune/adopt/doctor (035).

## Status of dependencies (built vs planned)

- **Schemas (lock.json / skills.yaml / manifest.json) — 032, PLANNED, not merged.**
  `lock.json` entry struct (per `docs/tasks/032-skill-project-schemas/context.md:40-41`):
  `Source, URL, VersionSpec, Ref, Commit, Subpath string; Private, Local bool; State string`.
  The **`State ∈ {resolved, unresolved}`** discriminator is the load-bearing field
  migration writes. Validation is `ValidateLock() → []config.ValidationError`.
- **Native sync/render + file-ref reverse index — 034 (T4), PLANNED, not merged.**
  Delivers `sync --check` (offline, `GIT_NO_LAZY_FETCH=1`, compares on-disk tree
  digest vs manifest `skill_version`), `sync --locked` (alias `--no-update`),
  `sync --target`, `sync --jobs`; the `manifest.json`; and the **reverse index**
  (`file → skills`, derived from the lock/manifest `replacements.files[].path`
  edges). `docs/tasks/034-skill-native-sync-render/context.md:138-140` explicitly
  names T7: *"migrate resolves then calls sync; pre-commit `sync --check` +
  post-merge `sync --locked` are wired in T7."*
- **doctor / prune / adopt + local-source split — 035 (T5) & 033 (T3), PLANNED.**
  `doctor` reports `unresolved` entries as needs-resolve; the local-source split
  (git repo → lock entry `local:true` non-portable; non-git dir → import into
  `./skills/` as authored; missing path → reported/skipped) is defined in 033 and
  reused by migration (`docs/remote-skills-design.md:1287-1296`).

T7 **reads** these contracts; it does not build the engine, the reverse-index
construction, the manifest, or doctor's classification.

## Key Files

- `auto-skill/internal/cli/root.go:60-96` — `NewRootCmd()` registers subcommands via
  `cmd.AddCommand(newInitCmd(resolveEnv), …, newSyncCmd())`. Each command is a
  `new<Name>Cmd()` returning `*cobra.Command`; receives the `resolveEnv envResolver`
  closure yielding `skill.Env{Root, RootOverride}`. **Add `newMigrateCmd(resolveEnv)`
  here.**
- `auto-skill/internal/cli/root.go:29-39` — `type ExitError struct { Code int; Err error }`,
  the shared non-zero-exit signal (migration uses `Code: 1` on partial/total failure).
- `auto-skill/internal/cli/sync.go:10-27` — current `newSyncCmd()` still shells out to
  `npx skills add`; 034 replaces the body. T7 only *invokes* `sync`/`sync --check`/
  `sync --locked` from hooks — it does not re-implement them.
- `auto-skill/internal/skill/skill.go:23` — skill-name regex `^[a-z0-9]+(?:-[a-z0-9]+)*$`;
  invalid upstream names need `--as`, never silently slugified.
- `auto-skill/internal/skill/skill.go:380-386` — `EncodeJSON(v any) ([]byte, error)`
  (indented + trailing newline) for stdout JSON payloads.
- `auto-shared/config/validation.go:5-12` — `ValidationError{Code, Path, Field, Message string; Value any}`
  + `ValidationErrorsError` wrapper. The canonical structured-error shape (G-schema-strict).
- `auto-skill/internal/cli/cli_integration_test.go:372-397` — `runCLI(t, args...) (stdout, stderr, code)`
  harness: isolates `HOME`, passes `--root` to a `t.TempDir()` project; helpers
  `decodeJSONMap`, `decodeDiagnostics`, `writeFile`, `assertExists`. **Reuse for migrate + hook tests.**

## The vercel input format (checked-in fixture)

- `skills-lock.json` (repo root, 219 lines) — the **actual vercel lock** in this repo,
  usable directly as a migration fixture. Schema: `{"version":1,"skills":{"<name>":{…}}}`.
  Observed entry shapes:
  - github: `{"source":"mistakenot/skills","sourceType":"github","skillPath":"skills/<name>/SKILL.md","computedHash":"<sha256>"}` — **no `ref` field** (defaults to default branch → migrate `version: latest`).
  - local: `{"source":"/home/vscode/.../worktrees/017-.../skills","sourceType":"local","computedHash":"<sha256>"}` — absolute path, no `skillPath`. Apply the local-source split.
  - Present sourceTypes in the fixture: **29 github, 8 local**. No gitlab / node_modules /
    well-known / huggingface / mintlify / `ref`-bearing entries appear — those branches
    need a **synthetic fixture** to test (tag ref, branch ref, sha ref, gitlab, each unsupported type).

## Git hooks infrastructure

- `make install-hooks` (`Makefile:190-191`) runs `git config core.hooksPath hooks/` —
  **all git hooks resolve from the checked-in `hooks/` dir**, so adding a new hook =
  adding a file there.
- `hooks/` currently holds `pre-commit` + `prepare-commit-msg`. `hooks/pre-commit` is a
  thin shim: `exec make -C "$ROOT" --no-print-directory pre-commit`.
- `Makefile:266` — `pre-commit: fmt-staged check verify-fixtures vulncheck-if-deps-changed autodoc-fix skills-sync beads-sync`.
- `Makefile:247-254` — `skills-sync` target: when anything under `skills/` is staged, runs
  `npx skills install "$(CURDIR)/skills" -y` then `git add .agents/`. **This is the
  npx-based, auto-staging stanza T7 replaces** with a check-only `auto skill` stanza.
- No `post-merge` / `post-checkout` / `pre-push` hooks exist yet — T7 adds them as new
  thin shims in `hooks/` delegating to new Makefile targets.

## Patterns / conventions that shape the solution

- **JSON-default I/O (G-json-default):** payload to stdout via `EncodeJSON`; diagnostics to
  `cmd.ErrOrStderr()`; `--format json|text` (persistent). Data-listing returns valid
  results then exits non-zero if any item failed (the migrate "skipped M" pattern).
- **Structured errors + remediation (G-schema-strict):** every hard error is a
  `config.ValidationError` with a remediation hint (e.g. "run `auto skill sync`").
- **Hook entry-point discipline:** the Makefile is the single entry point; `hooks/*` are
  thin shims (`exec make -C "$ROOT" <target>`). New hooks follow that shape.
- **Additive migration:** never write to `skills-lock.json` or installed files; only create/
  extend `.auto/skills/lock.json` + `.auto/skills/skills.yaml`.

## Related Tasks

- **032 (schemas):** defines lock/skills.yaml/manifest structs + `ValidateLock`; migration
  writes entries those structs validate (with `state:"unresolved"`).
- **033 (add/discovery):** the local-source split + skills.yaml stub seeding (empty
  `replacements: {}`); migration reuses the split and seeds an explicit `version`.
- **034 (native sync/render):** `sync --check`/`--locked`, manifest, reverse index — the
  engine migrate's output feeds into and the hooks invoke.
- **035 (prune/adopt/doctor):** `doctor`/`sync --check` report `unresolved` as needs-resolve.
- **036 (inspection triad + deprecations):** sibling house style for the Verification/Solution
  tabs (`docs/tasks/036-skill-inspect-deprecations/plan.html`); also confirms `ls`→`list`
  and the `update` reclaim happen there, not here.

## Git-history check (file-ref drift)

- No prior `migrate` work exists: `auto-skill/internal/migrate/` is absent (new package).
- Command registration confirmed at `auto-skill/internal/cli/root.go:82-93`
  (`cmd.AddCommand(newInitCmd(resolveEnv), …, newSyncCmd())`) — the insertion point for
  `newMigrateCmd(resolveEnv)` still holds. Note `newSyncCmd()` (root.go:92) currently takes
  no `resolveEnv`; migrate follows the `resolveEnv`-taking pattern of `init`/`lint`/`ls`.
- The pre-commit pipeline was consolidated behind the Makefile (`9654eac chore(make):
  consolidate pre-commit pipeline behind Makefile`); the `skills-sync`/`beads-sync` stanza
  shape this task mirrors is current as of `Makefile:247-266`.
- All Solution-tab file paths verified present (or correctly absent, for `add`s) at planning
  time; none have drifted.
