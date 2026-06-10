package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

type eventListResp struct {
	Scope  string `json:"scope"`
	Events []struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		TS        string `json:"ts"`
		Seq       int    `json:"seq"`
		SessionID string `json:"session_id"`
		Agent     string `json:"agent"`
		Summary   string `json:"summary"`
	} `json:"events"`
}

func listEvents(t *testing.T, repo string, args ...string) eventListResp {
	t.Helper()
	full := append([]string{"events", "list"}, args...)
	stdout, stderr, code := runCLIAt(t, repo, full...)
	if code != 0 {
		t.Fatalf("events list failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var resp eventListResp
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("decode events list: %v\nraw:\n%s", err, stdout)
	}
	return resp
}

// seedLoopAndObservations drives a full retrieve->select->feedback loop plus a
// couple of observations so the event log has every type the reader summarises.
func seedLoopAndObservations(t *testing.T, repo string) string {
	t.Helper()
	ruleID := createTestRuleWith(t, repo,
		"--use-when", "writing go cli flags with cobra",
		"--content", "Use cobra StringSliceVar for repeatable flags",
		"--causal-note", "manual parsing dropped values",
		"--domain", "cli", "--type", "soft")

	stdout, _, code := runCLIAt(t, repo, "retrieve", "writing go cli flags", "--domain", "cli")
	if code != 0 {
		t.Fatalf("retrieve failed")
	}
	var retrieved []map[string]any
	if err := json.Unmarshal([]byte(stdout), &retrieved); err != nil {
		t.Fatalf("decode retrieve: %v\nraw:\n%s", err, stdout)
	}
	rtID := retrieved[0]["retrieval_id"].(string)

	stdout, _, code = runCLIAt(t, repo, "select", rtID)
	if code != 0 {
		t.Fatalf("select failed")
	}
	var selected []map[string]any
	if err := json.Unmarshal([]byte(stdout), &selected); err != nil {
		t.Fatalf("decode select: %v\nraw:\n%s", err, stdout)
	}
	fbID := selected[0]["feedback_id"].(string)

	complete := `{"outcome":"success","summary":"shipped","rankings":[{"feedback_id":"` + fbID + `","rank":1,"reason":"used the rule"}]}`
	if _, stderr, code := runCLIAt(t, repo, "feedback", complete); code != 0 {
		t.Fatalf("feedback failed: %s", stderr)
	}

	addObservation(t, repo, "--kind", "gap", "--subject", "first gap", "--evidence-session", "s1", "--domain", "docs")
	addObservation(t, repo, "--kind", "correction", "--subject", "a correction", "--evidence-session", "s2")

	return ruleID
}

func TestEventsListFiltersAndOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_SESSION_ID", "events-list")
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	seedLoopAndObservations(t, repo)

	// Unfiltered: every event type present, newest-first (ts non-increasing).
	all := listEvents(t, repo)
	if len(all.Events) == 0 {
		t.Fatal("expected events, got none")
	}
	for i := 1; i < len(all.Events); i++ {
		if all.Events[i-1].TS < all.Events[i].TS {
			t.Fatalf("events not newest-first: %s before %s", all.Events[i-1].TS, all.Events[i].TS)
		}
	}
	seenTypes := map[string]bool{}
	for _, e := range all.Events {
		seenTypes[e.Type] = true
	}
	for _, want := range []string{"rule_created", "retrieval", "selection", "feedback", "observation"} {
		if !seenTypes[want] {
			t.Fatalf("expected a %s event in the log; got types %v", want, seenTypes)
		}
	}

	// --type filter returns only feedback.
	feedback := listEvents(t, repo, "--type", "feedback")
	if len(feedback.Events) != 1 {
		t.Fatalf("expected 1 feedback event, got %d", len(feedback.Events))
	}
	if feedback.Events[0].Type != "feedback" {
		t.Fatalf("type filter leaked %q", feedback.Events[0].Type)
	}
	if !strings.Contains(feedback.Events[0].Summary, "outcome=success") {
		t.Fatalf("expected feedback summary, got %q", feedback.Events[0].Summary)
	}

	// Repeatable --type returns the union.
	obsAndRetr := listEvents(t, repo, "--type", "observation", "--type", "retrieval")
	for _, e := range obsAndRetr.Events {
		if e.Type != "observation" && e.Type != "retrieval" {
			t.Fatalf("unexpected type in union filter: %q", e.Type)
		}
	}
	if len(obsAndRetr.Events) != 3 { // 2 observations + 1 retrieval
		t.Fatalf("expected 3 events in union, got %d", len(obsAndRetr.Events))
	}

	// --limit caps results.
	limited := listEvents(t, repo, "--limit", "2")
	if len(limited.Events) != 2 {
		t.Fatalf("expected 2 events with --limit 2, got %d", len(limited.Events))
	}

	// --since 1h includes everything just written.
	recent := listEvents(t, repo, "--since", "1h")
	if len(recent.Events) != len(all.Events) {
		t.Fatalf("expected --since 1h to include all %d events, got %d", len(all.Events), len(recent.Events))
	}

	// --session filters by the capturing session.
	scoped := listEvents(t, repo, "--session", "events-list")
	if len(scoped.Events) != len(all.Events) {
		t.Fatalf("expected all events for the session, got %d of %d", len(scoped.Events), len(all.Events))
	}
	none := listEvents(t, repo, "--session", "no-such-session")
	if len(none.Events) != 0 {
		t.Fatalf("expected no events for an unknown session, got %d", len(none.Events))
	}
}

func TestEventsListBadTypeFailsFast(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_SESSION_ID", "events-bad")
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	stdout, stderr, code := runCLIAt(t, repo, "events", "list", "--type", "bogus")
	if code == 0 {
		t.Fatalf("expected non-zero exit for bad --type\nstdout:\n%s", stdout)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("expected empty stdout on bad --type, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "invalid --type") || !strings.Contains(stderr, "feedback") {
		t.Fatalf("expected remediation listing valid types, got:\n%s", stderr)
	}
}

func TestStatsEnrichedFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_SESSION_ID", "stats-enriched")
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	ruleID := seedLoopAndObservations(t, repo)

	stdout, stderr, code := runCLIAt(t, repo, "stats")
	if code != 0 {
		t.Fatalf("stats failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var report struct {
		UnconsolidatedObservations int `json:"unconsolidated_observations"`
		Rules                      []struct {
			RuleID           string         `json:"rule_id"`
			FeedbackCount    int            `json:"feedback_count"`
			RankDistribution map[string]int `json:"rank_distribution"`
			OutcomeCounts    map[string]int `json:"outcome_counts"`
		} `json:"rules"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode stats: %v\nraw:\n%s", err, stdout)
	}

	// Two observations were added, none consolidated yet.
	if report.UnconsolidatedObservations != 2 {
		t.Fatalf("expected 2 unconsolidated observations, got %d", report.UnconsolidatedObservations)
	}
	if len(report.Rules) != 1 || report.Rules[0].RuleID != ruleID {
		t.Fatalf("unexpected rules: %#v", report.Rules)
	}
	r := report.Rules[0]
	if r.FeedbackCount != 1 {
		t.Fatalf("expected feedback_count 1, got %d", r.FeedbackCount)
	}
	if r.RankDistribution["1"] != 1 {
		t.Fatalf("expected rank 1 -> 1, got %#v", r.RankDistribution)
	}
	if r.OutcomeCounts["success"] != 1 {
		t.Fatalf("expected outcome success -> 1, got %#v", r.OutcomeCounts)
	}
}
