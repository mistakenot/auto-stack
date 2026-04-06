---
hash: "26d7635c"
id: "ba149ace"
summary: "Research notes from the coding_agent_session_search (CASS) Rust CLI covering search modes, query patterns, output formats, indexing architecture, and design patterns informing autosearch."
title: "CASS Inspiration Notes"
---

# CASS Inspiration Notes

Research notes from [coding_agent_session_search](https://github.com/anthropics/coding_agent_session_search) (CASS) — a Rust-based CLI/TUI for indexing and searching local coding agent history. These findings inform `autosearch` design decisions.

## Search Modes

CASS offers three search modes selectable via `--mode`:

| Mode | Engine | Typical Latency | Notes |
|------|--------|-----------------|-------|
| `lexical` (default) | BM25 via Tantivy | 5-100ms | Inverted index with edge n-grams |
| `semantic` | Vector embeddings | 100-1000ms | FastEmbed MiniLM or hash fallback |
| `hybrid` | RRF fusion of both | 100-1500ms | Reciprocal Rank Fusion (K=60) |

A **two-tier progressive** semantic mode returns fast hash-embedder results (~1ms), then refines with a quality transformer (~130ms) via a background daemon. Controlled via `--two-tier`, `--fast-only`, `--quality-only`.

**Takeaway:** Start with lexical (BM25). Semantic and hybrid are additive layers that can come later without changing the CLI contract.

## Query Patterns

- **Boolean:** `(foo OR bar) AND baz`, `NOT error`
- **Phrases:** `"auth error"` (exact adjacency)
- **Wildcards:** `foo*` (prefix, fast via edge n-grams), `*foo` (suffix, regex), `*foo*` (contains)
- **Field-scoped:** `agent:claude_code`
- **Auto-fallback:** sparse exact results automatically retry with `*term*` wildcards

**Takeaway:** Edge n-grams at index time make prefix search O(1) instead of regex scan. Wildcard auto-fallback improves UX when exact queries return few hits.

## Filtering

- `--agent <slug>` — repeatable, filter by agent type
- `--workspace <path>` — repeatable, filter by project
- `--source local|remote|<hostname>` — filter by data origin
- `--days N`, `--today`, `--week`, `--since/--until <ISO-date>` — time filters
- `--sessions-from <file>` — search within results of a prior search (pipe chaining)

**Takeaway:** `--sessions-from` enables chained/intersected searches — a powerful composition pattern for agents that want to narrow iteratively.

## Output Formats

CASS is robot-first: JSON by default, human display as opt-in.

| Format | Flag | Use Case |
|--------|------|----------|
| JSON (pretty) | `--robot` / `--json` | Default machine output |
| JSONL (streaming) | `--robot-format jsonl` | Streaming pipelines |
| Compact JSON | `--robot-format compact` | Minimal wire size |
| Sessions (paths only) | `--robot-format sessions` | Pipe chaining with `--sessions-from` |
| TOON | `--robot-format toon` | Token-optimized notation for LLMs |
| Table | `--display table` | Human default |
| Markdown | `--display markdown` | Documentation |

**Takeaway:** The `sessions` format (one path per line) is cheap to produce and unlocks composability. TOON (token-optimized) is novel — worth evaluating whether the token savings justify the complexity.

## Token/Size Control for AI Consumers

- `--max-content-length N` — truncate content fields to N UTF-8 chars
- `--max-tokens N` — soft token budget (4 chars ~ 1 token), adjusts truncation
- `--fields minimal|summary|<csv-list>` — select specific fields or use presets
- `--robot-meta` — include `_meta` block with elapsed_ms, cursor, cache stats

**Takeaway:** Field selection presets (`minimal`, `summary`) reduce noise for agents without requiring them to know the full schema. Token budgets let the caller control output size — important when results feed into an LLM context window.

## Pagination

- `--limit N --offset N` — classic offset pagination
- `--cursor <token>` — cursor-based (base64-encoded offset from `_meta.next_cursor`)
- `--request-id <ID>` — correlation ID echoed in `_meta`

**Takeaway:** Cursor pagination is more robust than offset for large/changing result sets. Request IDs are cheap to add and invaluable for debugging agent pipelines.

## Aggregation

`--aggregate agent,workspace,date,match_type` returns buckets with counts instead of full results. Server-side grouping avoids pulling all hits just to count by category.

**Takeaway:** Aggregation is essential for `autoreflect` — it needs to answer "which agents/workspaces have the most errors?" without streaming every hit.

## Query Introspection

- `--explain` — shows parsed query AST, index strategy, cost estimate
- `--dry-run` — validate query without executing
- `--timeout N` — return partial results if exceeded

**Takeaway:** `--explain` and `--dry-run` are invaluable for debugging. Partial results on timeout keeps agents unblocked.

## Machine Discovery

- `cass robot-docs <topic>` — structured docs for: commands, env, paths, schemas, exit-codes, examples, contracts
- `cass introspect --json` — full API schema (all commands, args, response types)
- `cass capabilities --json` — features, versions, limits
- `cass api-version --json` — contract versioning with stability guarantees

**Takeaway:** Self-describing APIs let agents discover capabilities without external docs. `autosearch docs` and `autosearch quickstart` already cover this; `introspect` is a heavier-weight option if we need structured schema output later.

## Indexing Architecture

- **Engine:** Tantivy (Rust full-text search library)
- **Edge n-grams:** pre-computed at index time (length 2 to word length) for fast prefix matching
- **Schema versioning:** automatic rebuild on mismatch detection
- **Warm worker:** background task reloads index every 300ms to pre-warm OS cache
- **Segment merging:** async merge when >= 4 segments (5 min cooldown)
- **Watch mode:** `cass index --watch` monitors for new session files and reindexes

**Takeaway:** For autosearch over parquet, Bleve (Go) is the natural Tantivy equivalent. Edge n-grams and schema versioning are worth adopting. Watch-mode indexing aligns with `autowatch` integration.

## Key Design Patterns to Adopt

1. **Robot-first output** — JSON default, human as opt-in. Token budgets and field selection for LLM consumers.
2. **Chained search** — `--sessions-from` lets one search pipe into another for iterative narrowing.
3. **Aggregation** — server-side grouping avoids pulling all hits for counting.
4. **Query explain/dry-run** — debugging and cost estimation without side effects.
5. **Edge n-grams at index time** — fast prefix matching without runtime regex.
6. **Wildcard auto-fallback** — improves UX when exact queries return sparse results.
7. **Cursor pagination** — more robust than offset for large result sets.
8. **Field selection presets** (`minimal`, `summary`) — reduces noise for programmatic consumers.
9. **Self-describing API** — agents discover capabilities via the tool itself.
10. **Progressive semantic search** — return fast results immediately, refine asynchronously.

## Differences from autosearch

CASS is a standalone Rust binary that owns its own indexing, connectors, and embedding pipeline. `autosearch` differs in that:

- **Data source:** reads pre-normalized parquet from `autoetl`, not raw session files.
- **Language:** Go, using Bleve or similar for full-text indexing.
- **Scope:** search + inspection primitives that feed `autoreflect`, not a standalone end-user tool.
- **Semantic search:** deferred — BM25 lexical search is the initial focus.
- **Agent detection:** handled upstream by `autoetl`, not by `autosearch`.
