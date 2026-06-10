package events

import (
	"encoding/json"
	"regexp"

	"github.com/mistakenot/auto-shared/config"
)

// SchemaVersion is the on-disk schema version stamped on every event. Events are
// append-only and must fold identically under every future code version, so this
// constant only bumps when the envelope or payload shapes change in a
// backward-incompatible way.
const SchemaVersion = 1

// Event types.
const (
	TypeRuleCreated = "rule_created"
	TypeRuleEdited  = "rule_edited"
	TypeRetrieval   = "retrieval"
	TypeSelection   = "selection"
	TypeFeedback    = "feedback"
	TypeObservation = "observation"
)

var (
	eventIDPattern = regexp.MustCompile(`^ev-[0-9a-f]{8}$`)
	typePattern    = regexp.MustCompile(`^[a-z_]+$`)
)

// ValidationError is the shared structured field-level error.
type ValidationError = config.ValidationError

// GitProvenance captures sanitized git context for an event. Both fields are
// empty when the repository has an unborn HEAD (no commits yet).
type GitProvenance struct {
	Hash   string `json:"hash"`
	Remote string `json:"remote"`
}

// Event is the canonical append-only envelope. Every record in an events shard
// decodes to one Event.
type Event struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	SchemaVersion int             `json:"schema_version"`
	Seq           int             `json:"seq"`
	TS            string          `json:"ts"`
	Host          string          `json:"host"`
	SessionID     string          `json:"session_id,omitempty"`
	Agent         string          `json:"agent,omitempty"`
	Git           GitProvenance   `json:"git"`
	Payload       json.RawMessage `json:"payload"`
}

// RuleCreatedPayload records the creation of a rule.
type RuleCreatedPayload struct {
	RuleID     string   `json:"rule_id"`
	Domain     []string `json:"domain"`
	UseWhen    string   `json:"use_when"`
	Content    string   `json:"content"`
	CausalNote string   `json:"causal_note"`
	RuleType   string   `json:"rule_type"`
	Lifecycle  string   `json:"lifecycle"`
}

// FieldDelta is a single field change within a rule_edited event.
type FieldDelta struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// RuleEditedPayload records one atomic edit of a rule. A multi-field edit is one
// event carrying several deltas and a single version bump, so sibling field
// changes can never masquerade as a concurrent-edit conflict.
type RuleEditedPayload struct {
	RuleID      string       `json:"rule_id"`
	FromVersion int          `json:"from_version"`
	ToVersion   int          `json:"to_version"`
	Deltas      []FieldDelta `json:"deltas"`
}

// RetrievalItem is one matched rule surfaced by a retrieve call.
type RetrievalItem struct {
	RetrievalID  string  `json:"retrieval_id"`
	RuleID       string  `json:"rule_id"`
	RuleVersion  int     `json:"rule_version"`
	MatchScore   float64 `json:"match_score"`
	HardInjected bool    `json:"hard_injected"`
}

// RetrievalPayload records a retrieve call and the ids it minted.
type RetrievalPayload struct {
	Intent string          `json:"intent"`
	Domain []string        `json:"domain"`
	Limit  int             `json:"limit"`
	Items  []RetrievalItem `json:"items"`
}

// SelectionItem is one rule the agent committed to, in selection order.
type SelectionItem struct {
	FeedbackID  string `json:"feedback_id"`
	RetrievalID string `json:"retrieval_id"`
	RuleID      string `json:"rule_id"`
}

// SelectionPayload records a select call preserving the input order.
type SelectionPayload struct {
	Items []SelectionItem `json:"items"`
}

// FeedbackRanking is one ranked feedback entry.
type FeedbackRanking struct {
	FeedbackID string `json:"feedback_id"`
	Rank       int    `json:"rank"`
	Reason     string `json:"reason"`
}

// FeedbackGap is an optional report of a missing rule.
type FeedbackGap struct {
	Report string `json:"report"`
	Moment string `json:"moment"`
}

// FeedbackPayload records a submitted feedback document.
type FeedbackPayload struct {
	Outcome  string            `json:"outcome"`
	Summary  string            `json:"summary"`
	Rankings []FeedbackRanking `json:"rankings"`
	Gap      *FeedbackGap      `json:"gap,omitempty"`
}

// ObservationEvidence links an observation to the session/message/quote it was
// grounded in. SessionID is required; MessageID and Quote are optional refinements.
type ObservationEvidence struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id,omitempty"`
	Quote     string `json:"quote,omitempty"`
}

// ObservationPayload is the canonical working-memory record: a situated finding
// (correction|pattern|gap|incident) backed by evidence, separate from the rules
// it may later be consolidated into. This shape is a contract that consolidation
// (1.4) and the reader API (1.5) depend on.
type ObservationPayload struct {
	ObservationID           string                `json:"observation_id"` // ob-[0-9a-f]{8}
	Kind                    string                `json:"kind"`           // correction|pattern|gap|incident
	Subject                 string                `json:"subject"`
	Evidence                []ObservationEvidence `json:"evidence"`
	Context                 string                `json:"context,omitempty"`
	SuggestedGeneralization string                `json:"suggested_generalization,omitempty"`
	Domain                  []string              `json:"domain,omitempty"`
	Severity                string                `json:"severity"` // normal|high
}

// IsRuleEvent reports whether an event mutates the rule projection. Only these
// events advance folded_through / dirty the snapshot. Observations are working
// memory and deliberately excluded so they never dirty the rule projection.
func IsRuleEvent(eventType string) bool {
	return eventType == TypeRuleCreated || eventType == TypeRuleEdited
}

// Validate checks the structural integrity of an event envelope, returning
// structured errors. The payload's domain-specific rules are validated by the
// rules/loop packages that own each payload type.
func Validate(e *Event) []ValidationError {
	var errs []ValidationError

	if e.ID == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "id", Message: "id is required"})
	} else if !eventIDPattern.MatchString(e.ID) {
		errs = append(errs, ValidationError{Code: "format", Field: "id", Message: "id must match ^ev-[0-9a-f]{8}$", Value: e.ID})
	}

	switch e.Type {
	case TypeRuleCreated, TypeRuleEdited, TypeRetrieval, TypeSelection, TypeFeedback, TypeObservation:
	case "":
		errs = append(errs, ValidationError{Code: "required", Field: "type", Message: "type is required"})
	default:
		errs = append(errs, ValidationError{Code: "enum", Field: "type", Message: "type must be one of rule_created, rule_edited, retrieval, selection, feedback, observation", Value: e.Type})
	}
	if e.Type != "" && !typePattern.MatchString(e.Type) {
		errs = append(errs, ValidationError{Code: "format", Field: "type", Message: "type must match ^[a-z_]+$", Value: e.Type})
	}

	if e.SchemaVersion != SchemaVersion {
		errs = append(errs, ValidationError{Code: "schema_version", Field: "schema_version", Message: "unsupported schema_version", Value: e.SchemaVersion})
	}

	if e.Seq < 1 {
		errs = append(errs, ValidationError{Code: "range", Field: "seq", Message: "seq must be >= 1", Value: e.Seq})
	}

	if e.TS == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "ts", Message: "ts is required"})
	}

	if e.Host == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "host", Message: "host is required"})
	}

	if len(e.Payload) == 0 {
		errs = append(errs, ValidationError{Code: "required", Field: "payload", Message: "payload is required"})
	}

	return errs
}
