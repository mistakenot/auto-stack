# Feedback: Task 012

## Problems faced

1. **`autoetl run --full` exits 1 on an unrelated source.** The full backfill reprocesses sessions cleanly (809 sessions / 78,766 messages) but the `github` ETL source aborts the run with a 404 on a now-missing repo (`mistakenot/gtm-langchain-demo`), so the process exits non-zero even though the in-scope `sessions` transform succeeded. To get a clean signal for just the schema change, run `autoetl run --full --only sessions`, or read the exit code per-source rather than process-wide.

2. **The AC-7 baseline number was wrong in a way that looked like a bug.** The plan asserted the live recommended-acceptance query should land within ±5pp of 55.7% (34/61). The structured query returned 73.8% (45/61). It took reproducing the *original* regex query against the same corpus (which returns 31/41 — finding only 41 of the 61 real calls) to prove the gap is the documented prose-template/adjacency-join undercount the task was built to eliminate, not an implementation error. The denominator (61) matching the baseline exactly was the clue that the mechanism was right and only the numerator methodology differed.

3. **`json_extract_string` is a DuckDB-ism, not SQLite.** The project's SQLite driver (`modernc.org/sqlite`) only has `json_extract`. The plan/spec referenced `json_extract_string` for the SQLite-index path; the e2e test and docs had to use plain `json_extract` over string-valued paths there, while keeping `json_extract_string` for the DuckDB-over-parquet examples.

4. **`.gitignore` ignores `**/testdata/`.** New fixture files under a `testdata/` dir need `git add -f`. Phase 2/3 sidestepped this entirely by generating parquet fixtures into `t.TempDir()` at runtime via a shared `testutil.GenerateAUQFixtures` helper — cleaner than checked-in fixtures and no force-add needed.

## Reflections

- **What was tricky:** the only real intellectual work was AC-7 — distinguishing "my query is wrong" from "the baseline was wrong." The instinct on a ±5pp miss is to assume the new code is broken; here the new code was *more correct* than the metric it was being checked against. Verifying by re-running the old methodology was the decisive move.
- **What I'd tell myself at the start:** when an acceptance check compares a new structured query against a number derived from the brittle approach you're replacing, expect the numbers to *diverge* — that divergence is often the success signal, not a failure. Budget time to prove which number is right rather than forcing a match.
- **What I almost did but didn't:** I almost reported AC-7 as a pass by quoting "55.7% confirmed," and almost hoisted the envelope assignment out of the per-block loop during review (point #3 from the bot) — both avoided because the plan was explicit and the change had no behavioural payoff.

## Useful context

- `docs/research/askuserquestion-analytics.md:116` is the load-bearing line: it states the Q5 regex "only catches `User has answered…` (205 of 262 rows)" — the explicit admission that the baseline undercounts, which made the AC-7 reconciliation defensible rather than hand-wavy.
- The column-addition precedent commits named in `context.md` (`ffc0df9` skill_name, `41bf300` bash_exit_code) gave the exact end-to-end threading order (model → parquet → schema → InsertMessage → MessageRow → describe) so each phase was mechanical.
- Keeping the new `InsertMessage` parameter trailing (plan decision) minimized the diff, but the INSERT column list / Exec args still wanted to match DDL order for readability — the two are independent, which the PR review (#2) correctly flagged.
- DuckDB's `[*]` JSON wildcard (`$.questions[*].options[*].label`) returning a list made the corpus-wide acceptance computation a one-liner without `json_each` table-function gymnastics.
