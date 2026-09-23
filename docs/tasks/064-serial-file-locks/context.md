# Context: Task 064 — serial-file-locks

Codebase grounding for the `auto lock` serial-file-locking feature. See [plan.html](plan.html) for requirements and design.

## Key Files

### Hook adapter (enforcement rides here)
- `auto-cli/cmd/auto/hookscmd.go:53` — `newHooksFireCmd`: reads the hook payload from stdin (bounded 1 MiB), resolves cwd/project, appends a durable log, POSTs a bus event, then calls `matchAndEmitHint`. **Always exits 0 today** (comment at `:49-52`). The lock guard adds a PreToolUse deny branch here.
- `auto-cli/cmd/auto/hookscmd.go:160` — `buildBusEvent`: already parses `hook_event_name`, `tool_name`, `session_id`, `cwd`, and resolves git `Provenance` (worktree root, branch, commit) + project. Reuse for identity.
- `auto-cli/cmd/auto/hookscmd.go:247,269` — `extractPathRefs`/`resolvePathRefs`: pull `tool_input.{file_path,notebook_path,path}` as abs+rel. This is the file(s) under edit to match against globs. (Edit/Write/MultiEdit all carry a top-level `file_path`; NotebookEdit uses `notebook_path`.)
- `auto-cli/cmd/auto/hints.go:136-157` — `hookResponse`/`hookSpecificOutput` (only `additionalContext`, scoped to PostToolUse). Emits **at most one** JSON object per hook. The deny path needs a **new** response shape (see below) — `grep permissionDecision` across auto-cli/auto-shared returns nothing.
- `auto-cli/cmd/auto/hooksinstallcmd.go:17,33` — `claudeHookEvents`/`codexHookEvents`: `auto hooks fire` is **already installed on `PreToolUse`** (and Codex `PermissionRequest`) for both agents, matcher-less. No new hook install needed; guard no-ops for non-edit tools.

### Deny schema (authoritative, from code.claude.com/docs/en/hooks-guide.md)
- Deny = exit 0 + stdout: `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"…"}}`.
- `permissionDecision` ∈ `allow|deny|ask|defer`. **`permissionDecisionReason` is fed back to the agent** (it can read + act on it). Most-restrictive-wins across hooks.
- Empty stdout = allow (mirror `hints_test.go:168` suppression assertion).
- Codex: same JSON shape but incomplete tool interception → comprehensive on Claude, best-effort on Codex.

### File lock for the store (copy this pattern)
- `auto-env/internal/registry/registry.go:44` — `withLock(fn func() error)`: `os.OpenFile("<dir>/environments.lock", O_CREATE|O_RDWR, 0600)` + **blocking** `syscall.Flock(fd, LOCK_EX)` + `defer LOCK_UN`, RMW entirely inside the closure. This is the exact template for `~/.auto/lock/locks.json` RMW. Combine with `config.WriteJSONFileAtomic` inside the closure for crash-safe writes.
- `auto-shared/config/jsonfile.go:63-65` — doc comment confirms atomic-rename alone does NOT serialize concurrent RMW; a flock is required. (`WriteJSONFileAtomic` at `:66`, `DecodeJSONFile` at `:13`.)
- `auto-watch/internal/daemon/daemon.go:230` — `AcquireLock` (non-blocking `LOCK_NB`, single-owner daemon). **Not** what we want (that's a long-held ownership lock); the registry pattern (short, blocking, closure-scoped) fits the store RMW. No shared flock helper exists — copying is the norm.

### Worker identity primitives
- `auto-env/internal/worktree/worktree.go:14,23` — `Detect(cwd) (*Info, error)`; `Info{Name, Branch, BranchSlug, IsMain, Slot, RepoRoot, WorktreePath}`. `IsMain` = `git worktree list --porcelain` first entry == repoRoot. `WorktreePath`=current worktree toplevel.
- `auto-shared/hooks/log.go:38` — `CaptureEnv()` filters `NTM_*`/`TMUX`/`TMUX_*` from the environment (incl. `TMUX_PANE`). Visible to both the Bash-run CLI and the hook subprocess. `CaptureContext()` (`:141`) merges env + tmux target. **`session_id` is only in the hook payload — invisible to the Bash CLI**, so it cannot key identity (this is why the resolution chain uses env/git, not session).
- `auto-shared/config/host.go:34` — `HostIDQuietly()` → `~/.auto/host.json` host id, hostname fallback, never errors.
- `auto-shared/config/projects.go:192,225` — `FindProjectByPath`/`FindProjectByRemote` map cwd/worktree → project id.
- `auto-shared/config/paths.go:28,46` — `AutoDir()`→`~/.auto`, `EnsureAutoDir()`. Store lives at `~/.auto/lock/locks.json`.
- `auto-shared/git/detect.go:13,28,49` — `RepoRoot`, `Provenance` (root, branch, commit), `OriginRemote`.

### Package placement & mounting (DECIDED: D-7 = in-tree, like `auto hooks`)
- **Chosen (D-7):** command lives **in-tree** at `auto-cli/cmd/auto/lockcmd.go` (+ subcommands), exactly like `auto hooks`/`auto hooks fire`. The pure library — config, store, identity, `Evaluate` — lives in a **new `auto-shared/lock/` package**, imported by BOTH `lockcmd.go` and the `hooks fire` guard in `hookscmd.go`. **No new module** (no go.mod / go.work / require+replace).
- `auto-cli/cmd/auto/main.go:48` — mount is one line: `root.AddCommand(newLockCmd())` (cf. the 2-line `mailcmd` mount in 0c73e78). `auto-cli/cmd/auto` already imports `auto-shared/*`, so importing `auto-shared/lock` is trivial.
- Precedent: `auto-shared/bus` (added in 021/e3a635b) is the template for a new `auto-shared/` subpackage consumed by the fire producer + other tools; `auto hooks`/`hints.go` is the template for an in-tree command+guard.
- **Superseded (was the separate-module idea):** a standalone `auto-lock` module (auto-mail style, 0c73e78) — rejected in D-7; the non-internal-evaluator import constraint it created disappears once the lib is in `auto-shared/lock`.

### Conventions (docs/auto-package-patterns.md)
- `doctor` (`:199-209`): emits `[]DoctorCheck{Check, Status:"pass"|"fail"|"warn", Message}`.
- JSON default (`:296-312`): stdout = parseable JSON only (2-space indent via `writeJSON`), stderr = diagnostics, `--text` opt-in, exit 1 on error even with partial results.
- Resource subcommands (`:239-294`): noun + `list`/`describe <id>`/`get <id>` + `search`. Relevant to `auto lock status`/`list`.
- Config file locations (`:316-322`): project `.auto/<name>/settings.json`.

### Config load template
- `auto-env/internal/config/config.go:27,39` — `ConfigPath(repoRoot)=<root>/.auto/env/config.json`; `Load` collects missing required fields and reports them together, then defaults zero values. `auto lock` mirrors this with `.auto/lock/settings.json` and per-group required fields (`name`, `glob`, `description`).

### Test harness for hooks
- `auto-cli/cmd/auto/hints_test.go:488` — `runFire(t, agent, stdinJSON) string` feeds a JSON payload on stdin, returns stdout; `decodeHint` unmarshals. Mirror `TestFireEmitsHintOnGitPush` (`:120`) for a deny test and `TestFireNoHintOnIsError` (`:168`, asserts empty stdout) for the allow path.
- Test files: `hookscmd_test.go`, `hints_test.go`, `hooksinstallcmd_test.go`, `hooksmail_test.go`.

## Patterns
- **Hot-path silence:** hook-path code loads config quietly and never errors the agent (see `loadHintsConfig` `hints.go:92`, `loadRegistryQuietly` `hookscmd.go`). The guard follows this — a missing/broken lock config or store means *allow*, never crash. The ONE deliberate exception is a confirmed lock violation, which emits a deny.
- **flock + atomic write:** blocking `LOCK_EX` on a `.lock` sidecar around a read → mutate → `WriteJSONFileAtomic` closure (registry pattern).
- **Structured validation:** collect `[]ValidationError` and report together (config.go pattern + CLAUDE.md `validate()` guidance).

## Related Tasks & Git History

Precedents to copy, newest-relevant first (SHA · task):

- **062-auto-mail-walking-skeleton** (`0c73e78`) — **closest end-to-end template**: new command + `auto <cmd>` mount (`main.go` +2 lines) + new harness scenario (`mail-flow`) + glossary terms, all in one task. Mirror its shape (though we mount in-tree per D-7, not a new module).
- **053-auto-hook-hints** (`d783cd8`) — the payload-matching, JSON-emitting `auto hooks fire` precedent. Copy `hints.go`/`hints_test.go` structure for the deny evaluator + tests. Gotcha: **all stdout emission scoped to one event inside the emit fn** so other events never write stdout; tests isolate via `t.TempDir()` + `t.Setenv("HOME")` + a real temp git repo per test.
- **047-hook-retarget-autowatch** (`6e4b5c8`) — the **cross-module internal-import constraint**: could not import auto-watch's `internal/config`; mirrored via `sharedconfig`. (D-7 sidesteps this by putting the lib in `auto-shared/lock`.) Also: staticcheck/golangci-lint pre-commit flags unused unexported consts — don't leave dead code.
- **021 auto-bus-standard** (`e3a635b`) — `buildBusEvent` provenance + **remote always normalized (never raw)** to avoid credential leakage; `auto-shared/bus` is the new-subpackage template for `auto-shared/lock`.
- **020 auto hooks install** (`71005da`) — confirms `PreToolUse` (Claude) / `PermissionRequest` (Codex) already installed matcher-less. No new install needed.
- **063-auto-mail-parent-handle** (`b29d77a` phase 4) — **concurrency-test discipline directly transferable to the lock store** (AC-8): a concurrency test that passes once proves nothing — run every assertion against a deliberately broken build first; use **four** concurrent actors, not two (two hides a lost write in "ambiguous"); each actor opens its **own** store client; run under `-race`. Also the G11/seam-guard gotcha: `glossary.py check` greps prose, and an `_Avoid_` bare word colliding with a canonical term fails the check (name it only parenthetically).
- **055-autowatch-daemon-hardening** (`e091b79`), **058-promote-e2e-harness** (`e566f6d`) — daemon-lock hardening mindset; the harness scaffold (Dockerfile + compose + scenario.py + tests dir) `lock-flow` follows.

Concurrency note: `auto-env/internal/registry/registry.go:44` `withLock` (introduced `0046385`) has been **stable, never patched for races** — safe to copy verbatim. The richer race lessons live in task 063's tests, not the flock code.

Glossary: `docs/concepts/UBIQUITOUS_LANGUAGE.md` is **hand-edited for term prose** (bold term + definition + `_Avoid_:` + optional `_Has_:`), and `glossary.py diagram --write` regenerates ONLY the marker-delimited mermaid block. Add Lock/Group/Worker via the `domain-modelling` skill's `add-term`; keep any avoided word that collides with a canonical term (e.g. "Message") parenthetical, or `glossary.py check` fails (per 062).
