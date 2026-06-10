package cli_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type consolidateResp struct {
	Applied []struct {
		Op             string   `json:"op"`
		RuleID         string   `json:"rule_id"`
		ObservationIDs []string `json:"observation_ids"`
		Rule           *struct {
			ID             string   `json:"id"`
			Lifecycle      string   `json:"lifecycle"`
			ObservationIDs []string `json:"observation_ids"`
		} `json:"rule"`
	} `json:"applied"`
	Skipped []struct {
		Reason string `json:"reason"`
	} `json:"skipped"`
	Conflicts []struct {
		RuleID string `json:"rule_id"`
	} `json:"conflicts"`
	DryRun bool `json:"dry_run"`
}

// consolidateOK runs `consolidate <doc>` (exit 0 expected — gate skips are normal
// output, not command failures) and returns the parsed envelope.
func consolidateOK(t *testing.T, repo, doc string, flags ...string) consolidateResp {
	t.Helper()
	args := append([]string{"consolidate", doc}, flags...)
	stdout, stderr, code := runCLIAt(t, repo, args...)
	if code != 0 {
		t.Fatalf("consolidate failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var resp consolidateResp
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("decode consolidate json: %v\nraw:\n%s", err, stdout)
	}
	return resp
}

func draftDoc(useWhen string, obIDs ...string) string {
	ids, _ := json.Marshal(obIDs)
	return fmt.Sprintf(`{"deltas":[{"op":"create-draft","use_when":%q,"content":"some durable guidance","causal_note":"a failure it prevents","domain":["consoldom"],"type":"soft","observation_ids":%s}]}`, useWhen, ids)
}

func countRules(t *testing.T, repo string) int {
	t.Helper()
	stdout, _, code := runCLIAt(t, repo, "rule", "list")
	if code != 0 {
		t.Fatalf("rule list failed")
	}
	var listed struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("decode rule list: %v\nraw:\n%s", err, stdout)
	}
	return len(listed.Rules)
}

func TestConsolidateCreateDraftGateDedupeDryRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_SESSION_ID", "consolidate-gate")
	repo := initGitRepo(t)
	gitAddCommitSeed(t, repo)

	ob1 := addObservation(t, repo, "--kind", "pattern", "--subject", "s1", "--evidence-session", "sess-1", "--domain", "consoldom").Observation.ObservationID
	ob2 := addObservation(t, repo, "--kind", "pattern", "--subject", "s2", "--evidence-session", "sess-2", "--domain", "consoldom").Observation.ObservationID

	// (a) Two distinct sessions → create-draft accepted, no force.
	resp := consolidateOK(t, repo, draftDoc("wiring a cobra command for consolidation", ob1, ob2))
	if len(resp.Applied) != 1 || len(resp.Skipped) != 0 {
		t.Fatalf("two-session draft should be applied: %#v", resp)
	}
	ruleA := resp.Applied[0].RuleID
	if resp.Applied[0].Rule == nil || resp.Applied[0].Rule.Lifecycle != "draft" {
		t.Fatalf("created rule should be a draft: %#v", resp.Applied[0])
	}
	if len(resp.Applied[0].Rule.ObservationIDs) != 2 {
		t.Fatalf("draft should carry both observation ids as provenance: %#v", resp.Applied[0].Rule)
	}

	// (b) One distinct session, no force → refused by the evidence gate.
	single := draftDoc("an entirely separate predicate about logging output", ob1)
	resp = consolidateOK(t, repo, single)
	if len(resp.Applied) != 0 || len(resp.Skipped) != 1 {
		t.Fatalf("single-session draft should be skipped: %#v", resp)
	}
	if !strings.Contains(resp.Skipped[0].Reason, "session") {
		t.Fatalf("skip reason should cite the session threshold: %q", resp.Skipped[0].Reason)
	}

	// (c) Same single-session draft with --force → accepted.
	resp = consolidateOK(t, repo, single, "--force")
	if len(resp.Applied) != 1 {
		t.Fatalf("--force should bypass the evidence gate: %#v", resp)
	}

	// Promote ruleA (2 sessions) to confirmed so dedupe has a live target.
	if _, _, code := runCLIAt(t, repo, "rule", "promote", ruleA); code != 0 {
		t.Fatalf("promote ruleA should succeed (2 sessions), code=%d", code)
	}

	// (d) A new draft duplicating the confirmed rule's use_when → refused as a dup.
	resp = consolidateOK(t, repo, draftDoc("wiring a cobra command for consolidation", ob1, ob2))
	if len(resp.Applied) != 0 || len(resp.Skipped) != 1 {
		t.Fatalf("duplicate draft should be skipped: %#v", resp)
	}
	if !strings.Contains(resp.Skipped[0].Reason, ruleA) || !strings.Contains(resp.Skipped[0].Reason, "attach-evidence") {
		t.Fatalf("dedupe reason should name the rule and suggest attach-evidence: %q", resp.Skipped[0].Reason)
	}

	// (e) --dry-run computes but writes nothing.
	before := countRules(t, repo)
	resp = consolidateOK(t, repo, draftDoc("a totally fresh predicate about retry backoff", ob1, ob2), "--dry-run")
	if !resp.DryRun || len(resp.Applied) != 1 {
		t.Fatalf("dry-run should report one would-be apply: %#v", resp)
	}
	if after := countRules(t, repo); after != before {
		t.Fatalf("dry-run must not write: rules %d -> %d", before, after)
	}
}

func TestConsolidatePromoteRetireAndUnconsolidated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_SESSION_ID", "consolidate-promote")
	repo := initGitRepo(t)
	gitAddCommitSeed(t, repo)

	ob1 := addObservation(t, repo, "--kind", "pattern", "--subject", "s1", "--evidence-session", "sess-1", "--domain", "retiredom").Observation.ObservationID
	ob2 := addObservation(t, repo, "--kind", "pattern", "--subject", "s2", "--evidence-session", "sess-2", "--domain", "retiredom").Observation.ObservationID
	ob3 := addObservation(t, repo, "--kind", "pattern", "--subject", "s3", "--evidence-session", "sess-3", "--domain", "retiredom").Observation.ObservationID
	ob4 := addObservation(t, repo, "--kind", "pattern", "--subject", "s4", "--evidence-session", "sess-4", "--domain", "retiredom").Observation.ObservationID

	// ruleA: two sessions; ruleC: one session via --force.
	ruleA := consolidateOK(t, repo, draftDocDomain("retiring stale cobra wiring", "retiredom", ob1, ob2)).Applied[0].RuleID
	ruleC := consolidateOK(t, repo, draftDocDomain("forcing a single session draft", "retiredom", ob3), "--force").Applied[0].RuleID

	// promote gate: ruleC has one distinct session → refused without --force.
	stdout, stderr, code := runCLIAt(t, repo, "rule", "promote", ruleC)
	if code == 0 {
		t.Fatalf("promote of a single-session rule should fail\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "session") {
		t.Fatalf("promote refusal should cite the session threshold: %q", stderr)
	}
	// With --force it is allowed.
	if _, _, code := runCLIAt(t, repo, "rule", "promote", ruleC, "--force"); code != 0 {
		t.Fatalf("--force promote should succeed, code=%d", code)
	}

	// Before retire, retrieve surfaces ruleA.
	if !retrieveSurfaces(t, repo, "retiring stale cobra wiring", "retiredom") {
		t.Fatal("expected ruleA to be surfaced before retire")
	}
	// retire ruleA → stale, then retrieve must not surface it (cross-check 1.2).
	if _, _, code := runCLIAt(t, repo, "rule", "retire", ruleA); code != 0 {
		t.Fatalf("retire should always succeed, code=%d", code)
	}
	if retrieveSurfaces(t, repo, "retiring stale cobra wiring", "retiredom") {
		t.Fatal("stale ruleA must not be surfaced by retrieve")
	}

	// --unconsolidated: ob1/ob2 (ruleA) and ob3 (ruleC) are consolidated; only ob4
	// remains.
	unconsolidated := listObservations(t, repo, "--unconsolidated")
	if len(unconsolidated.Observations) != 1 || unconsolidated.Observations[0].ObservationID != ob4 {
		t.Fatalf("only ob4 should remain unconsolidated, got %#v", unconsolidated.Observations)
	}
}

func draftDocDomain(useWhen, domain string, obIDs ...string) string {
	ids, _ := json.Marshal(obIDs)
	return fmt.Sprintf(`{"deltas":[{"op":"create-draft","use_when":%q,"content":"durable guidance","causal_note":"a failure it prevents","domain":[%q],"type":"soft","observation_ids":%s}]}`, useWhen, domain, ids)
}

// retrieveSurfaces reports whether `retrieve <intent> --domain <domain>` returns
// any item whose use_when matches the intent (used to confirm stale exclusion).
func retrieveSurfaces(t *testing.T, repo, intent, domain string) bool {
	t.Helper()
	stdout, stderr, code := runCLIAt(t, repo, "retrieve", intent, "--domain", domain)
	if code != 0 {
		t.Fatalf("retrieve failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var items []struct {
		UseWhen   string `json:"use_when"`
		Lifecycle string `json:"lifecycle"`
	}
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("decode retrieve json: %v\nraw:\n%s", err, stdout)
	}
	for _, it := range items {
		if it.UseWhen == intent {
			return true
		}
	}
	return false
}

// gitAddCommitSeed writes a README and makes the first commit so provenance
// resolves; mirrors the setup other integration tests inline.
func gitAddCommitSeed(t *testing.T, repo string) {
	t.Helper()
	writeFile(t, repo+"/README.md", "hello\n")
	gitAddCommit(t, repo, "seed")
}
