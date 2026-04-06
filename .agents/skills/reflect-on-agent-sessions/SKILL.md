---
name: reflect-on-agent-sessions
description: >-
  Reflect over coding agent sessions using autosearch, find recurring workflow
  problems, and produce a markdown report with reproducible query evidence and
  explicit reasoning for each issue.
---

# Reflect On Agent Sessions

Use this skill when you need to find process or engineering problems in past coding sessions and write a report.

## Output Contract

- Output is a single markdown file.
- Keep the report machine- and human-readable.
- Report filename must include the executing agent name.
- Every issue must include:
  - issue frequency fields (`times seen`, `first seen`, `most recent seen`),
  - search evidence bullets (exact query commands used),
  - thought process bullets (how you interpreted the evidence),
  - representative message/session evidence.
- Bullets should be one sentence each.

## Report Filename Rule

Use this filename format:

```text
<report-topic>-<agent-name>-<yyyy-mm-dd>.md
```

Examples:

- `workflow-issues-codex-2026-03-22.md`
- `workflow-issues-claude-opus-4-6-2026-03-22.md`

If the runtime exposes a more specific agent identifier, include it in `agent-name`.
If not, use a stable fallback such as `codex`.

## Standard Workflow

1. Define the analysis window and workspace.
2. Refresh index with `autosearch index`.
3. Run broad discovery searches to map failure patterns.
4. Run focused searches per issue family to quantify impact.
5. Validate representative incidents with `autosearch message describe`.
6. Expand context around user-question signals before drawing conclusions.
7. Write a markdown report with issue severity, evidence, reasoning, and remediation.

## Required Issue Frequency Fields

For each issue, always report:

- `times seen`: how many matching incidents were found.
- `first seen`: earliest timestamp among matching incidents.
- `most recent seen`: latest timestamp among matching incidents.

Use this pattern to compute from message hits:

```bash
autosearch search '<query>' --scope messages --cwd <workspace> \
  | jq -r '.hits[].messageId' \
  | while read -r id; do
      autosearch message describe "$id" | jq -c '.message | {id,sessionId,timestamp}'
    done \
  | jq -s '{
      times_seen: length,
      first_seen: (min_by(.timestamp)),
      most_recent_seen: (max_by(.timestamp))
    }'
```

If query results are capped (for example `total_hits` is 50), either:

- refine the query and/or split by time buckets, or
- mark the count as `50+ (capped)` and clearly state that it is a lower bound.

## Query Playbook (Starting Point)

Run broad discovery first:

```bash
autosearch search 'error OR fail OR timeout OR busy OR tool_use_error' --scope sessions --cwd <workspace> --since <window>
autosearch search 'error OR fail OR timeout OR busy OR tool_use_error' --scope messages --cwd <workspace> --since <window> --highlight
```

Then run focused families:

```bash
# DB / locking / sync contention
autosearch search '"database is busy" OR "WAL frame salt mismatch" OR "database is locked"' --scope messages --cwd <workspace>

# Build / test breakage
autosearch search '"--- FAIL:" OR "FAIL\t./... [setup failed]" OR "panic: runtime error" OR "undefined:" OR "imported and not used"' --scope messages --cwd <workspace>

# Tool or edit orchestration failures
autosearch search '"tool_use_error" OR "File has not been read yet" OR "modified since read"' --scope messages --cwd <workspace>

# Environment/setup blockers
autosearch search '"command not found" OR "Text file busy" OR "cannot create regular file" OR "Directory does not exist"' --scope messages --cwd <workspace>

# User anti-signals (friction, correction, pushback)
autosearch search '"no " OR "undo" OR "stop" OR "didn''t ask" OR "not what I asked" OR "wrong"' --scope messages --cwd <workspace> --highlight
autosearch search '"why" OR "why did" OR "why are" OR "why would" OR "why didn''t"' --scope messages --cwd <workspace> --highlight
```

## Tool Invocation Searches

Tool_use messages (assistant calling a tool) now have their input indexed in `content_truncated`. This means you can search for what the agent *did*, not just what it *said* or what tools *returned*.

- **AskUserQuestion** inputs render as markdown: `## Question` header, bold section names, option lists. Search for question content directly.
- **All other tools** store raw JSON input. Search for field values like file paths, command strings, prompt text.

Useful patterns:

```bash
# Find what questions the agent asked the user (rendered as markdown)
autosearch search '"## Question" Options' --scope messages --cwd <workspace> --highlight

# Find Agent subagent invocations by prompt content
autosearch search "subagent_type Explore" --scope messages --cwd <workspace>

# Find specific Bash commands the agent ran
autosearch search "golangci-lint run" --scope messages --cwd <workspace>

# Find Edit tool changes to a specific file
autosearch search "old_string" --scope messages --cwd <workspace>
```

Note: tool_use rows have `messageType: "assistant"` in search results. The corresponding tool_result (output) has `messageType: "tool"`. Both are now searchable for the same tool call.

## Heuristic Triage (Usefulness and Noise)

Do not treat all string matches equally; apply this triage:

### Default (high-value, use every run)

- **User correction language** (`"no, "`, `"no this is wrong"`, `"didn't ask"`, `"undo"`, `"stop"`): high precision signal that the agent is off track.
- **Omission checks from user** (`"did we include"`): strong signal that expected output was missing.
- **Retrieval failure signals** (`"not found"`, `"can't find"`, `"cannot find"`): strong signal for blocked progress or poor discovery.
- **Recovery signals** (`"Found it"`): useful paired with failure signals to measure search/fix loop length.
- **AskUserQuestion traces** (`"## Question"`, `"User has answered your questions"`): high-value checkpoints for requirement clarification and decision gating. Tool invocations render as markdown with `## Question` headers, bold section names, and formatted option lists.

Recommended queries:

```bash
autosearch search '"no, " OR "no this is wrong" OR "didn''t ask" OR "undo" OR "stop"' --scope messages --cwd <workspace> --highlight
autosearch search '"did we include"' --scope messages --cwd <workspace> --highlight
autosearch search '"not found" OR "can''t find" OR "cannot find"' --scope messages --cwd <workspace> --highlight
autosearch search '"Found it"' --scope messages --cwd <workspace> --highlight
autosearch search '"## Question" OR "User has answered your questions"' --scope messages --cwd <workspace> --highlight
```

### Context-First Rule For User Questions (VITAL)

Never derive a generalized lesson from an isolated user-question hit.

For each `AskUserQuestion` (or user question/correction) signal:

1. Read local context around the hit (before and after messages).
2. Identify what uncertainty triggered the question.
3. Verify what changed after the answer (decision, command, code, or behavior).
4. Classify as:
   - `local-only` (project/session-specific),
   - `portable` (likely useful across sessions/tools).
5. Only promote to a reusable rule when evidence is portable, or appears in multiple sessions.

Minimum context retrieval pattern:

```bash
# 1) Get metadata, including prev/next links
autosearch message describe <message_id>

# 2) Read full message content
autosearch message get <message_id>

# 3) Read adjacent messages for immediate context
autosearch message get <previousMessageId>
autosearch message get <nextMessageId>

# 4) If still ambiguous, read the full transcript
autosearch session get <session_id>
```

### Conditional (use with guardrails)

- **Generic `why` from user**: useful for challenge detection but noisy because many `why` messages are neutral planning questions.
- **`The commands are still running`**: can indicate orchestration friction, but is often normal progress reporting.

Guardrails:

- For `why`, count only user messages and classify each hit as `challenge` vs `neutral`.
- For `commands are still running`, only count as an issue when repeated in the same task without progress evidence.

### Exploratory (higher effort, high potential)

- **Agent question extraction** (assistant messages with `?` near the end): useful for discovering recurring uncertainty and requirement gaps.
- **Q/A session patterning** (long agent question + short rapid user replies): useful for identifying requirement-clarification phases and churn.

Implementation guidance:

- Extract assistant question sentences with regex on full message content (for example sentence ending in `?` near message end).
- Build embeddings/vector index on extracted questions to cluster repeated uncertainty themes.
- Measure turn patterns (`agent question` -> `short user reply` sequences) to detect high-friction clarification loops.

## Time Window Reliability Rule

If date filters (`--since`, `--after`, `--before`) look inconsistent, enforce an exact window by post-filtering session timestamps.

```bash
START_MS=<epoch_ms_start>
END_MS=<epoch_ms_end>
autosearch search '<query>' --scope sessions --cwd <workspace> \
  | jq --argjson start "$START_MS" --argjson end "$END_MS" \
    '.hits | map(select(.lastMessageAt >= $start and .firstMessageAt < $end))'
```

Use the filtered session IDs when counting message-level hits.

## Required Report Format

Use this section layout:

```md
# <Report Title>
File: <report-topic>-<agent-name>-<yyyy-mm-dd>.md
Window analyzed: <start> to <end>
Workspace: <workspace>

## Issues

### <Issue Name> (Severity: HIGH|MEDIUM|LOW)
- Symptom: <one sentence>.
- Times seen: <one sentence with count, e.g. `23`, `50+ (capped)`>.
- First seen: <one sentence with timestamp and message/session reference>.
- Most recent seen: <one sentence with timestamp and message/session reference>.
- Context check: <one sentence on what surrounding context was reviewed>.
- Transferability: <one sentence labeling `local-only` or `portable`, with reason>.
- Search evidence:
  - `<exact autosearch command>` — <one sentence describing why this query was used>.
  - `<exact autosearch command>` — <one sentence describing what signal this query isolated>.
- Thought process:
  - <one sentence on how evidence was grouped into this issue>.
  - <one sentence on why severity was assigned>.
- Representative incidents:
  - `<messageId>` — <one sentence summarizing what happened>.
  - `<messageId>` — <one sentence summarizing impact>.
- Recommendation:
  - <one sentence fix or mitigation>.

## User Anti-Signals
- <one sentence count/summary of explicit corrections ("no", "undo", "stop", etc.)>.
- <one sentence count/summary of "why" challenge signals and whether they were neutral vs corrective>.

## Commands Run
```bash
<all major commands, in order>
```
```

## Quality Bar

- Do not report an issue without at least one concrete message/session reference.
- Do not omit `times seen`, `first seen`, or `most recent seen` for any issue.
- Do not generalize from `AskUserQuestion` or user-question hits without explicit context and transferability checks.
- Do not claim severity without stating impact on workflow speed, correctness, or reliability.
- Do not hide false positives; briefly note refinements when a query was noisy.
- Keep issue bullets short, specific, and auditable.

## Evolving This Skill

When you discover a better query pattern, update this skill file:

- Add the new query to the playbook.
- Add one sentence on when it works well.
- Add one sentence on common false positives and how to refine it.
