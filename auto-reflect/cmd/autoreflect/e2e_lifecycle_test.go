package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// e2eAddObservationTask adds an observation carrying an explicit task_id and a
// single evidence session, returning its observation id. It is the task-keyed
// analogue of e2eAddObservation.
func e2eAddObservationTask(t *testing.T, repo, session, taskID string) string {
	t.Helper()
	stdout, stderr, err := runBinary(repo, "observation", "add",
		"--kind", "pattern",
		"--subject", "task-keyed pattern in "+session+"/"+taskID,
		"--evidence-session", session,
		"--task-id", taskID,
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

// e2eConsolidateDraft consolidates a single create-draft delta grounded in the
// given observation ids and returns the new rule id. force bypasses the
// create-draft session-evidence gate (so a draft can be minted from
// single-session, task-keyed provenance).
func e2eConsolidateDraft(t *testing.T, repo, useWhen, domain string, force bool, obIDs ...string) string {
	t.Helper()
	quoted := make([]string, 0, len(obIDs))
	for _, id := range obIDs {
		quoted = append(quoted, fmt.Sprintf("%q", id))
	}
	doc := fmt.Sprintf(`{"deltas":[{"op":"create-draft","use_when":%q,"content":"keep flags explicit","causal_note":"ambiguous flags confused agents","domain":[%q],"type":"soft","observation_ids":[%s]}]}`,
		useWhen, domain, strings.Join(quoted, ","))
	args := []string{"consolidate"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, doc)
	stdout, stderr, err := runBinary(repo, args...)
	if err != nil {
		t.Fatalf("consolidate failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var cresp struct {
		Applied []struct {
			RuleID string `json:"rule_id"`
		} `json:"applied"`
		Skipped []json.RawMessage `json:"skipped"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &cresp); jerr != nil {
		t.Fatalf("consolidate stdout not JSON: %v\nraw:\n%s", jerr, stdout)
	}
	if len(cresp.Applied) != 1 || len(cresp.Skipped) != 0 {
		t.Fatalf("expected one applied draft, no skips: %s", stdout)
	}
	return cresp.Applied[0].RuleID
}

// TestE2EPromoteTaskGate is a black-box check of the task-keyed promote gate:
// (a) a draft whose provenance carries >=3 distinct task_ids promotes without
// --force even though it covers a single session; (b) the legacy >=2-session
// path still confirms with no task_ids; (c) a draft with <3 tasks AND <2
// sessions and no --force is refused with a message naming both shortfalls.
func TestE2EPromoteTaskGate(t *testing.T) {
	repo := initE2ERepo(t)
	writeE2EFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	t.Setenv("AUTO_SESSION_ID", "e2e-promote-taskgate")

	if stdout, stderr, err := runBinary(repo, "init", "--project"); err != nil {
		t.Fatalf("init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// (a) Task path: three observations, three distinct task_ids, one shared
	// session. create-draft needs --force (single session); promote does not.
	taskA := e2eAddObservationTask(t, repo, "sess-shared", "049-task-a")
	taskB := e2eAddObservationTask(t, repo, "sess-shared", "049-task-b")
	taskC := e2eAddObservationTask(t, repo, "sess-shared", "049-task-c")
	taskRule := e2eConsolidateDraft(t, repo, "wiring cobra command flags end to end", "taskdom", true, taskA, taskB, taskC)

	pStdout, pStderr, err := runBinary(repo, "rule", "promote", taskRule)
	if err != nil {
		t.Fatalf("task-path promote should succeed: %v\nstdout:\n%s\nstderr:\n%s", err, pStdout, pStderr)
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
		t.Fatalf("expected confirmed rule after task-path promote: %#v", presp)
	}

	// (b) Session path still works: two observations, two sessions, no task_ids.
	sessA := e2eAddObservation(t, repo, "sess-legacy-1")
	sessB := e2eAddObservation(t, repo, "sess-legacy-2")
	sessRule := e2eConsolidateDraft(t, repo, "deploying containers to staging clusters", "sessdom", false, sessA, sessB)
	if _, sStderr, serr := runBinary(repo, "rule", "promote", sessRule); serr != nil {
		t.Fatalf("session-path promote should still succeed: %v\nstderr:\n%s", serr, sStderr)
	}

	// (c) Below both thresholds with no --force: one observation, one session,
	// one task. promote is refused naming both shortfalls.
	loneOb := e2eAddObservationTask(t, repo, "sess-lone", "049-task-lone")
	loneRule := e2eConsolidateDraft(t, repo, "rotating database credentials safely", "lonedom", true, loneOb)
	fStdout, fStderr, ferr := runBinary(repo, "rule", "promote", loneRule)
	if ferr == nil {
		t.Fatalf("under-evidenced promote should fail, got success\nstdout:\n%s", fStdout)
	}
	if !strings.Contains(fStderr, "distinct task(s)") || !strings.Contains(fStderr, "distinct session(s)") {
		t.Fatalf("error should name both task and session shortfalls, got:\n%s", fStderr)
	}
	if !strings.Contains(fStderr, "--force") {
		t.Fatalf("error should mention --force remediation, got:\n%s", fStderr)
	}
}

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
