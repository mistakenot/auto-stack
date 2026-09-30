# Context: Task 065

Facts gathered from the codebase for [plan.html](plan.html) (the `auto plan` graph CLI). Paths are relative to the repo root. Line numbers were taken on 2026-09-29 at `origin/main` 43aa2b8 and re-verified on 2026-09-30 (seven drifted citations corrected).

## Key Files

### Package scaffold and mounting (auto-mail, merged in 0c73e78, is the cleanest template)
- `docs/auto-package-patterns.md:507-523`: the new-package checklist. `.claude/skills/new-package/SKILL.md:94-101` is stale because it still names per-tool `_BIN` targets; the Makefile now builds one binary (`Makefile:42-45`).
- `auto-mail/go.mod`: `module github.com/mistakenot/auto-mail`, `go 1.26.1`, requires auto-shared + cobra `v1.10.2`, and `replace github.com/mistakenot/auto-shared => ../auto-shared`.
- `auto-mail/rootcmd/rootcmd.go:16-19`: the mounting wrapper, `New(stdout, stderr io.Writer) *cobra.Command` → `cli.NewRootCmd(app.New(stdout, stderr, cwd))`.
- `auto-mail/internal/app/app.go:6-14`: `App{Stdout, Stderr, CWD}`.
- `auto-mail/internal/cli/root.go:17-32` `ExitError{Code int; Err error}`; `:35-57` `Execute`; `:60-82` `NewRootCmd` (SilenceErrors/Usage, `version.Version`); `:84-88` `writeJSON`.
- `auto-cli/cmd/auto/main.go:13-27` (imports), `:41-54` (`root.AddCommand(...)`), `:92-106` (`exitCodeFor` preserves `ExitError` codes).
- `auto-cli/cmd/auto/main_test.go:63`: the `TestAllToolsMounted` stems list; add `plan`. `mail` is also missing, because 062 forgot it (0c73e78).
- `Makefile:21` `PROJECTS` (auto-cli must stay last); `go.work` `use (...)`; `CLAUDE.md:61-74` Sub-Projects table.
- `scripts/check-no-stale-binary-refs.sh` (`make stale-refs`) rejects per-tool binary names, so docs must say `auto plan`, never `autoplan`.

### Validation and lint
- `auto-shared/config/validation.go:6-12`:
  ```go
  type ValidationError struct { Code string `json:"code"`; Path string `json:"path"`; Field string `json:"field"`; Message string `json:"message"`; Value any `json:"value,omitempty"` }
  ```
  Modules alias it (`auto-reflect/internal/rules/model.go:79`).
- `auto-shared/config/projects.go:254-300` `ValidateProjects`: JSONPath-style `Path`, `invalid_*` / `duplicate_*` codes, returns `[]ValidationError{}` and never nil.
- `auto-skill/internal/cli/root.go:192-244` `newLintCmd` (`auto skill lint`): the closest precedent. It prints JSON by default, prints text with `--text`, and returns `&ExitError{Code: 1}` on errors. `auto-skill/internal/skill/skill.go:325-332` `Diagnostic` = ValidationError + `Severity`.
- `auto-reflect/internal/cli/doctor.go:20-51`: `{check, status pass|warn|fail, message, hint}`, fixed order, exit 1 on any fail.
- Anti-pattern: `auto-doc/internal/cli/root.go:153,313` calls `os.Exit(1)` directly and bypasses `ExitError`.
- `.claude/skills/rich-doc/scripts/pd-lint.mjs` (minified v0.9.0) has the codes `unplanned-file`, `untracked-file`, `missing-dep`, `dependency-cycle` (3-colour DFS, path joined with " → ") and `open-question`. Output: `{file, ok, issueCount, issues:[{code,message}]}`, exit 0/1, and 2 on a usage error.

### Deterministic JSON
- `auto-shared/config/jsonfile.go:27-38` `DecodeJSONFileStrict` (DisallowUnknownFields); `:42-56` `WriteJSONFile` (2-space indent + trailing newline); `:66-98` `WriteJSONFileAtomic` (temp file + rename, but no read-modify-write lock).
- `MarshalIndent` HTML-escapes `<>&`. Readable output needs an Encoder with `SetEscapeHTML(false)` (`auto-graph/internal/contextpack/json.go:15`).
- Struct fields serialise in declaration order. Slices must be sorted explicitly (`auto-graph/internal/codegraph/build.go:164`).
- `auto-shared/lock/store.go:148` `withLock` is a flock pattern for serialising read-modify-write.

### IDs
- No Go code allocates sequential `NNN` folders. That rule exists only as prose in `.claude/skills/new-task/SKILL.md:24` and `.claude/skills/new-epic/SKILL.md:25`.
- `auto-reflect/internal/observations/model.go:47`: task-folder regex `^[0-9]{3}-[a-z0-9]+(?:-[a-z0-9]+)*$`.
- `auto-shared/config/projects.go:58-76` `SlugifyID` produces kebab-case.
- `auto-reflect/internal/cli/output.go:15-21` `mutationResult(id, fields)` (and `:27-37` `mutationResultIDs`): every mutation result carries a top-level `id` / `ids`.
- `docs/plans/` does not exist. `docs/epics/` mixes the naming styles `001-*.md`, `003-*/` and `epic-004-*.html`.

### Graph code to mirror (all under `internal/`, so it can't be imported)
- `auto-graph/internal/graph/model.go:5-41`: typed `NodeKind` / `EdgeKind` constants, `Node{ID, Kind, …, Attrs}`, `Edge{Source, Target, Kind, Attrs}`, `Graph{Nodes, Edges}`.
- `auto-graph/internal/contextpack/builder.go:623-696` `computeSCCs`: deterministic Tarjan SCC (tests at `builder_test.go:370,565`). No topological sort exists anywhere in the repo.

### Text rendering and format flags
- `auto-doc/internal/commands/tree.go:86-115`: `├── ` / `└── ` + `│   ` prefix renderer (duplicated in `stale.go:121-127`).
- `auto-search/internal/sessionoutline/render_text.go:13-70`: 2-space indented text whose doc comment states that "text carries the same information as JSON".
- No `+ - ~` gutter renderer exists.
- Two flag styles: `--format json|text` via `normalizeFormat` (`auto-reflect/internal/cli/root.go:93-102`), and the `--text`/`--json` booleans in auto-skill. `docs/auto-package-patterns.md:296-312` prescribes `--text`.

### Glossary
- `docs/concepts/UBIQUITOUS_LANGUAGE.md`: auto-doc frontmatter (`hash`, and `summary` lists every term); generated ER block at `:12`; sections run through Locks at `:172`.
- Lines to narrow per Q-1: `:98` Segment `_Avoid_: Phase, …`; `:108` Observation `_Avoid_: Finding, …`; `:123` TaskDef `_Avoid_: Task, …, step`.
- Further collisions:
  - `:84` Host avoids `node`, so "Node" must not become a canonical glossary term.
  - `:138-140` **Context Pack** is canonical (auto-graph's file bundle) and avoids `Pack`, so a per-stage agent bundle needs a different name.
- `.claude/skills/domain-modelling/scripts/glossary.py check` (`:104-171`) rejects a canonical term that appears on another term's `_Avoid_` list (exact lowercase match). `diagram --write` (`:205-235`) regenerates the ER block. It passes today with 24 terms, but no CI step runs it.
- `.claude/skills/domain-modelling/references/language-format.md`: the entry format, in which `_Has_` holds only `one|many <term>`.

### Tests
- `auto-mail/internal/cli/cli_test.go:25-45` `runCLI`: builds the root command in-process with a temp `cwd`, maps `*ExitError` to a code, and uses `t.Setenv("HOME", t.TempDir())`.
- `auto-graph/e2e/e2e_test.go:57`: builds the binary, then drives it with `exec.Command`.
- `.gitignore:11` `**/testdata/` ignores fixtures, so add `!auto-plan/<path>/testdata/` + `!…/testdata/**` (precedent at `:14-15`).

### Shared helpers (stdlib-only rule)
- `auto-shared/rpc/layering_test.go:18-73` `TestLayeringGate`: `auto-shared/go.mod` must have no `require`, so any new shared helper must use only the standard library.
- `auto-shared/git/detect.go:13` `RepoRoot(dir)` locates the repo root, and from it `docs/plans`.

## Patterns
- Data commands print JSON on stdout by default, send diagnostics and remediation hints to stderr, and provide `--text` for human-readable output (`CLAUDE.md:18-55`, `docs/auto-package-patterns.md:296-312`).
- One shared `validate()` returns `[]ValidationError`. Listing and lint commands return every valid result and still exit non-zero when anything is invalid.
- Resource verbs: `list` / `search` return IDs + metadata, `describe` returns a summary, and `get` returns full fidelity. Truncated output prints the exact command that recovers the full text (`docs/auto-package-patterns.md:239-294`).
- Baseline subcommands are `init`, `doctor`, `quickstart`, `docs` and `update` (`docs/auto-package-patterns.md:173-237`).
- A vocabulary change lands in the glossary before any code uses it (precedent: task 062 AC-1). Run `auto doc fixed <file>` after editing a doc; the pre-commit `autodoc-fix` fails on drift (`Makefile:297-311`).

## Related Tasks
- **062 auto-mail walking skeleton:** the module scaffold, the glossary-first rule, a conformance-suite pattern, and a harness scenario. Its plan.html is one of the two plan-lab subjects.
- **052 reflect-tool hardening:** the `mutationResult` top-level id, `doctor` checks, and content-hash ids (`idhash`). It is the brownfield plan-lab subject.
- **Plan-lab (uncommitted, `docs/plan-lab-00-index.html` + 21 experiments):**
  - It found six plan defects a linter could catch: diagram vs AC contradictions, `phases=` vs Success Criteria drift on 9 ACs, and empty `tests=`.
  - Batch-2 model JSON (scratchpad `model-062.json` / `model-052.json`) is the prototype schema. It surfaced these gaps: symbols need `introducedIn`, forbidden edges were free text, stale G-ids leaked into rail text, and one edge was bogus.
- **Show-me session** `e873ddac-46e6-47dd-b70e-ec5b6e007376`: proposed a form-selection table (pseudocode / call tree / file tree / shape diff) and a prose-budget lint. This supports the `tree` node.
- `docs/research/better-planning-autonomy.md:35-96`: a schema validator plus cross-artifact consistency checks as the "Analyze" gate.
- `docs/research/turtle-spike-findings.md:148-275`: link constraints to tests (`verifies`); the queries "constraint with no test" / "test with no constraint"; `dependsOn+` cycle detection caught a planted bug.

## History and Lessons (from git + feedback.md)
- **How new modules have landed:** auto-mail `0c73e78` (#146) and auto-artifact `63a3df3` (#114)
  each landed as one squashed PR. Each touched `go.work`, `Makefile` `PROJECTS`, root `CLAUDE.md`,
  `auto-cli/cmd/auto/main.go` (+2 lines), and `auto-cli/go.mod`/`go.sum`. auto-artifact also
  updated `main_test.go`; auto-mail did not.
- **The glossary lands inside the feature commit:** 062 changed the frontmatter `hash` and
  `summary`, regenerated the ER block with `glossary.py diagram --write`, and appended a `## Mail`
  section. 061 feedback: the only rebase conflict was that `summary` line, and the mermaid block
  must never be hand-edited.
- **051 feedback:**
  - Add the require + replace to `auto-cli/go.mod` by hand. `go mod tidy` there upgraded
    unrelated dependencies.
  - `golangci-lint cache clean` clears pollution from a sibling worktree.
- **061 feedback:**
  - `make check` takes about 5 minutes and `make test` about 15, so raise Bash timeouts.
  - Read the actual `--text` output before calling a renderer done; label bugs were invisible to
    unit tests.
- **064 feedback:**
  - Put the Go 1.26 toolchain's `bin` first on `PATH` for gofmt and lint. The 1.27 snap mismatches
    CI.
  - Pass PR bodies with `--body-file`.
- **052 feedback:** pin environment variables in e2e tests. An env-derived `omitempty` field passed
  locally and failed in CI.
- **063 feedback:**
  - Re-verify citations with `git show <sha>:<path>` before execution.
  - Tell each stage agent explicitly when its stage is closed.
- **pd-lint:** `.claude/skills/{planning-doc,rich-doc}/scripts/pd-lint.mjs` is a vendored
  minified bundle (pd-components v0.9.0, from mistakenot/skills; commits `4f61a27`, `fa64b71`,
  `ea3d11a`). No pd-lint logic was ever written in this repo, so parity means matching that
  bundle's codes and meanings.
- **golangci-lint (`.golangci.yml`, v2.12.2):**
  - Rules likely to bite new code: errchkjson (check `Encode`/`Marshal` errors), unparam,
    perfsprint, modernize/intrange, errorlint, gocritic diagnostics.
  - `_test.go` and `e2e/` get relaxed errcheck and gosec.
  - `make check` = `fmt-check vet lint stale-refs`, run per module over `PROJECTS`.
