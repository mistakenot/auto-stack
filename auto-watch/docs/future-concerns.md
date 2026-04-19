---
hash: "58faed4b"
id: "8b9036fd"
read_when: "when planning autowatch demo or production deployment"
summary: "Deferred architectural concerns for auto-watch covering SQLite migration strategy, task output capture, and multi-instance safety, each with a trigger stage and suggested fix."
title: "Future Concerns"
---

# Future Concerns

Current stage: **prototype**  
Next stage: **demo**

Scope reviewed: `auto-watch/docs/solution.md`

1. **ID:** fut-sol-001
   **File:** `auto-watch/docs/solution.md`
   **Doc Type:** solution
   **Identity:** systems_architect
   **Priority:** medium
   **Concern:** The solution omits a SQLite schema migration/versioning strategy for future releases.
   **Gap analysis:** Tables are declared with `CREATE TABLE IF NOT EXISTS`, but there is no plan for additive/renaming migrations once v1 ships.
   **Why not now:** Prototype can tolerate reset/rebuild workflows; demo environments need non-destructive upgrades.
   **Trigger stage:** demo
   **Suggested fix:** Add a schema version table plus forward-only migrations executed at startup, with clear rollback/failure behavior.

2. **ID:** fut-sol-002
   **File:** `auto-watch/docs/solution.md`
   **Doc Type:** solution
   **Identity:** systems_architect
   **Priority:** medium
   **Concern:** There is no task output capture contract, limiting observability beyond exit codes and event metadata.
   **Gap analysis:** The plan stores run state and logs but defines no retained artifact path for agent outputs or key diagnostics.
   **Why not now:** Prototype operators can inspect tmux manually; demo users need reproducible post-run evidence without shell access.
   **Trigger stage:** demo
   **Suggested fix:** Define optional output capture (path, retention, truncation rules) linked from each run record.

3. **ID:** fut-sol-003
   **File:** `auto-watch/docs/solution.md`
   **Doc Type:** solution
   **Identity:** systems_architect
   **Priority:** high
   **Concern:** Multi-instance operation is undefined while dedup/state assumes a single daemon loop.
   **Gap analysis:** The architecture has no distributed lock/leader mechanism, so parallel daemons can duplicate task launches.
   **Why not now:** Single-host prototype use avoids contention; production-like deployments commonly run redundant watchers.
   **Trigger stage:** production
   **Suggested fix:** Add an explicit single-writer constraint or implement DB-backed leasing/leader election before multi-host deployment.
