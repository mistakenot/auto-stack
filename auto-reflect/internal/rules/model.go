package rules

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/mistakenot/auto-reflect/internal/events"
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

// NewRuleID mints a fresh rule id matching ^r-[0-9a-f]{8}$ from crypto/rand,
// falling back to UnixNano when randomness is unavailable.
func NewRuleID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("r-%08x", uint32(time.Now().UnixNano()))
	}
	return fmt.Sprintf("r-%02x%02x%02x%02x", buf[0], buf[1], buf[2], buf[3])
}
