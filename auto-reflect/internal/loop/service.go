// Package loop implements the agent-facing retrieval state machine on top of the
// event log: retrieve (mint rt- ids) -> select (mint fb- ids) -> feedback (close
// the loop) -> gate (block until feedback is submitted). Rules are the folded
// projection; everything here appends events and reads back from the log.
package loop

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/mistakenot/auto-shared/config"

	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/rules"
	"github.com/mistakenot/auto-reflect/internal/store"
)

// ValidationError is the shared structured field-level error.
type ValidationError = config.ValidationError

// Service exposes the retrieval loop operations for a single repository root.
type Service struct {
	cwd string
}

// NewService builds a loop Service rooted at cwd. The cwd resolves to the
// worktree root via the lenient repo detector on each call.
func NewService(cwd string) *Service {
	return &Service{cwd: cwd}
}

// RetrievedRule is one predicate-only result returned to the agent. It carries
// no content: the agent commits to an ordering via Select before content is
// revealed.
type RetrievedRule struct {
	RetrievalID string   `json:"retrieval_id"`
	UseWhen     string   `json:"use_when"`
	Domain      []string `json:"domain"`
	RuleType    string   `json:"rule_type"`
	Lifecycle   string   `json:"lifecycle"`
	Draft       bool     `json:"draft"`
}

// Retrieve matches rules against intent (optionally filtered by domains),
// appends one retrieval event that mints an rt- id per match, and returns the
// predicate-only view in match order. limit <= 0 means no limit. includeDrafts
// surfaces draft rules (flagged Draft) alongside confirmed ones; stale rules are
// never surfaced regardless.
func (s *Service) Retrieve(intent string, domains []string, limit int, includeDrafts bool) ([]RetrievedRule, error) {
	repo, err := gitutil.DetectRepoLenient(s.cwd)
	if err != nil {
		return nil, err
	}

	playbook, err := rules.Load(repo.Root, store.PlaybookPath(repo.Root))
	if err != nil {
		return nil, err
	}

	matches := rules.MatchRules(playbook.Rules, intent, domains, includeDrafts)
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}

	items := make([]events.RetrievalItem, 0, len(matches))
	results := make([]RetrievedRule, 0, len(matches))
	for i := range matches {
		m := &matches[i]
		rtID, err := newRetrievalID()
		if err != nil {
			return nil, err
		}
		items = append(items, events.RetrievalItem{
			RetrievalID:  rtID,
			RuleID:       m.Rule.ID,
			RuleVersion:  m.Rule.Version,
			MatchScore:   m.MatchScore,
			HardInjected: m.HardInjected,
		})
		results = append(results, RetrievedRule{
			RetrievalID: rtID,
			UseWhen:     m.Rule.UseWhen,
			Domain:      m.Rule.Domain,
			RuleType:    m.Rule.RuleType,
			Lifecycle:   m.Rule.Lifecycle,
			Draft:       m.Rule.Lifecycle == rules.LifecycleDraft,
		})
	}

	payload := events.RetrievalPayload{
		Intent: intent,
		Domain: domains,
		Limit:  limit,
		Items:  items,
	}
	if _, err := events.AppendEvent(s.cwd, events.TypeRetrieval, payload, events.AppendOptions{}); err != nil {
		return nil, err
	}

	return results, nil
}

// SelectedRule is one rule the agent committed to, full content revealed, in the
// input order. The feedback_id is the handle the agent must rank in its later
// feedback submission.
type SelectedRule struct {
	FeedbackID string `json:"feedback_id"`
	Content    string `json:"content"`
	CausalNote string `json:"causal_note"`
	RuleType   string `json:"rule_type"`
}

// Select resolves the ordered retrieval ids against prior retrieval events,
// mints an fb- id per rule, appends one ordered selection event, and returns the
// full rule content in the same order. An r-/fb- prefixed argument or an unknown
// rt- id produces a distinct structured error with remediation.
func (s *Service) Select(orderedRetrievalIDs []string) ([]SelectedRule, error) {
	repo, err := gitutil.DetectRepoLenient(s.cwd)
	if err != nil {
		return nil, err
	}

	if len(orderedRetrievalIDs) == 0 {
		return nil, &LoopError{Message: "no retrieval ids given: run `auto reflect retrieve <intent>` first, then pass the returned retrieval_id(s)"}
	}

	// Build a rt-id -> rule_id index from retrieval events.
	all, err := events.ReadAll(repo.Root)
	if err != nil {
		return nil, err
	}
	ruleByRetrieval := indexRetrievals(all)

	playbook, err := rules.Load(repo.Root, store.PlaybookPath(repo.Root))
	if err != nil {
		return nil, err
	}
	ruleIndex := make(map[string]rules.Rule, len(playbook.Rules))
	for i := range playbook.Rules {
		ruleIndex[playbook.Rules[i].ID] = playbook.Rules[i]
	}

	items := make([]events.SelectionItem, 0, len(orderedRetrievalIDs))
	results := make([]SelectedRule, 0, len(orderedRetrievalIDs))
	for _, rawID := range orderedRetrievalIDs {
		id := strings.TrimSpace(rawID)
		if err := classifyRetrievalArg(id); err != nil {
			return nil, err
		}
		ruleID, ok := ruleByRetrieval[id]
		if !ok {
			return nil, &LoopError{Message: fmt.Sprintf("unknown retrieval_id %q: it was not minted by a prior retrieve in this repo; re-run `auto reflect retrieve <intent>` and use the returned retrieval_id(s)", id)}
		}
		rule, ok := ruleIndex[ruleID]
		if !ok {
			return nil, &LoopError{Message: fmt.Sprintf("retrieval_id %q points at rule %q which is no longer in the playbook: run `auto reflect retrieve <intent>` again", id, ruleID)}
		}

		fbID, err := newFeedbackID()
		if err != nil {
			return nil, err
		}
		items = append(items, events.SelectionItem{
			FeedbackID:  fbID,
			RetrievalID: id,
			RuleID:      ruleID,
		})
		results = append(results, SelectedRule{
			FeedbackID: fbID,
			Content:    rule.Content,
			CausalNote: rule.CausalNote,
			RuleType:   rule.RuleType,
		})
	}

	payload := events.SelectionPayload{Items: items}
	if _, err := events.AppendEvent(s.cwd, events.TypeSelection, payload, events.AppendOptions{}); err != nil {
		return nil, err
	}

	return results, nil
}

// RuleStats is the per-rule usage summary returned by Stats. RankDistribution
// maps each feedback rank to the number of times this rule received it;
// OutcomeCounts maps each feedback outcome (success/partial/fail/abandoned) to
// the number of feedback events covering this rule with that outcome. Both are
// always non-nil so they marshal as {} rather than null.
type RuleStats struct {
	RuleID           string         `json:"rule_id"`
	Surfaced         int            `json:"surfaced"`
	Selected         int            `json:"selected"`
	SelectionRate    float64        `json:"selection_rate"`
	FeedbackCount    int            `json:"feedback_count"`
	RankDistribution map[int]int    `json:"rank_distribution"`
	OutcomeCounts    map[string]int `json:"outcome_counts"`
}

// StatsReport is the full stats projection: the repo-level unconsolidated
// observation backlog plus the per-rule usage rows. Rules is always listed in
// full (the list-returns-all convention).
type StatsReport struct {
	UnconsolidatedObservations int         `json:"unconsolidated_observations"`
	Rules                      []RuleStats `json:"rules"`
}

// Stats folds the event log into the per-rule retrieval/selection/feedback
// counts plus rank distribution, outcome counts, and the repo-level
// unconsolidated-observation backlog. Every rule in the playbook is listed (per
// the list-returns-all convention); selection_rate is hard-defined to 0 when
// surfaced == 0 so it never marshals a NaN that encoding/json would reject. It is
// a pure fold over events.ReadAll and writes no events.
func (s *Service) Stats() (StatsReport, error) {
	repo, err := gitutil.DetectRepoLenient(s.cwd)
	if err != nil {
		return StatsReport{}, err
	}

	playbook, err := rules.Load(repo.Root, store.PlaybookPath(repo.Root))
	if err != nil {
		return StatsReport{}, err
	}

	all, err := events.ReadAll(repo.Root)
	if err != nil {
		return StatsReport{}, err
	}

	surfaced := map[string]int{}
	selected := map[string]int{}
	feedbackCount := map[string]int{}
	rankDist := map[string]map[int]int{}
	outcomeCounts := map[string]map[string]int{}
	// observationIDs is the set of distinct observation ids seen; consolidatedIDs
	// is the set referenced by any consolidation event. The unconsolidated backlog
	// is the difference, computed after the fold so a consolidation event ordered
	// before its observation (it never is, but be order-independent) still counts.
	observationIDs := map[string]struct{}{}
	consolidatedIDs := map[string]struct{}{}

	// fb-id -> rule-id, so feedback can attribute to rules. Selection events
	// precede their feedback in time order, so the mapping is populated by the
	// time a feedback event is folded.
	ruleByFeedback := map[string]string{}
	for i := range all {
		ev := &all[i]
		switch ev.Type {
		case events.TypeRetrieval:
			var p events.RetrievalPayload
			if decodePayload(ev, &p) != nil {
				continue
			}
			for _, it := range p.Items {
				surfaced[it.RuleID]++
			}
		case events.TypeSelection:
			var p events.SelectionPayload
			if decodePayload(ev, &p) != nil {
				continue
			}
			for _, it := range p.Items {
				selected[it.RuleID]++
				ruleByFeedback[it.FeedbackID] = it.RuleID
			}
		case events.TypeFeedback:
			var p events.FeedbackPayload
			if decodePayload(ev, &p) != nil {
				continue
			}
			// rank_distribution counts per ranking; outcome_counts counts each
			// (feedback event, rule) once, so a rule ranked twice in one event
			// still records a single outcome.
			rulesInEvent := map[string]struct{}{}
			for _, r := range p.Rankings {
				ruleID, ok := ruleByFeedback[r.FeedbackID]
				if !ok {
					continue
				}
				feedbackCount[ruleID]++
				if rankDist[ruleID] == nil {
					rankDist[ruleID] = map[int]int{}
				}
				rankDist[ruleID][r.Rank]++
				rulesInEvent[ruleID] = struct{}{}
			}
			for ruleID := range rulesInEvent {
				if outcomeCounts[ruleID] == nil {
					outcomeCounts[ruleID] = map[string]int{}
				}
				outcomeCounts[ruleID][p.Outcome]++
			}
		case events.TypeObservation:
			var p events.ObservationPayload
			if decodePayload(ev, &p) != nil {
				continue
			}
			observationIDs[p.ObservationID] = struct{}{}
		case events.TypeConsolidation:
			var p events.ConsolidationPayload
			if decodePayload(ev, &p) != nil {
				continue
			}
			for _, id := range p.ObservationIDs {
				consolidatedIDs[id] = struct{}{}
			}
		}
	}

	// An observation is unconsolidated until some consolidation event references it.
	unconsolidated := 0
	for id := range observationIDs {
		if _, done := consolidatedIDs[id]; !done {
			unconsolidated++
		}
	}

	out := make([]RuleStats, 0, len(playbook.Rules))
	for i := range playbook.Rules {
		r := &playbook.Rules[i]
		s := surfaced[r.ID]
		sel := selected[r.ID]
		rate := 0.0
		if s > 0 {
			rate = float64(sel) / float64(s)
		}
		ranks := rankDist[r.ID]
		if ranks == nil {
			ranks = map[int]int{}
		}
		outcomes := outcomeCounts[r.ID]
		if outcomes == nil {
			outcomes = map[string]int{}
		}
		out = append(out, RuleStats{
			RuleID:           r.ID,
			Surfaced:         s,
			Selected:         sel,
			SelectionRate:    rate,
			FeedbackCount:    feedbackCount[r.ID],
			RankDistribution: ranks,
			OutcomeCounts:    outcomes,
		})
	}
	return StatsReport{
		UnconsolidatedObservations: unconsolidated,
		Rules:                      out,
	}, nil
}

// indexRetrievals returns a rt-id -> rule-id map across all retrieval events.
func indexRetrievals(all []events.Event) map[string]string {
	out := map[string]string{}
	for i := range all {
		ev := &all[i]
		if ev.Type != events.TypeRetrieval {
			continue
		}
		var p events.RetrievalPayload
		if decodePayload(ev, &p) != nil {
			continue
		}
		for _, it := range p.Items {
			out[it.RetrievalID] = it.RuleID
		}
	}
	return out
}

// classifyRetrievalArg returns a targeted wrong-id-type error for an argument
// that is clearly the wrong kind of id (r- rule id, fb- feedback id) so the
// agent gets a specific remediation rather than a generic "unknown id".
func classifyRetrievalArg(id string) error {
	switch {
	case strings.HasPrefix(id, "r-"):
		return &LoopError{Message: fmt.Sprintf("%q is a rule id, not a retrieval_id: pass the rt- id from `auto reflect retrieve`, or use `auto reflect rule get %s` to read the rule directly", id, id)}
	case strings.HasPrefix(id, "fb-"):
		return &LoopError{Message: fmt.Sprintf("%q is a feedback id, not a retrieval_id: that rule has already been selected; submit feedback with `auto reflect feedback <json>` to close the loop", id)}
	}
	return nil
}

// LoopError is a structured loop error carrying a remediation-bearing message.
type LoopError struct {
	Message string
}

func (e *LoopError) Error() string { return e.Message }

func newRetrievalID() (string, error) { return newPrefixedID("rt-") }

func newFeedbackID() (string, error) { return newPrefixedID("fb-") }

// newPrefixedID mints an id matching <prefix>[0-9a-f]{8} using crypto/rand, the
// same scheme as rules.NewRuleID.
func newPrefixedID(prefix string) (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		//nolint:nilerr // intentional fallback: degrade to a UnixNano-seeded id rather than failing id minting
		return fmt.Sprintf("%s%08x", prefix, uint32(time.Now().UnixNano())), nil
	}
	return fmt.Sprintf("%s%02x%02x%02x%02x", prefix, buf[0], buf[1], buf[2], buf[3]), nil
}
