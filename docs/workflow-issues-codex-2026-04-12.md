# Recent Workflow Probe
File: workflow-issues-codex-2026-04-12.md
Window analyzed: 2026-03-13 to 2026-04-12 (UTC, last 30 days)
Workspace: /home/vscode/src/auto-stack

## Issues

### Beads SQLite Contention During Issue Operations (Severity: HIGH)
- Symptom: `br` operations intermittently fail with WAL salt mismatch logs and `database is busy` errors during active workflows.
- Times seen: 5 runtime incidents were found after filtering strict `tool` outputs for the exact busy-error signature.
- First seen: 2026-03-21T11:56:28Z in message `6c71f534-8a37-4157-9ae2-cabe1ab541c9-98`.
- Most recent seen: 2026-03-22T17:23:16Z in message `ae60077a-6e73-4fa3-998e-01d40e8e8a2e-96`.
- Context check: I reviewed neighboring messages `6c71f534-...-97/98/99` and `ae60077a-...-95/96/97` to confirm the failures happened inside issue-close/dependency-update flows with immediate retries.
- Transferability: portable, because every sub-project in this stack uses the same Beads-backed issue flow and can hit the same SQLite contention mode.
- Search evidence:
  - `autosearch search '"Error: Database error: database is busy"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d` - This isolated explicit busy-error payloads instead of generic mentions.
  - `jq -r '.hits[] | select(.messageType=="tool") | select((.snippet|contains("Total hits:"))|not) | select((.snippet|contains("autosearch search"))|not) | .messageId' /tmp/autosearch-probe-2026-04-12/stats/db_busy_strict.search.json` - This removed meta-analysis echoes and kept runtime tool failures.
- Thought process:
  - The strict error phrase plus `tool` filtering gave high-confidence runtime failures rather than references in docs or retrospective analysis output.
  - I marked this HIGH because it blocks issue state transitions and can derail close/sync workflows mid-task.
- Representative incidents:
  - `6c71f534-8a37-4157-9ae2-cabe1ab541c9-98` - A dependency-removal sequence logs `database is busy` while partially succeeding, leaving the operation in an uncertain state.
  - `ae60077a-6e73-4fa3-998e-01d40e8e8a2e-96` - Repeated WAL mismatch errors accompany a close/skip sequence and force retry behavior.
- Recommendation:
  - Add a Beads-safe wrapper that serializes `br` mutations, applies bounded retries with backoff, and emits a deterministic remediation step when contention persists.

### Tool Read/Write Contract Violations (Severity: HIGH)
- Symptom: write/edit calls frequently fail with `tool_use_error` because files were not read first or changed between read and write.
- Times seen: 44 incidents matched `modified since read` or `File has not been read yet` in the 30-day window.
- First seen: 2026-03-20T11:43:27Z in message `d3b3f45c-2c43-41f8-ae25-7a2efbc073ab-5`.
- Most recent seen: 2026-04-12T19:10:27Z in message `1028af22-f1f9-4f69-a132-a3ab08072bcb-121`.
- Context check: I reviewed adjacent messages `d3b3f45c-...-4/5/6` and `1028af22-...-120/121/122` to confirm these failures occur directly around attempted edits and retries.
- Transferability: portable, because this is a core tool orchestration pattern that applies to any repo using the same coding-agent toolchain.
- Search evidence:
  - `autosearch search '"modified since read" OR "File has not been read yet"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d` - This captured both major variants of read/write precondition failures.
  - `autosearch message get d3b3f45c-2c43-41f8-ae25-7a2efbc073ab-5` - This verified the canonical first-seen `tool_use_error` payload.
- Thought process:
  - The two error strings are direct protocol violations, so they are precise indicators of avoidable edit-loop churn.
  - I marked this HIGH because these failures recur across sessions and add repeated non-value retries.
- Representative incidents:
  - `d3b3f45c-2c43-41f8-ae25-7a2efbc073ab-5` - The tool rejects a write attempt with `File has not been read yet`.
  - `1028af22-f1f9-4f69-a132-a3ab08072bcb-121` - The tool rejects a write with `File has been modified since read`, forcing re-read and replay.
- Recommendation:
  - Enforce a standard edit loop that always re-reads immediately before write when any formatter/linter or concurrent process might touch the target file.

### Install Pipeline Collision: `Text file busy` (Severity: MEDIUM)
- Symptom: `make install` intermittently fails when copying `autowatch` into a running target path.
- Times seen: 5 incidents matched the exact `Text file busy` signature.
- First seen: 2026-03-22T16:40:49Z in message `17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-649`.
- Most recent seen: 2026-04-12T19:44:53Z in message `1028af22-f1f9-4f69-a132-a3ab08072bcb-202`.
- Context check: I reviewed neighboring messages `17c23bc9-...-648/649/650` and `1028af22-...-201/202/203` to confirm this appears during install and then triggers manual mitigation or Makefile hardening.
- Transferability: portable, because any long-running daemon binary in-place install can hit the same busy-file failure mode.
- Search evidence:
  - `autosearch search '"Text file busy"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d` - This isolated install-time file-lock collisions.
  - `autosearch message get 17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-649` - This validated the concrete `cp ... Text file busy` failure path inside `make install`.
- Thought process:
  - The error is narrow and operationally clear, which makes the issue highly actionable.
  - I marked this MEDIUM because it is disruptive but already has evidence of recent mitigation work in Makefile diffs.
- Representative incidents:
  - `17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-649` - `cp` fails for `/home/vscode/.local/bin/autowatch` and aborts `make install` with exit code 2.
  - `1028af22-f1f9-4f69-a132-a3ab08072bcb-202` - A follow-up Makefile patch shows targeted handling for the busy-file case.
- Recommendation:
  - Keep installs atomic per binary and make busy-target handling explicit so one daemon collision does not fail the entire install batch.

### Go Compile Churn From Unused Imports and Runtime Panic Loops (Severity: MEDIUM)
- Symptom: build/test iterations repeatedly fail on unused imports and panic traces before converging.
- Times seen: 20 incidents matched `"imported and not used" OR "panic: runtime error"` in this workspace window.
- First seen: 2026-03-20T22:01:47Z in message `88943e4d-fa33-444d-9c32-4fe269718550-249`.
- Most recent seen: 2026-03-30T09:56:18Z in message `9e8c7cca-0ce9-474e-be58-a396bbaa10cf-103`.
- Context check: I reviewed adjacent messages `88943e4d-...-248/249/250` and `9e8c7cca-...-102/103/104` to verify these were active fix loops rather than static references.
- Transferability: portable, because this pattern follows common Go edit/build loops across multiple sub-project modules.
- Search evidence:
  - `autosearch search '"imported and not used" OR "panic: runtime error"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d` - This focused on high-signal compile/test failure classes.
  - `autosearch message get 88943e4d-fa33-444d-9c32-4fe269718550-249` - This confirmed a representative unused-import compile break in a real test run.
- Thought process:
  - These signatures directly indicate failed feedback cycles that should be caught earlier with tighter build discipline.
  - I marked this MEDIUM because impact is mostly iteration cost rather than irreversible workflow blockage.
- Representative incidents:
  - `88943e4d-fa33-444d-9c32-4fe269718550-249` - `go test` fails in `auto-etl` due to an imported-but-unused package in `transform_test.go`.
  - `9e8c7cca-0ce9-474e-be58-a396bbaa10cf-103` - Build fails in `autodoc` config code due to unused `fmt` import.
- Recommendation:
  - Enforce per-file/per-module compile checks immediately after each Go edit and auto-run import cleanup before the next action.

## User Anti-Signals
- The explicit correction query `autosearch search '"didn''t ask" OR "not what I asked" OR "no this is wrong" OR "undo that"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d` returned 13 hits, including direct rollback language in `51f5dc53-...-40`.
- The broad why-query `autosearch search '"why" OR "why did" OR "why are" OR "why would" OR "why didn''t"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d` is capped at 50 hits, with 10 user-message hits in the returned set that were mostly neutral clarification and at least one direct challenge (`44325cd4-...-108` context was explanatory, not corrective).

## Proposed New Skills
1. `beads-lock-recovery`: Detect Beads WAL/busy contention, serialize mutating `br` commands, retry with bounded backoff, and emit exact remediation output when lock pressure persists.
2. `safe-edit-loop`: Force `read -> edit -> verify -> reread-on-change` sequencing so `tool_use_error` preconditions are handled proactively instead of reactively.
3. `daemon-safe-install`: Install binaries with per-target atomic copy semantics, busy-binary detection, and optional stop/install/start hooks for known daemons like `autowatch`.
4. `go-compile-guard`: After every Go-file modification, run scoped `go build`/`go test` checks plus import-cleanup checks to collapse compile-fix loops early.

## Commands Run
```bash
autosearch quickstart
autosearch index
autosearch search 'error OR fail OR timeout OR busy OR tool_use_error' --scope messages --cwd /home/vscode/src/auto-stack --since 30d
autosearch search 'error OR fail OR timeout OR busy OR tool_use_error' --scope sessions --cwd /home/vscode/src/auto-stack --since 30d
autosearch search '"database is busy"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d
autosearch search '"Error: Database error: database is busy"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d
autosearch search '"modified since read" OR "File has not been read yet"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d
autosearch search '"Text file busy"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d
autosearch search '"imported and not used" OR "panic: runtime error"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d
autosearch search '"didn''t ask" OR "not what I asked" OR "no this is wrong" OR "undo that"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d
autosearch search '"why" OR "why did" OR "why are" OR "why would" OR "why didn''t"' --scope messages --cwd /home/vscode/src/auto-stack --since 30d
autosearch message describe <message_id>
autosearch message get <message_id>
```
