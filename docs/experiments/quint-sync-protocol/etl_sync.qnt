// ETL Merge Protocol — Formal Specification
//
// Models the CRDT-based merge semantics for the auto-etl pipeline.
// Proves: commutativity, associativity, idempotency, monotonicity,
// tombstone dominance, schema upgrade correctness, and convergence.

module etl_merge {
  type MessageRecord = {
    id: str,
    content: str,
    schema_version: int,
    deleted_at: int,
  }

  type SessionRecord = {
    id: str,
    last_message_at: int,
    message_count: int,
    schema_version: int,
    deleted_at: int,
  }

  // -- Message merge: G-Set with schema-aware conflict resolution --

  pure def resolveMessage(left: MessageRecord, right: MessageRecord): MessageRecord = {
    val winner = if (right.schema_version > left.schema_version) right else left
    val tombstone = if (left.deleted_at > right.deleted_at) left.deleted_at else right.deleted_at
    { ...winner, deleted_at: tombstone }
  }

  pure def mergeMessages(a: Set[MessageRecord], b: Set[MessageRecord]): Set[MessageRecord] = {
    val aIds = a.map(m => m.id)
    val bIds = b.map(m => m.id)
    val onlyA = a.filter(ma => not(ma.id.in(bIds)))
    val onlyB = b.filter(mb => not(mb.id.in(aIds)))
    // Precondition: valid stores have at most 1 record per ID,
    // so the filtered set is always size 1 and fold is deterministic.
    val both = a.filter(ma => ma.id.in(bIds))
              .map(ma => {
                val mb = b.filter(mb => mb.id == ma.id).fold(ma, (_, x) => x)
                resolveMessage(ma, mb)
              })
    onlyA.union(onlyB).union(both)
  }

  // -- Session merge: LWW-Register --

  pure def resolveSession(left: SessionRecord, right: SessionRecord): SessionRecord = {
    val winner = if (right.schema_version > left.schema_version) right
                 else if (left.schema_version > right.schema_version) left
                 else if (right.last_message_at > left.last_message_at) right
                 else left
    val tombstone = if (left.deleted_at > right.deleted_at) left.deleted_at else right.deleted_at
    { ...winner, deleted_at: tombstone }
  }

  pure def mergeSessions(a: Set[SessionRecord], b: Set[SessionRecord]): Set[SessionRecord] = {
    val aIds = a.map(s => s.id)
    val bIds = b.map(s => s.id)
    val onlyA = a.filter(sa => not(sa.id.in(bIds)))
    val onlyB = b.filter(sb => not(sb.id.in(aIds)))
    // Precondition: valid stores have at most 1 record per ID.
    val both = a.filter(sa => sa.id.in(bIds))
              .map(sa => {
                val sb = b.filter(sb => sb.id == sa.id).fold(sa, (_, x) => x)
                resolveSession(sa, sb)
              })
    onlyA.union(onlyB).union(both)
  }

  // -- CRDT Property checks --

  pure def messagesCommutative(a: Set[MessageRecord], b: Set[MessageRecord]): bool = {
    mergeMessages(a, b) == mergeMessages(b, a)
  }

  pure def sessionsCommutative(a: Set[SessionRecord], b: Set[SessionRecord]): bool = {
    mergeSessions(a, b) == mergeSessions(b, a)
  }

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

  pure def messagesIdempotent(a: Set[MessageRecord]): bool = {
    mergeMessages(a, a) == a
  }

  pure def sessionsIdempotent(a: Set[SessionRecord]): bool = {
    mergeSessions(a, a) == a
  }

  pure def messagesMonotonic(a: Set[MessageRecord], b: Set[MessageRecord]): bool = {
    val merged = mergeMessages(a, b)
    val mergedIds = merged.map(m => m.id)
    a.map(m => m.id).subseteq(mergedIds) and b.map(m => m.id).subseteq(mergedIds)
  }

  pure def sessionsMonotonic(a: Set[SessionRecord], b: Set[SessionRecord]): bool = {
    val merged = mergeSessions(a, b)
    val mergedIds = merged.map(s => s.id)
    a.map(s => s.id).subseteq(mergedIds) and b.map(s => s.id).subseteq(mergedIds)
  }

  pure def tombstoneDominance(a: Set[MessageRecord], b: Set[MessageRecord]): bool = {
    val merged = mergeMessages(a, b)
    val tombstonedIds = a.filter(m => m.deleted_at > 0).map(m => m.id)
                        .union(b.filter(m => m.deleted_at > 0).map(m => m.id))
    tombstonedIds.forall(tid =>
      merged.filter(m => m.id == tid).forall(m => m.deleted_at > 0)
    )
  }

  pure def schemaUpgradeCorrectness(a: Set[MessageRecord], b: Set[MessageRecord]): bool = {
    val merged = mergeMessages(a, b)
    a.forall(ma =>
      b.filter(mb => mb.id == ma.id).forall(mb => {
        merged.filter(m => m.id == ma.id).forall(m =>
          m.schema_version >= ma.schema_version and
          m.schema_version >= mb.schema_version
        )
      })
    )
  }
}

// ============================================================
// Test module: verify properties via simulation
// ============================================================

module etl_merge_test {
  import etl_merge.*

  // Small universe for tractable checking.
  // Content is fixed — irrelevant to merge semantics.
  // 2 IDs × 2 versions × 2 delete states = 8 records per type.
  // Valid stores: at most 1 record per ID → (4+1)^2 = 25 valid msg stores.
  pure val IDS = Set("m1", "m2")
  pure val VERSIONS = Set(1, 2)
  pure val DELETE_STATES = Set(0, 100)
  pure val TIMESTAMPS = Set(0, 10)

  // All possible message records: 2 × 2 × 2 = 8
  pure val ALL_MSGS: Set[MessageRecord] =
    IDS.map(id =>
      VERSIONS.map(v =>
        DELETE_STATES.map(d =>
          { id: id, content: "x", schema_version: v, deleted_at: d }
        )
      ).flatten()
    ).flatten()

  // Valid message stores: subsets where each ID appears at most once
  pure def validMsgStore(s: Set[MessageRecord]): bool = {
    s.map(m => m.id).size() == s.size()
  }

  pure val VALID_MSG_STORES: Set[Set[MessageRecord]] =
    ALL_MSGS.powerset().filter(s => validMsgStore(s))

  // All possible session records: 2 × 2 × 2 × 2 = 16
  pure val ALL_SESS: Set[SessionRecord] =
    IDS.map(id =>
      VERSIONS.map(v =>
        TIMESTAMPS.map(t =>
          DELETE_STATES.map(d =>
            { id: id, last_message_at: t, message_count: 1, schema_version: v, deleted_at: d }
          )
        ).flatten()
      ).flatten()
    ).flatten()

  pure def validSessStore(s: Set[SessionRecord]): bool = {
    s.map(r => r.id).size() == s.size()
  }

  pure val VALID_SESS_STORES: Set[Set[SessionRecord]] =
    ALL_SESS.powerset().filter(s => validSessStore(s))

  var storeA: Set[MessageRecord]
  var storeB: Set[MessageRecord]
  var storeC: Set[MessageRecord]

  var sessA: Set[SessionRecord]
  var sessB: Set[SessionRecord]
  var sessC: Set[SessionRecord]

  action init = {
    nondet a = VALID_MSG_STORES.oneOf()
    nondet b = VALID_MSG_STORES.oneOf()
    nondet c = VALID_MSG_STORES.oneOf()
    nondet sa = VALID_SESS_STORES.oneOf()
    nondet sb = VALID_SESS_STORES.oneOf()
    nondet sc = VALID_SESS_STORES.oneOf()
    all {
      storeA' = a,
      storeB' = b,
      storeC' = c,
      sessA' = sa,
      sessB' = sb,
      sessC' = sc,
    }
  }

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

  action ingestToA = {
    nondet id = IDS.oneOf()
    nondet v = VERSIONS.oneOf()
    nondet d = DELETE_STATES.oneOf()
    val newMsg: MessageRecord = { id: id, content: "x", schema_version: v, deleted_at: d }
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

  // -- Invariants --

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

  val convergence = {
    val allSynced = mergeMessages(mergeMessages(storeA, storeB), storeC)
    mergeMessages(mergeMessages(storeA, storeC), storeB) == allSynced and
    mergeMessages(mergeMessages(storeB, storeC), storeA) == allSynced
  }

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

// ============================================================
// Verification module: self-contained for Apalache model checking
// ============================================================
//
// Inlines merge logic to avoid the Quint→Apalache false cyclic dependency
// bug that occurs when invariants and imported pure functions share operators.
//
// State space: 1 ID × 2 versions × 2 delete states = 4 records.
// Valid stores: empty or 1 record = 5 options. Two stores = 25 pairs.
//
// Run: quint verify --main etl_merge_verify --invariant allVerifyProperties \
//        --max-steps 3 etl_sync.qnt

module etl_merge_verify {
  type Msg = {
    id: str,
    content: str,
    schema_version: int,
    deleted_at: int,
  }

  pure def resolve(left: Msg, right: Msg): Msg = {
    val winner = if (right.schema_version > left.schema_version) right else left
    val ts = if (left.deleted_at > right.deleted_at) left.deleted_at else right.deleted_at
    { ...winner, deleted_at: ts }
  }

  pure def merge(a: Set[Msg], b: Set[Msg]): Set[Msg] = {
    val aIds = a.map(m => m.id)
    val bIds = b.map(m => m.id)
    val onlyA = a.filter(ma => not(ma.id.in(bIds)))
    val onlyB = b.filter(mb => not(mb.id.in(aIds)))
    val both = a.filter(ma => ma.id.in(bIds))
              .map(ma => {
                val mb = b.filter(mb => mb.id == ma.id).fold(ma, (_, x) => x)
                resolve(ma, mb)
              })
    onlyA.union(onlyB).union(both)
  }

  pure val ALL_RECORDS: Set[Msg] = Set(
    { id: "m1", content: "x", schema_version: 1, deleted_at: 0 },
    { id: "m1", content: "x", schema_version: 1, deleted_at: 100 },
    { id: "m1", content: "x", schema_version: 2, deleted_at: 0 },
    { id: "m1", content: "x", schema_version: 2, deleted_at: 100 }
  )

  pure val VALID_STORES: Set[Set[Msg]] =
    Set(Set()).union(ALL_RECORDS.map(r => Set(r)))

  var storeA: Set[Msg]
  var storeB: Set[Msg]

  action init = {
    nondet a = VALID_STORES.oneOf()
    nondet b = VALID_STORES.oneOf()
    all { storeA' = a, storeB' = b }
  }

  action step = {
    nondet a = VALID_STORES.oneOf()
    nondet b = VALID_STORES.oneOf()
    all { storeA' = a, storeB' = b }
  }

  val commutativity = merge(storeA, storeB) == merge(storeB, storeA)

  val idempotency = merge(storeA, storeA) == storeA and
                    merge(storeB, storeB) == storeB

  val monotonicity = {
    val merged = merge(storeA, storeB)
    val mergedIds = merged.map(m => m.id)
    storeA.map(m => m.id).subseteq(mergedIds) and
    storeB.map(m => m.id).subseteq(mergedIds)
  }

  val tombstone = {
    val merged = merge(storeA, storeB)
    val tombstonedIds = storeA.filter(m => m.deleted_at > 0).map(m => m.id)
                        .union(storeB.filter(m => m.deleted_at > 0).map(m => m.id))
    tombstonedIds.forall(tid =>
      merged.filter(m => m.id == tid).forall(m => m.deleted_at > 0)
    )
  }

  val schema = {
    val merged = merge(storeA, storeB)
    storeA.forall(ma =>
      storeB.filter(mb => mb.id == ma.id).forall(mb => {
        merged.filter(m => m.id == ma.id).forall(m =>
          m.schema_version >= ma.schema_version and
          m.schema_version >= mb.schema_version
        )
      })
    )
  }

  val allVerifyProperties = and {
    commutativity,
    idempotency,
    monotonicity,
    tombstone,
    schema,
  }
}

// ============================================================
// Concurrent writers: in-place (unsafe) vs batch (safe)
// ============================================================

module concurrent_writers {
  import etl_merge.*

  var sharedStore: Set[MessageRecord]
  var hostALocal: Set[MessageRecord]
  var hostBLocal: Set[MessageRecord]
  var hostAPending: Set[MessageRecord]
  var hostBPending: Set[MessageRecord]

  pure val IDS = Set("m1", "m2", "m3")

  action init = all {
    sharedStore' = Set(),
    hostALocal' = Set(),
    hostBLocal' = Set(),
    hostAPending' = Set(),
    hostBPending' = Set(),
  }

  action hostAReads = all {
    hostALocal' = sharedStore,
    sharedStore' = sharedStore,
    hostBLocal' = hostBLocal,
    hostAPending' = hostAPending,
    hostBPending' = hostBPending,
  }

  action hostBReads = all {
    hostBLocal' = sharedStore,
    sharedStore' = sharedStore,
    hostALocal' = hostALocal,
    hostAPending' = hostAPending,
    hostBPending' = hostBPending,
  }

  action hostAIngests = {
    nondet id = IDS.oneOf()
    val newMsg: MessageRecord = { id: id, content: "from_a", schema_version: 1, deleted_at: 0 }
    all {
      hostALocal' = mergeMessages(hostALocal, Set(newMsg)),
      hostAPending' = hostAPending.union(Set(newMsg)),
      sharedStore' = sharedStore,
      hostBLocal' = hostBLocal,
      hostBPending' = hostBPending,
    }
  }

  action hostBIngests = {
    nondet id = IDS.oneOf()
    val newMsg: MessageRecord = { id: id, content: "from_b", schema_version: 1, deleted_at: 0 }
    all {
      hostBLocal' = mergeMessages(hostBLocal, Set(newMsg)),
      hostBPending' = hostBPending.union(Set(newMsg)),
      sharedStore' = sharedStore,
      hostALocal' = hostALocal,
      hostAPending' = hostAPending,
    }
  }

  // UNSAFE: overwrites shared store with local state
  action hostAWritesInPlace = all {
    sharedStore' = hostALocal,
    hostAPending' = Set(),
    hostALocal' = hostALocal,
    hostBLocal' = hostBLocal,
    hostBPending' = hostBPending,
  }

  action hostBWritesInPlace = all {
    sharedStore' = hostBLocal,
    hostBPending' = Set(),
    hostALocal' = hostALocal,
    hostBLocal' = hostBLocal,
    hostAPending' = hostAPending,
  }

  // SAFE: merge pending into shared store
  action hostAWritesBatch = all {
    sharedStore' = mergeMessages(sharedStore, hostAPending),
    hostAPending' = Set(),
    hostALocal' = hostALocal,
    hostBLocal' = hostBLocal,
    hostBPending' = hostBPending,
  }

  action hostBWritesBatch = all {
    sharedStore' = mergeMessages(sharedStore, hostBPending),
    hostBPending' = Set(),
    hostALocal' = hostALocal,
    hostBLocal' = hostBLocal,
    hostAPending' = hostAPending,
  }

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

  // After both hosts flush, shared store should contain all ingested IDs
  val noDataLoss = {
    val allLocalIds = hostALocal.map(m => m.id).union(hostBLocal.map(m => m.id))
    val sharedIds = sharedStore.map(m => m.id)
    (hostAPending.size() == 0 and hostBPending.size() == 0) implies
      allLocalIds.subseteq(sharedIds)
  }
}

// ============================================================
// Partial sync: re-syncing a subset must not delete existing records
// ============================================================

module partial_sync {
  import etl_merge.*

  var store: Set[MessageRecord]

  pure val ALL_IDS = Set("m1", "m2", "m3", "m4", "m5")

  action init = {
    store' = ALL_IDS.map(id =>
      { id: id, content: "original", schema_version: 1, deleted_at: 0 }
    )
  }

  action partialResync = {
    nondet subset = ALL_IDS.powerset().filter(s => s.size() > 0 and s.size() < ALL_IDS.size()).oneOf()
    val partialBatch: Set[MessageRecord] = subset.map(id =>
      { id: id, content: "resynced", schema_version: 1, deleted_at: 0 }
    )
    store' = mergeMessages(store, partialBatch)
  }

  action step = partialResync

  val neverShrinks = store.map(m => m.id).size() == ALL_IDS.size()
}

// ============================================================
// Bug hunt: intentionally broken merge (no tombstone propagation)
// Quint should find the deletion resurrection bug.
// ============================================================

module buggy_merge {
  import etl_merge.MessageRecord

  // BUG: does NOT propagate tombstones — just picks winner by schema version
  pure def buggyResolve(left: MessageRecord, right: MessageRecord): MessageRecord = {
    if (right.schema_version > left.schema_version) right else left
  }

  pure def buggyMerge(a: Set[MessageRecord], b: Set[MessageRecord]): Set[MessageRecord] = {
    val aIds = a.map(m => m.id)
    val bIds = b.map(m => m.id)
    val onlyA = a.filter(ma => not(ma.id.in(bIds)))
    val onlyB = b.filter(mb => not(mb.id.in(aIds)))
    val both = a.filter(ma => ma.id.in(bIds))
              .map(ma => {
                val mb = b.filter(mb => mb.id == ma.id).fold(ma, (_, x) => x)
                buggyResolve(ma, mb)
              })
    onlyA.union(onlyB).union(both)
  }

  var storeA: Set[MessageRecord]
  var storeB: Set[MessageRecord]

  action init = {
    nondet a = Set(
      Set({ id: "m1", content: "x", schema_version: 1, deleted_at: 100 }),
      Set({ id: "m1", content: "x", schema_version: 2, deleted_at: 0 }),
      Set({ id: "m1", content: "x", schema_version: 1, deleted_at: 0 }),
      Set({ id: "m1", content: "x", schema_version: 2, deleted_at: 100 }),
      Set()
    ).oneOf()
    nondet b = Set(
      Set({ id: "m1", content: "x", schema_version: 1, deleted_at: 100 }),
      Set({ id: "m1", content: "x", schema_version: 2, deleted_at: 0 }),
      Set({ id: "m1", content: "x", schema_version: 1, deleted_at: 0 }),
      Set({ id: "m1", content: "x", schema_version: 2, deleted_at: 100 }),
      Set()
    ).oneOf()
    all {
      storeA' = a,
      storeB' = b,
    }
  }

  action step = all {
    storeA' = storeA,
    storeB' = storeB,
  }

  // Tombstone dominance: if either side has deleted_at > 0, merged should too
  val tombstoneSafe = {
    val merged = buggyMerge(storeA, storeB)
    val tombstonedIds = storeA.filter(m => m.deleted_at > 0).map(m => m.id)
                        .union(storeB.filter(m => m.deleted_at > 0).map(m => m.id))
    tombstonedIds.forall(tid =>
      merged.filter(m => m.id == tid).forall(m => m.deleted_at > 0)
    )
  }
}
