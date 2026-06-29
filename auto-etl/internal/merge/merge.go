// Package merge implements spec-aligned CRDT merge functions matching the Quint
// formal specification in etl_sync.qnt. These are pure functions operating on
// simplified record types for model-based testing verification.
package merge

import "sort"

// MessageRecord matches the Quint MessageRecord type exactly.
type MessageRecord struct {
	ID            string
	Content       string
	SchemaVersion int
	DeletedAt     int
}

// SessionRecord matches the Quint SessionRecord type exactly.
type SessionRecord struct {
	ID            string
	LastMessageAt int
	MessageCount  int
	SchemaVersion int
	DeletedAt     int
}

// ResolveMessage resolves a conflict between two MessageRecords with the same ID.
// Higher schema_version wins; max deleted_at is always propagated (tombstone dominance).
// Matches Quint: resolveMessage(left, right)
func ResolveMessage(left, right MessageRecord) MessageRecord {
	winner := left
	if right.SchemaVersion > left.SchemaVersion {
		winner = right
	}
	tombstone := max(right.DeletedAt, left.DeletedAt)
	winner.DeletedAt = tombstone
	return winner
}

// MergeMessages performs G-Set merge by ID with schema-aware conflict resolution.
// For records present in both sets, ResolveMessage determines the winner.
// Output is sorted by ID for deterministic comparison.
// Matches Quint: mergeMessages(a, b)
func MergeMessages(a, b []MessageRecord) []MessageRecord {
	aByID := make(map[string]MessageRecord, len(a))
	for _, m := range a {
		aByID[m.ID] = m
	}
	bByID := make(map[string]MessageRecord, len(b))
	for _, m := range b {
		bByID[m.ID] = m
	}

	merged := make(map[string]MessageRecord)

	// Only in A
	for id, ma := range aByID {
		if _, ok := bByID[id]; !ok {
			merged[id] = ma
		}
	}
	// Only in B
	for id, mb := range bByID {
		if _, ok := aByID[id]; !ok {
			merged[id] = mb
		}
	}
	// In both: resolve
	for id, ma := range aByID {
		if mb, ok := bByID[id]; ok {
			merged[id] = ResolveMessage(ma, mb)
		}
	}

	result := make([]MessageRecord, 0, len(merged))
	for _, m := range merged {
		result = append(result, m)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// ResolveSession resolves a conflict between two SessionRecords with the same ID.
// Higher schema_version wins; on tie, higher last_message_at wins.
// Max deleted_at is always propagated (tombstone dominance).
// Matches Quint: resolveSession(left, right)
func ResolveSession(left, right SessionRecord) SessionRecord {
	var winner SessionRecord
	if right.SchemaVersion > left.SchemaVersion {
		winner = right
	} else if left.SchemaVersion > right.SchemaVersion {
		winner = left
	} else if right.LastMessageAt > left.LastMessageAt {
		winner = right
	} else {
		winner = left
	}
	tombstone := max(right.DeletedAt, left.DeletedAt)
	winner.DeletedAt = tombstone
	return winner
}

// MergeSessions performs LWW-Register merge by ID with schema-aware conflict resolution.
// For records present in both sets, ResolveSession determines the winner.
// Output is sorted by ID for deterministic comparison.
// Matches Quint: mergeSessions(a, b)
func MergeSessions(a, b []SessionRecord) []SessionRecord {
	aByID := make(map[string]SessionRecord, len(a))
	for _, s := range a {
		aByID[s.ID] = s
	}
	bByID := make(map[string]SessionRecord, len(b))
	for _, s := range b {
		bByID[s.ID] = s
	}

	merged := make(map[string]SessionRecord)

	// Only in A
	for id, sa := range aByID {
		if _, ok := bByID[id]; !ok {
			merged[id] = sa
		}
	}
	// Only in B
	for id, sb := range bByID {
		if _, ok := aByID[id]; !ok {
			merged[id] = sb
		}
	}
	// In both: resolve
	for id, sa := range aByID {
		if sb, ok := bByID[id]; ok {
			merged[id] = ResolveSession(sa, sb)
		}
	}

	result := make([]SessionRecord, 0, len(merged))
	for _, s := range merged {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}
