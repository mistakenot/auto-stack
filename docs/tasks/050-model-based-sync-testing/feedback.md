# Feedback: Task 050

## Problems faced
1. **The fix wasn't on main yet (and then it was).** The `--target` fix lived in open
   PR #112, not `origin/main`, so the test had to be authored against the *buggy*
   pipeline. Mid-task #112 merged — and its final merged form had grown a manifest fix
   that didn't exist in the commit I'd cherry-picked. Main moved under me; I rebased and
   dropped my now-redundant production fix.
2. **The fix changes `planPrune`'s signature.** A white-box test calling internal
   `planPrune`/`ScanOwnership` would not compile against both the buggy and fixed
   pipeline. Had to keep the test strictly black-box (only `sync.Run` + filesystem reads)
   so the same file runs on both.
3. **The harness found a *second* bug.** A scoped `sync --target X` replaced
   `manifest.json` with only the scoped skill, so every other skill became "foreign" and
   all later syncs (scoped or full) failed with foreign-dir collisions — convergence
   broken. PR #112's *first* form fixed only the prune facet; its final form fixed the
   manifest facet too. This is exactly the pipeline-interaction class the task targets.
4. **A model false-positive that was actually real behaviour.** First state-machine run
   failed because authored `./skills/**` skills bypass `--target` scope (`discoverAuthored`
   always runs), so a scoped sync re-renders the targeted skill *plus* every authored
   skill. The model had to account for that — it's correct behaviour, not a bug.

## Reflections
- **What was tricky:** keeping the test compilable+meaningful across two pipeline versions
  while the "correct" version was a moving target. Black-box was the unlock.
- **What I'd tell myself at the start:** check whether the prerequisite fix is actually on
  `origin/main` (it was on an open PR) and whether `main` is moving (a parallel executor
  merged #112 and #114 during the task). Re-fetch before rebasing/merging.
- **What I almost did but didn't:** ship my own `mergeScopedManifest` as part of this PR.
  Once #112 merged with an equivalent (scope-based) fix, that became redundant — rebased it
  out so the PR is purely the test harness (the task's original scope).
- **Disk during shrinking:** rapid re-runs the property many times while shrinking a
  failing case; each run builds a git fixture + temp env. Registering `rt.Cleanup` to
  eagerly remove per-sequence temp trees keeps a failing/shrinking run from accumulating
  disk.

## Useful context
- `helpers_test.go` already has everything: `newEnv`, `newFixture`, `commitSkill`,
  `lockEntry`, `writeAuthoredSkill`, `approve`, `writeLock`, `writeSkillsYAML` — all
  `package sync`, directly callable. No extraction needed.
- A local `file://` fixture repo's history is cumulative, so the final HEAD contains every
  committed skill's subtree — point all initial lock entries at one commit.
- `ResolveValues` ignores supplied replacement vars not declared in a template's
  `customize:` block, so `EditConfig` can add a benign replacement without breaking render.
- `rapid` v1.3.0 state-machine API: implement `Check(*rapid.T)` + exported action methods;
  there is no `Init` in the interface — do setup in the `rapid.Check` closure and drive with
  `t.Repeat(rapid.StateMachineActions(sm))`. Defaults: 100 checks × ~30 steps.
- Only `StateManagedOrphan` is prune-eligible; `managedUnion` is built from the manifest's
  per-target `ManagedSkills`, which is why a shrunk manifest reclassifies skills as foreign.
