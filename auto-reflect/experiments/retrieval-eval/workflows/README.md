# Method scripts

How the data in `../data/` was produced. The `.mjs` files are **Claude Code
Workflow-tool scripts** (they use `agent()` / `parallel()` / `phase()` globals and
run inside the Workflow harness, fanning out one sub-agent per item). They are
recorded here for method transparency and reuse — they do **not** run standalone
`node`. At run time a corpus/query payload is injected in place of the
`__PLACEHOLDER__` token, then the script is launched via the Workflow tool.

| script | role | output |
|---|---|---|
| `mine_queries.mjs` | one agent per held-out session → realistic retrieval intents + domain guesses + leakage flags | `../data/queries/queries.jsonl` (via `land_queries.py`) |
| `oracle.mjs` | one agent per query grades TRUE relevance of all 120 rules (graded 1–3) | `../data/qrels/*.qrels.jsonl` (via `retrieval_eval.analyze_pilot`) |
| `land_queries.py` | dedupe + assign ids + write the query set | `../data/queries/queries.jsonl` |

Analysis lives in the package: `retrieval_eval.analyze_pilot` (coverage + the
domain-gate effect preview) and `retrieval_eval.metrics`.

Provenance of each artifact (source commit, counts, leakage handling) is in the
sibling `SNAPSHOT.md` / `QUERIES.md` files under `../data/`.
