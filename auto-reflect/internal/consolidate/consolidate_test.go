package consolidate

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/rules"
)

// obEvent builds an observation event with the given id, severity, and evidence
// session ids, for feeding NewObservationIndex.
func obEvent(t *testing.T, id, severity string, sessions ...string) events.Event {
	t.Helper()
	return obEventTask(t, id, severity, "", sessions...)
}

// obEventTask is obEvent with an explicit (optional) task_id on the observation.
func obEventTask(t *testing.T, id, severity, taskID string, sessions ...string) events.Event {
	t.Helper()
	ev := make([]events.ObservationEvidence, 0, len(sessions))
	for _, s := range sessions {
		ev = append(ev, events.ObservationEvidence{SessionID: s})
	}
	payload, err := json.Marshal(events.ObservationPayload{
		ObservationID: id,
		TaskID:        taskID,
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

func TestParseDocumentOpVocabulary(t *testing.T) {
	// Unknown op → fail fast as a structured DocumentError.
	_, err := ParseDocument([]byte(`{"deltas":[{"op":"bogus"}]}`))
	var de *DocumentError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DocumentError for unknown op, got %v", err)
	}
	if len(de.Errors) != 1 || de.Errors[0].Code != "enum" || de.Errors[0].Field != "deltas[0].op" {
		t.Fatalf("expected one enum error on deltas[0].op, got %#v", de.Errors)
	}

	// Missing op → required error.
	_, err = ParseDocument([]byte(`{"deltas":[{"rule_id":"r-aaaaaaaa"}]}`))
	if !errors.As(err, &de) {
		t.Fatalf("expected *DocumentError for missing op, got %v", err)
	}
	if len(de.Errors) != 1 || de.Errors[0].Code != "required" {
		t.Fatalf("expected one required error for missing op, got %#v", de.Errors)
	}

	// split is in the allow-list and parses cleanly.
	doc, err := ParseDocument([]byte(`{"deltas":[{"op":"split","rule_id":"r-aaaaaaaa","into":[{"use_when":"a","content":"b","causal_note":"c"},{"use_when":"d","content":"e","causal_note":"f"}]}]}`))
	if err != nil {
		t.Fatalf("split should parse: %v", err)
	}
	if len(doc.Deltas) != 1 || doc.Deltas[0].Op != OpSplit || len(doc.Deltas[0].Into) != 2 {
		t.Fatalf("bad split parse: %#v", doc.Deltas)
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

	// task_id-less observations contribute no distinct tasks, but sessions still count.
	if len(cov.Tasks) != 0 {
		t.Fatalf("expected zero distinct tasks for task-id-less observations, got %v", cov.Tasks)
	}
}

func TestCoverageDistinctTasks(t *testing.T) {
	idx := NewObservationIndex([]events.Event{
		obEventTask(t, "ob-00000001", "normal", "049-task-a", "sess-a"),
		obEventTask(t, "ob-00000002", "normal", "049-task-b", "sess-a"), // same session, distinct task
		obEventTask(t, "ob-00000003", "normal", "049-task-c", "sess-a"),
		obEventTask(t, "ob-00000004", "normal", "049-task-a", "sess-a"), // repeated task_id dedupes
		obEvent(t, "ob-00000005", "normal", "sess-b"),                   // no task_id at all
	})

	// Three observations carrying three distinct task_ids → three distinct tasks,
	// even though a fourth repeats one and they share a single session.
	cov := idx.Coverage([]string{"ob-00000001", "ob-00000002", "ob-00000003", "ob-00000004"})
	if len(cov.Tasks) != 3 {
		t.Fatalf("expected 3 distinct tasks, got %v", cov.Tasks)
	}
	if len(cov.Sessions) != 1 {
		t.Fatalf("expected 1 distinct session, got %v", cov.Sessions)
	}

	// A task-id-less observation contributes a session but no task.
	covNone := idx.Coverage([]string{"ob-00000005"})
	if len(covNone.Tasks) != 0 {
		t.Fatalf("expected 0 distinct tasks for task-id-less observation, got %v", covNone.Tasks)
	}
	if len(covNone.Sessions) != 1 {
		t.Fatalf("expected 1 distinct session, got %v", covNone.Sessions)
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

// TestDetectDuplicateInsulatedFromDomainBoost is the AC-4 regression guard: the
// Phase-1 IDF domain boost must never enter the dedupe scoring path. The 0.5
// DedupeScoreThreshold is calibrated to the pure [0,1] lexical scale, so
// DetectDuplicate pre-filters to in-domain rules then scores with no domain
// filter — the boost stays out of the score.
func TestDetectDuplicateInsulatedFromDomainBoost(t *testing.T) {
	// True positive preserved: a strong use_when overlap against a same-domain rule
	// still trips the 0.5 gate.
	pb := []rules.Rule{
		{ID: "r-aaaaaaaa", UseWhen: "wiring a cobra command flag", Domain: []string{"go", "cli"}, RuleType: rules.RuleTypeSoft, Lifecycle: rules.LifecycleConfirmed},
	}
	if dup, ok := DetectDuplicate(pb, "wiring a cobra command flag", []string{"cli"}); !ok || dup.RuleID != "r-aaaaaaaa" {
		t.Fatalf("strong use_when overlap should still trip the dedupe gate, got %#v ok=%v", dup, ok)
	}

	// Regression guard: an in-domain but lexically-weak rule must NOT false-positive.
	// "metrics" is a rare tag (1 of 8 rules → high IDF), so if the boost fired it
	// would inflate the score past 0.5 even with zero keyword overlap.
	weak := rules.Rule{ID: "r-00000001", UseWhen: "dashboards and alerting", Domain: []string{"metrics"}, RuleType: rules.RuleTypeSoft, Lifecycle: rules.LifecycleConfirmed}
	playbook := []rules.Rule{weak}
	for i := 2; i <= 8; i++ {
		playbook = append(playbook, rules.Rule{
			ID:        fmt.Sprintf("r-0000000%d", i),
			UseWhen:   "unrelated guidance",
			Domain:    []string{fmt.Sprintf("other-%d", i)},
			RuleType:  rules.RuleTypeSoft,
			Lifecycle: rules.LifecycleConfirmed,
		})
	}

	// Sanity: passing the domain as a filter (the un-insulated path) lets the boost
	// alone push the lexically-empty rule at/over 0.5 — exactly the false positive
	// the insulation must prevent.
	boosted := rules.MatchRules(playbook, "telemetry", []string{"metrics"}, false)
	if len(boosted) == 0 || boosted[0].Rule.ID != "r-00000001" || boosted[0].MatchScore < DedupeScoreThreshold {
		t.Fatalf("expected the boost to push the in-domain rule >= %.2f when domain is a filter, got %#v", DedupeScoreThreshold, boosted)
	}

	// Insulated path: DetectDuplicate scores with nil domain, so the boost never
	// fires and the lexically-weak candidate is not flagged.
	if dup, ok := DetectDuplicate(playbook, "telemetry", []string{"metrics"}); ok {
		t.Fatalf("in-domain but lexically-weak candidate must not be a duplicate (boost must not enter dedupe), got %#v", dup)
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
