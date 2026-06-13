---
hash: "c43d5293"
id: "01c33e72"
read_when: "reviewing the playbook retrieval loop task context or understanding why plan.md was absent at solution review"
summary: "Codebase context for the playbook retrieval loop task, noting that plan.md is intentionally absent at this review stage and open design questions were resolved before /new-plan."
title: "Context: Task 019 — Playbook Retrieval Loop"
---

# Context: Task 019

<!-- REJECTED(P2): plan.md is missing — planning is incomplete
REVIEW: This task folder has requirements.md, solution.md, context.md, and loop-flow.md but no
plan.md. Per the workflow (/new-plan produces context.md + plan.md together), planning isn't
finished. Without plan.md I cannot review the execution sequence, phase dependencies, per-phase
build/test commands, or whether success criteria cover every AC — and the task can't be executed.
The solution is design-heavy and several open items below (sharding key, seq allocation, snapshot
staleness, gate scope) must be resolved before a plan is written, since they change the phase
breakdown. Resolve the P1/P2 design comments, then run /new-plan to produce plan.md.
AUTHOR: plan.md is intentionally absent — this review was requested at the solution stage,
deliberately *before* /new-plan, so design issues (which the review confirmed exist: sharding key,
seq allocation, staleness, gate scope — all now resolved in solution.md) get fixed before the phase
breakdown is written. The comment's own last sentence agrees with this ordering. /new-plan runs
next; the plan can be reviewed in a follow-up pass once it exists.
-->


Verified codebase facts grounding [solution.md](solution.md) — what exists in auto-reflect today,
what gets reused, and the conventions the new code must follow.

## Key Files

### To be replaced/deleted
- `auto-reflect/internal/rules/model.go:12-19` — current `Rule{ID, Content, Category, Tags, CreatedAt, UpdatedAt}`; `Playbook{SchemaVersion, Rules}` at 21-24; ID regex `^r-[0-9a-f]{8}$`, tag regex `^[a-z0-9]+(?:-[a-z0-9]+)*$` at lines 5-9 (both regexes carry over).
- `auto-reflect/internal/rules/store.go:23-66` — `Create()` writes whole playbook JSON; `newRuleID()` at 142-148 (`r-` + 4 crypto/rand bytes hex, UnixNano fallback) — pattern reused for `rt-`/`fb-` ids.
- `auto-reflect/internal/rules/lookup.go:13-25,27-81` — `normalizeKeywords()` (lowercase, dedupe) and `rankMatches()` scoring (content=3, category=2, tags=1 pts/keyword, normalized by `3*len(keywords)`, sort score DESC then ID ASC). Normalization + scoring shape carries into `match.go` (use_when=3, domain=1).
- `auto-reflect/internal/feedback/` — legacy annotations package, deleted wholesale. Salvage points: `service.go:258-266` `detectSessionID()` (env order `AUTO_SESSION_ID`, `CODEX_SESSION_ID`, `CLAUDE_SESSION_ID`) and `service.go:268-276` `detectAgent()` — both move to `internal/events`; `service.go:222-243` feedback ID scheme (`f-` + SHA256 prefix + random hex).
- `auto-reflect/internal/cli/rule.go`, `internal/cli/feedback.go` — old command surfaces, rewritten/deleted.
- `auto-reflect/cmd/autoreflect/e2e_feedback_test.go` — legacy e2e, replaced by e2e_loop_test.go.

### Reused as-is
- `auto-reflect/internal/store/jsonl.go:15-51` — `AppendJSONLine()`: marshal, `syscall.Flock(LOCK_EX)`, append + newline, sync. NOT the event-append call target — it opens `O_APPEND` and never reads, so it cannot allocate a monotonic seq. It is the locking/append *pattern reference* for the new `events.AppendEvent` (read last line for max seq → append seq+1, under the same flock); see solution.md §1.
- `auto-reflect/internal/store/jsonl.go:53-84` — `ReadJSONLines()` line-handler reader.
- `auto-reflect/internal/store/jsonfile.go:10-29` — atomic JSON write (`.tmp` + rename) for the snapshot.
- `auto-reflect/internal/store/paths.go:11-14` — `stateDirName = ".auto/reflect"`, `PlaybookPath()` at 28-30; add `EventsDir()`, drop `FeedbackPath()`.
- `auto-reflect/internal/gitutil/git.go:24-54` — `DetectRepo()` returns `RepoInfo{Root, Head, Tree, Remote, Dirty}`; `normalizeRemote()` at 124-152 is the credential sanitizer (strips userinfo/tokens; see `auto-reflect/docs/bugfix-git-remote-credential-leak.md`) — every event's `git.remote` goes through it.
- `auto-shared/config/validation.go:6-12` — `ValidationError{Code, Path, Field, Message, Value}`; `ValidationErrorsError` at 15-32. All new validation returns these.
- `auto-shared/config/host.go:28-57` — `EnsureHost()` creates/loads `~/.auto/host.json` `{hostId, hostname}` — source of the host component in event shard names.
- `auto-shared/config/paths.go:16-52` — `HomeDir()`, `AutoDir()`, `EnsureAutoDir()`.
- `auto-reflect/internal/config/settings.go:116-162` — `EnsureSharedSettings()` (`~/.auto/settings.json`, host default) + `EnsureReflectSettings()` (`~/.auto/reflect/settings.json`, `default_output=json`) — `init` keeps calling both.
- `auto-reflect/internal/app/app.go:8-20`, `auto-reflect/rootcmd/rootcmd.go:12-14` — `App{Stdout, Stderr, CWD}` and the uniform `New(stdout, stderr) *cobra.Command` hook the unified binary consumes (`auto-cli/cmd/auto/main.go:27-53`).

### CLI plumbing conventions (follow exactly)
- `auto-reflect/internal/cli/root.go:16-26` — `ExitError{Code, Err}`; `Execute()` at 28-43 maps it to process exit, errors → stderr; `SilenceErrors/SilenceUsage: true` at 49.
- `auto-reflect/internal/cli/root.go:67-71` — JSON output via `json.NewEncoder(w).SetIndent("", "  ")`.
- `--format json|text` flag defaulting to `"json"` (`rule.go:83`, `feedback.go:101`).
- `auto-reflect/internal/cli/validation.go:10-22` — `writeValidationErrors()` formats structured errors to stderr.
- `auto-reflect/internal/cli/init.go:16-83` — init sequence: shared settings → reflect settings → `store.EnsureStateDir()` → seed files.
- `auto-reflect/internal/cli/quickstart.go:9-74` — embedded markdown string printed verbatim; AC-7 treats it as the executable spec.

### Test harnesses
- `auto-reflect/cmd/autoreflect/e2e_*_test.go` — `TestMain` builds the binary once to a temp dir (e2e_feedback_test.go:14-30); `runBinary()` helper; `requireFields()` JSON assertion helper (e2e_helpers_test.go:54-68).
- `auto-reflect/internal/cli/cli_integration_test.go:359-396` — `runCLIAt()` invokes `cli.NewRootCmd()` directly, captures stdout/stderr + exit code from `ExitError`.
- `auto-reflect/internal/cli/cli_integration_test.go:398-412` — `initGitRepo()` / `gitAddCommit()` temp-repo fixtures (git init, user config, origin remote).

## Patterns

- **JSON stdout purity**: payload only on stdout, diagnostics/errors on stderr; exit non-zero on any validation error even with partial results (root `CLAUDE.md` Cross-Project Coding Guidance; `docs/auto-package-patterns.md` "JSON Output").
- **Resource pattern**: noun + `list`/`describe`/`get` triad, cheap rungs return IDs+metadata, `get` is full fidelity (`docs/auto-package-patterns.md` "Resource Subcommands") — the two-phase `retrieve`→`get` loop is this pattern applied to rules.
- **Every hard error carries remediation** (`docs/auto-package-patterns.md` "Error Handling") — gate failure must print the exact follow-up command.
- **Validation**: one shared `validate()` per schema returning `[]config.ValidationError`; required = presence *and* format; normalize filters (trim/lowercase/dedupe) before matching.
- **Event-sourced persistence NFR** (`auto-reflect/docs/self-improving-playbook-retrieval.md` "Persistence: event-sourced log", lines 354-379): events canonical with `type`+`schema_version`+timestamp+monotonic seq; rules are a fold; snapshot is a disposable cache; shard by host+day; sanitize before write.
- **Go build discipline**: `go build ./...` in the module after each file; gofmt + `go vet` enforced by pre-commit hook (root `CLAUDE.md`).
- Build/test: `cd auto-reflect && go build ./cmd/autoreflect/ && go test ./...` (`auto-reflect/CLAUDE.md`).

## Related Tasks

- No prior `docs/tasks/` folder touches auto-reflect — this is the first task against it; prior
  rule/feedback work shipped via commits `767e223` (v1 rule memory + feedback capture), `9f375a1`
  (required `--effective-at`), `4d2c7de` (git-remote credential sanitizer) and the docs
  `auto-reflect/docs/requirements.md` / `v1-feedback-annotations-design.md` (both now partially
  superseded: rule schema and annotation commands replaced; JSONL/provenance/sanitizer machinery
  retained). Design source: commit `3015c1e` (self-improving playbook retrieval v2).
- Task 017 (`017-unify-binaries-into-auto`): established the `rootcmd.New(stdout, stderr)` wiring
  this task must not break.
- Tasks 017 + 018 feedback.md (execution lessons, both hit the same failures): concurrent subagents
  sharing a worktree leak writes into the MAIN worktree — dispatch phases serially and verify files
  on disk + clean main `git status` between phases; `golangci-lint` cache pollutes across sibling
  worktrees (`golangci-lint cache clean` before trusting lint); real findings can surface only via
  full `make check`, not `go vet`/`go test`.
- Task 016 feedback.md: test assertions must `t.Error` on the failure path (an inverted
  `t.Log`-only assertion passed invisibly); state column/arg counts explicitly when schemas change.
- Task 015 feedback.md: verify CLI surfaces with `--help` before scripting e2e steps; regenerate
  fixtures after schema changes or round-trip tests fail silently.
