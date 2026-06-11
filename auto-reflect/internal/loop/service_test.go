package loop

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/rules"
	"github.com/mistakenot/auto-reflect/internal/store"
)

// mustAppend appends a raw event to repo, failing the test on error. It bypasses
// the loop's validating writers so a Stats fold can be exercised over a precise,
// hand-built event sequence.
func mustAppend(t *testing.T, repo, eventType string, payload any) {
	t.Helper()
	if _, err := events.AppendEvent(repo, eventType, payload, events.AppendOptions{}); err != nil {
		t.Fatalf("append %s: %v", eventType, err)
	}
}

// seedRule appends a rule_created event and refolds the snapshot, returning the
// rule id.
func seedRule(t *testing.T, repo, useWhen, content, domain, ruleType string) string {
	t.Helper()
	id := rules.NewRuleID()
	payload := events.RuleCreatedPayload{
		RuleID:     id,
		Domain:     []string{domain},
		UseWhen:    useWhen,
		Content:    content,
		CausalNote: "because " + content,
		RuleType:   ruleType,
		Lifecycle:  rules.LifecycleDraft,
	}
	if _, err := events.AppendEvent(repo, events.TypeRuleCreated, payload, events.AppendOptions{}); err != nil {
		t.Fatalf("append rule_created: %v", err)
	}
	if _, _, err := rules.Rebuild(repo, store.PlaybookPath(repo)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	return id
}

func TestRetrieveReturnsPredicatesOnlyAndAppendsEvent(t *testing.T) {
	repo := initLoopRepo(t)
	seedRule(t, repo, "writing go cli flags", "use cobra StringSliceVar", "cli", rules.RuleTypeSoft)

	svc := NewService(repo)
	results, err := svc.Retrieve("writing go cli flags", nil, 0, true)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].RetrievalID == "" || !strings.HasPrefix(results[0].RetrievalID, "rt-") {
		t.Fatalf("expected rt- retrieval id, got %q", results[0].RetrievalID)
	}
	// Predicate-only: marshal and ensure no content/causal_note keys.
	b, err := json.Marshal(results[0])
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(b), "content") || strings.Contains(string(b), "causal_note") {
		t.Fatalf("retrieve result leaked content: %s", b)
	}

	all, err := events.ReadAll(repo)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	var retrievals int
	for _, ev := range all {
		if ev.Type == events.TypeRetrieval {
			retrievals++
		}
	}
	if retrievals != 1 {
		t.Fatalf("expected exactly one retrieval event, got %d", retrievals)
	}
}

func TestSelectPreservesInputOrder(t *testing.T) {
	repo := initLoopRepo(t)
	seedRule(t, repo, "alpha topic about go", "first content", "go", rules.RuleTypeSoft)
	seedRule(t, repo, "beta topic about go", "second content", "go", rules.RuleTypeSoft)
	seedRule(t, repo, "gamma topic about go", "third content", "go", rules.RuleTypeSoft)

	svc := NewService(repo)
	retrieved, err := svc.Retrieve("alpha beta gamma topic about go", nil, 0, true)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(retrieved) != 3 {
		t.Fatalf("expected 3 retrieved, got %d", len(retrieved))
	}

	// Select in reverse order; output must follow the input order.
	ordered := []string{retrieved[2].RetrievalID, retrieved[0].RetrievalID, retrieved[1].RetrievalID}
	selected, err := svc.Select(ordered)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(selected) != 3 {
		t.Fatalf("expected 3 selected, got %d", len(selected))
	}
	for _, s := range selected {
		if !strings.HasPrefix(s.FeedbackID, "fb-") {
			t.Fatalf("expected fb- feedback id, got %q", s.FeedbackID)
		}
		if s.Content == "" {
			t.Fatalf("select must reveal content")
		}
	}
	// The selection event must record the same order.
	all, _ := events.ReadAll(repo)
	var sel *events.SelectionPayload
	for _, ev := range all {
		if ev.Type == events.TypeSelection {
			var p events.SelectionPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatalf("decode selection: %v", err)
			}
			sel = &p
		}
	}
	if sel == nil || len(sel.Items) != 3 {
		t.Fatalf("expected one selection event with 3 items, got %#v", sel)
	}
	if sel.Items[0].RetrievalID != ordered[0] || sel.Items[1].RetrievalID != ordered[1] || sel.Items[2].RetrievalID != ordered[2] {
		t.Fatalf("selection did not preserve input order: %#v", sel.Items)
	}
}

func TestSelectDistinguishesUnknownVsWrongIDType(t *testing.T) {
	repo := initLoopRepo(t)
	svc := NewService(repo)

	_, errUnknown := svc.Select([]string{"rt-deadbeef"})
	if errUnknown == nil {
		t.Fatal("expected error for unknown rt-id")
	}

	_, errRulePrefix := svc.Select([]string{"r-12345678"})
	if errRulePrefix == nil {
		t.Fatal("expected error for r- prefixed id")
	}

	if errUnknown.Error() == errRulePrefix.Error() {
		t.Fatalf("unknown rt-id and r- id should produce distinct remediation, both were: %q", errUnknown.Error())
	}
	if !strings.Contains(errRulePrefix.Error(), "rule id") {
		t.Fatalf("r- prefix remediation should mention rule id, got: %q", errRulePrefix.Error())
	}
	if !strings.Contains(errUnknown.Error(), "unknown retrieval_id") {
		t.Fatalf("unknown rt-id remediation should say so, got: %q", errUnknown.Error())
	}

	_, errFbPrefix := svc.Select([]string{"fb-12345678"})
	if errFbPrefix == nil || errFbPrefix.Error() == errUnknown.Error() {
		t.Fatalf("fb- prefixed id should produce a distinct error, got: %v", errFbPrefix)
	}
}

func TestStatsNeverSurfacedRuleMarshalsWithZeroRate(t *testing.T) {
	repo := initLoopRepo(t)
	seedRule(t, repo, "never matched topic", "isolated content", "obscure", rules.RuleTypeSoft)

	svc := NewService(repo)
	report, err := svc.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	stats := report.Rules
	if len(stats) != 1 {
		t.Fatalf("expected 1 rule in stats, got %d", len(stats))
	}
	if stats[0].Surfaced != 0 {
		t.Fatalf("expected surfaced 0, got %d", stats[0].Surfaced)
	}
	if stats[0].SelectionRate != 0 {
		t.Fatalf("expected selection_rate 0 for never-surfaced rule, got %v", stats[0].SelectionRate)
	}
	// Must marshal cleanly (no NaN).
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("stats must marshal to valid JSON, got: %v", err)
	}
}

func TestStatsFoldRankOutcomeAndUnconsolidated(t *testing.T) {
	repo := initLoopRepo(t)
	ruleID := seedRule(t, repo, "topic", "content", "go", rules.RuleTypeSoft)

	// Selection precedes feedback so fb-id -> rule-id is known when feedback folds.
	mustAppend(t, repo, events.TypeSelection, events.SelectionPayload{Items: []events.SelectionItem{
		{FeedbackID: "fb-aaaaaaaa", RetrievalID: "rt-aaaaaaaa", RuleID: ruleID},
		{FeedbackID: "fb-bbbbbbbb", RetrievalID: "rt-bbbbbbbb", RuleID: ruleID},
	}})
	mustAppend(t, repo, events.TypeRetrieval, events.RetrievalPayload{Items: []events.RetrievalItem{
		{RetrievalID: "rt-aaaaaaaa", RuleID: ruleID},
		{RetrievalID: "rt-bbbbbbbb", RuleID: ruleID},
	}})
	mustAppend(t, repo, events.TypeFeedback, events.FeedbackPayload{
		Outcome:  OutcomeSuccess,
		Summary:  "worked",
		Rankings: []events.FeedbackRanking{{FeedbackID: "fb-aaaaaaaa", Rank: 1, Reason: "used it"}},
	})
	mustAppend(t, repo, events.TypeFeedback, events.FeedbackPayload{
		Outcome:  OutcomePartial,
		Summary:  "meh",
		Rankings: []events.FeedbackRanking{{FeedbackID: "fb-bbbbbbbb", Rank: 2, Reason: "kind of"}},
	})
	for i := range 3 {
		mustAppend(t, repo, events.TypeObservation, events.ObservationPayload{
			ObservationID: fmt.Sprintf("ob-0000000%d", i+1),
			Kind:          "gap",
			Subject:       "subject",
			Evidence:      []events.ObservationEvidence{{SessionID: "s1"}},
			Severity:      "normal",
		})
	}

	report, err := NewService(repo).Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if report.UnconsolidatedObservations != 3 {
		t.Fatalf("expected 3 unconsolidated observations, got %d", report.UnconsolidatedObservations)
	}
	if len(report.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(report.Rules))
	}
	rs := report.Rules[0]
	if rs.RuleID != ruleID {
		t.Fatalf("unexpected rule id %q", rs.RuleID)
	}
	if rs.Surfaced != 2 || rs.Selected != 2 || rs.FeedbackCount != 2 {
		t.Fatalf("unexpected counts: %#v", rs)
	}
	if rs.RankDistribution[1] != 1 || rs.RankDistribution[2] != 1 || len(rs.RankDistribution) != 2 {
		t.Fatalf("unexpected rank_distribution: %#v", rs.RankDistribution)
	}
	if rs.OutcomeCounts[OutcomeSuccess] != 1 || rs.OutcomeCounts[OutcomePartial] != 1 || len(rs.OutcomeCounts) != 2 {
		t.Fatalf("unexpected outcome_counts: %#v", rs.OutcomeCounts)
	}

	// Maps must marshal as objects, never null.
	b, err := json.Marshal(rs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"rank_distribution"`) || !strings.Contains(string(b), `"outcome_counts"`) {
		t.Fatalf("expected new fields in JSON: %s", b)
	}
}

// TestStatsOutcomeCountedOncePerFeedbackEvent asserts that a rule ranked twice
// within ONE feedback event records a single outcome (deduped per event), while
// rank_distribution still counts each ranking.
func TestStatsOutcomeCountedOncePerFeedbackEvent(t *testing.T) {
	repo := initLoopRepo(t)
	ruleID := seedRule(t, repo, "topic", "content", "go", rules.RuleTypeSoft)

	mustAppend(t, repo, events.TypeSelection, events.SelectionPayload{Items: []events.SelectionItem{
		{FeedbackID: "fb-aaaaaaaa", RetrievalID: "rt-aaaaaaaa", RuleID: ruleID},
		{FeedbackID: "fb-bbbbbbbb", RetrievalID: "rt-bbbbbbbb", RuleID: ruleID},
	}})
	mustAppend(t, repo, events.TypeFeedback, events.FeedbackPayload{
		Outcome: OutcomeSuccess,
		Summary: "both",
		Rankings: []events.FeedbackRanking{
			{FeedbackID: "fb-aaaaaaaa", Rank: 1, Reason: "a"},
			{FeedbackID: "fb-bbbbbbbb", Rank: 1, Reason: "b"},
		},
	})

	report, err := NewService(repo).Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	rs := report.Rules[0]
	if rs.OutcomeCounts[OutcomeSuccess] != 1 {
		t.Fatalf("expected outcome counted once per feedback event, got %#v", rs.OutcomeCounts)
	}
	if rs.RankDistribution[1] != 2 {
		t.Fatalf("expected rank 1 counted per ranking (2), got %#v", rs.RankDistribution)
	}
	if rs.FeedbackCount != 2 {
		t.Fatalf("expected feedback_count 2 (per ranking), got %d", rs.FeedbackCount)
	}
}

func TestStatsCountsAcrossSessions(t *testing.T) {
	repo := initLoopRepo(t)
	id := seedRule(t, repo, "go cli flags topic", "rule body", "go", rules.RuleTypeSoft)
	svc := NewService(repo)

	// Two simulated sessions each retrieve + select the rule.
	for range 2 {
		retrieved, err := svc.Retrieve("go cli flags topic", nil, 0, true)
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if _, err := svc.Select([]string{retrieved[0].RetrievalID}); err != nil {
			t.Fatalf("select: %v", err)
		}
	}

	report, err := svc.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	stats := report.Rules
	if len(stats) != 1 || stats[0].RuleID != id {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if stats[0].Surfaced != 2 || stats[0].Selected != 2 {
		t.Fatalf("expected surfaced=2 selected=2, got surfaced=%d selected=%d", stats[0].Surfaced, stats[0].Selected)
	}
	if stats[0].SelectionRate != 1.0 {
		t.Fatalf("expected selection_rate 1.0, got %v", stats[0].SelectionRate)
	}
}
