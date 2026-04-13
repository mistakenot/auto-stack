---
name: explorer
kind: service
---

requires:
- focus: the tool or area to explore
- codebase-context: optional project context
- preflight: data pipeline status from preflight step (ETL and index freshness)

ensures:
- problems: a structured list of problems, setbacks, and friction encountered while using the tool as a real user. Each problem includes what happened, reproduction steps, and severity. No suggestions or improvements — just raw problems.

errors:
- no-problems-found: exploration completed but no meaningful problems were encountered

strategies:
- check preflight status first — if data is stale or missing, note this as context but still proceed
- act as a real user, not an auditor. Run the tool's commands, try its workflows end-to-end
- use autosearch to query session history for recurring error patterns related to the focus area
- run the tool's test suite and note any failures, slow tests, or flaky behavior
- try the quickstart/docs output and note where guidance is wrong, missing, or confusing
- try edge cases: empty inputs, invalid flags, large datasets, conflicting options
- explore the tool's CLI help and note inconsistencies or missing documentation
- capture exact commands run, exact error messages, and exact output
- classify each problem by severity: blocking (prevents core workflow), degraded (works but painful), minor (cosmetic or inconvenient)

invariants:
- output contains only observed problems, never suggestions or recommendations
- every problem includes the exact command or action that triggered it
- problems are deduplicated — same root cause appears once with all manifestations listed
