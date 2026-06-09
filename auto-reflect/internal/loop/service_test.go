package loop

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/rules"
	"github.com/mistakenot/auto-reflect/internal/store"
)

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
	results, err := svc.Retrieve("writing go cli flags", nil, 0)
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
	b, _ := json.Marshal(results[0])
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
	retrieved, err := svc.Retrieve("alpha beta gamma topic about go", nil, 0)
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
	stats, err := svc.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
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
	if _, err := json.Marshal(stats); err != nil {
		t.Fatalf("stats must marshal to valid JSON, got: %v", err)
	}
}

func TestStatsCountsAcrossSessions(t *testing.T) {
	repo := initLoopRepo(t)
	id := seedRule(t, repo, "go cli flags topic", "rule body", "go", rules.RuleTypeSoft)
	svc := NewService(repo)

	// Two simulated sessions each retrieve + select the rule.
	for i := 0; i < 2; i++ {
		retrieved, err := svc.Retrieve("go cli flags topic", nil, 0)
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if _, err := svc.Select([]string{retrieved[0].RetrievalID}); err != nil {
			t.Fatalf("select: %v", err)
		}
	}

	stats, err := svc.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
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
