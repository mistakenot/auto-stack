---
hash: "0cdd17b6"
id: "e5d39c11"
read_when: "implementing two-stage rule discovery and promotion workflows"
summary: "Describes the cass-memory trauma workflow where regex candidates are discovered from sessions but only persisted after explicit user promotion."
title: "Trauma Candidate Promotion Pattern"
---

# Trauma Candidate Promotion Pattern

## Purpose

This note documents a safety pattern in `.tmp/cass_memory_system` that separates:

1. discovering risky session evidence, and
2. persisting durable blocking rules.

The pattern is useful for `auto-reflect` because it keeps automation high-signal but prevents silent policy writes.

## Pattern Summary

`cm` uses a two-step workflow:

1. **Discover candidates (read-only)**
2. **Promote selected candidates to persistent regex rules (write path)**

Candidate discovery does not modify `~/.cass-memory/traumas.jsonl`.

## Current Behavior In cass-memory

### 1. Discovery Phase (No Writes)

`cm audit --trauma` calls `scanForTraumas()`.

- It queries for apology/disaster language in recent sessions.
- It exports those sessions.
- It checks session text against dangerous regexes (`DOOM_PATTERNS`).
- It returns `TraumaCandidate` records for human review.

This phase is intentionally read-only.

### 2. Promotion Phase (Explicit Writes)

Persistent trauma rules are written only when the operator explicitly chooses one of these commands:

- `cm trauma add "<regex>" --severity CRITICAL --scope global`
- `cm trauma import <file> --scope global`
- `cm init` trauma scan flow, only if user confirms import

When scope is global, entries are appended as JSONL to:

`~/.cass-memory/traumas.jsonl`

The file is created lazily on first save.

## Why This Pattern Is Good

- Prevents silent, irreversible safety policy growth from noisy heuristics.
- Keeps false positives in a review queue rather than in active enforcement.
- Preserves operator intent and auditability.
- Makes incident review explicit: discovery output can be discussed before enforcement.

## Data Contract

Persisted entries follow `TraumaEntry` shape with key fields:

- `id`
- `severity` (`CRITICAL` or `FATAL`)
- `pattern` (regex string)
- `scope` (`global` or `project`)
- `status` (`active` or `healed`)
- `trigger_event` metadata
- `created_at`

## Practical Operator Flow

```bash
# 1) Find potential incidents in recent history (no writes)
cm audit --trauma --days 30 --json

# 2) Promote a reviewed pattern to active policy
cm trauma add "^git\\s+push\\s+.*--force($|\\s)" --severity CRITICAL --scope global

# 3) Verify active policy
cm trauma list --json
```

## Adoption Notes For auto-reflect

If we adopt this pattern, prefer the same control boundary:

1. `discover` commands produce reviewable candidates only.
2. `promote` commands perform durable writes.
3. never auto-write enforcement regexes from heuristic discovery alone.

This gives us a safer default while still enabling rapid learning from past failures.
