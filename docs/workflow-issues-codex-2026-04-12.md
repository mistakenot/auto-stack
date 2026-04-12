# Auto-Stack Session Workflow Issues (Recent)
File: workflow-issues-codex-2026-04-12.md
Window analyzed: 2026-03-29T00:00:00Z to 2026-04-12T20:20:00Z (rolling 14d queries)
Workspace: /home/vscode/src/auto-stack

## Issues

### Edit Tool State Errors (Severity: HIGH)
- Symptom: Edit operations repeatedly fail with stale-read and replacement-target mismatches, causing avoidable retry loops.
- Times seen: 9 matching incidents.
- First seen: 2026-04-05T15:16:46Z in `a1a34b0915fc65f5b-49`.
- Most recent seen: 2026-04-12T19:52:12Z in `0e160d60-06ee-4e5b-8bff-fccdc0138c9a-77`.
- Context check: Reviewed adjacent messages for `1028af22-f1f9-4f69-a132-a3ab08072bcb-120/121/122` and `b6c7afc8-4e01-4559-9874-745676564ddc-239/240/241/242/243/244`.
- Transferability: portable, because the failure mode is tied to generic agent edit workflows (read-write ordering and brittle string replacement).
- Search evidence: `autosearch search '"File has not been read yet" OR "modified since read" OR "String to replace not found in file"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --limit 200` — isolated direct edit-tool failure messages.
- Search evidence: `autosearch session get 1028af22-f1f9-4f69-a132-a3ab08072bcb | rg -n 'index=120|index=121|index=122|modified since read|Read it again'` — confirmed in-session failure and immediate retry context.
- Thought process: I grouped all failures where edits were blocked by stale buffers or missing replacement strings because they share the same root cause (unsafe edit sequencing).
- Thought process: Severity is HIGH because each incident interrupts code changes mid-task and forces additional read/edit cycles.
- Representative incident: `1028af22-f1f9-4f69-a132-a3ab08072bcb-121` — tool returned `File has been modified since read... Read it again before attempting to write it.`
- Representative incident: `b6c7afc8-4e01-4559-9874-745676564ddc-240` — tool returned `String to replace not found in file`, followed by another replacement miss at `...-243`.
- Recommendation: Create a `tool-edit-recovery` skill that enforces re-read-before-write, validates replacement anchors before edit, and automatically falls back to narrower patch scopes.

### Environment Prereq / Install Blockers (Severity: MEDIUM)
- Symptom: Build/test/install steps fail due missing toolchain prerequisites and live-binary overwrite conflicts.
- Times seen: 3 matching incidents.
- First seen: 2026-04-09T14:53:11Z in `e35ca62d-f692-4e3e-8b62-bd8674e9726a-499`.
- Most recent seen: 2026-04-09T15:34:39Z in `44325cd4-d81a-44ee-bc56-ccb77c319af7-99`.
- Context check: Reviewed adjacent messages for `44325cd4-d81a-44ee-bc56-ccb77c319af7-91/92/93` and `44325cd4-d81a-44ee-bc56-ccb77c319af7-98/99/100`.
- Transferability: portable, because these are standard preflight gaps for local dev environments and CI-like runs.
- Search evidence: `autosearch search '"cannot create regular file '/home/vscode/.local/bin/autowatch': Text file busy" OR "cgo: C compiler \"gcc\" not found"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --limit 200` — isolated high-precision install/test blockers.
- Search evidence: `autosearch session get 44325cd4-d81a-44ee-bc56-ccb77c319af7 | rg -n 'index=91|index=92|index=93|index=98|index=99|index=100|gcc|Text file busy|autowatch is running'` — validated immediate remediation behavior after failures.
- Thought process: I excluded noisy generic `command not found` matches and kept only exact blocker strings to avoid false positives.
- Thought process: Severity is MEDIUM because failures were recoverable but still consumed significant execution time.
- Representative incident: `44325cd4-d81a-44ee-bc56-ccb77c319af7-92` — `cgo: C compiler "gcc" not found` caused race-enabled test failure.
- Representative incident: `44325cd4-d81a-44ee-bc56-ccb77c319af7-99` — `make install` failed with `autowatch ... Text file busy` during binary copy.
- Recommendation: Create an `env-preflight-and-safe-install` skill that checks required compilers/tools, detects running binaries, and applies non-destructive install fallbacks with explicit remediation commands.

### Search Signal Quality Gaps (Severity: HIGH)
- Symptom: Session-analysis passes produce low-confidence ranking because of saturated hit caps and repeated complaints about missing practical signal quality.
- Times seen: 10 matching incidents.
- First seen: 2026-04-12T00:38:07Z in `b815b122-99ad-48b0-9717-ba8577c7387d-308`.
- Most recent seen: 2026-04-12T20:19:36Z in `0e160d60-06ee-4e5b-8bff-fccdc0138c9a-274`.
- Context check: Reviewed `b815b122-99ad-48b0-9717-ba8577c7387d-307/308/309` and `0e160d60-06ee-4e5b-8bff-fccdc0138c9a-169/183/274` to verify these were not isolated one-off comments.
- Transferability: portable, because this affects any workflow that relies on autosearch hit counts for prioritization.
- Search evidence: `autosearch search '"hits 50" OR "highlights were always null" OR "--count mode" OR "can''t do structured queries"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --limit 200` — captured recurring critiques of search usefulness.
- Search evidence: `autosearch message get b815b122-99ad-48b0-9717-ba8577c7387d-308` — confirmed explicit user feedback calling out cap saturation, null highlights, and missing structured aggregations.
- Thought process: I treated this as a reliability issue in the analysis loop because poor signal quality degrades decision-making even when data exists.
- Thought process: Severity is HIGH because this directly limits the stack's ability to identify true priorities from session history.
- Representative incident: `b815b122-99ad-48b0-9717-ba8577c7387d-308` — user reported cap saturation (`50` everywhere), null highlights, and no structured aggregation.
- Representative incident: `0e160d60-06ee-4e5b-8bff-fccdc0138c9a-169` — downstream analysis excerpt still reported capped hit patterns (`total_hits: 50`) during issue discovery.
- Recommendation: Create an `autosearch-evidence-hardening` skill that forces precision query design, false-positive checks, uncapped frequency estimation strategies, and transcript-first validation when counts saturate.

### User Correction Churn (Severity: MEDIUM)
- Symptom: Users repeatedly intervene to redirect execution, indicating action drift or premature assumptions.
- Times seen: 10 matching incidents.
- First seen: 2026-04-05T15:20:10Z in `941bcf49-5646-4aeb-9049-b094ecf5cefc-139`.
- Most recent seen: 2026-04-12T00:38:07Z in `b815b122-99ad-48b0-9717-ba8577c7387d-308`.
- Context check: Reviewed adjacent messages for `1b0a7009-560d-488b-a239-3a1a92ffa331-270/271/272`, `44325cd4-d81a-44ee-bc56-ccb77c319af7-242/243/244`, and `e35ca62d-f692-4e3e-8b62-bd8674e9726a-500/501/502`.
- Transferability: portable, because correction language appears across multiple sessions and task types.
- Search evidence: `autosearch search '"no, " OR "didn''t ask" OR "undo" OR "stop" OR "not what I asked" OR "wrong"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --role user --limit 200` — surfaced explicit user redirect signals.
- Search evidence: `autosearch message get 44325cd4-d81a-44ee-bc56-ccb77c319af7-243` — validated concrete correction tied to workspace/worktree safety.
- Thought process: I retained this issue despite some noisy matches because there are multiple direct imperative corrections with clear redirection intent.
- Thought process: Severity is MEDIUM because corrections are recoverable but increase churn and reduce trust in autonomous execution.
- Representative incident: `44325cd4-d81a-44ee-bc56-ccb77c319af7-243` — user correction: `no this isnt on a worktree, you'll conflict with other work, move back to main`.
- Representative incident: `1b0a7009-560d-488b-a239-3a1a92ffa331-271` — user correction: `no add to todo.md` immediately after agent asserted completion.
- Recommendation: Create a `correction-aware-execution` skill that pauses on correction phrases, restates updated intent in one sentence, and gates further tool calls until the new plan is explicit.

## Proposed New Skills
- `tool-edit-recovery`: Mitigate edit-state failures by enforcing read freshness checks, anchor validation, and deterministic fallback edit strategies.
- `env-preflight-and-safe-install`: Run prerequisite checks (`gcc`, toolchain, writable install targets, busy binary detection) before build/install commands.
- `autosearch-evidence-hardening`: Standardize high-precision query packs, anti-noise filtering, and count saturation handling (`50+ lower-bound` labeling and time-bucket splits).
- `correction-aware-execution`: Detect user correction language and switch to confirmation-first behavior before continuing execution.
- `session-pattern-aggregator`: Add reusable workflow for deriving structured aggregates from transcripts (top files read, top docs referenced, retry-loop hotspots).

## User Anti-Signals
- Explicit correction language query returned 10 hits, with at least 3 clearly directive corrections that changed execution (`...-271`, `...-243`, `...-501`).
- `why` query returned 5 user hits; manual context classification found 3 challenge/diagnostic prompts and 2 neutral clarification prompts.

## Commands Run
```bash
cat .agents/skills/reflect-on-agent-sessions/SKILL.md
autosearch --help
autosearch quickstart
autosearch index
autosearch search "error OR fail OR timeout OR busy OR tool_use_error" --scope sessions --cwd /home/vscode/src/auto-stack --since 14d --limit 50
autosearch search "error OR fail OR timeout OR busy OR tool_use_error" --scope messages --cwd /home/vscode/src/auto-stack --since 14d --highlight --limit 50
autosearch search '"File has not been read yet" OR "modified since read" OR "String to replace not found in file"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --limit 200
autosearch search '"cannot create regular file '/home/vscode/.local/bin/autowatch': Text file busy" OR "cgo: C compiler \"gcc\" not found"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --limit 200
autosearch search '"hits 50" OR "highlights were always null" OR "--count mode" OR "can''t do structured queries"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --limit 200
autosearch search '"no, " OR "didn''t ask" OR "undo" OR "stop" OR "not what I asked" OR "wrong"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --role user --limit 200
autosearch search '"why" OR "why did" OR "why are" OR "why would" OR "why didn''t"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --role user --limit 200
autosearch message describe <message_id>
autosearch message get <message_id>
autosearch session get <session_id>
```
