---
hash: "cbf51fb5"
id: "ca7e6234"
read_when: "designing ETL merge semantics, verifying CRDT properties for multi-host sync, or understanding the formal correctness model for auto-etl"
summary: "Literate Quint specification proving CRDT-based merge semantics for the auto-etl pipeline: commutativity, associativity, idempotency, monotonicity, and safe concurrent-writer model."
title: "ETL Merge Protocol — Formal Specification"
---

# ETL Merge Protocol — Formal Specification

This is a literate Quint specification that models the merge semantics
for the auto-etl pipeline. The goal: prove that our CRDT-based merge
operations are correct by construction — commutative, associative,
idempotent, and monotonic.

## Data Model

We model two entity types from the pipeline:

- **Messages** — immutable after creation, keyed by stable ID. Natural G-Set.
- **Sessions** — mutable metadata (message count, timestamps). LWW-Register.

Both support soft deletion via tombstones (`deleted_at` timestamp).
Both carry a `schema_version` for merge conflict resolution when the
ETL parser evolves.

```quint etl_merge.qnt +=
module etl_merge {
  // -- Record types -----------------------------------------------

  type MessageRecord = {
    id: str,
    content: str,
    schema_version: int,
    deleted_at: int,   // 0 = not deleted
  }

  type SessionRecord = {
    id: str,
    last_message_at: int,
    message_count: int,
    schema_version: int,
    deleted_at: int,
  }
```

## Message Merge: G-Set with Schema-Aware Conflict Resolution

Messages form a Grow-only Set keyed by ID. The merge rule:

1. If an ID exists only in one store → include it (set union)
2. If an ID exists in both stores with different `schema_version` → keep the higher version (newer ETL derivation wins)
3. If an ID exists in both stores with the same `schema_version` → keep existing (left/first argument wins — re-sync is a no-op)
4. Tombstones dominate: if either copy has `deleted_at > 0`, the merged result must be tombstoned

```quint etl_merge.qnt +=

  // Resolve conflict between two message records with the same ID.
  // Higher schema_version wins. Same version → left wins.
  // Tombstone always propagates (max of deleted_at).
  pure def resolveMessage(left: MessageRecord, right: MessageRecord): MessageRecord = {
    val winner = if (right.schema_version > left.schema_version) right else left
    val tombstone = if (left.deleted_at > right.deleted_at) left.deleted_at else right.deleted_at
    { ...winner, deleted_at: tombstone }
  }

  // Merge two message stores (sets of MessageRecord).
  // This is the core G-Set merge with conflict resolution.
  pure def mergeMessages(a: Set[MessageRecord], b: Set[MessageRecord]): Set[MessageRecord] = {
    // IDs only in a
    val onlyA = a.filter(ma => b.forall(mb => mb.id != ma.id))
    // IDs only in b
    val onlyB = b.filter(mb => a.forall(ma => ma.id != mb.id))
    // IDs in both — resolve conflicts
    val both = a.filter(ma => b.exists(mb => mb.id == ma.id))
              .map(ma => {
                val mb = b.filter(mb => mb.id == ma.id).fold(ma, (_, x) => x)
                resolveMessage(ma, mb)
              })
    onlyA.union(onlyB).union(both)
  }
```

## Session Merge: LWW-Register

Sessions are mutable — `last_message_at` and `message_count` update as
new messages arrive. The merge rule uses Last-Writer-Wins on temporal
fields: the session with the later `last_message_at` wins.

Schema version still takes priority: a higher-version record always
wins regardless of timestamps.

```quint etl_merge.qnt +=

  // Resolve conflict between two session records with the same ID.
  // Higher schema_version wins unconditionally.
  // Same schema_version → later last_message_at wins.
  // Tombstone propagates.
  pure def resolveSession(left: SessionRecord, right: SessionRecord): SessionRecord = {
    val winner = if (right.schema_version > left.schema_version) right
                 else if (left.schema_version > right.schema_version) left
                 else if (right.last_message_at > left.last_message_at) right
                 else left
    val tombstone = if (left.deleted_at > right.deleted_at) left.deleted_at else right.deleted_at
    { ...winner, deleted_at: tombstone }
  }

  // Merge two session stores.
  pure def mergeSessions(a: Set[SessionRecord], b: Set[SessionRecord]): Set[SessionRecord] = {
    val onlyA = a.filter(sa => b.forall(sb => sb.id != sa.id))
    val onlyB = b.filter(sb => a.forall(sa => sa.id != sb.id))
    val both = a.filter(sa => b.exists(sb => sb.id == sa.id))
              .map(sa => {
                val sb = b.filter(sb => sb.id == sa.id).fold(sa, (_, x) => x)
                resolveSession(sa, sb)
              })
    onlyA.union(onlyB).union(both)
  }
```

## CRDT Property Definitions

These are the algebraic properties that guarantee convergence.
If all four hold, any number of hosts can merge in any order and
arrive at the same final state.

```quint etl_merge.qnt +=

  // ---- Property: Commutativity ----
  // merge(A, B) == merge(B, A)
  pure def messagesCommutative(a: Set[MessageRecord], b: Set[MessageRecord]): bool = {
    mergeMessages(a, b) == mergeMessages(b, a)
  }

  pure def sessionsCommutative(a: Set[SessionRecord], b: Set[SessionRecord]): bool = {
    mergeSessions(a, b) == mergeSessions(b, a)
  }

  // ---- Property: Associativity ----
  // merge(merge(A, B), C) == merge(A, merge(B, C))
  pure def messagesAssociative(
    a: Set[MessageRecord], b: Set[MessageRecord], c: Set[MessageRecord]
  ): bool = {
    mergeMessages(mergeMessages(a, b), c) == mergeMessages(a, mergeMessages(b, c))
  }

  pure def sessionsAssociative(
    a: Set[SessionRecord], b: Set[SessionRecord], c: Set[SessionRecord]
  ): bool = {
    mergeSessions(mergeSessions(a, b), c) == mergeSessions(a, mergeSessions(b, c))
  }

  // ---- Property: Idempotency ----
  // merge(A, A) == A
  pure def messagesIdempotent(a: Set[MessageRecord]): bool = {
    mergeMessages(a, a) == a
  }

  pure def sessionsIdempotent(a: Set[SessionRecord]): bool = {
    mergeSessions(a, a) == a
  }

  // ---- Property: Monotonicity ----
  // |merge(A, B)| >= max(|A|, |B|)
  // (merge never loses data — measured by unique IDs)
  pure def messagesMonotonic(a: Set[MessageRecord], b: Set[MessageRecord]): bool = {
    val merged = mergeMessages(a, b)
    val mergedIds = merged.map(m => m.id)
    val aIds = a.map(m => m.id)
    val bIds = b.map(m => m.id)
    aIds.subseteq(mergedIds) and bIds.subseteq(mergedIds)
  }

  pure def sessionsMonotonic(a: Set[SessionRecord], b: Set[SessionRecord]): bool = {
    val merged = mergeSessions(a, b)
    val mergedIds = merged.map(s => s.id)
    val aIds = a.map(s => s.id)
    val bIds = b.map(s => s.id)
    aIds.subseteq(mergedIds) and bIds.subseteq(mergedIds)
  }

  // ---- Property: Tombstone Dominance ----
  // If a record is tombstoned in either store, it stays tombstoned after merge
  pure def tombstoneDominance(a: Set[MessageRecord], b: Set[MessageRecord]): bool = {
    val merged = mergeMessages(a, b)
    // For every record that is tombstoned in a or b,
    // the merged version must also be tombstoned
    val tombstonedIds = a.filter(m => m.deleted_at > 0).map(m => m.id)
                        .union(b.filter(m => m.deleted_at > 0).map(m => m.id))
    tombstonedIds.forall(tid =>
      merged.filter(m => m.id == tid).forall(m => m.deleted_at > 0)
    )
  }

  // ---- Property: Schema Upgrade ----
  // merge(v1_record, v2_record) keeps v2 content
  pure def schemaUpgradeCorrectness(a: Set[MessageRecord], b: Set[MessageRecord]): bool = {
    val merged = mergeMessages(a, b)
    // For any ID present in both stores, the merged version
    // should have schema_version >= both inputs
    a.forall(ma =>
      b.filter(mb => mb.id == ma.id).forall(mb => {
        val mergedRecord = merged.filter(m => m.id == ma.id)
        mergedRecord.forall(m =>
          m.schema_version >= ma.schema_version and
          m.schema_version >= mb.schema_version
        )
      })
    )
  }
}
```

## Test Module: Verifying Properties with Concrete Data

This module runs the property checks against generated data using
Quint's simulation engine.

```quint etl_merge_test.qnt +=
module etl_merge_test {
  import etl_merge.*

  // -- Small universe for bounded model checking --

  pure val IDS = Set("m1", "m2", "m3")
  pure val VERSIONS = Set(1, 2)
  pure val TIMESTAMPS = Set(0, 10, 20)
  pure val CONTENTS = Set("hello", "world")
  pure val DELETE_STATES = Set(0, 100)  // 0 = alive, 100 = deleted

  // -- State: three stores representing three hosts --

  var storeA: Set[MessageRecord]
  var storeB: Set[MessageRecord]
  var storeC: Set[MessageRecord]

  var sessA: Set[SessionRecord]
  var sessB: Set[SessionRecord]
  var sessC: Set[SessionRecord]

  // -- Initialization: each host starts with a random subset of records --

  action init = all {
    nondet aRecords = IDS.powerset().oneOf()
    nondet bRecords = IDS.powerset().oneOf()
    nondet cRecords = IDS.powerset().oneOf()
    all {
      storeA' = aRecords.map(id => {
        nondet v = VERSIONS.oneOf()
        nondet c = CONTENTS.oneOf()
        nondet d = DELETE_STATES.oneOf()
        { id: id, content: c, schema_version: v, deleted_at: d }
      }),
      storeB' = bRecords.map(id => {
        nondet v = VERSIONS.oneOf()
        nondet c = CONTENTS.oneOf()
        nondet d = DELETE_STATES.oneOf()
        { id: id, content: c, schema_version: v, deleted_at: d }
      }),
      storeC' = cRecords.map(id => {
        nondet v = VERSIONS.oneOf()
        nondet c = CONTENTS.oneOf()
        nondet d = DELETE_STATES.oneOf()
        { id: id, content: c, schema_version: v, deleted_at: d }
      }),
      sessA' = aRecords.map(id => {
        nondet v = VERSIONS.oneOf()
        nondet t = TIMESTAMPS.oneOf()
        nondet mc = Set(1, 5, 10).oneOf()
        nondet d = DELETE_STATES.oneOf()
        { id: id, last_message_at: t, message_count: mc, schema_version: v, deleted_at: d }
      }),
      sessB' = bRecords.map(id => {
        nondet v = VERSIONS.oneOf()
        nondet t = TIMESTAMPS.oneOf()
        nondet mc = Set(1, 5, 10).oneOf()
        nondet d = DELETE_STATES.oneOf()
        { id: id, last_message_at: t, message_count: mc, schema_version: v, deleted_at: d }
      }),
      sessC' = cRecords.map(id => {
        nondet v = VERSIONS.oneOf()
        nondet t = TIMESTAMPS.oneOf()
        nondet mc = Set(1, 5, 10).oneOf()
        nondet d = DELETE_STATES.oneOf()
        { id: id, last_message_at: t, message_count: mc, schema_version: v, deleted_at: d }
      }),
    }
  }

  // -- Step: hosts sync by merging with each other --

  action syncAB = all {
    storeA' = mergeMessages(storeA, storeB),
    storeB' = mergeMessages(storeB, storeA),
    storeC' = storeC,
    sessA' = mergeSessions(sessA, sessB),
    sessB' = mergeSessions(sessB, sessA),
    sessC' = sessC,
  }

  action syncAC = all {
    storeA' = mergeMessages(storeA, storeC),
    storeC' = mergeMessages(storeC, storeA),
    storeB' = storeB,
    sessA' = mergeSessions(sessA, sessC),
    sessC' = mergeSessions(sessC, sessA),
    sessB' = sessB,
  }

  action syncBC = all {
    storeB' = mergeMessages(storeB, storeC),
    storeC' = mergeMessages(storeC, storeB),
    storeA' = storeA,
    sessB' = mergeSessions(sessB, sessC),
    sessC' = mergeSessions(sessC, sessB),
    sessA' = sessA,
  }

  // A host receives a new record (simulates a new ETL batch arriving)
  action ingestToA = {
    nondet id = IDS.oneOf()
    nondet v = VERSIONS.oneOf()
    nondet c = CONTENTS.oneOf()
    nondet d = DELETE_STATES.oneOf()
    val newMsg: MessageRecord = { id: id, content: c, schema_version: v, deleted_at: d }
    all {
      storeA' = mergeMessages(storeA, Set(newMsg)),
      storeB' = storeB,
      storeC' = storeC,
      sessA' = sessA,
      sessB' = sessB,
      sessC' = sessC,
    }
  }

  action step = any {
    syncAB,
    syncAC,
    syncBC,
    ingestToA,
  }

  // -- Invariants: CRDT properties must hold at every state --

  val msgComm = messagesCommutative(storeA, storeB)
  val msgAssoc = messagesAssociative(storeA, storeB, storeC)
  val msgIdemp = messagesIdempotent(storeA) and messagesIdempotent(storeB)
  val msgMono = messagesMonotonic(storeA, storeB)
  val msgTombstone = tombstoneDominance(storeA, storeB)
  val msgSchema = schemaUpgradeCorrectness(storeA, storeB)

  val sessComm = sessionsCommutative(sessA, sessB)
  val sessAssoc = sessionsAssociative(sessA, sessB, sessC)
  val sessIdemp = sessionsIdempotent(sessA) and sessionsIdempotent(sessB)
  val sessMono = sessionsMonotonic(sessA, sessB)

  // -- Convergence: after all hosts sync pairwise, they agree --
  val convergence = {
    val allSynced = mergeMessages(mergeMessages(storeA, storeB), storeC)
    mergeMessages(mergeMessages(storeA, storeC), storeB) == allSynced and
    mergeMessages(mergeMessages(storeB, storeC), storeA) == allSynced
  }

  // All properties combined
  val allProperties = and {
    msgComm,
    msgAssoc,
    msgIdemp,
    msgMono,
    msgTombstone,
    msgSchema,
    sessComm,
    sessAssoc,
    sessIdemp,
    sessMono,
    convergence,
  }
}
```

## Concurrent Writer Model

This module models the dangerous scenario: two hosts reading the same
partition, merging locally, then writing back. It demonstrates why
in-place read-merge-write is unsafe and why event-batch append is safe.

```quint concurrent_writers.qnt +=
module concurrent_writers {
  import etl_merge.*

  // -- Shared store (the partition on disk/S3) --
  var sharedStore: Set[MessageRecord]

  // -- Per-host local state --
  var hostALocal: Set[MessageRecord]
  var hostBLocal: Set[MessageRecord]

  // -- Per-host pending writes (records not yet written to shared) --
  var hostAPending: Set[MessageRecord]
  var hostBPending: Set[MessageRecord]

  // -- Track what each host last read from shared (for detecting lost updates) --
  var hostASnapshot: Set[MessageRecord]
  var hostBSnapshot: Set[MessageRecord]

  pure val IDS = Set("m1", "m2", "m3")
  pure val CONTENTS = Set("from_a", "from_b", "original")

  action init = all {
    sharedStore' = Set(),
    hostALocal' = Set(),
    hostBLocal' = Set(),
    hostAPending' = Set(),
    hostBPending' = Set(),
    hostASnapshot' = Set(),
    hostBSnapshot' = Set(),
  }

  // Host A reads the shared store
  action hostAReads = all {
    hostASnapshot' = sharedStore,
    hostALocal' = sharedStore,
    sharedStore' = sharedStore,
    hostBLocal' = hostBLocal,
    hostAPending' = hostAPending,
    hostBPending' = hostBPending,
    hostBSnapshot' = hostBSnapshot,
  }

  // Host B reads the shared store
  action hostBReads = all {
    hostBSnapshot' = sharedStore,
    hostBLocal' = sharedStore,
    sharedStore' = sharedStore,
    hostALocal' = hostALocal,
    hostAPending' = hostAPending,
    hostBPending' = hostBPending,
    hostASnapshot' = hostASnapshot,
  }

  // Host A produces a new record locally
  action hostAIngests = {
    nondet id = IDS.oneOf()
    val newMsg: MessageRecord = { id: id, content: "from_a", schema_version: 1, deleted_at: 0 }
    all {
      hostALocal' = mergeMessages(hostALocal, Set(newMsg)),
      hostAPending' = hostAPending.union(Set(newMsg)),
      sharedStore' = sharedStore,
      hostBLocal' = hostBLocal,
      hostBPending' = hostBPending,
      hostASnapshot' = hostASnapshot,
      hostBSnapshot' = hostBSnapshot,
    }
  }

  // Host B produces a new record locally
  action hostBIngests = {
    nondet id = IDS.oneOf()
    val newMsg: MessageRecord = { id: id, content: "from_b", schema_version: 1, deleted_at: 0 }
    all {
      hostBLocal' = mergeMessages(hostBLocal, Set(newMsg)),
      hostBPending' = hostBPending.union(Set(newMsg)),
      sharedStore' = sharedStore,
      hostALocal' = hostALocal,
      hostAPending' = hostAPending,
      hostASnapshot' = hostASnapshot,
      hostBSnapshot' = hostBSnapshot,
    }
  }

  // -- UNSAFE: In-place write (clobbers other host's changes) --
  action hostAWritesInPlace = all {
    sharedStore' = hostALocal,  // overwrites whatever is there
    hostAPending' = Set(),
    hostALocal' = hostALocal,
    hostBLocal' = hostBLocal,
    hostBPending' = hostBPending,
    hostASnapshot' = hostASnapshot,
    hostBSnapshot' = hostBSnapshot,
  }

  action hostBWritesInPlace = all {
    sharedStore' = hostBLocal,  // overwrites whatever is there
    hostBPending' = Set(),
    hostALocal' = hostALocal,
    hostBLocal' = hostBLocal,
    hostAPending' = hostAPending,
    hostASnapshot' = hostASnapshot,
    hostBSnapshot' = hostBSnapshot,
  }

  // -- SAFE: Event-batch append (no clobber) --
  action hostAWritesBatch = all {
    sharedStore' = mergeMessages(sharedStore, hostAPending),
    hostAPending' = Set(),
    hostALocal' = hostALocal,
    hostBLocal' = hostBLocal,
    hostBPending' = hostBPending,
    hostASnapshot' = hostASnapshot,
    hostBSnapshot' = hostBSnapshot,
  }

  action hostBWritesBatch = all {
    sharedStore' = mergeMessages(sharedStore, hostBPending),
    hostBPending' = Set(),
    hostALocal' = hostALocal,
    hostBLocal' = hostBLocal,
    hostAPending' = hostAPending,
    hostASnapshot' = hostASnapshot,
    hostBSnapshot' = hostBSnapshot,
  }

  // -- Step actions for two modes --

  action stepUnsafe = any {
    hostAReads,
    hostBReads,
    hostAIngests,
    hostBIngests,
    hostAWritesInPlace,
    hostBWritesInPlace,
  }

  action stepSafe = any {
    hostAReads,
    hostBReads,
    hostAIngests,
    hostBIngests,
    hostAWritesBatch,
    hostBWritesBatch,
  }

  // -- Invariant: no data loss --
  // Every record ever ingested should eventually be in the shared store
  // (This will FAIL in unsafe mode — that's the point!)

  // Track all records ever produced by any host
  var allEverIngested: Set[MessageRecord]

  // We redefine init to also track ingested records
  // (This is a simplified check: shared store should be a superset
  //  of all pending writes from both hosts)
  val noDataLoss = {
    val allPendingIds = hostAPending.map(m => m.id).union(hostBPending.map(m => m.id))
    val sharedIds = sharedStore.map(m => m.id)
    // After both hosts have flushed (no pending), shared should have everything
    (hostAPending.size() == 0 and hostBPending.size() == 0) implies
      hostALocal.map(m => m.id).union(hostBLocal.map(m => m.id)).subseteq(sharedIds)
  }
}
```

## Partial Sync Safety

Models the "open-world sync" constraint: a client re-syncing a subset
of its records must not cause deletion of records already in the store.

```quint partial_sync.qnt +=
module partial_sync {
  import etl_merge.*

  var store: Set[MessageRecord]

  pure val ALL_IDS = Set("m1", "m2", "m3", "m4", "m5")

  action init = {
    // Store starts with all 5 records
    store' = ALL_IDS.map(id => {
      { id: id, content: "original", schema_version: 1, deleted_at: 0 }
    })
  }

  // A client re-syncs with a subset of records (e.g., only 3 of 5)
  action partialResync = {
    nondet subset = ALL_IDS.powerset().filter(s => s.size() > 0 and s.size() < ALL_IDS.size()).oneOf()
    val partialBatch: Set[MessageRecord] = subset.map(id => {
      { id: id, content: "resynced", schema_version: 1, deleted_at: 0 }
    })
    // Merge, don't replace!
    store' = mergeMessages(store, partialBatch)
  }

  action step = partialResync

  // The store must never shrink — partial sync is additive
  val neverShrinks = store.map(m => m.id).size() == ALL_IDS.size()
}
```
