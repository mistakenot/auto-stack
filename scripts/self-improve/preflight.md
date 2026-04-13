---
name: preflight
kind: service
---

requires:
- focus: the tool or area being improved (used to decide which data pipelines to refresh)

ensures:
- result: confirmation that data pipelines are up to date, including ETL row counts, index row counts, and any warnings from either step

strategies:
- step 1 — run autoetl to refresh session data:
  - run: autoetl run
  - this transforms raw coding session logs into the normalized parquet format
  - capture the output (sessions processed, messages processed)
  - if autoetl is not installed or fails, log the warning but continue — the index may still have usable data

- step 2 — run autosearch index to rebuild the search index:
  - run: autosearch index
  - this reads the parquet output from autoetl and updates the SQLite FTS index
  - capture the output (sessions indexed, messages indexed)
  - if autosearch is not installed or the index step fails, log the warning but continue

- step 3 — verify data is available:
  - run a quick smoke test: autosearch search "test" --limit 1
  - if this returns zero hits and the focus area is autosearch, warn that the index may be empty
  - report total sessions and messages available

- output should include:
  - etl_status: success/warning/skipped
  - etl_summary: row counts or error message
  - index_status: success/warning/skipped
  - index_summary: row counts or error message
  - smoke_test: pass/warn
  - data_age: timestamp of newest indexed data if available

invariants:
- preflight never fails the pipeline — it warns and continues
- preflight never modifies source code, only runs data pipeline commands
- if both etl and index fail, the warning is prominent so the explorer knows data may be stale
