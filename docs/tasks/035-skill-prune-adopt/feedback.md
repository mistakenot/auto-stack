# Feedback: Task 035 (skill-prune-adopt / epic-004 T5)

## Problems faced
1. **The plan was written against unmerged dependencies (T1/T2/T3/T4).** It explicitly said "confirm seams at execution time." By execution, T4 (034) was merged, so the real seam shapes had to be read off disk before any code — the planned `schema.Manifest`/`schema.Receipts` names did not exist. Mitigation: a single authoritative "seam brief" was written up front from the merged code and handed to every phase subagent, keeping contracts consistent.
2. **Import cycle risk.** `Receipts` and `journalPrune` live in `internal/sync`, but the sync prune pass needs `internal/ownership`. The classifier had to be a true pure leaf — receipts passed in as a plain `map[string]map[string]string`, digests passed in, importing only `internal/skill`. Getting this contract right in phase 1 was load-bearing for every later phase.
3. **Receipts key by target STYLE name, not absolute path** (contrary to the design's worktree-rule wording). The deletion authority is correct because the receipts *file* is per-checkout (`<path-hash projectID>.json`). The classifier scan keys and receipt keys both had to be style names; a path-keyed scan would have silently made nothing prune-eligible (AC-1 would pass vacuously).
4. **T4's merged `doctor` was config-only** — no drift compare, despite the plan's assumption it shipped one. T5 added the entire offline ownership/drift section, not just orphan/foreign lists.
5. **The renamed-upstream signal didn't exist.** `git archive` surfaced only `exit status 128` for a missing subpath, so a `cache.ErrSubpathNotFound` sentinel + `RenamedUpstreamError` type had to be added to make the rename detectable and reportable.

## Reflections
- **What was tricky:** wiring the prune into T4's journaled commit without breaking crash-consistency. Pruned receipt entries must be dropped from the embedded journal receipts *before* `writeJournal`, so recovery re-writes correct bytes; rollback must restore pruned dirs from trash exactly like writes.
- **What I'd tell myself at the start:** read the *merged* seam files exhaustively before delegating anything — the plan's symbol names are aspirational. The hour spent mapping seams up front paid for itself six times over.
- **What I almost did but didn't:** make `ownership` import `sync.Receipts` directly (clean-looking, but a cycle). Decoupling to a plain map was the right call and made the classifier trivially table-testable.

## Useful context
- `internal/sync/journal.go` is the crash-consistency heart: `commit` order (stage→swap→receipts→manifest→lock→clear), fault points, `recoverJournal`/`rollForward`/`rollBack`. The prune rides this; do not invent a second commit path (decision D-T5-2).
- `internal/sync/receipts.go` — receipts are the deletion authority, keyed `style → name → digest`, file per path-hashed project id.
- `render.ComputeSkillVersion` / `onDiskDigest` — the single canonicalization oracle; the in-file `metadata.auto_skill` stamp is excluded from the digest, so a forged stamp authorizes nothing.
- `remove` reconciles by simply calling `sync.Run(env, {Locked:true})` after dropping the source of truth — the now-undesired skill becomes an orphan and is pruned under the same receipt-gated, journaled authority. Reuse over reimplementation.
- Tests isolate via `--root <tmpdir>` (RootOverride redirects receipts/cache/trust into the temp root) — no `$HOME` games needed; `git init` the temp root for adopt/remove.
