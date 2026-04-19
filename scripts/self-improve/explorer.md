---
name: explorer
kind: service
---

requires:
- focus: the tool or area to explore
- codebase-context: optional project context
- preflight: data pipeline status from preflight step (ETL and index freshness)

ensures:
- problems: a structured list of findings, separated into STRUCTURAL (cross-cutting patterns about why agents fail) and TACTICAL (specific bugs and config issues). Each finding includes evidence from autosearch with session counts, reproduction steps, and severity. Structural findings explain the underlying cause, not just the symptom. No suggestions or improvements — just raw observations and patterns.

errors:
- no-problems-found: exploration completed but no meaningful problems were encountered

strategies:

- step 1 — structural pattern mining (do this FIRST, it is the most valuable):
  - use autosearch to query session history broadly across many sessions related to the focus area
  - look for recurring patterns: what do agents consistently struggle with? what workflows break repeatedly? what context is missing when agents need it?
  - ask WHY, not just WHAT. "Docs aren't read" is a symptom. "Nothing triggers agents to read docs at the point of need" is the structural insight.
  - search for patterns like: agents re-doing work that was done before, agents failing to find information that exists, agents taking wrong approaches because they lack context, features that exist but agents don't discover them
  - look at how information flows (or fails to flow) between sessions, between tools, between docs and code
  - identify boundary problems: where do docs end and skills begin? where does config end and code begin? where does one tool's responsibility end and another's begin?
  - quantify patterns with session counts and concrete queries — "X happened in Y of Z sessions" is evidence

- step 2 — hands-on exploration (tactical, do this second):
  - check preflight status first — if data is stale or missing, note this as context but still proceed
  - act as a real user, not an auditor. Run the tool's commands, try its workflows end-to-end
  - run the tool's test suite and note any failures, slow tests, or flaky behavior
  - try the quickstart/docs output and note where guidance is wrong, missing, or confusing
  - try edge cases: empty inputs, invalid flags, large datasets, conflicting options
  - capture exact commands run, exact error messages, and exact output

- step 3 — classify findings into two tiers:
  - STRUCTURAL: cross-cutting patterns that affect how agents work across many sessions. These are about missing capabilities, wrong boundaries, broken information flows. They require design thinking, not just code fixes.
  - TACTICAL: specific bugs, config issues, missing validations. These can be fixed with a PR.
  - weight structural findings higher — a structural insight that explains 20 session failures is more valuable than 5 tactical bugs

invariants:
- output contains only observed problems and patterns, never suggestions or recommendations
- every finding includes evidence: session counts, autosearch queries, exact commands or output
- structural findings explain WHY the pattern exists, not just WHAT the pattern is
- problems are deduplicated — same root cause appears once with all manifestations listed
- structural findings are listed before tactical findings
