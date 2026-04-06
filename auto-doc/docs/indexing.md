---
hash: "9324b2f6"
id: "bdc8c577"
summary: "Research notes on BM25 search implementation using Bluge"
title: "Full Text Search"
---

# Full text search

Consider using https://blugelabs.com/bluge/introduction/ for BM25 based search.

## How does CASS do it?

```markdown
Indexing Pipeline Overview

  CASS uses a hybrid search architecture with three complementary indices:

  1. BM25 (Tantivy)

  - Delegates to frankensearch crate which wraps Tantivy
  - Raw message content indexed as-is (no preprocessing for BM25 — Tantivy
  handles tokenization internally)
  - Custom tokenizer registered via cass_ensure_tokenizer in frankensearch
  - Supports edge n-grams for prefix matching (e.g. "cal" → "calculate")
  - Stored at <data_dir>/index/v6/ with schema version hashing for auto-rebuild
  on mismatch
  - Lazy segment merging: 4-segment threshold, 300s cooldown via
  optimize_if_idle()

  2. Text Transformation (for semantic indexing only)

  canonicalize.rs applies a pipeline before embedding:
  1. Unicode NFC normalization
  2. Markdown stripping — bold/italic markers, link URLs (keeps text), header #
  markers, blockquotes
  3. Code block collapsing — short blocks kept; long blocks truncated to first 20
   + last 10 lines
  4. Whitespace normalization
  5. Low-signal filtering — removes "OK", "Done", "Got it" etc.
  6. Truncation to 2000 chars (MAX_EMBED_CHARS)

  BM25 does not use this pipeline — it indexes raw text.

  3. Semantic/Vector Search (dual-tier)

  Two embedder backends:

  - Hash Embedder (FNV-1a feature hashing) — instant (<1ms), no ML model needed,
  384-dim. Lowercase + split on non-alphanumeric + L2 normalize.
  - FastEmbed (MiniLM all-minilm-l6-v2 via ONNX) — higher quality, ~130ms. Never
  auto-downloads models.

  Vectors stored in FSVI format (frankensearch), f16-quantized, memory-mapped,
  with optional HNSW graph for approximate NN.

  Two-tier search returns hash-embedder results immediately, then refines with ML
   embedder in background.

  4. Document Granularity

  - Message-level indexing — each NormalizedMessage is an indexable unit (not
  chunked further)
  - chunk_idx: u8 field exists for future sub-message splitting but is always 0
  currently

  5. Ingestion Pipeline

  - 15 connectors (Claude Code, Codex, Cursor, ChatGPT, Gemini, Cline, Aider,
  Copilot, etc.)
  - Streaming architecture: one producer thread per connector, bounded channel
  (32 messages) with backpressure
  - Parallel batch processing: Rayon par_chunks(7) for concurrent SQLite +
  Tantivy writes
  - Periodic commits every 5s for incremental visibility
  - All platforms normalized to NormalizedConversation schema

  6. Storage

  - SQLite (agent_search.db) — conversations, messages, metadata, FTS5
  - Tantivy segments — BM25 full-text
  - FSVI files — vector embeddings
  - LRU caches — string interner (10K cap) + query result cache

  Search Fusion

  BM25 + semantic results combined via RRF (Reciprocal Rank Fusion) for hybrid
  search.
```

https://github.com/knights-analytics/hugot for embedding?
https://github.com/blugelabs/bluge for bm25?

store index data in .autodoc folder in project in gitignored file