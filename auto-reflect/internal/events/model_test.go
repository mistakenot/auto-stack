package events

import (
	"encoding/json"
	"strings"
	"testing"
)

func validEvent() Event {
	return Event{
		ID:            "ev-0a1b2c3d",
		Type:          TypeRuleCreated,
		SchemaVersion: SchemaVersion,
		Seq:           1,
		TS:            "2026-06-09T12:00:00Z",
		Host:          "host1",
		Payload:       json.RawMessage(`{"rule_id":"r-00000001"}`),
	}
}

func TestValidateAcceptsValidEvent(t *testing.T) {
	e := validEvent()
	if errs := Validate(&e); len(errs) != 0 {
		t.Fatalf("expected no errors, got %+v", errs)
	}
}

func TestValidateRejectsBadFields(t *testing.T) {
	cases := map[string]func(*Event){
		"bad id":          func(e *Event) { e.ID = "nope" },
		"missing id":      func(e *Event) { e.ID = "" },
		"bad type":        func(e *Event) { e.Type = "unknown" },
		"missing type":    func(e *Event) { e.Type = "" },
		"bad schema":      func(e *Event) { e.SchemaVersion = 99 },
		"zero seq":        func(e *Event) { e.Seq = 0 },
		"missing ts":      func(e *Event) { e.TS = "" },
		"missing host":    func(e *Event) { e.Host = "" },
		"missing payload": func(e *Event) { e.Payload = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := validEvent()
			mutate(&e)
			if errs := Validate(&e); len(errs) == 0 {
				t.Fatalf("expected validation error for %q", name)
			}
		})
	}
}

func TestRuleCreatedPayloadLineageRoundTrip(t *testing.T) {
	// The lineage + lint fields added in task 049 survive a marshal/unmarshal
	// round-trip and are dropped from JSON when empty (omitempty).
	in := RuleCreatedPayload{
		RuleID: "r-00000001", Domain: []string{"go"}, UseWhen: "x", Content: "c",
		CausalNote: "n", RuleType: "soft", Lifecycle: "enforced",
		PredecessorIDs: []string{"r-00000002"},
		SuccessorIDs:   []string{"r-00000003"},
		LintRef:        &LintRef{Linter: "golangci-lint", Check: "errcheck"},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out RuleCreatedPayload
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.PredecessorIDs) != 1 || out.PredecessorIDs[0] != "r-00000002" {
		t.Fatalf("predecessor_ids did not round-trip: %#v", out.PredecessorIDs)
	}
	if len(out.SuccessorIDs) != 1 || out.SuccessorIDs[0] != "r-00000003" {
		t.Fatalf("successor_ids did not round-trip: %#v", out.SuccessorIDs)
	}
	if out.LintRef == nil || out.LintRef.Linter != "golangci-lint" || out.LintRef.Check != "errcheck" {
		t.Fatalf("lint_ref did not round-trip: %#v", out.LintRef)
	}

	// Empty payload omits the new keys entirely.
	bare, err := json.Marshal(RuleCreatedPayload{
		RuleID: "r-00000001", Domain: []string{"go"}, UseWhen: "x", Content: "c",
		CausalNote: "n", RuleType: "soft", Lifecycle: "draft",
	})
	if err != nil {
		t.Fatalf("marshal bare: %v", err)
	}
	for _, key := range []string{"predecessor_ids", "successor_ids", "lint_ref"} {
		if strings.Contains(string(bare), key) {
			t.Fatalf("empty payload should omit %q, got: %s", key, bare)
		}
	}
}

func TestObservationEvidenceSourceProvenanceRoundTrip(t *testing.T) {
	// File/LineRange/Commit survive a round-trip and are omitted when empty.
	in := ObservationEvidence{
		SessionID: "sess-1", File: "internal/cli/rule.go", LineRange: "12-20", Commit: "abc1234",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ObservationEvidence
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.File != "internal/cli/rule.go" || out.LineRange != "12-20" || out.Commit != "abc1234" {
		t.Fatalf("source provenance did not round-trip: %#v", out)
	}

	bare, err := json.Marshal(ObservationEvidence{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("marshal bare: %v", err)
	}
	for _, key := range []string{"file", "line_range", "commit"} {
		if strings.Contains(string(bare), key) {
			t.Fatalf("empty evidence should omit %q, got: %s", key, bare)
		}
	}
}

func TestObservationPayloadTaskIDRoundTrip(t *testing.T) {
	// TaskID survives a round-trip and is omitted when empty.
	in := ObservationPayload{
		ObservationID: "ob-00000001", TaskID: "049-reflect-audit-lineage-lint",
		Kind: "gap", Subject: "s", Severity: "normal",
		Evidence: []ObservationEvidence{{SessionID: "sess-1"}},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ObservationPayload
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.TaskID != "049-reflect-audit-lineage-lint" {
		t.Fatalf("task_id did not round-trip: %q", out.TaskID)
	}

	bare, err := json.Marshal(ObservationPayload{
		ObservationID: "ob-00000001", Kind: "gap", Subject: "s", Severity: "normal",
		Evidence: []ObservationEvidence{{SessionID: "sess-1"}},
	})
	if err != nil {
		t.Fatalf("marshal bare: %v", err)
	}
	if strings.Contains(string(bare), "task_id") {
		t.Fatalf("empty payload should omit task_id, got: %s", bare)
	}
}

func TestObservationPayloadEvidenceCommandTouchedFileRoundTrip(t *testing.T) {
	in := ObservationPayload{
		ObservationID:       "ob-00000001",
		Kind:                "incident",
		Subject:             "build broke",
		Severity:            "normal",
		Evidence:            []ObservationEvidence{{SessionID: "sess-1"}},
		EvidenceCommand:     "git commit -am 'wip'",
		EvidenceTouchedFile: "internal/cli/rule.go",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ObservationPayload
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.EvidenceCommand != "git commit -am 'wip'" {
		t.Fatalf("evidence_command did not round-trip: %q", out.EvidenceCommand)
	}
	if out.EvidenceTouchedFile != "internal/cli/rule.go" {
		t.Fatalf("evidence_touched_file did not round-trip: %q", out.EvidenceTouchedFile)
	}

	bare, err := json.Marshal(ObservationPayload{
		ObservationID: "ob-00000001", Kind: "gap", Subject: "s", Severity: "normal",
		Evidence: []ObservationEvidence{{SessionID: "sess-1"}},
	})
	if err != nil {
		t.Fatalf("marshal bare: %v", err)
	}
	for _, key := range []string{"evidence_command", "evidence_touched_file"} {
		if strings.Contains(string(bare), key) {
			t.Fatalf("empty payload should omit %q, got: %s", key, bare)
		}
	}
}

func TestValidateUnchangedForEventWithNewPayloadFields(t *testing.T) {
	// Envelope validation is payload-agnostic: an event whose payload carries the
	// new task-049 fields still validates exactly as before.
	e := validEvent()
	e.Payload = json.RawMessage(`{"rule_id":"r-00000001","predecessor_ids":["r-00000002"],"lint_ref":{"linter":"golangci-lint","check":"errcheck"}}`)
	if errs := Validate(&e); len(errs) != 0 {
		t.Fatalf("expected no errors, got %+v", errs)
	}
}

func TestIsRuleEvent(t *testing.T) {
	if !IsRuleEvent(TypeRuleCreated) || !IsRuleEvent(TypeRuleEdited) {
		t.Fatal("rule_created/rule_edited must be rule events")
	}
	for _, et := range []string{TypeRetrieval, TypeSelection, TypeFeedback} {
		if IsRuleEvent(et) {
			t.Fatalf("%s must not be a rule event", et)
		}
	}
}
