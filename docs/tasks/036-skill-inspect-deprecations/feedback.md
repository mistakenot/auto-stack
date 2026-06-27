# Feedback: Task 036

## Problems faced
1. **Plan assumed dependencies were unmerged; they had merged.** The context.md
   and plan.html were written treating 032/033/034 as "planned-not-merged" (greps
   for `manifest`/`lock`/`render` returned zero hits *at planning time*). By
   execution they were all merged, so every consumed symbol was real on disk. The
   real signatures differed from the planned outline: the typed readers live in the
   `skill` package (`skill.ParseLock`/`ParseManifest`/`ParseSkillsYAML`, taking
   `[]byte`), not a separate loader, and the digest helper is
   `render.ComputeSkillVersion([]render.TreeFile)`. Resolution: explore the merged
   surfaces first, then adapt — the plan's `loaders.go` seam still held as the
   single point of contact.
2. **The stale-digest semantics had a load-bearing subtlety.** `render.Tree.SkillVersion`
   is computed over the *unstamped* tree, but the on-disk SKILL.md could carry a
   `metadata.auto_skill` provenance stamp — which would make any naive digest
   compare always-stale. The resolving fact is in `process.go:30-34`: the **sync
   pipeline does not stamp** (`in.Provenance` is nil), so the on-disk tree hashes
   byte-for-byte back to the manifest `skill_version`. Had to read the render
   finalisation + sync process layer to confirm the compare is exact.
3. **The `update` reclaim was handed to T6 deliberately, not just "freed".** 034
   shipped the `sync.Update` engine but left `auto skill update` bound to binary
   self-update, with an explicit comment ("the public `auto skill update` command
   name is T6's — this exposes the engine only, with no cobra command"). So T6 had
   to perform the actual swap, not merely assert an end-state.
4. **Review caught that the first `update` wiring lied.** Routing the verb at
   `sync.Update` (plan-only) reported success without applying. Codex P1/P3 flagged
   it; the fix was to route through `sync.Run(AutoUpdate:true)` — the real
   plan→fetch→render→write path — and surface `Result.Errors` via `ExitCode()`.

## Reflections
- **What was tricky?** Distinguishing what is T6's (read layer + CLI wiring +
  decisive deprecations) from what is 034's engine (the write/apply semantics).
  The `update` verb sits on that exact boundary: T6 owns the CLI surface, 034 owns
  the engine. The honest wiring is to *call* 034's apply path, not re-implement it.
- **What would I tell myself at the start?** When a plan says "planned-not-merged",
  re-verify against disk before trusting any signature — the merge may have landed.
  And for any new CLI verb that *looks* read-only, check whether it actually mutates
  (the first `update` looked fine but silently no-op'd the write).
- **What did I almost do but didn't?** Almost left `auto skill update` on the
  plan-only `sync.Update` engine (it satisfied the literal AC-9 "not self-update").
  The review was right that a verb reporting an update it didn't perform is worse
  than no verb.

## Useful context
- `auto-skill/internal/sync/targets.go` — `onDiskDigest`/`readTree`/`targetDir`
  are unexported, but `render.CanonicalTreeFile`/`ComputeSkillVersion`/`ModeFile`
  are exported, so `inspect` replicates the ~30-line read-tree+digest over the
  exported primitives and stays free of the write engine. This is the seam that let
  the stale flag be computed offline and identically to sync.
- `process.go:30-34` (no-stamp invariant) — the single comment that makes the whole
  stale design exact.
- `sync.Run` + `Result.ExitCode()` — the apply path and error→exit mapping that the
  reclaimed `update` verb delegates to.
- The white-box (`package inspect`) unit tests can call `treeDigest` to seed
  fixtures whose digests *match* the manifest, while the black-box (`cli_test`)
  e2e seeds real rendered trees via the hermetic authored `sync` path — two layers,
  both offline, no network or npx.
