package events

import (
	"encoding/json"
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
	if errs := Validate(validEvent()); len(errs) != 0 {
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
			if errs := Validate(e); len(errs) == 0 {
				t.Fatalf("expected validation error for %q", name)
			}
		})
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
