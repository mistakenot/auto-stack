---
hash: "9e22e8a0"
id: "05cf0817"
summary: "High-priority coding workflow issues found via autosearch in the March 20-22, 2026 window, with evidence and exact commands run"
title: "High-Priority Workflow Issues: March 20-22, 2026"
---

# High-Priority Workflow Issues: March 20-22, 2026

Window analyzed: `2026-03-20T23:22:30Z` to `2026-03-22T23:22:30Z`.

## High-Priority Issues

### P1. Beads DB contention and WAL errors are still a major blocker

- Signal: `"database is busy" OR "WAL frame salt mismatch"`
- In-window footprint: `50` message hits (cap reached) across `5` sessions.
- Evidence:
  - `07c5ecd3-4be8-4764-9986-56ed8da1c1be-347` (`Bash`): `Database error: database is busy` with repeated `WAL frame salt mismatch`.
  - `17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-387` (`Bash`): repeated WAL mismatch while running `br close ...` sequence.
- Why high priority: it blocks issue-state operations (`br`) and creates retry churn in active implementation sessions.

### P1. Build/test breakage loops are frequent and expensive

- Signal: `"--- FAIL:" OR "FAIL\t./... [setup failed]" OR "panic: runtime error" OR "imported and not used" OR "undefined:"`
- In-window footprint: `42` message hits (from 50 returned) across `10` sessions.
- Evidence:
  - `17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-468` (`Bash`): `go build ./...` fails on multiple `undefined` symbols in `internal/daemoninstall`.
  - `07c5ecd3-4be8-4764-9986-56ed8da1c1be-288` (`Bash`): `go test ./...` fails due to unused imports in CLI integration tests.
  - `045565fa-b64d-45b1-be0e-87ce03b7f093-57` (`Bash`): repeated test failures including `panic: runtime error: invalid memory address or nil pointer dereference`.
- Why high priority: this is a direct throughput killer; sessions spend large chunks in compile-fix-retest cycles.

### P1. Write/edit workflow orchestration errors are causing avoidable retries

- Signal: `"tool_use_error" OR "File has not been read yet" OR "modified since read"`
- In-window footprint: `21` message hits across `8` sessions.
- Evidence:
  - `17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-54` (`Write`): `File has not been read yet. Read it first before writing to it.`
  - `045565fa-b64d-45b1-be0e-87ce03b7f093-103` (`Write`): same failure mode during doc generation.
- Why high priority: these are preventable process errors that waste turns and fragment progress.

### P2. Environment/tooling gaps continue to break workflows

- Signal: `"Text file busy" OR "cannot create regular file" OR "command not found" OR "Directory does not exist"`
- In-window footprint: `10` message hits across `6` sessions.
- Evidence:
  - `17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-649` (`Bash`): `make install` fails: `cannot create regular file ... autowatch: Text file busy`.
  - `17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-201` (`Bash`): `sqlite3: command not found`.
- Why high priority: toolchain/setup failures block validation and increase manual workaround effort.

## User Anti-Signal Check (Requested Follow-Up)

I ran a user-message-focused scan for frustration/correction language (`"no..."`, `"didn't ask"`, `"undo"`, `"stop"`, `"wrong"`) and questioning signals (`"why..."`).

- `no/correction` signals in-window: `0` confirmed user messages.
- `why` signals in-window: `4` user messages.
  - 3 were neutral/planning/explanatory (`when/why to use`, `why field behavior`).
  - 1 was a mild challenge to output semantics: `17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-676` (`"scores are coming back like \"-5.999\"... why minus numbers?"`).

Interpretation: in this 48-hour slice, we saw almost no explicit user frustration language, but we did see occasional user questioning of agent outputs. That questioning should be treated as a quality signal even when not overtly annoyed.

## Commands Run And Why

### 1) Rebuild index

```bash
autosearch index
```

Purpose: ensure latest sessions/messages are searchable.

### 2) Initial broad scans (to find candidate issue families)

```bash
autosearch search 'error OR fail OR timeout OR busy OR "Internal Server Error" OR rate_limit_error OR tool_use_error' --scope sessions --since 48h --cwd /home/vscode/src/auto-stack --highlight
autosearch search 'error OR fail OR timeout OR busy OR "Internal Server Error" OR rate_limit_error OR tool_use_error' --scope messages --since 48h --cwd /home/vscode/src/auto-stack --highlight
```

Purpose: map the overall failure landscape and identify dominant patterns.

### 3) Focused issue-family queries

```bash
autosearch search '"database is busy" OR "WAL frame salt mismatch"' --scope messages --cwd /home/vscode/src/auto-stack
autosearch search '"--- FAIL:" OR "FAIL\t./... [setup failed]" OR "panic: runtime error" OR "imported and not used" OR "undefined:"' --scope messages --cwd /home/vscode/src/auto-stack
autosearch search '"tool_use_error" OR "File has not been read yet" OR "modified since read"' --scope messages --cwd /home/vscode/src/auto-stack
autosearch search '"Text file busy" OR "cannot create regular file" OR "command not found" OR "Directory does not exist"' --scope messages --cwd /home/vscode/src/auto-stack
autosearch search '"no " OR "didn''t ask" OR "not what I asked" OR "stop" OR "undo" OR "that''s wrong" OR "wrong" OR "what are you doing"' --scope messages --cwd /home/vscode/src/auto-stack --highlight
autosearch search '"why" OR "why did" OR "why are" OR "why would" OR "why didn''t"' --scope messages --cwd /home/vscode/src/auto-stack --highlight
```

Purpose: quantify each high-priority category with dedicated signals.

### 4) Enforce exact 48-hour window via post-filtered session timestamps

```bash
START_MS=1774048950000  # 2026-03-20T23:22:30Z
END_MS=1774221750000    # 2026-03-22T23:22:30Z

autosearch search '<query>' --scope sessions --cwd /home/vscode/src/auto-stack \
  | jq --argjson start "$START_MS" --argjson end "$END_MS" \
    '.hits | map(select(.lastMessageAt >= $start and .firstMessageAt < $end))'
```

Purpose: strict windowing by session timestamps.

Note: while analyzing, `autosearch` date filters (`--since`, `--after`, `--before`) still returned some out-of-window sessions; I therefore used explicit epoch filtering on session metadata for the final in-window counts.

### 5) Validate incidents with message metadata

```bash
autosearch message describe 07c5ecd3-4be8-4764-9986-56ed8da1c1be-347
autosearch message describe 17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-468
autosearch message describe 17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-54
autosearch message describe 17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-649
autosearch message describe 17c23bc9-8d0e-40b7-9e17-ac363b6c77d0-676
```

Purpose: confirm command context, message type, and exact error preview for representative incidents.
