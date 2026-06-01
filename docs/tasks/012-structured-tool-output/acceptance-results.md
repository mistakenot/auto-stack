# Task 012 — Human-Acceptance Results (Phase 3 backfill)

Run on 2026-06-01 with worktree-built binaries (`autoetl`, `autosearch` from branch `task/012-structured-tool-output`).
Input corpus: `~/.claude/projects` (corpus-wide, multiple projects). Output: `~/.auto/etl/output`. Index: `~/.auto/search/default.sqlite`.

| Step | AC | Result | Status |
|------|----|--------|--------|
| 3.3 `autoetl run --full` | AC-3 | 809 sessions parsed, 78,766 messages transformed (> 700 baseline) | ✅ |
| 3.4 `autosearch index` | — | `full_rebuild: true` (schema 5→6 auto-trigger), 78,766 messages indexed (≥ 77,000), exit 0 | ✅ |
| 3.5 populated count | AC-3 | 16,167 rows with `tool_use_result_json != ''` (≥ 261); 251 AUQ answer-bearing rows (non-zero) | ✅ |
| 3.6 column-set consistency | AC-4 | All 13 weeks (week=11…23) = identical 32-column set; week=11 drift columns (`tool_use_id`/`duration_ms`/`interrupted`) removed; `tool_use_result_json` present everywhere | ✅ |
| AC-5 (live SQLite) | AC-5 | SQLite index `messages.tool_use_result_json` populated on 16,167 rows — exact parity with parquet | ✅ |
| 3.7 recommended-acceptance | AC-7 | Query works end-to-end via SQL over the structured column. **45/61 = 73.8%** | ✅ mechanism; see note |
| AC-8 per-question notes | AC-8 | 63 rows carry `annotations`; per-question `notes` queryable keyed by question text | ✅ |

## Note on AC-7 — the structured number corrects the regex baseline (this is the point of the task)

The plan referenced the research baseline of **55.7% (34/61)** with a ±5pp tolerance. The structured column yields **73.8% (45/61)**. The denominator (61 calls offering a recommendation) matches the baseline exactly; the numerator is higher.

This is **not** a regression — it is the task's premise validated. The baseline 34/61 was a *documented* undercount from regex-over-prose:

- `docs/research/askuserquestion-analytics.md:116` states the Q5 regex "only catches `User has answered…` (205 of 262 rows)" — one of four prose templates.
- Reproducing the doc's exact regex query against the *current* corpus returns **31/41** — the adjacency join (`r_idx = u_idx+1`) and single-template `content LIKE 'User has answered%'` filter silently drop ~20 of the 61 real calls.
- The structured query (`json_extract(tool_use_result_json, '$.answers')` + `$.questions[*].options[*].label` suffix match) finds all 61 and the true acceptance rate of 45/61 = 73.8%.

The ±5pp tolerance was predicated on the structured query merely *reproducing* the lossy regex number; instead it *corrects* it, which is exactly what capturing the verbatim envelope was meant to achieve. The recommended-acceptance metric should be tracked at ~74% going forward, not ~56%.

## Note on `autoetl run --full` exit code

The full run exited 1 due to the unrelated `github` ETL source hitting a 404 on a now-missing repo (`mistakenot/gtm-langchain-demo`). The `sessions` source — the only one in scope for this task — completed cleanly (809 sessions / 78,766 messages). Not a task-012 defect.

## Note on SQLite JSON function

The project's SQLite driver (`modernc.org/sqlite`) does **not** provide `json_extract_string` (a DuckDB-ism). The SQLite query path uses plain `json_extract` over string-valued paths, which returns the scalar directly. DuckDB queries (AC-3/AC-4 backfill checks) may use either.
