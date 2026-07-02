MODEL := etl_sync.qnt

.PHONY: all typecheck run verify bughunt clean

# Full spike: static check, simulate, exhaustively model-check.
all: typecheck run verify bughunt

# Parse + typecheck every module.
typecheck:
	quint typecheck $(MODEL)

# Randomised simulation of the sync state machine (fast, non-exhaustive).
# Explores syncAB/AC/BC + ingest interleavings, asserting all CRDT properties.
run:
	quint run $(MODEL) --main etl_merge_test --invariant allProperties \
		--max-samples 2000 --max-steps 6

# Exhaustive model check of the CRDT properties via Apalache (SMT).
# Proves commutativity, idempotency, monotonicity, tombstone dominance,
# and schema-upgrade correctness over the full bounded state space.
verify:
	quint verify $(MODEL) --main etl_merge_verify \
		--invariant allVerifyProperties --max-steps 3

# Negative control: a merge with tombstone propagation removed MUST fail.
# Expected outcome: Apalache returns a counterexample (deletion resurrection).
bughunt:
	@echo "Expecting a counterexample below (deletion resurrection):"
	quint verify $(MODEL) --main buggy_merge \
		--invariant tombstoneSafe --max-steps 0 || true

clean:
	@echo "Remove _apalache-out/ by hand (guarded rm)."
