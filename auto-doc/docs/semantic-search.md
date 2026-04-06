---
hash: "a89d6902"
id: "b65a8bb0"
summary: "Research notes on semantic search using hugot and Go-native embeddings"
title: "Semantic Search"
---

# Semantic Search

https://github.com/knights-analytics/hugot

## Research notes

These are notes from a research session I did related to this. Just use it as advice and input. You don't have to follow it to the letter. These are just ideas.

```markdown
# Documentation Search Tool — Engineering Summary

## Overview

A Go CLI tool to enable coding agents to search and retrieve relevant documentation chunks from a corpus of ~40 markdown files. The tool should be fast, local, and require no external services to operate.

## Indexing: Bluge + BM25

- Use **Bluge** as the Go indexing library (BM25 scoring is the default similarity)
- Index is built once from the doc corpus and queried at runtime — no rebuild needed unless docs change
- BM25 handles multi-keyword queries naturally; passing space-separated terms ranks chunks by combined term relevance

## Chunking Strategy

- Chunk documents on **heading boundaries** (##, ###) rather than character count
- Only subdivide on size if a section is excessively long
- Each chunk should carry metadata: file path, heading hierarchy, doc type (narrative vs reference), position in file

## Query Interface

- Accept free-text queries (natural language or keyword-style)
- Support multi-keyword queries in the style of `"auth middleware JWT"` — BM25 handles ranking across all terms natively
- Return top-K chunks with metadata (file, heading, score)

## Eval Pipeline

The retrieval quality should be validated with a small eval harness:

- **Input data:** Existing plan files (feature descriptions, task specs) serve as real-world queries
- **Candidate generation:** For each plan, run BM25 to retrieve the top ~25 candidate chunks
- **Relevance labelling:** Pass the plan + candidate chunks to a model to label which chunks are genuinely relevant
- **Metrics:** Recall@K and MRR — was the relevant chunk in the top 1/3/5 results?
- **Negatives:** Chunks surfaced by BM25 but labelled not relevant serve as hard negatives

This produces a labelled eval set from real tasks without manual annotation.

## Query Construction (for eval and production)

Two strategies worth comparing during evals:
1. **Full plan text** as the BM25 query (simple, works well)
2. **Keyword extraction** from the plan first, then query with extracted terms (tighter signal)

The eval pipeline should test both to determine which produces better recall on the actual plan corpus.

## Future: Semantic Search

BM25 alone may be sufficient at this corpus size. If evals reveal recall gaps — particularly on conceptual or paraphrased queries — the recommended addition is:

- **Embeddings:** Local ONNX model via [hugot](https://github.com/knights-analytics/hugot) (no API dependency); `multi-qa-minilm-l6-cos-v1` recommended over the standard MiniLM variant as it is trained for retrieval rather than symmetric similarity
- **Fusion:** Reciprocal Rank Fusion (RRF) to combine BM25 and vector results — parameter-free and robust
- **Reranking:** Optional cross-encoder reranker as a final pass on the merged candidate set

Start with BM25 only, run evals, and add semantic search only if the numbers justify it.
```