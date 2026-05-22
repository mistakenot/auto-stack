---
name: explorer
kind: service
---

requires:
- focus: the tool or area to explore
- codebase-context: optional project context
- preflight: data pipeline status from preflight step (ETL and index freshness)

ensures:
- problems: a structured list of findings, separated into STRUCTURAL (systemic tool issues: broken workflows, missing features, bad defaults, architectural limitations) and TACTICAL (specific bugs, crashes, wrong output, missing validation). Each finding includes reproduction steps, exact commands and output, and severity. Structural findings explain the underlying cause in the tool's code, not just the symptom. No suggestions or improvements — just raw observations and evidence.

errors:
- no-problems-found: exploration completed but no meaningful problems were encountered

strategies:

- step 1 — hands-on tool exploration (do this FIRST, it is the most valuable):
  - check preflight status first — if data is stale or missing, note this as context but still proceed
  - act as a real user of the compiled tool, not an auditor of the repo. Run the tool's commands, try its workflows end-to-end
  - run the tool's test suite and note any failures, slow tests, or flaky behavior
  - try the quickstart/docs output and note where guidance is wrong, missing, or confusing
  - try edge cases: empty inputs, invalid flags, large datasets, conflicting options
  - look for: commands that error unexpectedly, output that is wrong or misleading, workflows that require too many steps, features that don't work as documented, missing error messages, bad defaults
  - capture exact commands run, exact error messages, and exact output
  - this step produces the primary findings — everything else is supplementary

- step 2 — session history mining (supplementary evidence, do this second):
  - use autosearch to query session history for sessions where users hit problems with the focus tool
  - look for recurring error patterns: what commands fail repeatedly? what workarounds do users apply? what features are attempted but abandoned?
  - quantify patterns with session counts and concrete queries — "X error occurred in Y of Z sessions" is evidence
  - session history adds weight to hands-on findings and may surface issues you did not encounter in step 1
  - DO NOT mine for meta-patterns about agent behavior, skill routing, instruction hierarchy, or repo ergonomics — focus on the tool's functionality as experienced by its users

- step 3 — classify findings into two tiers:
  - STRUCTURAL: systemic tool issues that affect multiple commands or workflows. Broken pipelines, missing features, bad architectural defaults, incorrect data handling. These may require design work but the fix lives in the tool's code.
  - TACTICAL: specific bugs, crashes, wrong output, missing validation. These can be fixed with a targeted PR.
  - weight structural findings higher — a broken workflow that affects every user is more valuable than an edge-case crash

invariants:
- output contains only observed problems with the tool's functionality, never suggestions or recommendations
- every finding includes reproduction evidence: exact commands, exact output, and optionally session counts from autosearch
- structural findings explain WHY the tool behaves this way (trace to code), not just WHAT the symptom is
- problems are deduplicated — same root cause appears once with all manifestations listed
- structural findings are listed before tactical findings
- findings are about the tool itself (bugs, missing features, broken workflows, bad output) — never about repo ergonomics, agent configuration, skill routing, or instruction hierarchy
