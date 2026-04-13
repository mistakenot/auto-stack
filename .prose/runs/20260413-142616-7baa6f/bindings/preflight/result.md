# Preflight Result

kind: output
service: preflight

---

## ETL Status

- **etl_status**: success
- **etl_summary**: Parsed 353 sessions, transformed 34,860 messages. Wrote incremental updates to week=16 messages (296 rows) and month=04 sessions (51 rows). GitHub PR sync: 4 repos, 0 PRs synced.

## Index Status

- **index_status**: success
- **index_summary**: Incremental reindex processed 3 partitions (week=15, week=16 messages; month=04 sessions). 5,217 messages indexed, 51 sessions indexed, 5 partitions skipped (already current). Index path: `/home/vscode/.auto/search/default.sqlite`

## Smoke Test

- **smoke_test**: pass
- Search for "error" returned 3,337 hits across 291 distinct sessions.
- Search for "test" returned 6,230 hits across 304 distinct sessions.

## Data Coverage

- **data_age**: Newest data is from week 16 of 2026 (current week, April 2026).
- **date_range**: ETL output covers week=11 through week=16 of 2026 (approximately mid-March through mid-April 2026). Session partitions cover month=03 and month=04 of 2026.
- **WARNING: No February 2026 data available.** The focus request asks for February 2026 sessions, but the ETL raw data only goes back to approximately mid-March 2026. The search index has zero hits for the `--after 2026-02-01 --before 2026-03-01` date range. The explorer should adjust the date range to March-April 2026, or the user should provide raw session logs from February 2026 and re-run ETL.

## Summary

| Metric | Value |
|--------|-------|
| etl_status | success |
| index_status | success |
| smoke_test | pass |
| total_sessions_etl | 353 |
| total_messages_etl | 34,860 |
| messages_indexed_this_run | 5,217 |
| sessions_indexed_this_run | 51 |
| earliest_data | ~week 11, 2026 (mid-March) |
| newest_data | week 16, 2026 (mid-April) |
| february_2026_data | **NOT AVAILABLE** |
