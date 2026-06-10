package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EEventsListAndStats drives the loop plus observations through the built
// binary, then asserts `events list` and the enriched `stats` JSON shapes.
func TestE2EEventsListAndStats(t *testing.T) {
	repo := initE2ERepo(t)
	writeE2EFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	t.Setenv("AUTO_SESSION_ID", "e2e-events")

	if stdout, stderr, err := runBinary(repo, "init", "--project"); err != nil {
		t.Fatalf("init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	ruleID := e2eCreateRule(t, repo,
		"--use-when", "writing flaky end-to-end tests",
		"--content", "Keep passing test logs short",
		"--causal-note", "noisy logs hid the failure",
		"--domain", "testing", "--type", "soft")
	retrievalID := e2eRetrieve(t, repo, "debugging flaky end-to-end tests")
	feedbackID := e2eSelect(t, repo, retrievalID)

	payload := map[string]any{
		"outcome":  "success",
		"summary":  "shipped the fix",
		"rankings": []map[string]any{{"feedback_id": feedbackID, "rank": 1, "reason": "kept logs short"}},
		"gap":      nil,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal feedback: %v", err)
	}
	if _, fbStderr, fbErr := runBinary(repo, "feedback", string(raw)); fbErr != nil {
		t.Fatalf("feedback failed: %v\nstderr:\n%s", fbErr, fbStderr)
	}

	if _, oStderr, oErr := runBinary(repo, "observation", "add",
		"--kind", "gap", "--subject", "docs unread", "--evidence-session", "e2e-events"); oErr != nil {
		t.Fatalf("observation add failed: %v\nstderr:\n%s", oErr, oStderr)
	}

	// events list (JSON shape).
	listStdout, listStderr, err := runBinary(repo, "events", "list")
	if err != nil {
		t.Fatalf("events list failed: %v\nstderr:\n%s", err, listStderr)
	}
	var listResp struct {
		Scope  string           `json:"scope"`
		Events []map[string]any `json:"events"`
	}
	if jerr := json.Unmarshal([]byte(listStdout), &listResp); jerr != nil {
		t.Fatalf("events list stdout not JSON: %v\nraw:\n%s", jerr, listStdout)
	}
	if listResp.Scope != "repo" || len(listResp.Events) == 0 {
		t.Fatalf("unexpected events list response: %#v", listResp)
	}
	for _, e := range listResp.Events {
		requireFields(t, e, "id", "type", "ts", "summary")
	}

	// events list --type observation narrows to the one observation.
	obsStdout, obsStderr, err := runBinary(repo, "events", "list", "--type", "observation")
	if err != nil {
		t.Fatalf("events list --type observation failed: %v\nstderr:\n%s", err, obsStderr)
	}
	var obsResp struct {
		Events []map[string]any `json:"events"`
	}
	if jerr := json.Unmarshal([]byte(obsStdout), &obsResp); jerr != nil {
		t.Fatalf("events list --type stdout not JSON: %v\nraw:\n%s", jerr, obsStdout)
	}
	if len(obsResp.Events) != 1 || obsResp.Events[0]["type"] != "observation" {
		t.Fatalf("expected exactly one observation event, got %#v", obsResp.Events)
	}

	// stats (enriched JSON shape).
	statsStdout, statsStderr, err := runBinary(repo, "stats")
	if err != nil {
		t.Fatalf("stats failed: %v\nstderr:\n%s", err, statsStderr)
	}
	var statsReport struct {
		UnconsolidatedObservations int              `json:"unconsolidated_observations"`
		Rules                      []map[string]any `json:"rules"`
	}
	if jerr := json.Unmarshal([]byte(statsStdout), &statsReport); jerr != nil {
		t.Fatalf("stats stdout not JSON: %v\nraw:\n%s", jerr, statsStdout)
	}
	if statsReport.UnconsolidatedObservations != 1 {
		t.Fatalf("expected 1 unconsolidated observation, got %d", statsReport.UnconsolidatedObservations)
	}
	if len(statsReport.Rules) != 1 || statsReport.Rules[0]["rule_id"] != ruleID {
		t.Fatalf("expected stats for rule %s, got %#v", ruleID, statsReport.Rules)
	}
	requireFields(t, statsReport.Rules[0], "rule_id", "surfaced", "selected", "rank_distribution", "outcome_counts")
	if !strings.Contains(statsStdout, "rank_distribution") || !strings.Contains(statsStdout, "outcome_counts") {
		t.Fatalf("stats JSON missing enriched fields:\n%s", statsStdout)
	}
}
