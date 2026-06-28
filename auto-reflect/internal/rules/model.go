package rules

import (
	"strings"

	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/idhash"
	"github.com/mistakenot/auto-shared/config"
)

const (
	idPattern  = `^r-[0-9a-f]{8}$`
	tagPattern = `^[a-z0-9]+(?:-[a-z0-9]+)*$`
)

// SchemaVersion is the playbook snapshot schema version. The snapshot is a
// disposable cache derived from the event log; this bumps only when the folded
// Playbook/Rule shape changes.
const SchemaVersion = 1

// Rule types.
const (
	RuleTypeHard = "hard"
	RuleTypeSoft = "soft"
)

// Rule lifecycle states.
const (
	LifecycleDraft     = "draft"
	LifecycleConfirmed = "confirmed"
	LifecycleStale     = "stale"
	LifecycleEnforced  = "enforced"
)

// Rule is the folded projection of a rule's create/edit history. Rules are never
// written directly; they are derived from the event log by Fold.
type Rule struct {
	ID         string   `json:"id"`
	Domain     []string `json:"domain"`
	UseWhen    string   `json:"use_when"`
	Content    string   `json:"content"`
	CausalNote string   `json:"causal_note"`
	RuleType   string   `json:"rule_type"`
	Lifecycle  string   `json:"lifecycle"`
	Version    int      `json:"version"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
	// ObservationIDs is the consolidation provenance: the observations this rule
	// generalizes. Empty for rules created directly via `rule create`; populated
	// by `consolidate`. Optional on legacy rules, so validation never requires it.
	ObservationIDs []string `json:"observation_ids,omitempty"`
	// PredecessorIDs / SuccessorIDs record rule lineage (the rules this one
	// supersedes / was superseded by). Declared now; populated in a later phase.
	PredecessorIDs []string `json:"predecessor_ids,omitempty"`
	SuccessorIDs   []string `json:"successor_ids,omitempty"`
	// LintRef is the record-only static lint check this rule graduated into. Set
	// by `rule graduate`; nil otherwise. The tool stores it verbatim.
	LintRef *events.LintRef `json:"lint_ref,omitempty"`
}

// Conflict records a from_version mismatch resolved during a fold. The losing
// edit is not lost (it remains in the event log); the conflict is surfaced so a
// human can review the overwritten change.
type Conflict struct {
	RuleID   string   `json:"rule_id"`
	Fields   []string `json:"fields"`
	Expected int      `json:"expected"`
	Actual   int      `json:"actual"`
}

// Playbook is the snapshot cache of the folded rule set. It is committed to git
// for human-reviewable PR diffs but is fully derivable from the event log.
type Playbook struct {
	SchemaVersion int            `json:"schema_version"`
	FoldedThrough map[string]int `json:"folded_through"`
	Rules         []Rule         `json:"rules"`
}

type ValidationError = config.ValidationError

// NewRuleID mints a content-derived rule id matching ^r-[0-9a-f]{8}$ via
// idhash.Derive over the supplied canonical content parts. Identical content
// yields an identical id, so a consolidate --dry-run and its apply mint the same
// id and re-running an identical consolidate is idempotent (a rule_created for an
// existing id is ignored by Fold). Pass the parts from Rule.CanonicalParts so
// minting agrees with the stored payload.
func NewRuleID(parts ...string) string {
	return idhash.Derive("r", parts...)
}

// CanonicalParts returns the normalized content fields that derive a rule's
// content-hash id: the domain tags, use_when, content, and rule_type. Lifecycle,
// version, provenance (observation_ids), and lineage are deliberately excluded —
// they are mutable metadata, not identity, so attaching evidence or retiring a
// rule never changes its id. The fields are read from the Rule after the caller's
// normalization (NormalizeDomain/TrimSpace) so id minting uses exactly the same
// content as the stored event payload.
func (r *Rule) CanonicalParts() []string {
	return []string{strings.Join(r.Domain, ","), r.UseWhen, r.Content, r.RuleType}
}
