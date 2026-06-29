# Query set provenance

- **Built:** 2026-06-29
- **Count:** 100 queries from 48 **held-out** sessions
- **Source:** auto-stack sessions NOT among the 95 that produced the playbook
  (the original observation-mining corpus). Excluded up front: the 95 originals,
  `planning-eval` task re-runs (same tasks 001–053, different session id),
  autowatch-cron noise, and trivial (<25 message) sessions. From 191 clean
  candidates, a diverse 48-session sample was mined (one Sonnet agent per session).
- **Method:** each agent read a full held-out transcript and extracted 1–3
  realistic retrieval intents — phrased as an agent would describe work it is
  about to start — plus a `domain_guess` (from the playbook's 24-tag vocab,
  deliberately allowed to be imperfect or empty) and a leakage flag.

## Leakage handling

`held_out: true` means the *source session* was outside the rule-mining corpus.
But ~a third of held-out work still topically overlaps a mined task (same author,
related areas). Each query carries `overlaps_mined_task` (a task slug or `"none"`)
so the analysis can report two tiers:

- **clean hold-out** (`overlaps_mined_task == "none"`): **64 queries** — the
  primary, lowest-leakage eval set.
- **all held-out**: **100 queries** — secondary; leakage (if any) inflates
  absolute recall but affects every retrieval variant equally, so the *relative*
  comparison stays valid.

## Schema (`queries.jsonl`, one object per line)

| field | meaning |
|---|---|
| `query_id` | stable `q-<sha1[:8]>` of the normalized intent |
| `intent` | the retrieval text (what the matcher sees) |
| `domain_guess` | domain tag(s) a hurried agent would pass to `--domain`; may be `[]` |
| `topic` | short label |
| `overlaps_mined_task` | mined-task slug this overlaps, or `"none"` (leakage flag) |
| `held_out` | always true here |
| `source_session` | originating session id |
| `rationale` | one-line grounding in the session |

## Stats at build

- clean hold-out (no task overlap): 64 · flagged overlap: 36
- with domain guess: 97 · empty domain guess: 3 · only-`go` guess: 1
- top domain guesses: go 51, cli 30, search 26, etl 25, doc 17, git 16, testing 13
