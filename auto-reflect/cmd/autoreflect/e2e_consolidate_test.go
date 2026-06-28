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

// TestE2EConsolidateSplit drives the split op through the built binary over the
// real event log: observe (two sessions) -> consolidate create-draft -> promote
// (confirmed) -> consolidate split into two narrower drafts. It asserts the
// parent folds to stale with successor_ids and each child folds to draft with
// predecessor_ids=[parent] (lineage wired both ways).
func TestE2EConsolidateSplit(t *testing.T) {
	repo := initE2ERepo(t)
	writeE2EFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	t.Setenv("AUTO_SESSION_ID", "e2e-consolidate-split")

	if stdout, stderr, err := runBinary(repo, "init", "--project"); err != nil {
		t.Fatalf("init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	ob1 := e2eAddObservation(t, repo, "split-sess-1")
	ob2 := e2eAddObservation(t, repo, "split-sess-2")

	// Create + promote a confirmed parent rule.
	doc := fmt.Sprintf(`{"deltas":[{"op":"create-draft","use_when":"handling cobra wiring broadly","content":"keep flags explicit","causal_note":"ambiguous flags confused agents","domain":["clidom"],"type":"soft","observation_ids":["%s","%s"]}]}`, ob1, ob2)
	cStdout, cStderr, err := runBinary(repo, "consolidate", doc)
	if err != nil {
		t.Fatalf("consolidate (draft) failed: %v\nstdout:\n%s\nstderr:\n%s", err, cStdout, cStderr)
	}
	var cresp struct {
		Applied []struct {
			RuleID string `json:"rule_id"`
		} `json:"applied"`
	}
	if jerr := json.Unmarshal([]byte(cStdout), &cresp); jerr != nil {
		t.Fatalf("consolidate stdout not JSON: %v\nraw:\n%s", jerr, cStdout)
	}
	if len(cresp.Applied) != 1 {
		t.Fatalf("expected one applied draft: %s", cStdout)
	}
	parent := cresp.Applied[0].RuleID
	if _, pStderr, perr := runBinary(repo, "rule", "promote", parent); perr != nil {
		t.Fatalf("promote failed: %v\nstderr:\n%s", perr, pStderr)
	}

	// Split the confirmed parent into two narrower drafts.
	splitDoc := fmt.Sprintf(`{"deltas":[{"op":"split","rule_id":"%s","into":[{"use_when":"handling cobra flag wiring","content":"keep flags explicit","causal_note":"ambiguous flags confused agents","domain":["clidom"]},{"use_when":"handling cobra arg validation","content":"validate positional args","causal_note":"missing arg checks crashed agents","domain":["clidom"]}]}]}`, parent)
	sStdout, sStderr, err := runBinary(repo, "consolidate", splitDoc)
	if err != nil {
		t.Fatalf("consolidate (split) failed: %v\nstdout:\n%s\nstderr:\n%s", err, sStdout, sStderr)
	}
	var sresp struct {
		Applied []struct {
			RuleID string `json:"rule_id"`
		} `json:"applied"`
		Skipped []json.RawMessage `json:"skipped"`
	}
	if jerr := json.Unmarshal([]byte(sStdout), &sresp); jerr != nil {
		t.Fatalf("split stdout not JSON: %v\nraw:\n%s", jerr, sStdout)
	}
	if len(sresp.Applied) != 1 || len(sresp.Skipped) != 0 {
		t.Fatalf("expected one applied split, no skips: %s", sStdout)
	}

	// Parent folds to stale with two successors.
	p := e2eGetRule(t, repo, parent)
	if p.Lifecycle != "stale" {
		t.Fatalf("split parent should be stale, got %q", p.Lifecycle)
	}
	if len(p.SuccessorIDs) != 2 {
		t.Fatalf("parent should have two successor_ids, got %#v", p.SuccessorIDs)
	}

	// Each child folds to draft with predecessor_ids=[parent].
	for _, childID := range p.SuccessorIDs {
		ch := e2eGetRule(t, repo, childID)
		if ch.Lifecycle != "draft" {
			t.Fatalf("child %s should be a draft, got %q", childID, ch.Lifecycle)
		}
		if len(ch.PredecessorIDs) != 1 || ch.PredecessorIDs[0] != parent {
			t.Fatalf("child %s predecessor_ids should be [%s], got %#v", childID, parent, ch.PredecessorIDs)
		}
	}
}

// e2eGetRule fetches a rule's lineage fields via the built binary's `rule get`.
func e2eGetRule(t *testing.T, repo, id string) struct {
	Lifecycle      string   `json:"lifecycle"`
	PredecessorIDs []string `json:"predecessor_ids"`
	SuccessorIDs   []string `json:"successor_ids"`
} {
	t.Helper()
	var r struct {
		Lifecycle      string   `json:"lifecycle"`
		PredecessorIDs []string `json:"predecessor_ids"`
		SuccessorIDs   []string `json:"successor_ids"`
	}
	stdout, stderr, err := runBinary(repo, "rule", "get", id)
	if err != nil {
		t.Fatalf("rule get %s failed: %v\nstderr:\n%s", id, err, stderr)
	}
	if jerr := json.Unmarshal([]byte(stdout), &r); jerr != nil {
		t.Fatalf("decode rule get json: %v\nraw:\n%s", jerr, stdout)
	}
	return r
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
