package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EConsolidatePromoteRetrieve drives the full Consolidate step through the
// built binary: observe (two sessions) -> consolidate create-draft (evidence gate
// passes) -> rule promote (draft -> confirmed) -> retrieve surfaces the confirmed
// rule. It then retires the rule and confirms retrieve no longer surfaces it.
func TestE2EConsolidatePromoteRetrieve(t *testing.T) {
	repo := initE2ERepo(t)
	writeE2EFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	t.Setenv("AUTO_SESSION_ID", "e2e-consolidate")

	if stdout, stderr, err := runBinary(repo, "init", "--project"); err != nil {
		t.Fatalf("init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	ob1 := e2eAddObservation(t, repo, "sess-1")
	ob2 := e2eAddObservation(t, repo, "sess-2")

	// Consolidate a draft grounded in two distinct sessions.
	doc := fmt.Sprintf(`{"deltas":[{"op":"create-draft","use_when":"wiring a cobra command end to end","content":"keep flags explicit","causal_note":"ambiguous flags confused agents","domain":["clidom"],"type":"soft","observation_ids":["%s","%s"]}]}`, ob1, ob2)
	cStdout, cStderr, err := runBinary(repo, "consolidate", doc)
	if err != nil {
		t.Fatalf("consolidate failed: %v\nstdout:\n%s\nstderr:\n%s", err, cStdout, cStderr)
	}
	var cresp struct {
		Applied []struct {
			RuleID string `json:"rule_id"`
			Rule   struct {
				Lifecycle      string   `json:"lifecycle"`
				ObservationIDs []string `json:"observation_ids"`
			} `json:"rule"`
		} `json:"applied"`
		Skipped []json.RawMessage `json:"skipped"`
	}
	if jerr := json.Unmarshal([]byte(cStdout), &cresp); jerr != nil {
		t.Fatalf("consolidate stdout not JSON: %v\nraw:\n%s", jerr, cStdout)
	}
	if len(cresp.Applied) != 1 || len(cresp.Skipped) != 0 {
		t.Fatalf("expected one applied draft, no skips: %s", cStdout)
	}
	ruleID := cresp.Applied[0].RuleID
	if cresp.Applied[0].Rule.Lifecycle != "draft" {
		t.Fatalf("consolidated rule should be a draft, got %q", cresp.Applied[0].Rule.Lifecycle)
	}
	if len(cresp.Applied[0].Rule.ObservationIDs) != 2 {
		t.Fatalf("consolidated rule should carry two-observation provenance: %#v", cresp.Applied[0].Rule)
	}

	// Promote draft -> confirmed (provenance covers two sessions, no --force needed).
	pStdout, pStderr, err := runBinary(repo, "rule", "promote", ruleID)
	if err != nil {
		t.Fatalf("promote failed: %v\nstdout:\n%s\nstderr:\n%s", err, pStdout, pStderr)
	}
	var presp struct {
		Promoted bool `json:"promoted"`
		Rule     struct {
			Lifecycle string `json:"lifecycle"`
		} `json:"rule"`
	}
	if jerr := json.Unmarshal([]byte(pStdout), &presp); jerr != nil {
		t.Fatalf("promote stdout not JSON: %v\nraw:\n%s", jerr, pStdout)
	}
	if !presp.Promoted || presp.Rule.Lifecycle != "confirmed" {
		t.Fatalf("expected confirmed rule after promote: %#v", presp)
	}

	// Retrieve surfaces the confirmed rule (with its lifecycle field).
	if !e2eRetrieveSurfaces(t, repo, "wiring a cobra command end to end", "confirmed") {
		t.Fatal("expected the confirmed rule to surface in retrieve")
	}

	// Retire -> stale -> retrieve no longer surfaces it (cross-check 1.2).
	if _, rStderr, rerr := runBinary(repo, "rule", "retire", ruleID); rerr != nil {
		t.Fatalf("retire failed: %v\nstderr:\n%s", rerr, rStderr)
	}
	if e2eRetrieveSurfaces(t, repo, "wiring a cobra command end to end", "") {
		t.Fatal("retired (stale) rule must not surface in retrieve")
	}
}

func e2eAddObservation(t *testing.T, repo, session string) string {
	t.Helper()
	stdout, stderr, err := runBinary(repo, "observation", "add",
		"--kind", "pattern",
		"--subject", "cobra wiring pattern in "+session,
		"--evidence-session", session,
		"--domain", "clidom",
	)
	if err != nil {
		t.Fatalf("observation add failed: %v\nstderr:\n%s", err, stderr)
	}
	var resp struct {
		Observation struct {
			ObservationID string `json:"observation_id"`
		} `json:"observation"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &resp); jerr != nil {
		t.Fatalf("decode observation add: %v\nraw:\n%s", jerr, stdout)
	}
	if resp.Observation.ObservationID == "" {
		t.Fatalf("observation add returned no id\nraw:\n%s", stdout)
	}
	return resp.Observation.ObservationID
}

// e2eRetrieveSurfaces reports whether retrieve returns an item matching intent.
// When wantLifecycle is non-empty it also asserts that item's lifecycle.
func e2eRetrieveSurfaces(t *testing.T, repo, intent, wantLifecycle string) bool {
	t.Helper()
	stdout, stderr, err := runBinary(repo, "retrieve", intent)
	if err != nil {
		t.Fatalf("retrieve failed: %v\nstderr:\n%s", err, stderr)
	}
	var items []map[string]any
	if jerr := json.Unmarshal([]byte(stdout), &items); jerr != nil {
		t.Fatalf("decode retrieve json: %v\nraw:\n%s", jerr, stdout)
	}
	for _, it := range items {
		uw, _ := it["use_when"].(string)
		if !strings.EqualFold(uw, intent) {
			continue
		}
		if wantLifecycle != "" && it["lifecycle"] != wantLifecycle {
			t.Fatalf("surfaced rule lifecycle = %v, want %q", it["lifecycle"], wantLifecycle)
		}
		return true
	}
	return false
}
