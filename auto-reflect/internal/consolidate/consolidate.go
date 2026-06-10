// Package consolidate holds the deterministic, LLM-free gates that turn a
// consolidation delta document into playbook changes: evidence-threshold checks,
// duplicate detection, and (non-blocking) conflict flagging. The CLI layer
// (internal/cli/consolidate.go) owns parsing the repo, writing events, and
// refolding; this package is pure logic over in-memory inputs so it is cheap to
// unit-test.
package consolidate

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/rules"
)

// Delta operations.
const (
	OpCreateDraft    = "create-draft"
	OpAttachEvidence = "attach-evidence"
	OpMerge          = "merge"
	OpDeprecate      = "deprecate"
)

// EvidenceMinSessions is the number of distinct evidence sessions a create-draft
// must cover before it may mint a rule without --force or a high-severity
// (incident) observation.
const EvidenceMinSessions = 2

// severityHigh marks an incident observation, which auto-bypasses the evidence
// threshold for the delta that cites it. Mirrors observations.SeverityHigh,
// duplicated to keep this package free of the observations import.
const severityHigh = "high"

// DedupeScoreThreshold is the normalized MatchRules score at or above which a
// candidate use_when is treated as a near-duplicate of an existing rule and the
// create-draft is refused (use attach-evidence instead). A use_when that fully
// overlaps an existing rule's use_when scores 0.75, so 0.5 catches strong
// overlap while leaving merely domain-adjacent rules alone.
const DedupeScoreThreshold = 0.5

// Document is a consolidation delta document, mirroring feedback's JSON input.
type Document struct {
	Deltas []Delta `json:"deltas"`
}

// Delta is one consolidation operation. Fields are a union across ops; Validate
// enforces which are required per Op.
type Delta struct {
	Op             string   `json:"op"`
	UseWhen        string   `json:"use_when,omitempty"`
	Content        string   `json:"content,omitempty"`
	CausalNote     string   `json:"causal_note,omitempty"`
	Domain         []string `json:"domain,omitempty"`
	Type           string   `json:"type,omitempty"`
	RuleID         string   `json:"rule_id,omitempty"`
	RuleIDs        []string `json:"rule_ids,omitempty"`
	IntoUseWhen    string   `json:"into_use_when,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	ObservationIDs []string `json:"observation_ids,omitempty"`
}

// ParseDocument strictly decodes a delta document, rejecting unknown fields so a
// typo'd op or field is a fast, explicit error rather than a silent no-op.
func ParseDocument(raw []byte) (Document, error) {
	var doc Document
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("invalid consolidation JSON: %w", err)
	}
	if len(doc.Deltas) == 0 {
		return Document{}, errors.New("no deltas: supply {\"deltas\":[{\"op\":\"create-draft\",...}]}")
	}
	return doc, nil
}

// ObservationIndex resolves ob- ids to their payloads, built once from the event
// log so per-delta gates avoid re-scanning events.
type ObservationIndex struct {
	byID map[string]events.ObservationPayload
}

// NewObservationIndex folds every observation event into a lookup keyed by
// observation id. A later observation event with the same id wins (observations
// are append-only and ids are unique in practice; this keeps it deterministic).
func NewObservationIndex(all []events.Event) ObservationIndex {
	byID := make(map[string]events.ObservationPayload)
	for i := range all {
		if all[i].Type != events.TypeObservation {
			continue
		}
		var p events.ObservationPayload
		if json.Unmarshal(all[i].Payload, &p) != nil {
			continue
		}
		byID[p.ObservationID] = p
	}
	return ObservationIndex{byID: byID}
}

// Lookup returns the observation payload for an id, if present.
func (idx ObservationIndex) Lookup(id string) (events.ObservationPayload, bool) {
	p, ok := idx.byID[id]
	return p, ok
}

// Coverage is the resolved evidence picture for a set of observation ids.
type Coverage struct {
	Sessions     []string // distinct evidence session ids, sorted
	HighSeverity bool     // any referenced observation is severity high (incident)
	Missing      []string // ids with no matching observation event
}

// Coverage resolves observation ids to their distinct evidence sessions and flags
// whether any is high severity or unknown.
func (idx ObservationIndex) Coverage(obIDs []string) Coverage {
	sessionSet := make(map[string]struct{})
	var cov Coverage
	for _, id := range obIDs {
		p, ok := idx.byID[strings.TrimSpace(id)]
		if !ok {
			cov.Missing = append(cov.Missing, id)
			continue
		}
		if p.Severity == severityHigh {
			cov.HighSeverity = true
		}
		for _, ev := range p.Evidence {
			s := strings.TrimSpace(ev.SessionID)
			if s == "" {
				continue
			}
			sessionSet[s] = struct{}{}
		}
	}
	cov.Sessions = make([]string, 0, len(sessionSet))
	for s := range sessionSet {
		cov.Sessions = append(cov.Sessions, s)
	}
	sort.Strings(cov.Sessions)
	return cov
}

// EvidenceGate reports whether a create-draft may proceed given its coverage and
// the --force flag, plus a human-readable reason when it may not. Unknown
// observation ids always block (you cannot ground a rule in evidence that does
// not exist), even under --force.
func EvidenceGate(cov Coverage, force bool) (ok bool, reason string) {
	if len(cov.Missing) > 0 {
		return false, "references unknown observation(s): " + strings.Join(cov.Missing, ", ")
	}
	if force || cov.HighSeverity || len(cov.Sessions) >= EvidenceMinSessions {
		return true, ""
	}
	return false, fmt.Sprintf("evidence covers %d distinct session(s); need >=%d (use --force, or cite a high-severity observation)", len(cov.Sessions), EvidenceMinSessions)
}

// Duplicate is the existing rule a create-draft collided with.
type Duplicate struct {
	RuleID  string  `json:"rule_id"`
	UseWhen string  `json:"use_when"`
	Score   float64 `json:"score"`
}

// DetectDuplicate runs the live matcher (drafts excluded, stale never surfaces)
// against the candidate use_when+domain. A top score at or above
// DedupeScoreThreshold means the candidate duplicates an existing rule and the
// delta should be refused in favour of attach-evidence.
func DetectDuplicate(playbook []rules.Rule, useWhen string, domain []string) (Duplicate, bool) {
	matches := rules.MatchRules(playbook, useWhen, domain, false)
	if len(matches) == 0 {
		return Duplicate{}, false
	}
	top := matches[0]
	if top.MatchScore < DedupeScoreThreshold {
		return Duplicate{}, false
	}
	return Duplicate{RuleID: top.Rule.ID, UseWhen: top.Rule.UseWhen, Score: top.MatchScore}, true
}

// Conflict is a non-blocking flag: an existing rule that shares a domain with a
// new/merged rule but whose guidance looks contradictory, surfaced for a human to
// reconcile rather than blocking the delta.
type Conflict struct {
	RuleID          string `json:"rule_id"`
	ExistingUseWhen string `json:"existing_use_when"`
	Reason          string `json:"reason"`
}

// DetectConflicts flags existing non-stale rules that share a domain tag with the
// candidate and whose content has the opposite negation polarity over a shared
// keyword (one says "do X", the other "don't X"). The check is deliberately
// conservative and never blocks; it only annotates the output.
func DetectConflicts(playbook []rules.Rule, domain []string, content string) []Conflict {
	want := lowerSet(domain)
	var out []Conflict
	for i := range playbook {
		r := &playbook[i]
		if r.Lifecycle == rules.LifecycleStale {
			continue
		}
		if !anyDomainShared(r.Domain, want) {
			continue
		}
		if looksContradictory(content, r.Content) {
			out = append(out, Conflict{
				RuleID:          r.ID,
				ExistingUseWhen: r.UseWhen,
				Reason:          "shares a domain but guidance polarity looks opposed; reconcile or supersede",
			})
		}
	}
	return out
}

var negationCues = []string{"don't", "do not", "never", "avoid", "without", "not ", "no "}

// looksContradictory reports whether two guidance strings share a meaningful
// keyword yet differ in negation polarity (exactly one carries a negation cue).
func looksContradictory(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if !shareKeyword(la, lb) {
		return false
	}
	return hasNegation(la) != hasNegation(lb)
}

func hasNegation(s string) bool {
	for _, cue := range negationCues {
		if strings.Contains(s, cue) {
			return true
		}
	}
	return false
}

// shareKeyword reports whether the two strings share a token of length >= 4
// (a crude content-overlap proxy that ignores short stop-words).
func shareKeyword(a, b string) bool {
	seen := make(map[string]struct{})
	for _, tok := range strings.FieldsFunc(a, isNotWord) {
		if len(tok) >= 4 {
			seen[tok] = struct{}{}
		}
	}
	for _, tok := range strings.FieldsFunc(b, isNotWord) {
		if len(tok) < 4 {
			continue
		}
		if _, ok := seen[tok]; ok {
			return true
		}
	}
	return false
}

func isNotWord(r rune) bool {
	return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
}

func anyDomainShared(have []string, want map[string]struct{}) bool {
	for _, h := range have {
		if _, ok := want[strings.ToLower(strings.TrimSpace(h))]; ok {
			return true
		}
	}
	return false
}

func lowerSet(tags []string) map[string]struct{} {
	out := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		n := strings.ToLower(strings.TrimSpace(t))
		if n != "" {
			out[n] = struct{}{}
		}
	}
	return out
}

// UnionObservationIDs merges two provenance lists, preserving first-seen order
// and dropping empties/duplicates, so attach-evidence/merge accumulate provenance
// without churn.
func UnionObservationIDs(existing, incoming []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	out := make([]string, 0, len(existing)+len(incoming))
	for _, group := range [][]string{existing, incoming} {
		for _, id := range group {
			n := strings.TrimSpace(id)
			if n == "" {
				continue
			}
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	return out
}
