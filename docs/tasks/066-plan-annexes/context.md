# Context: Task 066 — Plan annexes for auto plan

Codebase grounding for adding **annexes** (Markdown files registered as graph nodes),
an `ac.layer` enum, annex lint rules, and freeze-time content hashing to `auto plan`.
See [plan.html](plan.html) for Requirements / Verification / Solution / Plan.

## Key Files

### Registry — the single extension point (D-6)
- `auto-plan/internal/schema/registry.go:143-163` — `NodeType{Name, Prefix, Term, Fields, MinLifecycle, RankScope, Help}`. `Prefix==""` ⇒ singleton (`Singleton()` at `:166`); a non-empty prefix generates IDs `prefix-xxxx`.
- `registry.go:124-141` — `FieldSpec{Name, Kind, Required, Enum, Pattern, Help, Fixed, Fields, PlanRef}`. **`Fixed` is set at create and refused by `update`** (`:131-133`) — NOT a fit for a freeze-computed hash (hash is unknown at create). There is **no "computed/system" field kind** today.
- `registry.go:106-120` — `FieldKind`: `KindString|KindText|KindEnum|KindList|KindObject`. No path/bool/ref kind (path = string + `Pattern`; plan-ref = `PlanRef` flag).
- `registry.go:311-327` — the `ac` node: fields `title` (req string), `gwt` (req text), `verify` (object: `cmd`, `tests[]`, `kind` enum `{command,manual}`). `Prefix:"ac"`, `MinLifecycle: LifecycleSolution`, `RankScope:"proves"`. **No `layer` field yet.**
- `registry.go:428-444` — edge table. **`about` is `From:["tree"] To:["*"]`** (`:439`, Help "The node this tree illustrates"). Enum example `plan.kind` at `:297`; `file.path` uses `RepoPathPattern` (`:271-275`, `:378`).
- `registry.go:180` — `AnyType = "*"`; comment already says "`about` is tree → any".

### Validation — write-time gate (generic, registry-driven)
- `auto-plan/internal/graph/validate.go:46` — `Validate(g) []ValidationError`; node loop `:85-99`, `node()→fields()→value()→scalar()` (`:137/:177/:197/:236`).
- `validate.go:90-102` — **type-specific precedent**: it counts `plan` nodes and reports `CodeMissingPlanNode` if zero. An "at most one annex per kind" check fits the same shape.
- `validate.go:246-248` — enum violation ⇒ `CodeInvalidField` "must be one of a|b". Codes at `:16-29`. Error shape `{Code,Path,Field,Message,Value}`.

### Lint — advisory, lifecycle-gated (D-10)
- `auto-plan/internal/lint/lint.go:50-55` — `Rule{Code, Severity, MinLifecycle, Check func(*Context) []Issue}`. `Context` at `:58-77` holds `Graph, Lifecycle, Set *workspace.PlanSet, Number, Folder` + helpers. **Rules never read disk today** — there is no `fs.FS` / file handle on `Context`.
- `auto-plan/internal/lint/rules.go:45-61` — the `Rules` table + codes: `open-question, ac-no-goal, dangling-prose-ref, ambiguous-ref, tree-syntax, dependency-cycle, goal-no-ac, ac-no-verify, unplanned-file, untracked-file, missing-dep, …`. Patterns to copy: `openQuestion`/`unplannedFile` (`:96-109`,`:402-421`) for node iteration; `danglingProseRef` + `proseRefs` helper (`:302-336`,`:259-278`) for `[[id]]` resolution (uses `graph.FindProseRefs`). `termOf`/`pluralTerm` at `:509-518`.
- `auto-plan/internal/lint/set.go:16-19` — `SetRules` (needs the PlanSet) for cross-plan codes `duplicate-plan-number|duplicate-plan-id`.
- `lint_test.go:749` — `TestRuleLifecyclesAreRegistrySteps` asserts every rule's `MinLifecycle` is a real registry step.

### Prose refs
- `auto-plan/internal/graph/prose.go:23` — `FindProseRefs(text string) []ProseRef`; `ProseRef{Start,End,Ref}` (`:11-16`). Skips inline code + fenced blocks (`codeSpans()` `:74-101`). `ReplaceProseRefs(text, fn)` at `:38-55` (the rewrite hook renumber uses).

### Renumber
- `auto-plan/internal/cli/renumber.go:77` — `runRenumber`; rewrites refs via `graph.RewritePlanRefs` (`rewrite.go:24` → `rewriteProse` → `ReplaceProseRefs`), stages temp graph.json files, **renames the folder last** (`os.Rename` at `:291`). It **only touches `graph.json`**. A folder rename moves any sidecar `.md` automatically ⇒ **folder-relative annex paths need no rewrite**. Cross-plan `[[NNN:id]]` refs *inside* annex markdown are NOT rewritten today.

### Freeze / lifecycle
- `auto-plan/internal/cli/write.go:445` — `runUpdate`; the `--lifecycle done` write is the last allowed write. Save at `:491-500`.
- `auto-plan/internal/graph/model.go:159-168` — `Frozen()` returns a reason when `lifecycle==done` (or other-major); enforced at `root.go:285-287` (`loadForWrite`) ⇒ code `frozen`. **No freeze-time side effects exist** — the hash hook is new.
- **No hashing anywhere in the repo** (no sha*/md5/fnv/crypto import). IDs are random Crockford base32 (`ids.go:13-62`), not hashes. A hashing helper must be introduced.

### CLI / workspace
- `auto-plan/internal/cli/write.go:50-109` — `typeFlags(nt, mode)` generates one flag per field + one per outgoing edge, entirely from the registry (D-7). Text flags accept `@file`/`@-` (`root.go:337`). A new node type gets flags with **zero CLI code** (`cli_integration_test.go:489 TestThrowawayTypeGetsFlagsWithoutCLIChanges`).
- `auto-plan/internal/cli/new.go:148-158` & `scaffold.go:42-75` — the only precedents for writing non-graph.json files (`os.Mkdir`, `os.WriteFile` for AGENTS.md/CLAUDE.md, **create-only-when-missing**). **No precedent for a command writing a content sidecar** — the annex stub write is new, modeled on scaffold's idempotent write.
- `auto-plan/internal/workspace/workspace.go` — `GraphPath(p)` `:111`, `Abs(rel)` `:108`, `Plan.Dir/Folder()` `:79-84`, `PlansDir=".auto/plan/plans"` `:24`, `GraphFile="graph.json"` `:27`. `Plans()` `:116-139` scans only `NNN-name` folders and reads only `graph.json`. **Nothing enumerates a plan folder's other files** — `unlinked-file` needs a new per-folder `ReadDir` of `*.md`. Note: AGENTS.md/CLAUDE.md live in `plans/` (the parent), NOT inside each `NNN-name/` folder, so a plan folder's only `.md` files are annexes.

## Patterns
- **Facts in the graph, exposition in text** (new rule for this task; mirrors D-5 "graph is generic nodes+edges"). Promote any annex-prose value that something computes from into a node/field.
- **Registry is the only extension point** (D-6): a new node type / enum member / edge endpoint is a registry edit; decode, validate, flags, help, docs all follow. Generic mechanisms (e.g. a `UniqueBy`/`Computed` FieldSpec flag) are preferred over hardcoded per-type branches, matching the plan-singleton being the *only* current special case.
- **Idempotent create-when-missing** for generated files (scaffold.go) — reuse for the annex stub.
- **Lint resolves `[[id]]` via `FindProseRefs`** so examples in code spans don't false-positive; renumber rewrites through the *same* function so "what lint checks is what renumber rewrites."

## Test surface
- Registry pin-tests to update when adding a type/field: `registry_test.go` — `TestDesignNodeTypes:79` (node count + required-field sets), `TestDesignEdgeEndpoints:118`, `TestPrefixesUnique:13`, `TestFieldSpecsWellFormed:186`, `TestEveryTypeHasGlossaryTerm:237` (**requires `\n**Annex**:\n` in the glossary or the build fails**).
- CLI coverage auto-exercises a new type: `TestEveryTypeGetsGeneratedFlags:449`, `TestDocsCoversEveryRegisteredType:1263`.
- Golden/snapshot: `internal/render/render_test.go:19` (`-update`); e2e `e2e/e2e_test.go` scenarios `lifecycle` & `dogfood` in `e2e/testdata/scenarios/<name>/commands.txt`, `# checkpoint` diffs every `graph.json` byte-for-byte. **e2e currently diffs only graph.json — a sidecar `.md` needs harness support to be asserted.**
- Glossary tooling: `.claude/skills/domain-modelling/scripts/glossary.py check <glossary>` (exit 1 on format errors) and `… diagram <glossary> --write` (regenerates the mermaid ER block; also add "Annex" to the frontmatter `summary:` by hand).

## Related Tasks
- **Task 065 (auto-plan-graph)** `docs/tasks/065-auto-plan-graph/plan.html` — built `auto plan`. Load-bearing decisions: D-5 (generic nodes+edges), D-6 (registry = one Go table), D-7 (flags generated per-type), **D-9 (plan-level relationships are nodes+edges, NOT metadata arrays — the precedent that makes an annex a node)**, D-10 (lint gated by lifecycle), D-17 (semver + freeze-not-migrate), D-18 (lifecycle sequence in the registry).
- **Dogfood fixture** `.auto/plan/plans/001-auto-plan-graph/graph.json` — 160 nodes / 266 edges, `lifecycle:"done"` (**frozen**), ACs carry `verify{cmd,tests[]}`+`gwt` but no `layer`. Rebuilt by the e2e `dogfood` scenario from CLI calls — the natural place to author real annexes before the freeze.
- **Preview spike** `docs/plan-graph-lab/07-preview.html` — fakes `example`/`contract`/`testlayer` nodes in a `<script id="plan-extras">` block (`:3728-3840`): `example{title,kind,why,body}`, `contract{name,kind,why,definition,example}`, `testlayer{layer,title,summary,harness,expects[]}`, with `about`/`in`/`constrains` edges. The page reads a layer via `layerOf(ac)` walking an `in` edge (`:3866`). The new design replaces `testlayer` with `ac.layer` (read `N[ac].fields.layer`) and example/contract bodies with annex Markdown. AC: the preview's data can be rebuilt from real annexes + `ac.layer`.
