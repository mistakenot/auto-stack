// Package observations is the working-memory tier of the reflect loop: situated,
// evidence-linked findings that agents record before (and as the raw material
// for) rules. An observation is an append-only `observation` event on the shared
// event log; this package owns its schema, validation, id minting, and the
// projected view used for list output. Consolidation (1.4) and the reader API
// (1.5) build on the canonical events.ObservationPayload shape, so it is treated
// as a contract.
package observations

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mistakenot/auto-shared/config"

	"github.com/mistakenot/auto-reflect/internal/events"
)

const (
	idPattern  = `^ob-[0-9a-f]{8}$`
	tagPattern = `^[a-z0-9]+(?:-[a-z0-9]+)*$`
)

// Observation kinds.
const (
	KindCorrection = "correction"
	KindPattern    = "pattern"
	KindGap        = "gap"
	KindIncident   = "incident"
)

// Observation severities.
const (
	SeverityNormal = "normal"
	SeverityHigh   = "high"
)

type ValidationError = config.ValidationError

var (
	idRegex     = regexp.MustCompile(idPattern)
	tagRegex    = regexp.MustCompile(tagPattern)
	taskIDRegex = regexp.MustCompile(`^[0-9]{3}-[a-z0-9]+(?:-[a-z0-9]+)*$`)
	// commitRegex matches a 7-40 char lowercase-hex git commit (abbreviated or full).
	commitRegex = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	// lineRangeRegex matches a single line (`12`) or an inclusive span (`12-34`).
	lineRangeRegex = regexp.MustCompile(`^[0-9]+(?:-[0-9]+)?$`)

	validKinds = map[string]struct{}{
		KindCorrection: {}, KindPattern: {}, KindGap: {}, KindIncident: {},
	}
	validSeverities = map[string]struct{}{
		SeverityNormal: {}, SeverityHigh: {},
	}
)

// Observation is the projected, list-friendly view of an observation event: the
// canonical events.ObservationPayload plus the envelope fields (event id, ts,
// session) attached during projection.
type Observation struct {
	ID        string `json:"id"`                   // event id (ev-...)
	TS        string `json:"ts"`                   // event timestamp (RFC3339)
	SessionID string `json:"session_id,omitempty"` // capturing session, from the envelope
	events.ObservationPayload
}

// Input is the unpaired, pre-validation form of an observation as supplied on the
// command line. Evidence arrives as parallel slices paired by index: Quotes[i],
// Messages[i], EvidenceFiles[i], EvidenceCommits[i], and EvidenceLineRanges[i]
// each attach to Sessions[i]. Extra entries beyond the session count are a
// validation error; fewer is fine. TaskID is an optional originating-task pointer.
type Input struct {
	Kind                    string
	Subject                 string
	Sessions                []string
	Quotes                  []string
	Messages                []string
	EvidenceFiles           []string
	EvidenceCommits         []string
	EvidenceLineRanges      []string
	TaskID                  string
	Context                 string
	SuggestedGeneralization string
	Domain                  []string
	Severity                string
}

// NewObservationID mints a fresh observation id matching ^ob-[0-9a-f]{8}$ from
// crypto/rand, falling back to UnixNano when randomness is unavailable. Mirrors
// rules.NewRuleID.
func NewObservationID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("ob-%08x", uint32(time.Now().UnixNano()))
	}
	return fmt.Sprintf("ob-%02x%02x%02x%02x", buf[0], buf[1], buf[2], buf[3])
}

// normalizeDomain trims and lowercases each tag, dropping empties while
// preserving order. Duplicate detection is left to Validate so duplicates surface
// as errors rather than being silently collapsed. Mirrors rules.NormalizeDomain;
// duplicated here to keep observations independent of the rules package.
func normalizeDomain(domain []string) []string {
	out := make([]string, 0, len(domain))
	for _, d := range domain {
		normalized := strings.ToLower(strings.TrimSpace(d))
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

// Validate checks an Input, returning structured errors. It enforces the kind and
// severity enums, a non-empty subject, at least one non-empty evidence session,
// quote/message counts within the session count (positional pairing), and the
// domain tag format with dedupe.
func (in *Input) Validate() []ValidationError {
	errs := make([]ValidationError, 0)

	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "kind", Message: "kind is required: pass --kind correction|pattern|gap|incident"})
	} else if _, ok := validKinds[kind]; !ok {
		errs = append(errs, ValidationError{Code: "enum", Field: "kind", Message: "kind must be one of correction, pattern, gap, incident", Value: kind})
	}

	severity := strings.ToLower(strings.TrimSpace(in.Severity))
	if severity == "" {
		severity = SeverityNormal
	}
	if _, ok := validSeverities[severity]; !ok {
		errs = append(errs, ValidationError{Code: "enum", Field: "severity", Message: "severity must be one of normal, high", Value: severity})
	}

	if strings.TrimSpace(in.Subject) == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "subject", Message: "subject is required: pass --subject <what this observation is about>"})
	}

	if taskID := strings.TrimSpace(in.TaskID); taskID != "" && !taskIDRegex.MatchString(taskID) {
		errs = append(errs, ValidationError{Code: "invalid_format", Field: "task_id", Message: "task_id must match ^[0-9]{3}-[a-z0-9]+(?:-[a-z0-9]+)*$ (e.g. 049-reflect-audit-lineage-lint)", Value: in.TaskID})
	}

	errs = append(errs, in.validateEvidence()...)
	errs = append(errs, validateDomainTags(normalizeDomain(in.Domain))...)

	return errs
}

func (in *Input) validateEvidence() []ValidationError {
	errs := make([]ValidationError, 0)

	nonEmpty := 0
	for i, s := range in.Sessions {
		if strings.TrimSpace(s) == "" {
			errs = append(errs, ValidationError{Code: "required", Field: fmt.Sprintf("evidence[%d].session_id", i), Message: "evidence session id cannot be empty"})
			continue
		}
		nonEmpty++
	}
	if nonEmpty == 0 {
		errs = append(errs, ValidationError{Code: "required", Field: "evidence", Message: "at least one evidence session is required: pass --evidence-session <id>"})
	}

	if len(in.Quotes) > len(in.Sessions) {
		errs = append(errs, ValidationError{Code: "range", Field: "evidence", Message: "more --evidence-quote than --evidence-session: quotes pair by position to sessions, so supply at most one quote per session", Value: len(in.Quotes)})
	}
	if len(in.Messages) > len(in.Sessions) {
		errs = append(errs, ValidationError{Code: "range", Field: "evidence", Message: "more --evidence-message than --evidence-session: messages pair by position to sessions, so supply at most one message per session", Value: len(in.Messages)})
	}
	if len(in.EvidenceFiles) > len(in.Sessions) {
		errs = append(errs, ValidationError{Code: "range", Field: "evidence", Message: "more --evidence-file than --evidence-session: files pair by position to sessions, so supply at most one file per session", Value: len(in.EvidenceFiles)})
	}
	if len(in.EvidenceCommits) > len(in.Sessions) {
		errs = append(errs, ValidationError{Code: "range", Field: "evidence", Message: "more --evidence-commit than --evidence-session: commits pair by position to sessions, so supply at most one commit per session", Value: len(in.EvidenceCommits)})
	}
	if len(in.EvidenceLineRanges) > len(in.Sessions) {
		errs = append(errs, ValidationError{Code: "range", Field: "evidence", Message: "more --evidence-line-range than --evidence-session: line ranges pair by position to sessions, so supply at most one line range per session", Value: len(in.EvidenceLineRanges)})
	}

	// Format-check the provenance values that are present so the audit trail stays
	// reliable: a commit must be lowercase-hex (7-40 chars) and a line range must be
	// a single line or an ascending span. Empty entries stay valid (capture is
	// best-effort), and excess entries are already flagged by the count checks above.
	for i, c := range in.EvidenceCommits {
		commit := strings.TrimSpace(c)
		if commit != "" && !commitRegex.MatchString(commit) {
			errs = append(errs, ValidationError{Code: "invalid_format", Field: fmt.Sprintf("evidence[%d].commit", i), Message: "commit must be a 7-40 char lowercase-hex git hash", Value: c})
		}
	}
	for i, lr := range in.EvidenceLineRanges {
		errs = append(errs, validateLineRange(i, lr)...)
	}

	return errs
}

// validateLineRange checks one --evidence-line-range value. Empty is valid
// (best-effort capture); otherwise it must match `start` or `start-end` with
// end >= start.
func validateLineRange(i int, lr string) []ValidationError {
	line := strings.TrimSpace(lr)
	if line == "" {
		return nil
	}
	field := fmt.Sprintf("evidence[%d].line_range", i)
	if !lineRangeRegex.MatchString(line) {
		return []ValidationError{{Code: "invalid_format", Field: field, Message: "line_range must be a single line (12) or an ascending span (12-34)", Value: lr}}
	}
	if start, end, ok := strings.Cut(line, "-"); ok {
		s, err1 := strconv.Atoi(start)
		e, err2 := strconv.Atoi(end)
		if err1 == nil && err2 == nil && e < s {
			return []ValidationError{{Code: "range", Field: field, Message: "line_range end must be >= start", Value: lr}}
		}
	}
	return nil
}

func validateDomainTags(domain []string) []ValidationError {
	errs := make([]ValidationError, 0)
	seen := make(map[string]struct{}, len(domain))
	for i, tag := range domain {
		field := fmt.Sprintf("domain[%d]", i)
		if !tagRegex.MatchString(tag) {
			errs = append(errs, ValidationError{Code: "invalid_format", Field: field, Message: "domain tag must match ^[a-z0-9]+(?:-[a-z0-9]+)*$", Value: tag})
			continue
		}
		if _, ok := seen[tag]; ok {
			errs = append(errs, ValidationError{Code: "duplicate", Field: field, Message: "duplicate domain tag after normalization", Value: tag})
			continue
		}
		seen[tag] = struct{}{}
	}
	return errs
}

// Payload pairs the parallel evidence slices and assembles the canonical
// ObservationPayload under the given id. Callers must Validate first; Payload
// assumes counts are already in range (extra quotes/messages are ignored). Quote
// text is preserved verbatim (not trimmed) since it is a captured excerpt.
func (in *Input) Payload(id string) events.ObservationPayload {
	evidence := make([]events.ObservationEvidence, 0, len(in.Sessions))
	for i, s := range in.Sessions {
		item := events.ObservationEvidence{SessionID: strings.TrimSpace(s)}
		if i < len(in.Quotes) {
			item.Quote = in.Quotes[i]
		}
		if i < len(in.Messages) {
			item.MessageID = strings.TrimSpace(in.Messages[i])
		}
		if i < len(in.EvidenceFiles) {
			item.File = strings.TrimSpace(in.EvidenceFiles[i])
		}
		if i < len(in.EvidenceLineRanges) {
			item.LineRange = strings.TrimSpace(in.EvidenceLineRanges[i])
		}
		if i < len(in.EvidenceCommits) {
			item.Commit = strings.TrimSpace(in.EvidenceCommits[i])
		}
		evidence = append(evidence, item)
	}

	severity := strings.ToLower(strings.TrimSpace(in.Severity))
	if severity == "" {
		severity = SeverityNormal
	}

	return events.ObservationPayload{
		ObservationID:           id,
		TaskID:                  strings.TrimSpace(in.TaskID),
		Kind:                    strings.ToLower(strings.TrimSpace(in.Kind)),
		Subject:                 strings.TrimSpace(in.Subject),
		Evidence:                evidence,
		Context:                 strings.TrimSpace(in.Context),
		SuggestedGeneralization: strings.TrimSpace(in.SuggestedGeneralization),
		Domain:                  normalizeDomain(in.Domain),
		Severity:                severity,
	}
}

// Project decodes an observation event and attaches the envelope fields (event
// id, ts, session) to the payload for list output. It returns an error if the
// event is not an observation or its payload does not decode.
func Project(e *events.Event) (Observation, error) {
	if e.Type != events.TypeObservation {
		return Observation{}, fmt.Errorf("event %s is not an observation (type %q)", e.ID, e.Type)
	}
	var payload events.ObservationPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return Observation{}, fmt.Errorf("decode observation payload in %s: %w", e.ID, err)
	}
	return Observation{
		ID:                 e.ID,
		TS:                 e.TS,
		SessionID:          e.SessionID,
		ObservationPayload: payload,
	}, nil
}

// ValidID reports whether s matches the observation id format. Exposed so later
// slices (consolidation/reader) can validate references without re-deriving the
// pattern.
func ValidID(s string) bool {
	return idRegex.MatchString(s)
}
