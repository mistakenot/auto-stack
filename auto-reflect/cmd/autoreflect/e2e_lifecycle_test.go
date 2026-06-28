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

// TestE2EGraduateEnforced is a black-box check through the real binary that a
// graduated rule becomes enforced, carries its lint_ref, lists under
// --lifecycle enforced, and is excluded from retrieval.
func TestE2EGraduateEnforced(t *testing.T) {
	repo := initE2ERepo(t)
	writeE2EFile(t, repo+"/README.md", "hello\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	if stdout, stderr, err := runBinary(repo, "init"); err != nil {
		t.Fatalf("init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	id := e2eCreateRule(t, repo,
		"--use-when", "checking unchecked errors in go",
		"--content", "always handle returned errors",
		"--causal-note", "swallowed errors hid a real bug",
		"--domain", "graduatetest",
		"--type", "soft",
		"--lifecycle", "confirmed",
	)

	stdout, stderr, err := runBinary(repo, "rule", "graduate", id,
		"--linter", "golangci-lint",
		"--check", "errcheck",
	)
	if err != nil {
		t.Fatalf("rule graduate failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var gradResp struct {
		Rule struct {
			Lifecycle string `json:"lifecycle"`
			LintRef   *struct {
				Linter string `json:"linter"`
				Check  string `json:"check"`
			} `json:"lint_ref"`
		} `json:"rule"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &gradResp); jerr != nil {
		t.Fatalf("decode graduate json: %v\nraw:\n%s", jerr, stdout)
	}
	if gradResp.Rule.Lifecycle != "enforced" {
		t.Fatalf("expected lifecycle enforced, got %q", gradResp.Rule.Lifecycle)
	}
	if gradResp.Rule.LintRef == nil || gradResp.Rule.LintRef.Check != "errcheck" {
		t.Fatalf("expected lint_ref with check errcheck, got %#v", gradResp.Rule.LintRef)
	}

	// list --lifecycle enforced shows the rule with its lint_ref.
	stdout, stderr, err = runBinary(repo, "rule", "list", "--lifecycle", "enforced")
	if err != nil {
		t.Fatalf("rule list --lifecycle enforced failed: %v\nstderr:\n%s", err, stderr)
	}
	var listResp struct {
		Rules []map[string]any `json:"rules"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &listResp); jerr != nil {
		t.Fatalf("decode list json: %v\nraw:\n%s", jerr, stdout)
	}
	if len(listResp.Rules) != 1 || listResp.Rules[0]["id"] != id {
		t.Fatalf("rule list --lifecycle enforced should return the graduated rule, got %#v", listResp.Rules)
	}

	// retrieve over a matching intent must exclude the enforced rule.
	stdout, stderr, err = runBinary(repo, "retrieve", "checking unchecked errors in go", "--domain", "graduatetest")
	if err != nil {
		t.Fatalf("retrieve failed: %v\nstderr:\n%s", err, stderr)
	}
	var retrieved []map[string]any
	if jerr := json.Unmarshal([]byte(stdout), &retrieved); jerr != nil {
		t.Fatalf("decode retrieve json: %v\nraw:\n%s", jerr, stdout)
	}
	for _, it := range retrieved {
		if it["lifecycle"] == "enforced" {
			t.Fatalf("enforced rule surfaced through the binary: %#v", retrieved)
		}
	}
}
