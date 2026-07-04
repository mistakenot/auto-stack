# Feedback: Task 058

## Problems faced
1. PR CI was red on a failure that looked unrelated to the harness work -- the failing test (`TestSyncStateMachine`, a `rapid` property-based test) lives in `auto-skill/internal/sync`, which this PR never touches. It was a real latent product bug that CI's whole-repo `make test` surfaced: `Remove(env, name, SelLocal)` deleted the `./skills/<name>/` source but left the `skills.yaml` entry dangling. A later locked reconcile with an empty lock then read that sourceless entry as a stale vendored ref and refused to prune, so `auto skill remove --local` could half-complete and error.
2. A second, genuinely flaky mutation kill-test (`TestClassifyProp_DesiredNeverOrphaned_Kill` in `internal/ownership`) intermittently fails (~1 in 5) on both this branch and `main`. It is unrelated to task 058 and is a test-quality issue (its random draw doesn't always construct a mutant-triggering scenario), not a product bug.

## Reflections
- What was tricky? Separating the real bug from the noise. The PBT failure was seed-dependent, so the reported action name (`RemoveLocal(auth-1)` vs `auth-3` vs `auth-4`) differed per run — a signal that it was a genuine property violation, not a fixed test bug. Reproducing on clean `main` confirmed it was pre-existing and not introduced by the harness promotion.
- What would you tell yourself at the start? When a PBT reddens an unrelated PR's CI, reproduce it on `main` first to classify it (real product bug vs flaky meta-test) before touching anything.
- What did you almost do but didn't? Almost merged past the red CI as "unrelated flakiness". Half of it was — but the `sync` failure was a real bug worth fixing, and fixing it unblocks `main` too, not just this PR.

## Useful context
- The fix mirrors the existing `SelVendored` branch in `remove.go`, which already calls `removeSkillsYAMLEntry`. The asymmetry between the two branches was the whole bug.
- `guardLockedPrune` in `sync.go` documents exactly which states it treats as a lost lock — an authored-only dangling `skills.yaml` entry was an unhandled false-positive case.
- The PBT model (`sync_statemachine_test.go`) mirrors product state; the fix required the model's `RemoveLocal` to also drop `cfg.Skills[name]`, matching `RemoveVendored`, so a later wholesale `writeSkillsYAML` can't resurrect the entry.
