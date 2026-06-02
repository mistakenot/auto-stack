---
title: "ETL Pipeline Integrity and Merge Architecture"
summary: "Research on CRDT-based merge models, event-sourced pipelines, concurrent writer safety, schema evolution, and property-based testing for the autoetl output store"
read_when: "designing ETL merge semantics, hardening the pipeline, or planning S3 backend support"
---

# Research: ETL Pipeline Integrity and Merge Architecture

Status: Active research — not yet translated into requirements.

## Motivation

autoetl produces the **source of truth** for all downstream tools (search, reflect, skill). Today the pipeline has significant integrity gaps:

- Past session/message partitions are skipped if the file exists — no merge path for historical data
- Parquet writes go directly to the target path — crash mid-write corrupts the partition
- No backup, manifest, or integrity verification
- No model for concurrent writers (needed for multi-host or S3 scenarios)
- No strategy for schema evolution

As the system grows to multiple clients, providers, and storage backends, the pipeline must be formally sound — not just patched.

## Assumptions and Constraints

### Data Assumptions

These describe the fundamental properties of the data flowing through the pipeline:

1. **Every entity type has a stable, unique ID** — messages, sessions, git commits, PRs, etc. are all keyed by an ID that does not change after creation. IDs are the primary dedup key across all merge operations.

2. **Two-layer immutability model**:
   - **Raw layer (truly immutable)**: The original session files on disk (`~/.auto/etl/raw/`) never change after creation. They are the source artifacts — the ground truth.
   - **ETL layer (derived, may change)**: The parquet output is a *computed projection* — a deterministic function of `(raw data + parser version + schema version)`. Re-running the ETL with a newer parser on the same raw file may produce different output, and that's expected.
   - Immutability guarantees apply to raw source data. ETL output is a derived view that can be regenerated.

3. **Entity body content is stable at the raw layer** — after a raw session file is written, the content of each message within it does not change. A message ID always refers to the same underlying content in the raw file.

4. **Entity metadata at the ETL layer may evolve** — the same raw message may produce a richer ETL record over time (new fields extracted, better parsing). The ETL representation of an entity can change even though the raw source hasn't.

5. **Deletion is rare but must be supported** — unlikely in normal operation, but required for data protection (GDPR-style). Deletions must propagate through merge operations and not be silently undone by re-syncs.

6. **Client data may be incomplete** — clients may lose data, re-sync partial histories, or submit overlapping batches. The pipeline must be additive: a re-sync with fewer records must not delete records already in the store.

### Architectural Constraints

These describe the system-level requirements the architecture must satisfy:

7. **Multiple storage backends** — local disk is the default implementation. S3-compatible object storage is the long-term target. The architecture must support both through a common interface.

8. **Concurrent writer safety** — with remote storage, multiple hosts may push to the same store simultaneously. The architecture needs a reusable mechanism to prevent concurrent edits from clobbering each other.

9. **Two-stage pipeline is acceptable** — e.g. append-only batch ingestion followed by a merge/compaction stage, if it simplifies correctness and concurrency.

10. **Incremental-first** — the pipeline runs frequently; a typical run should only process new data. Fast incremental is more important than fast full rebuild.

11. **Full reindex must be supported** — equivalent of `--full`, re-parses raw files with the current parser. Since ETL output is derived (assumption 4), a full reindex may produce different records than the original parse. How this interacts with merge semantics needs more research.

12. **Schema evolution without breaking merges** — new fields may be added over time. Since ETL output is a derived projection (assumption 4), schema changes are a re-derivation, not a mutation of immutable records. But the merge model must handle mixed-version records gracefully (see Schema Evolution section).

## Current State

### What works

- **Git/GitHub writers** already use read-merge-write with ID-based dedup. Incoming rows win on conflict. Sync state files use atomic temp+rename.
- **Sync state tracking** exists for git (SHA-based seen set) and GitHub (per-PR sync info with retry logic).

### What doesn't

- **Session/message writers** skip past partitions entirely (`if !isCurrent && fileExists(path) { continue }`). No merge path.
- **All parquet writes** use direct `os.Create()` — no atomicity, no temp file + rename.
- **No backup or manifest** mechanism exists anywhere.
- **No dedup or conflict detection** for sessions/messages.
- **No concurrency model** — single-host, single-process assumption baked in.

## Formal Merge Model

### Messages as a G-Set

Messages are immutable after creation and keyed by a stable ID. This makes the message store a natural **G-Set (Grow-only Set)** — the simplest CRDT.

- **Merge operation**: set union by ID
- **Conflict resolution**: existing wins (if ID already present, keep existing; log the conflict)
- **Properties** (all formally provable for set union):
  - **Commutativity**: `merge(A, B) == merge(B, A)` — host order doesn't matter
  - **Associativity**: `merge(merge(A, B), C) == merge(A, merge(B, C))` — grouping doesn't matter
  - **Idempotency**: `merge(A, A) == A` — re-syncing same data is a no-op
  - **Monotonicity**: `|merge(A, B)| >= max(|A|, |B|)` — merge never loses data

These properties guarantee **convergence**: any number of hosts can merge in any order, any number of times, and arrive at the same final state.

### Sessions as LWW-Registers

Sessions are mutable — message counts and timestamps update as new messages arrive. This maps to a **LWW-Register (Last-Writer-Wins Register)** on temporal fields.

- **Merge operation**: for each session ID, take the version with the later `LastMessageAt`
- **Conflict resolution**: latest-wins on temporal fields (`LastMessageAt`, `MessageCount`, `Duration`, etc.)
- **Properties**:
  - Commutativity and idempotency hold (max is commutative and idempotent)
  - Temporal consistency: `merge(S1, S2).LastMessageAt == max(S1.LastMessageAt, S2.LastMessageAt)`

### Handling Deletion: 2P-Set or Tombstones

Pure G-Sets don't support deletion. Two options:

**Option A: 2P-Set (Two-Phase Set)**
- Maintain two sets: an add-set (G-Set of entities) and a remove-set (G-Set of deleted IDs)
- An entity is "present" if it's in the add-set and NOT in the remove-set
- Once deleted, an entity can never be re-added (which is fine for data protection deletions)
- Both sets are CRDTs, so the composite is also a CRDT

**Option B: Tombstone records**
- Instead of physical deletion, mark records with a `deleted_at` timestamp
- Merge treats tombstoned records as present but excluded from query results
- Periodically compact (physically remove) tombstoned records after a retention period
- Simpler to implement in parquet (just a column), but grows the dataset

**Recommendation**: Tombstone records (Option B) are simpler for parquet-based storage and align with the "entities are immutable" constraint. A `deleted_at` column is cheap in columnar format. Compaction can be a separate maintenance command.

### Summary Table

| Entity Type | CRDT Model | Merge Strategy | Deletion |
|-------------|-----------|----------------|----------|
| Messages | G-Set | Union by ID, existing wins, log conflicts | Tombstone (`deleted_at`) |
| Sessions | LWW-Register | Latest `LastMessageAt` wins | Tombstone |
| Git Commits | G-Set | Union by ID (already implemented) | Tombstone |
| Git Files/Hunks | G-Set | Union by ID (already implemented) | Tombstone |
| GitHub PRs | G-Set | Union by ID (already implemented) | Tombstone |
| PR Comments | G-Set | Union by ID, with full-PR refresh on retry (already implemented) | Tombstone |

## Pipeline Architecture Options

### Option 1: In-Place Read-Merge-Write (Current Direction)

Each `autoetl run` reads existing partitions, merges incoming data, writes back.

```
raw files → parse → merge with existing partition → write partition
```

**Pros**: Simple, single-pass, low disk overhead.
**Cons**: Concurrent writers can clobber each other. No audit trail of what changed when. A bug in merge logic can corrupt the store with no undo beyond backups.

### Option 2: Append-Only Event Batches + Compaction

Each run appends an immutable batch file. A separate compaction step materializes the merged view.

```
Stage 1: raw files → parse → write immutable batch file (never modified)
Stage 2: compact all batch files → materialized parquet partitions
```

**Pros**: 
- Writers never modify existing files — eliminates concurrent write conflicts on storage
- Full audit trail (every batch is preserved)
- Compaction is a pure function over immutable inputs — deterministic and repeatable
- S3-friendly: each batch is a new key, never an overwrite
- `--full` is just "re-compact from all batches" — clean semantic

**Cons**: 
- Two-stage pipeline is more complex
- Batch files accumulate disk (need pruning after compaction)
- Read latency slightly higher if you query batches directly before compaction

**Recommendation**: Option 2 is the stronger long-term architecture. It naturally solves concurrent writes, makes `--full` a compaction operation rather than a destructive rebuild, and the batch files serve as both an audit log and a backup mechanism (reducing the need for separate pre-write backups).

### How `--full` Works in Each Model

**Option 1 (in-place)**: Delete the output store, re-parse all raw files, write fresh partitions. Requires a pre-nuke backup to be safe. Destroys and rebuilds.

**Option 2 (event batches)**: Batches are never deleted. `--full` means "delete materialized views and re-compact from all batches." The source data (batches) is untouched. This is inherently safer — you can't lose data by recompacting.

**Open question**: In option 2, should `--full` also re-parse raw files into new batches, or only re-compact existing batches? If a parser bug was fixed, you'd want to re-parse. If only the compaction logic changed, re-compacting is sufficient. Perhaps two flags: `--reparse` (stage 1 redo) and `--recompact` (stage 2 redo), with `--full` doing both.

## Concurrent Writer Safety

### The Problem

Two hosts read the same partition, both merge their data, both write back. The second writer silently overwrites the first writer's additions.

### Solutions by Backend

**Local disk**: 
- Single-process assumption is probably fine for now
- File-level advisory locks (`flock`) for multi-process safety on the same machine
- For the event-batch model: no locking needed — each writer creates a new batch file with a unique name (host ID + timestamp + random suffix)

**S3-compatible storage**:
- **S3 conditional writes** (`If-None-Match` / `If-Match` on PutObject): write only succeeds if the ETag matches what you read. On conflict, retry with fresh read-merge cycle.
- For the event-batch model: each batch is a new S3 key — no overwrites, no conflicts. Only compaction needs coordination (single-writer or conditional write on the materialized view).

### StorageBackend Interface

Abstract the storage layer so merge logic doesn't care about the backend:

```
StorageBackend interface:
  Read(path) → bytes
  Write(path, bytes, opts) → error   // opts includes conditional write support
  List(prefix) → paths
  Delete(path) → error
  AtomicWrite(path, bytes) → error   // temp + rename on local, conditional put on S3
```

Local disk and S3 implement this interface differently. Concurrency strategy is part of the backend contract, not the merge logic.

## Schema Evolution

### The Tension

Entities are immutable after creation, but the schema evolves. If we add a new field (e.g. `token_count` on messages), old records won't have it. This creates three scenarios:

**Scenario A: New field with a sensible default**
- Add `token_count int64` with default `0`
- Old records read back with `token_count=0`
- No reprocessing needed
- **No conflict with immutability** — the record content didn't change, we just added a column with a default

**Scenario B: New field that can be backfilled from existing data**
- Add `content_hash string` computed from the `content` column
- Could be backfilled by reading existing records and computing the hash
- **Mild tension with immutability** — the record "changes" (gains a new field), but the original content is untouched
- In the event-batch model: backfill creates a new batch of "enrichment" records keyed by ID, compaction merges them

**Scenario C: New field that requires re-parsing raw data**
- Add `tool_name string` extracted from raw transcript parsing that the old parser didn't capture
- Requires re-running the parser on raw files to populate the field
- **Direct tension with immutability** — the "same" message now has different content after re-parsing
- This is effectively `--full --reparse`

### Proposed Rules

1. **All new fields must have a default value** — non-negotiable for schema compatibility
2. **Additive-only schema changes** — never remove or rename columns; deprecated columns get a `deprecated_` prefix and stop being populated
3. **Backfill via enrichment batches** (Option 2 model) — don't modify existing records; write new batch files that add fields to existing IDs. Compaction merges them.
4. **Re-parse is a separate, explicit operation** — `--reparse` creates new batch files from raw data. The old batches remain (audit trail). Compaction takes the latest batch per message ID.
5. **Schema version in manifests** — each batch/partition records the schema version it was written with. Compaction can detect mixed-version inputs.

### How This Interacts with CRDT Properties

The two-layer model clarifies this. The raw source files are immutable — a message ID always refers to the same underlying content. But the ETL representation of that message can change (new parser extracts more fields, schema adds columns, etc.).

This means two versions of the same message ID can legitimately differ — not because the source changed, but because the ETL derivation improved. The merge rule "existing wins" would keep the old, less-rich version — which is wrong.

**Resolution**: Introduce a `schema_version` (or `parser_version`) field on ETL output records. Merge rule becomes:

1. If IDs match and schema versions differ → keep the higher schema version (newer derivation wins)
2. If IDs match and schema versions are equal → existing wins (no source change, so re-sync is a no-op)

This preserves CRDT properties because `max(schema_version)` is commutative, associative, and idempotent. And it correctly handles the case where a `--full` reindex produces richer records than the original incremental parse.

**Implication for the event-batch model**: A `--full --reparse` creates new batch files at the current schema version. During compaction, the higher-version record wins per ID. Old batches at the previous schema version are effectively superseded but retained as audit trail.

## Verification: Property-Based Testing

The CRDT properties are algebraic and can be verified with property-based tests rather than exhaustive scenario tests.

### Core Property Tests

| Property | Test | What it proves |
|----------|------|----------------|
| Commutativity | `merge(A, B) == merge(B, A)` for random A, B | Host ordering doesn't matter |
| Associativity | `merge(merge(A, B), C) == merge(A, merge(B, C))` for random A, B, C | Grouping doesn't matter |
| Idempotency | `merge(A, A) == A` for random A | Re-sync is safe |
| Monotonicity | `len(merge(A, B)) >= max(len(A), len(B))` | No data loss |
| Convergence | Merge N random batches in all permutations → same result | Multi-host safety |
| Schema upgrade | merge(v1_record, v2_record) keeps v2 | Backfill correctness |
| Tombstone | merge(record, tombstone) → tombstoned | Deletion propagates |

### Testing Layers

1. **Pure function tests** — test `mergeMessages()`, `mergeSessions()` as pure functions with property-based generators (Go `testing/quick` or `gopter`). Fast, no I/O.
2. **Serialization round-trip** — write to parquet, read back, merge, verify properties hold through serialization. Catches encoding bugs.
3. **Concurrent stress tests** — N goroutines writing batches through the real writer path, assert convergence on final state.
4. **Fault injection** — wrap StorageBackend with a decorator that simulates failures (crash mid-write, partial upload, corrupt read). Verify recovery.

## Open Research Questions

- [ ] **Event batch format**: What format for batch files? Parquet (same as output), JSONL (human-readable), or MessagePack (compact)? Trade-offs between debuggability, size, and parse speed.
- [ ] **Compaction trigger**: Should compaction run automatically after ingestion, or be a separate command? If automatic, should it be lazy (compact only partitions with new batches)?
- [ ] **Batch pruning**: After compaction, can old batches be deleted? Or do we keep them as an audit trail? If pruned, after how long?
- [ ] **`--full` semantics in event model**: `--reparse` + `--recompact` as separate flags? Or `--full` always does both?
- [ ] **Manifest scope**: Per-partition manifest (current thinking) vs. global manifest (single file listing all partitions). Per-partition is more granular but more files.
- [ ] **Tombstone compaction**: When and how to physically remove tombstoned records? Separate maintenance command? Time-based retention?
- [ ] **Schema version registry**: Where to store the schema version history? In the code? In a metadata file in the output store?
- [ ] **Backward-compatible readers**: If a downstream tool reads a partition written with schema v2 but only understands v1, should it gracefully ignore unknown columns or fail?
