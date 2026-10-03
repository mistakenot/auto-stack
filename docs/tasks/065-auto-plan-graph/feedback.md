# Feedback: Task 065

## Problems faced
1. **The toolchain didn't match what the hooks expect.** The Go snap is 1.27, golangci-lint and CI expect 1.26, and the pinned go1.26.1 fails the hook's vulncheck on stdlib CVEs. Commits only passed with go1.26.6 first on `PATH`, and lint needed `GOTOOLCHAIN=go1.26.1`. Every stage prompt had to carry both recipes.
2. **The glossary check caught a collision the plan missed.** Outline's `_Avoid_: Tree` clashed with the new canonical term **Tree**. `glossary.py check` refused it, and the fix was to narrow it to "Tree (for a Session map)".
3. **Some AC wording changed meaning once implemented.** AC-9 ("a requirements plan with questions has no errors") conflicts with "open questions gate as pd-question does". AC-11's "a rail **no** child honours" was too weak: Codex found that one compliant child hid the others, so the check became per child. ACs that use *no* / *any* / *every* deserve a second read for which quantifier is meant.
4. **The dogfood found real gaps it couldn't express.**
   - An epic can't reach `lifecycle: plan` (legs have no `deferred`, and a child node for a plan that doesn't exist yet fires `child-missing`).
   - There's no goal→goal edge across plans.
   - A cross-cutting AC must pick one goal.

   These are recorded as follow-ups rather than worked around.
5. **The file format changed after the build, during PR review.** Plan IDs (`NNN-xxxx`), edge IDs, semver plus freezing, the lifecycle in the registry, and the plans root moving `docs/plans` → `.auto/plan/plans` (D-14 – D-20). All of it was cheap only because no real plan existed yet.
6. **auto-doc treats every `.md` under `docs/` as a doc,** including agent memory files. The scaffolded `AGENTS.md`/`CLAUDE.md` got flagged stale, asked for frontmatter, and had an index injected. This was moot once plans moved to `.auto/`, but the auto-doc behaviour is still worth a follow-up.
7. **The `dcg` guard matches words inside arguments.** It blocked `rm -rf` in a scratch script, and it blocked PR replies and a commit body because they contained the word "restore". Commit messages and PR replies go through `-F file`.

## Reflections
- **Most of the work was the second review.** Ask "what does this lock us into once real data exists?" *before* execution starts, not after the PR is open. The plan-identity, edge-ID and versioning decisions should have been planning questions.
- **The registry as the single extension point paid off.** Generated flags, `docs`, validation, lint and the trace table all followed every new type and field without CLI changes. The throwaway-type test enforces this.
- **Byte-identical `graph.json` snapshots were the best regression net.** Every format change showed up as a reviewable diff. Drawing the node ID before any edge IDs kept the 315-line dogfood script stable across the edge-ID change.
- **What I'd tell myself at the start:** ask for a dogfood with a *real* planning task, not just re-expressing old plans. Re-expression proves the format can say things; it doesn't prove agents can work with it (for example, the friction of capturing IDs).
- **Almost did:** hard-coded `docs/plans` permanently, and made the per-repo auto-doc ignore the "fix" for agent files. Both were reversed.

## Useful context
- auto-mail (`0c73e78`) as the scaffold template, and its `runCLI` in-process test pattern.
- `auto-graph/internal/contextpack/builder.go` `computeSCCs`, mirrored for deterministic cycle reporting.
- The vendored `pd-lint.mjs` message wording, for one-for-one parity codes.
- The decisions D-1 – D-20 in `plan.html`, especially D-14 – D-20 (from the PR review).
- **Follow-ups worth planning:**
  - `deferred` on legs, or "planned" child nodes.
  - A cross-plan goal→goal edge.
  - `show` marking superseded decisions.
  - `auto plan verify` (run AC commands; settle cwd and the expected exit code first).
  - auto-doc excluding agent memory files from discovery.
  - Migrating the planning skills onto `auto plan`.
