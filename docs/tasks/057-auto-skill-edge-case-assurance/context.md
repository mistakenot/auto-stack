# Context: Task 057

Codebase context for the auto-skill edge-case assurance task — see [plan.html](plan.html).

## Key Files

### Warning propagation chain

- `auto-skill/internal/sync/process.go:215` -- `gatherSources()` returns `([]*skillSource, []error)` — **no warning return path**. The `skillSource` struct (line 77) has no `Warnings` field.
- `auto-skill/internal/sync/process.go:249-255` -- Authored skills shadow vendored in `byName` map. Only output is `trace.Logf` (requires `--trace`). No warning reaches `ProcessResult.Warnings`.
- `auto-skill/internal/sync/process.go:342-381` -- `renderSource` populates `StagedSkill.Warnings` from `tree.Warnings` (line 376) and `tokenBudgetWarning` (lines 378-379).
- `auto-skill/internal/sync/process.go:154-160` -- `Process()` fans `st.Warnings` from each `StagedSkill` into `ProcessResult.Warnings` at line 159.
- `auto-skill/internal/sync/sync.go:268` -- `result.Warnings = append(result.Warnings, proc.Warnings...)` — the single line bridging `ProcessResult` → `Result.Warnings`.

### Ownership decision table

- `auto-skill/internal/ownership/classify.go` -- Pure `classifyOne(managed, desired, hasReceipt bool, onDisk, receiptDigest string) State` with 5 states: `StateManagedCurrent`, `StateManagedOrphan`, `StateManagedUnestablished`, `StateModified`, `StateForeign`.
- `auto-skill/internal/ownership/classify_test.go:27` -- Table-driven `TestClassify_StateMatrix` with 9 rows. Asserts state, target, name, digests.
- `auto-skill/internal/sync/prune.go` -- `DesiredSet()` builds authored ∪ vendored names; `ScanOwnership()` builds `ownership.Inputs`; `planPrune()` scope-gates orphan deletion; `detectForeignCollisions()` flags desired/foreign name collisions.

### Remove reconcile

- `auto-skill/internal/sync/remove.go` -- `Remove(env, name, sel)` with `SelUnset`, `SelLocal`, `SelVendored`. Drops source of truth, then re-runs `sync.Run()` to prune orphans. `survivingTargetCopies()` computes surviving copies → `RemoveResult.Reported`.
- `auto-skill/internal/sync/remove_test.go` -- 5 tests: `TestRemoveLocal`, `TestRemoveVendored`, `TestRemoveAmbiguous`, `TestRemoveNotFound`, `TestRemoveReportsModifiedCopy`. Asserts `Removed`, `Errors`, `Pruned`, `Reported` fields.

### Existing test infrastructure

- `auto-skill/internal/sync/helpers_test.go` -- `newFixture(t)` (real git repo), `newEnv(t)` (isolated `skill.Env`), `approve(t, env, url)` (trust pre-approval), `writeLock()`, `writeSkillsYAML()`.
- `auto-skill/internal/sync/process_test.go:14` -- `writeAuthoredSkill(t, env, name, body)` writes `./skills/<name>/SKILL.md`.
- `auto-skill/internal/sync/sync_statemachine_test.go` -- 507 lines, 9 operations, `rapid.StateMachineActions`, bidirectional disk-presence invariant. **No warning/diagnostic assertions.**
- `auto-skill/internal/sync/prune_test.go:108` -- `contains(items []string, want string)` helper wrapping `slices.Contains`.

### Warning assertion patterns (existing)

- `auto-skill/internal/sync/sync_test.go:219-237` -- `TestRunBudgetWarnsExitsZero`: inline `for` loop + `strings.Contains(w, "advisory budget")`. Asserts exit 0 + no errors + warning present.
- `auto-skill/internal/sync/process_test.go:369-384` -- Same pattern at process level. **No `containsWarning` helper exists anywhere.**

## Patterns

- **Warning propagation**: `StagedSkill.Warnings` → `ProcessResult.Warnings` (line 159) → `Result.Warnings` (line 268). The plumbing exists end-to-end; only `gatherSources` doesn't feed into it.
- **Warning assertion style**: inline `for` loop scanning `res.Warnings` with `strings.Contains`. No shared helper.
- **Edge case test style**: named `Test<Op><Scenario>` functions using `newEnv(t)` + seed `Run()` + operation + field assertions on result struct.
- **Property testing**: `pgregory.net/rapid` v1.3.0, used in `sync_statemachine_test.go` and 3 prop test files.
- **`Result` struct fields**: `Warnings []string`, `Errors []string`, `Conflicts []Conflict`, `Stale []StaleItem`, `Pruned []string`. Exit code derived from errors (exit 0 when only warnings).

## Related Tasks

- **048-auto-skill-property-tests** (complete, `09598ae`) -- Added PBT infrastructure (`rapid` v1.3.0, lock round-trip, render determinism, URL canonicalization). Direct predecessor; task 057 extends this with ownership properties.
- **050-model-based-sync-testing** (complete, `44272e3`) -- Added `sync_statemachine_test.go` with 9 operations + disk-presence invariant. Found the `--target` prune bug. Direct predecessor; task 057 extends the state machine with diagnostic model.
- **035-skill-prune-adopt** (complete, `7952f81`) -- Introduced `ownership/classify.go`, `remove.go`, `prune.go`. The code this task targets for edge-case coverage.
- **056-auto-skill-sync-perf** (in-flight/queued) -- May touch `process.go`; if it lands first, rebase needed.

## Recent Commits

- `7704969` feat(skill-trace): `--trace` timing instrumentation — modified `process.go`; plan accounts for current `gatherSources` shape with tracing calls.
- `b7a72fb` fix(skill-sync): scope orphan pruning to `--target` set — the bug the state machine caught.
