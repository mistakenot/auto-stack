package observations

import (
	"encoding/json"
	"regexp"
	"strings"
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
	in := validInput()
	if errs := in.Validate(); len(errs) != 0 {
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

func TestValidateTooManyEvidenceFiles(t *testing.T) {
	in := validInput()
	in.EvidenceFiles = []string{"a.go", "b.go"} // 2 files, 1 session
	errs := in.Validate()
	if !hasFieldCode(errs, "evidence", "range") {
		t.Fatalf("expected range error on evidence for too many files, got %+v", errs)
	}
}

func TestValidateTooManyEvidenceCommits(t *testing.T) {
	in := validInput()
	in.EvidenceCommits = []string{"abc123", "def456"} // 2 commits, 1 session
	errs := in.Validate()
	if !hasFieldCode(errs, "evidence", "range") {
		t.Fatalf("expected range error on evidence for too many commits, got %+v", errs)
	}
}

func TestValidateTooManyEvidenceLineRanges(t *testing.T) {
	in := validInput()
	in.EvidenceLineRanges = []string{"1-2", "3-4"} // 2 line ranges, 1 session
	errs := in.Validate()
	if !hasFieldCode(errs, "evidence", "range") {
		t.Fatalf("expected range error on evidence for too many line ranges, got %+v", errs)
	}
}

func TestValidateFewerEvidenceProvenanceIsFine(t *testing.T) {
	in := validInput()
	in.Sessions = []string{"s1", "s2"}
	in.EvidenceFiles = []string{"a.go"} // fewer than sessions is allowed
	in.EvidenceCommits = []string{"abc1234"}
	in.EvidenceLineRanges = []string{"1-10"}
	if errs := in.Validate(); len(errs) != 0 {
		t.Fatalf("expected no errors for fewer provenance entries, got %+v", errs)
	}
}

func TestValidateEmptyTaskIDIsFine(t *testing.T) {
	in := validInput()
	in.TaskID = ""
	if errs := in.Validate(); len(errs) != 0 {
		t.Fatalf("expected empty task_id to be valid, got %+v", errs)
	}
}

func TestValidateValidTaskID(t *testing.T) {
	in := validInput()
	in.TaskID = "049-reflect-audit-lineage-lint"
	if errs := in.Validate(); len(errs) != 0 {
		t.Fatalf("expected valid task_id to pass, got %+v", errs)
	}
}

func TestValidateInvalidTaskID(t *testing.T) {
	for _, bad := range []string{"bad id", "49-x", "049-Bad", "abc-def", "049-"} {
		in := validInput()
		in.TaskID = bad
		errs := in.Validate()
		if !hasFieldCode(errs, "task_id", "invalid_format") {
			t.Fatalf("expected invalid_format error on task_id for %q, got %+v", bad, errs)
		}
	}
}

func TestPayloadSetsProvenanceAndTaskID(t *testing.T) {
	in := Input{
		Kind:               KindIncident,
		Subject:            "build broke",
		Sessions:           []string{"s1", "s2"},
		EvidenceFiles:      []string{" main.go "},
		EvidenceLineRanges: []string{" 10-20 "},
		EvidenceCommits:    []string{" abc1234 "},
		TaskID:             " 049-reflect-audit-lineage-lint ",
		Severity:           SeverityNormal,
	}
	if errs := in.Validate(); len(errs) != 0 {
		t.Fatalf("expected valid input, got %+v", errs)
	}

	payload := in.Payload("ob-deadbeef")
	if payload.TaskID != "049-reflect-audit-lineage-lint" {
		t.Fatalf("task_id not trimmed/set: %q", payload.TaskID)
	}
	if len(payload.Evidence) != 2 {
		t.Fatalf("expected 2 evidence items, got %d", len(payload.Evidence))
	}
	if payload.Evidence[0].File != "main.go" {
		t.Fatalf("evidence[0] file not trimmed/set: %q", payload.Evidence[0].File)
	}
	if payload.Evidence[0].LineRange != "10-20" {
		t.Fatalf("evidence[0] line_range not trimmed/set: %q", payload.Evidence[0].LineRange)
	}
	if payload.Evidence[0].Commit != "abc1234" {
		t.Fatalf("evidence[0] commit not trimmed/set: %q", payload.Evidence[0].Commit)
	}
	// Second session has no provenance paired.
	if payload.Evidence[1].File != "" || payload.Evidence[1].LineRange != "" || payload.Evidence[1].Commit != "" {
		t.Fatalf("unexpected provenance on evidence[1]: %+v", payload.Evidence[1])
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
	id := NewObservationID("gap", "subject", "s1")
	if !pattern.MatchString(id) {
		t.Fatalf("minted id %q does not match %s", id, idPattern)
	}
	if !ValidID(id) {
		t.Fatalf("ValidID rejected minted id %q", id)
	}
	// Deterministic: identical content parts mint an identical id.
	if again := NewObservationID("gap", "subject", "s1"); again != id {
		t.Fatalf("expected deterministic id, got %q then %q", id, again)
	}
	// Distinct content parts mint a distinct id.
	if other := NewObservationID("gap", "subject", "s2"); other == id {
		t.Fatalf("expected different id for different parts, both %q", id)
	}
}

func TestCanonicalPartsDeriveStableID(t *testing.T) {
	in := validInput()
	parts := in.CanonicalParts()
	id := NewObservationID(parts...)
	if !ValidID(id) {
		t.Fatalf("id %q from CanonicalParts is not a valid observation id", id)
	}
	// CanonicalParts normalizes via Payload, so whitespace/case variants of the
	// same finding mint the same id (idempotency).
	noisy := validInput()
	noisy.Kind = "  GAP  "
	noisy.Subject = "  " + in.Subject + "  "
	if got := NewObservationID(noisy.CanonicalParts()...); got != id {
		t.Fatalf("expected normalized variants to mint the same id, got %q vs %q", got, id)
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

func TestPayloadSetsEvidenceCommandAndTouchedFile(t *testing.T) {
	in := Input{
		Kind:                KindIncident,
		Subject:             "sweep commit included unrelated files",
		Sessions:            []string{"s1"},
		Severity:            SeverityHigh,
		EvidenceCommand:     " git commit -am 'wip' ",
		EvidenceTouchedFile: " internal/cli/rule.go ",
	}
	if errs := in.Validate(); len(errs) != 0 {
		t.Fatalf("expected valid input, got %+v", errs)
	}
	payload := in.Payload("ob-deadbeef")
	if payload.EvidenceCommand != "git commit -am 'wip'" {
		t.Fatalf("evidence_command not trimmed/set: %q", payload.EvidenceCommand)
	}
	if payload.EvidenceTouchedFile != "internal/cli/rule.go" {
		t.Fatalf("evidence_touched_file not trimmed/set: %q", payload.EvidenceTouchedFile)
	}
}

func TestEvidenceCommandNotInCanonicalParts(t *testing.T) {
	a := validInput()
	b := validInput()
	b.EvidenceCommand = "git commit -am 'wip'"
	b.EvidenceTouchedFile = "internal/cli/rule.go"
	idA := NewObservationID(a.CanonicalParts()...)
	idB := NewObservationID(b.CanonicalParts()...)
	if idA != idB {
		t.Fatalf("evidence_command/evidence_touched_file should not affect canonical id: %q vs %q", idA, idB)
	}
}

func TestEvidenceCommandAndTouchedFileOmittedWhenEmpty(t *testing.T) {
	in := validInput()
	payload := in.Payload("ob-deadbeef")
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	for _, key := range []string{"evidence_command", "evidence_touched_file"} {
		if strings.Contains(s, key) {
			t.Fatalf("empty payload should omit %q, got: %s", key, s)
		}
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
	obs, err := Project(&e)
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
	if _, err := Project(&events.Event{ID: "ev-1", Type: events.TypeFeedback}); err == nil {
		t.Fatal("expected error projecting a non-observation event")
	}
}
