package observations

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/mistakenot/auto-reflect/internal/events"
)

func validInput() Input {
	return Input{
		Kind:     KindGap,
		Subject:  "docs are read constantly but rarely opened",
		Sessions: []string{"s1"},
		Severity: SeverityNormal,
	}
}

func hasFieldCode(errs []ValidationError, field, code string) bool {
	for _, e := range errs {
		if e.Field == field && e.Code == code {
			return true
		}
	}
	return false
}

func TestValidateHappyPath(t *testing.T) {
	if errs := validInput().Validate(); len(errs) != 0 {
		t.Fatalf("expected no errors, got %+v", errs)
	}
}

func TestValidateMissingKind(t *testing.T) {
	in := validInput()
	in.Kind = ""
	errs := in.Validate()
	if !hasFieldCode(errs, "kind", "required") {
		t.Fatalf("expected required error on kind, got %+v", errs)
	}
}

func TestValidateInvalidKind(t *testing.T) {
	in := validInput()
	in.Kind = "bogus"
	errs := in.Validate()
	if !hasFieldCode(errs, "kind", "enum") {
		t.Fatalf("expected enum error on kind, got %+v", errs)
	}
}

func TestValidateEmptySubject(t *testing.T) {
	in := validInput()
	in.Subject = "   "
	errs := in.Validate()
	if !hasFieldCode(errs, "subject", "required") {
		t.Fatalf("expected required error on subject, got %+v", errs)
	}
}

func TestValidateZeroEvidenceSessions(t *testing.T) {
	in := validInput()
	in.Sessions = nil
	errs := in.Validate()
	if !hasFieldCode(errs, "evidence", "required") {
		t.Fatalf("expected required error on evidence, got %+v", errs)
	}
}

func TestValidateEmptyEvidenceSession(t *testing.T) {
	in := validInput()
	in.Sessions = []string{"  "}
	errs := in.Validate()
	if !hasFieldCode(errs, "evidence[0].session_id", "required") {
		t.Fatalf("expected required error on evidence[0].session_id, got %+v", errs)
	}
}

func TestValidateTooManyQuotes(t *testing.T) {
	in := validInput()
	in.Quotes = []string{"q1", "q2"} // 2 quotes, 1 session
	errs := in.Validate()
	if !hasFieldCode(errs, "evidence", "range") {
		t.Fatalf("expected range error on evidence for too many quotes, got %+v", errs)
	}
}

func TestValidateTooManyMessages(t *testing.T) {
	in := validInput()
	in.Messages = []string{"m1", "m2"} // 2 messages, 1 session
	errs := in.Validate()
	if !hasFieldCode(errs, "evidence", "range") {
		t.Fatalf("expected range error on evidence for too many messages, got %+v", errs)
	}
}

func TestValidateFewerQuotesIsFine(t *testing.T) {
	in := validInput()
	in.Sessions = []string{"s1", "s2"}
	in.Quotes = []string{"q1"} // fewer quotes than sessions is allowed
	if errs := in.Validate(); len(errs) != 0 {
		t.Fatalf("expected no errors for fewer quotes, got %+v", errs)
	}
}

func TestValidateBadDomainTag(t *testing.T) {
	in := validInput()
	in.Domain = []string{"Not_A_Tag"}
	errs := in.Validate()
	if !hasFieldCode(errs, "domain[0]", "invalid_format") {
		t.Fatalf("expected invalid_format error on domain[0], got %+v", errs)
	}
}

func TestValidateDuplicateDomainTag(t *testing.T) {
	in := validInput()
	in.Domain = []string{"docs", "Docs"} // normalize to the same tag
	errs := in.Validate()
	if !hasFieldCode(errs, "domain[1]", "duplicate") {
		t.Fatalf("expected duplicate error on domain[1], got %+v", errs)
	}
}

func TestValidateBadSeverity(t *testing.T) {
	in := validInput()
	in.Severity = "critical"
	errs := in.Validate()
	if !hasFieldCode(errs, "severity", "enum") {
		t.Fatalf("expected enum error on severity, got %+v", errs)
	}
}

func TestValidateEmptySeverityDefaultsNormal(t *testing.T) {
	in := validInput()
	in.Severity = ""
	if errs := in.Validate(); len(errs) != 0 {
		t.Fatalf("expected empty severity to default to normal, got %+v", errs)
	}
}

func TestNewObservationIDFormat(t *testing.T) {
	pattern := regexp.MustCompile(idPattern)
	for range 50 {
		id := NewObservationID()
		if !pattern.MatchString(id) {
			t.Fatalf("minted id %q does not match %s", id, idPattern)
		}
		if !ValidID(id) {
			t.Fatalf("ValidID rejected freshly minted id %q", id)
		}
	}
}

func TestPayloadPairsEvidenceAndNormalizes(t *testing.T) {
	in := Input{
		Kind:     "  GAP ",
		Subject:  "  trimmed subject  ",
		Sessions: []string{" s1 ", "s2"},
		Quotes:   []string{"verbatim  quote"},
		Messages: []string{"  msg-1  "},
		Domain:   []string{" Docs ", "search"},
		Severity: " HIGH ",
	}
	if errs := in.Validate(); len(errs) != 0 {
		t.Fatalf("expected valid input, got %+v", errs)
	}

	payload := in.Payload("ob-deadbeef")
	if payload.ObservationID != "ob-deadbeef" {
		t.Fatalf("observation_id = %q", payload.ObservationID)
	}
	if payload.Kind != "gap" {
		t.Fatalf("kind not normalized: %q", payload.Kind)
	}
	if payload.Subject != "trimmed subject" {
		t.Fatalf("subject not trimmed: %q", payload.Subject)
	}
	if payload.Severity != "high" {
		t.Fatalf("severity not normalized: %q", payload.Severity)
	}
	if len(payload.Domain) != 2 || payload.Domain[0] != "docs" || payload.Domain[1] != "search" {
		t.Fatalf("domain not normalized: %+v", payload.Domain)
	}
	if len(payload.Evidence) != 2 {
		t.Fatalf("expected 2 evidence items, got %d", len(payload.Evidence))
	}
	if payload.Evidence[0].SessionID != "s1" {
		t.Fatalf("evidence[0] session not trimmed: %q", payload.Evidence[0].SessionID)
	}
	if payload.Evidence[0].Quote != "verbatim  quote" {
		t.Fatalf("quote should be preserved verbatim: %q", payload.Evidence[0].Quote)
	}
	if payload.Evidence[0].MessageID != "msg-1" {
		t.Fatalf("evidence[0] message not trimmed: %q", payload.Evidence[0].MessageID)
	}
	// Second session has no quote/message paired.
	if payload.Evidence[1].SessionID != "s2" || payload.Evidence[1].Quote != "" || payload.Evidence[1].MessageID != "" {
		t.Fatalf("unexpected evidence[1]: %+v", payload.Evidence[1])
	}
}

func TestProjectRoundTrip(t *testing.T) {
	in := validInput()
	payload := in.Payload("ob-12345678")
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	e := events.Event{
		ID:        "ev-aabbccdd",
		Type:      events.TypeObservation,
		TS:        "2026-06-10T12:00:00Z",
		SessionID: "s1",
		Payload:   raw,
	}
	obs, err := Project(e)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if obs.ID != "ev-aabbccdd" || obs.TS != "2026-06-10T12:00:00Z" || obs.SessionID != "s1" {
		t.Fatalf("envelope fields not projected: %+v", obs)
	}
	if obs.ObservationID != "ob-12345678" || obs.Kind != KindGap {
		t.Fatalf("payload fields not projected: %+v", obs)
	}
}

func TestProjectRejectsNonObservation(t *testing.T) {
	if _, err := Project(events.Event{ID: "ev-1", Type: events.TypeFeedback}); err == nil {
		t.Fatal("expected error projecting a non-observation event")
	}
}
