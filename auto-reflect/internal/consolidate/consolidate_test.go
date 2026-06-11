package consolidate

import (
	"encoding/json"
	"testing"

	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/rules"
)

// obEvent builds an observation event with the given id, severity, and evidence
// session ids, for feeding NewObservationIndex.
func obEvent(t *testing.T, id, severity string, sessions ...string) events.Event {
	t.Helper()
	ev := make([]events.ObservationEvidence, 0, len(sessions))
	for _, s := range sessions {
		ev = append(ev, events.ObservationEvidence{SessionID: s})
	}
	payload, err := json.Marshal(events.ObservationPayload{
		ObservationID: id,
		Kind:          "pattern",
		Subject:       "subject",
		Evidence:      ev,
		Severity:      severity,
	})
	if err != nil {
		t.Fatalf("marshal observation payload: %v", err)
	}
	return events.Event{Type: events.TypeObservation, Payload: payload}
}

func TestParseDocumentRejectsUnknownFieldsAndEmpty(t *testing.T) {
	if _, err := ParseDocument([]byte(`{"deltas":[{"op":"create-draft","bogus":1}]}`)); err == nil {
		t.Fatal("expected error on unknown field")
	}
	if _, err := ParseDocument([]byte(`{"deltas":[]}`)); err == nil {
		t.Fatal("expected error on empty deltas")
	}
	doc, err := ParseDocument([]byte(`{"deltas":[{"op":"deprecate","rule_id":"r-aaaaaaaa","reason":"x"}]}`))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(doc.Deltas) != 1 || doc.Deltas[0].Op != OpDeprecate {
		t.Fatalf("bad parse: %#v", doc.Deltas)
	}
}

func TestCoverageDistinctSessionsAndSeverity(t *testing.T) {
	idx := NewObservationIndex([]events.Event{
		obEvent(t, "ob-00000001", "normal", "sess-a", "sess-a"), // duplicate session collapses
		obEvent(t, "ob-00000002", "normal", "sess-b"),
		obEvent(t, "ob-00000003", "high", "sess-c"),
	})

	cov := idx.Coverage([]string{"ob-00000001", "ob-00000002"})
	if len(cov.Sessions) != 2 {
		t.Fatalf("expected 2 distinct sessions, got %v", cov.Sessions)
	}
	if cov.HighSeverity {
		t.Fatal("did not expect high severity")
	}
	if len(cov.Missing) != 0 {
		t.Fatalf("unexpected missing: %v", cov.Missing)
	}

	covOne := idx.Coverage([]string{"ob-00000001", "ob-00000001"})
	if len(covOne.Sessions) != 1 {
		t.Fatalf("same ob twice should still be 1 session, got %v", covOne.Sessions)
	}

	covHigh := idx.Coverage([]string{"ob-00000003"})
	if !covHigh.HighSeverity {
		t.Fatal("expected high severity from ob-00000003")
	}

	covMissing := idx.Coverage([]string{"ob-deadbeef"})
	if len(covMissing.Missing) != 1 {
		t.Fatalf("expected one missing id, got %v", covMissing.Missing)
	}
}

func TestEvidenceGate(t *testing.T) {
	// Below threshold, no force, normal severity → blocked.
	if ok, reason := EvidenceGate(Coverage{Sessions: []string{"s1"}}, false); ok || reason == "" {
		t.Fatalf("single-session draft should be blocked with a reason, got ok=%v reason=%q", ok, reason)
	}
	// At threshold → allowed.
	if ok, _ := EvidenceGate(Coverage{Sessions: []string{"s1", "s2"}}, false); !ok {
		t.Fatal("two sessions should pass the gate")
	}
	// Force bypasses below threshold.
	if ok, _ := EvidenceGate(Coverage{Sessions: []string{"s1"}}, true); !ok {
		t.Fatal("--force should bypass the threshold")
	}
	// High severity bypasses below threshold.
	if ok, _ := EvidenceGate(Coverage{Sessions: []string{"s1"}, HighSeverity: true}, false); !ok {
		t.Fatal("high severity should bypass the threshold")
	}
	// Missing ids block even with force.
	if ok, reason := EvidenceGate(Coverage{Sessions: []string{"s1", "s2"}, Missing: []string{"ob-x"}}, true); ok || reason == "" {
		t.Fatalf("missing observation should block even under force, got ok=%v", ok)
	}
}

func TestDetectDuplicate(t *testing.T) {
	playbook := []rules.Rule{
		{ID: "r-aaaaaaaa", UseWhen: "wiring a cobra command flag", Domain: []string{"go", "cli"}, RuleType: rules.RuleTypeSoft, Lifecycle: rules.LifecycleConfirmed},
	}
	// Near-identical use_when → duplicate.
	if dup, ok := DetectDuplicate(playbook, "wiring a cobra command flag", []string{"go"}); !ok || dup.RuleID != "r-aaaaaaaa" {
		t.Fatalf("expected duplicate against r-aaaaaaaa, got %#v ok=%v", dup, ok)
	}
	// Unrelated use_when → not a duplicate.
	if _, ok := DetectDuplicate(playbook, "deploying containers to production", []string{"ops"}); ok {
		t.Fatal("unrelated candidate should not be a duplicate")
	}
	// A draft existing rule must NOT count (dedupe only against live rules).
	draftbook := []rules.Rule{
		{ID: "r-bbbbbbbb", UseWhen: "wiring a cobra command flag", Domain: []string{"go"}, RuleType: rules.RuleTypeSoft, Lifecycle: rules.LifecycleDraft},
	}
	if _, ok := DetectDuplicate(draftbook, "wiring a cobra command flag", []string{"go"}); ok {
		t.Fatal("draft rules should not trigger dedupe (matched with includeDrafts=false)")
	}
}

func TestDetectConflicts(t *testing.T) {
	playbook := []rules.Rule{
		{ID: "r-aaaaaaaa", UseWhen: "committing", Content: "always squash commits before merge", Domain: []string{"git"}, Lifecycle: rules.LifecycleConfirmed},
		{ID: "r-bbbbbbbb", UseWhen: "deploying", Content: "rotate the database credentials", Domain: []string{"ops"}, Lifecycle: rules.LifecycleConfirmed},
		{ID: "r-cccccccc", UseWhen: "committing", Content: "never squash commits before merge", Domain: []string{"git"}, Lifecycle: rules.LifecycleStale},
	}
	// New rule shares domain "git" and opposes the squash guidance → conflict with
	// the confirmed rule, but NOT the stale one (stale excluded).
	conflicts := DetectConflicts(playbook, []string{"git"}, "never squash commits before merge")
	if len(conflicts) != 1 || conflicts[0].RuleID != "r-aaaaaaaa" {
		t.Fatalf("expected one conflict with r-aaaaaaaa, got %#v", conflicts)
	}
	// Different domain → no conflict even if polarity differs.
	if c := DetectConflicts(playbook, []string{"python"}, "never squash commits"); len(c) != 0 {
		t.Fatalf("expected no conflict across domains, got %#v", c)
	}
	// Same polarity → no conflict.
	if c := DetectConflicts(playbook, []string{"git"}, "always squash commits before merge"); len(c) != 0 {
		t.Fatalf("agreeing guidance should not conflict, got %#v", c)
	}
}

func TestUnionObservationIDs(t *testing.T) {
	got := UnionObservationIDs([]string{"ob-1", "ob-2"}, []string{"ob-2", "ob-3", " "})
	want := []string{"ob-1", "ob-2", "ob-3"}
	if len(got) != len(want) {
		t.Fatalf("union = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("union order = %v, want %v", got, want)
		}
	}
}
