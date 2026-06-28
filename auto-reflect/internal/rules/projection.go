package rules

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/mistakenot/auto-reflect/internal/events"
)

// Editable rule field names carried in rule_edited deltas.
const (
	FieldDomain         = "domain"
	FieldUseWhen        = "use_when"
	FieldContent        = "content"
	FieldCausalNote     = "causal_note"
	FieldRuleType       = "rule_type"
	FieldLifecycle      = "lifecycle"
	FieldObservationIDs = "observation_ids"
	FieldLintRef        = "lint_ref"
	FieldSuccessorIDs   = "successor_ids"
	FieldPredecessorIDs = "predecessor_ids"
)

// FoldResult is the output of folding the event log: the projected playbook plus
// any conflicts encountered while applying edits in total order.
type FoldResult struct {
	Playbook  Playbook
	Conflicts []Conflict
}

// Fold takes events already in the canonical total order (ts, shard, seq) as
// returned by events.ReadAllSharded. It applies rule_created/rule_edited events
// deterministically and returns the folded playbook with conflict entries for
// any from_version mismatch.
func Fold(sharded []events.ShardedEvent) FoldResult {
	byID := make(map[string]*Rule)
	var order []string // rule ids in creation order, for stable output
	folded := make(map[string]int)
	var conflicts []Conflict

	for i := range sharded {
		ev := sharded[i].Event
		shard := sharded[i].Shard

		switch ev.Type {
		case events.TypeRuleCreated:
			var p events.RuleCreatedPayload
			if json.Unmarshal(ev.Payload, &p) != nil {
				continue
			}
			if _, exists := byID[p.RuleID]; exists {
				// A create for an existing id is ignored; the original wins.
				advanceFolded(folded, shard, ev.Seq)
				continue
			}
			rule := &Rule{
				ID:             p.RuleID,
				Domain:         append([]string{}, p.Domain...),
				UseWhen:        p.UseWhen,
				Content:        p.Content,
				CausalNote:     p.CausalNote,
				RuleType:       p.RuleType,
				Lifecycle:      p.Lifecycle,
				Version:        1,
				CreatedAt:      ev.TS,
				UpdatedAt:      ev.TS,
				ObservationIDs: copyNonEmpty(p.ObservationIDs),
				PredecessorIDs: copyNonEmpty(p.PredecessorIDs),
				SuccessorIDs:   copyNonEmpty(p.SuccessorIDs),
				LintRef:        p.LintRef,
			}
			byID[p.RuleID] = rule
			order = append(order, p.RuleID)
			advanceFolded(folded, shard, ev.Seq)

		case events.TypeRuleEdited:
			var p events.RuleEditedPayload
			if json.Unmarshal(ev.Payload, &p) != nil {
				continue
			}
			rule, exists := byID[p.RuleID]
			if !exists {
				// Edit for an unknown rule is dropped; nothing to apply.
				advanceFolded(folded, shard, ev.Seq)
				continue
			}

			if p.FromVersion != rule.Version {
				conflicts = append(conflicts, Conflict{
					RuleID:   p.RuleID,
					Fields:   deltaFields(p.Deltas),
					Expected: p.FromVersion,
					Actual:   rule.Version,
				})
			}

			for _, d := range p.Deltas {
				applyDelta(rule, d)
			}
			rule.Version++
			rule.UpdatedAt = ev.TS
			advanceFolded(folded, shard, ev.Seq)

		default:
			// Non-rule events never dirty the projection or folded_through.
		}
	}

	out := make([]Rule, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return FoldResult{
		Playbook: Playbook{
			SchemaVersion: SchemaVersion,
			FoldedThrough: folded,
			Rules:         out,
		},
		Conflicts: conflicts,
	}
}

// advanceFolded records the seq of a rule event as the per-shard high-water
// mark. Only rule events reach here, so non-rule events never advance it.
func advanceFolded(folded map[string]int, shard string, seq int) {
	if shard == "" {
		return
	}
	if seq > folded[shard] {
		folded[shard] = seq
	}
}

func deltaFields(deltas []events.FieldDelta) []string {
	fields := make([]string, 0, len(deltas))
	for _, d := range deltas {
		fields = append(fields, d.Field)
	}
	return fields
}

// applyDelta mutates the rule with one field delta. The delta's New value is
// decoded from the generic JSON representation into the field's concrete type.
func applyDelta(rule *Rule, d events.FieldDelta) {
	switch d.Field {
	case FieldDomain:
		rule.Domain = toStringSlice(d.New)
	case FieldUseWhen:
		rule.UseWhen = toString(d.New)
	case FieldContent:
		rule.Content = toString(d.New)
	case FieldCausalNote:
		rule.CausalNote = toString(d.New)
	case FieldRuleType:
		rule.RuleType = toString(d.New)
	case FieldLifecycle:
		rule.Lifecycle = toString(d.New)
	case FieldObservationIDs:
		rule.ObservationIDs = copyNonEmpty(toStringSlice(d.New))
	case FieldSuccessorIDs:
		rule.SuccessorIDs = copyNonEmpty(toStringSlice(d.New))
	case FieldPredecessorIDs:
		rule.PredecessorIDs = copyNonEmpty(toStringSlice(d.New))
	case FieldLintRef:
		rule.LintRef = toLintRef(d.New)
	}
}

// toLintRef decodes a delta's generic JSON value into a *events.LintRef. A nil
// or absent value (a cleared lint_ref) decodes to nil so omitempty drops it.
func toLintRef(v any) *events.LintRef {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var ref events.LintRef
	if json.Unmarshal(b, &ref) != nil {
		return nil
	}
	return &ref
}

// copyNonEmpty returns a copy of src, or nil when src is empty, so an absent
// provenance list stays nil (and omitempty drops it) rather than becoming [].
func copyNonEmpty(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	return append([]string{}, src...)
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	// Round-trip through JSON for any non-string scalar; unknown shapes become "".
	if v == nil {
		return ""
	}
	if b, err := json.Marshal(v); err == nil {
		var s string
		if json.Unmarshal(b, &s) == nil {
			return s
		}
	}
	return fmt.Sprintf("%v", v)
}

func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return append([]string{}, t...)
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			out = append(out, toString(item))
		}
		return out
	case nil:
		return []string{}
	default:
		// Round-trip through JSON to coerce e.g. json.RawMessage.
		if b, err := json.Marshal(v); err == nil {
			var out []string
			if json.Unmarshal(b, &out) == nil {
				return out
			}
		}
		return []string{}
	}
}
