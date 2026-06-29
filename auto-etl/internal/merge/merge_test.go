package merge

import (
	"testing"
)

func TestResolveMessage_HigherSchemaWins(t *testing.T) {
	left := MessageRecord{ID: "m1", Content: "x", SchemaVersion: 1, DeletedAt: 0}
	right := MessageRecord{ID: "m1", Content: "x", SchemaVersion: 2, DeletedAt: 0}

	got := ResolveMessage(left, right)
	if got.SchemaVersion != 2 {
		t.Errorf("expected schema_version=2, got %d", got.SchemaVersion)
	}

	// Commutative: same result regardless of argument order
	got2 := ResolveMessage(right, left)
	if got2.SchemaVersion != 2 {
		t.Errorf("reverse: expected schema_version=2, got %d", got2.SchemaVersion)
	}
}

func TestResolveMessage_TombstonePropagation(t *testing.T) {
	left := MessageRecord{ID: "m1", Content: "x", SchemaVersion: 2, DeletedAt: 0}
	right := MessageRecord{ID: "m1", Content: "x", SchemaVersion: 1, DeletedAt: 100}

	got := ResolveMessage(left, right)
	if got.SchemaVersion != 2 {
		t.Errorf("expected schema_version=2 (higher wins), got %d", got.SchemaVersion)
	}
	if got.DeletedAt != 100 {
		t.Errorf("expected deleted_at=100 (tombstone propagated), got %d", got.DeletedAt)
	}
}

func TestResolveMessage_EqualSchemaLeftWins(t *testing.T) {
	left := MessageRecord{ID: "m1", Content: "left", SchemaVersion: 1, DeletedAt: 0}
	right := MessageRecord{ID: "m1", Content: "right", SchemaVersion: 1, DeletedAt: 0}

	got := ResolveMessage(left, right)
	// When schema versions are equal, left wins (Quint: "if right > left then right else left")
	if got.Content != "left" {
		t.Errorf("expected left content when schema_version tied, got %q", got.Content)
	}
}

func TestMergeMessages_Commutativity(t *testing.T) {
	a := []MessageRecord{
		{ID: "m1", Content: "x", SchemaVersion: 1, DeletedAt: 0},
		{ID: "m2", Content: "x", SchemaVersion: 2, DeletedAt: 100},
	}
	b := []MessageRecord{
		{ID: "m1", Content: "x", SchemaVersion: 2, DeletedAt: 0},
		{ID: "m2", Content: "x", SchemaVersion: 1, DeletedAt: 0},
	}

	ab := MergeMessages(a, b)
	ba := MergeMessages(b, a)

	if len(ab) != len(ba) {
		t.Fatalf("commutativity: lengths differ: %d vs %d", len(ab), len(ba))
	}
	for i := range ab {
		if ab[i] != ba[i] {
			t.Errorf("commutativity: element %d differs: %+v vs %+v", i, ab[i], ba[i])
		}
	}
}

func TestMergeMessages_Idempotency(t *testing.T) {
	a := []MessageRecord{
		{ID: "m1", Content: "x", SchemaVersion: 1, DeletedAt: 0},
		{ID: "m2", Content: "x", SchemaVersion: 2, DeletedAt: 100},
	}

	aa := MergeMessages(a, a)

	if len(aa) != len(a) {
		t.Fatalf("idempotency: lengths differ: %d vs %d", len(aa), len(a))
	}
	for i := range a {
		if aa[i] != a[i] {
			t.Errorf("idempotency: element %d differs: %+v vs %+v", i, aa[i], a[i])
		}
	}
}

func TestMergeMessages_TombstoneDominance(t *testing.T) {
	a := []MessageRecord{
		{ID: "m1", Content: "x", SchemaVersion: 2, DeletedAt: 0},
	}
	b := []MessageRecord{
		{ID: "m1", Content: "x", SchemaVersion: 1, DeletedAt: 100},
	}

	result := MergeMessages(a, b)
	if len(result) != 1 {
		t.Fatalf("expected 1 record, got %d", len(result))
	}
	if result[0].DeletedAt == 0 {
		t.Error("tombstone dominance violated: deleted_at should be > 0")
	}
}

func TestMergeMessages_SchemaUpgrade(t *testing.T) {
	a := []MessageRecord{
		{ID: "m1", Content: "x", SchemaVersion: 1, DeletedAt: 0},
	}
	b := []MessageRecord{
		{ID: "m1", Content: "x", SchemaVersion: 2, DeletedAt: 0},
	}

	result := MergeMessages(a, b)
	if len(result) != 1 {
		t.Fatalf("expected 1 record, got %d", len(result))
	}
	if result[0].SchemaVersion < 2 {
		t.Errorf("schema upgrade: expected schema_version >= 2, got %d", result[0].SchemaVersion)
	}
}

func TestMergeMessages_DisjointSets(t *testing.T) {
	a := []MessageRecord{
		{ID: "m1", Content: "x", SchemaVersion: 1, DeletedAt: 0},
	}
	b := []MessageRecord{
		{ID: "m2", Content: "x", SchemaVersion: 1, DeletedAt: 0},
	}

	result := MergeMessages(a, b)
	if len(result) != 2 {
		t.Fatalf("expected 2 records for disjoint sets, got %d", len(result))
	}
}

func TestMergeMessages_EmptySets(t *testing.T) {
	a := []MessageRecord{
		{ID: "m1", Content: "x", SchemaVersion: 1, DeletedAt: 0},
	}

	result := MergeMessages(a, nil)
	if len(result) != 1 {
		t.Fatalf("merge with nil: expected 1, got %d", len(result))
	}

	result2 := MergeMessages(nil, a)
	if len(result2) != 1 {
		t.Fatalf("merge nil with a: expected 1, got %d", len(result2))
	}

	result3 := MergeMessages(nil, nil)
	if len(result3) != 0 {
		t.Fatalf("merge nil with nil: expected 0, got %d", len(result3))
	}
}

func TestResolveSession_SchemaWins(t *testing.T) {
	left := SessionRecord{ID: "s1", SchemaVersion: 1, LastMessageAt: 10, MessageCount: 1, DeletedAt: 0}
	right := SessionRecord{ID: "s1", SchemaVersion: 2, LastMessageAt: 0, MessageCount: 1, DeletedAt: 0}

	got := ResolveSession(left, right)
	if got.SchemaVersion != 2 {
		t.Errorf("expected schema_version=2, got %d", got.SchemaVersion)
	}
}

func TestResolveSession_LastMessageAtTiebreak(t *testing.T) {
	left := SessionRecord{ID: "s1", SchemaVersion: 1, LastMessageAt: 0, MessageCount: 1, DeletedAt: 0}
	right := SessionRecord{ID: "s1", SchemaVersion: 1, LastMessageAt: 10, MessageCount: 1, DeletedAt: 0}

	got := ResolveSession(left, right)
	if got.LastMessageAt != 10 {
		t.Errorf("expected last_message_at=10 (tiebreak), got %d", got.LastMessageAt)
	}
}

func TestResolveSession_TombstonePropagation(t *testing.T) {
	left := SessionRecord{ID: "s1", SchemaVersion: 2, LastMessageAt: 10, MessageCount: 1, DeletedAt: 0}
	right := SessionRecord{ID: "s1", SchemaVersion: 1, LastMessageAt: 0, MessageCount: 1, DeletedAt: 100}

	got := ResolveSession(left, right)
	if got.DeletedAt != 100 {
		t.Errorf("expected deleted_at=100 (tombstone propagated), got %d", got.DeletedAt)
	}
}

func TestMergeSessions_Commutativity(t *testing.T) {
	a := []SessionRecord{
		{ID: "m1", SchemaVersion: 1, LastMessageAt: 10, MessageCount: 1, DeletedAt: 0},
	}
	b := []SessionRecord{
		{ID: "m1", SchemaVersion: 2, LastMessageAt: 0, MessageCount: 1, DeletedAt: 100},
	}

	ab := MergeSessions(a, b)
	ba := MergeSessions(b, a)

	if len(ab) != len(ba) {
		t.Fatalf("commutativity: lengths differ: %d vs %d", len(ab), len(ba))
	}
	for i := range ab {
		if ab[i] != ba[i] {
			t.Errorf("commutativity: element %d differs: %+v vs %+v", i, ab[i], ba[i])
		}
	}
}

func TestMergeSessions_Idempotency(t *testing.T) {
	a := []SessionRecord{
		{ID: "m1", SchemaVersion: 1, LastMessageAt: 10, MessageCount: 1, DeletedAt: 100},
		{ID: "m2", SchemaVersion: 2, LastMessageAt: 0, MessageCount: 1, DeletedAt: 0},
	}

	aa := MergeSessions(a, a)
	if len(aa) != len(a) {
		t.Fatalf("idempotency: lengths differ: %d vs %d", len(aa), len(a))
	}
	for i := range a {
		if aa[i] != a[i] {
			t.Errorf("idempotency: element %d differs: %+v vs %+v", i, aa[i], a[i])
		}
	}
}
