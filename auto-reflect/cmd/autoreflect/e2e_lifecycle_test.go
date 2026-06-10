package main

import (
	"encoding/json"
	"testing"
)

// TestE2ELifecycleRetrieval is a black-box check that lifecycle actually gates
// retrieval through the real binary: stale never surfaces, drafts surface by
// default and are excluded by --no-drafts, and each item carries lifecycle+draft.
func TestE2ELifecycleRetrieval(t *testing.T) {
	repo := initE2ERepo(t)
	writeE2EFile(t, repo+"/README.md", "hello\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	if stdout, stderr, err := runBinary(repo, "init"); err != nil {
		t.Fatalf("init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	seed := func(lifecycle string) {
		e2eCreateRule(t, repo,
			"--use-when", "lifecycle retrieval "+lifecycle+" case",
			"--content", "guidance for the "+lifecycle+" rule",
			"--causal-note", "covers the "+lifecycle+" lifecycle path",
			"--domain", "lifecycletest",
			"--type", "soft",
			"--lifecycle", lifecycle,
		)
	}
	seed("draft")
	seed("confirmed")
	seed("stale")

	retrieve := func(args ...string) []map[string]any {
		t.Helper()
		full := append([]string{"retrieve", "lifecycle retrieval case"}, args...)
		stdout, stderr, err := runBinary(repo, full...)
		if err != nil {
			t.Fatalf("retrieve %v failed: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout, stderr)
		}
		var results []map[string]any
		if jerr := json.Unmarshal([]byte(stdout), &results); jerr != nil {
			t.Fatalf("decode retrieve json: %v\nraw:\n%s", jerr, stdout)
		}
		return results
	}

	// Default: draft + confirmed surface, stale excluded; each item carries the
	// new lifecycle/draft fields.
	def := retrieve()
	lifecycles := map[string]bool{}
	for _, row := range def {
		requireFields(t, row, "retrieval_id", "use_when", "rule_type", "lifecycle")
		lc, _ := row["lifecycle"].(string)
		lifecycles[lc] = true
		if lc == "stale" {
			t.Fatalf("stale rule surfaced through the binary: %#v", def)
		}
		if _, ok := row["draft"]; !ok {
			t.Fatalf("retrieve item missing draft field: %#v", row)
		}
		if (lc == "draft") != row["draft"].(bool) {
			t.Fatalf("draft flag inconsistent with lifecycle: %#v", row)
		}
	}
	if !lifecycles["draft"] || !lifecycles["confirmed"] {
		t.Fatalf("default retrieve should surface draft and confirmed, got %#v", def)
	}

	// --no-drafts: confirmed only.
	noDraft := retrieve("--no-drafts")
	if len(noDraft) != 1 || noDraft[0]["lifecycle"] != "confirmed" {
		t.Fatalf("--no-drafts should return only the confirmed rule, got %#v", noDraft)
	}
}
