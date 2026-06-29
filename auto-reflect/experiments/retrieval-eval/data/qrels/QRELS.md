# Golden relevance judgments (qrels)

The reusable core of the bench. `qrels.jsonl` is the frozen gold standard: for
each query, the rules an LLM oracle judged genuinely relevant, graded 1–3. Any
future retrieval variant evaluates against this with **zero new oracle calls**.

- **Built:** 2026-06-29 (20-query pilot 2026-06-29, extended to the full 100 same day)
- **Corpus:** the pinned 120-rule snapshot (`../corpus/SNAPSHOT.md`, commit cc5db4d)
- **Queries:** all 100 from `../queries/queries.jsonl` (held-out; 64 clean / 36 leakage-flagged)
- **Oracle:** Claude Sonnet, one agent per query grading TRUE relevance over the
  **whole** 120-rule playbook (full judgments — no pooling bias), graded
  1=marginal, 2=relevant, 3=directly on-point. Prompt: `../../workflows/oracle.mjs`.
  Single-judge — labels are "Claude-judged relevance," not absolute truth (a
  second-judge kappa pass is a future add; see DIARY.md).

## Stats

- 100 queries, **89 have ≥1 relevant rule** (89% coverage), **585 labels**
- mean **5.85** relevant rules/query (median 5, max 17; 6.57 over the 89 covered)
- grade distribution: 1 → 115, 2 → 303, 3 → 167

## Files

| file | what |
|---|---|
| `qrels.jsonl` | the gold standard — `{query_id, intent, domain_guess, overlaps_mined_task, relevant:[{rule_id, grade, why}]}` per line |
| `qrels.conditions.json` | derived: per-condition (guess/none/wrong) recall/precision/surfaced from `retrieval_eval.conditions` |

## Validity / reuse notes

- Tied to the pinned corpus snapshot. If the playbook materially changes (rules
  added/edited), re-snapshot and re-label — new rules have no judgments here.
- Use the **clean 64** (`overlaps_mined_task == "none"`) as the primary decision
  set; the 36 flagged-overlap as sensitivity only.
- Queries from the same `source_session` are not independent — cluster on it for
  significance testing.
