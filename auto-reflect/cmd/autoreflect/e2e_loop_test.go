package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// shardNameShape matches the event shard filename: <host>-<YYYY-MM-DD>-<wt8>.jsonl
// where wt8 is the first 8 hex chars of SHA-256 of the worktree root.
var shardNameShape = regexp.MustCompile(`^.+-\d{4}-\d{2}-\d{2}-[0-9a-f]{8}\.jsonl$`)

// TestE2ELoopQuickstart drives the full retrieval loop through the built binary,
// following the quickstart sequence: init --project -> rule create -> retrieve ->
// select -> feedback -> gate check. It threads the minted rt-/fb- ids parsed from
// each command's JSON output into the next, exactly as an agent would, and asserts
// events land on disk and the gate flips closed (AC-7).
func TestE2ELoopQuickstart(t *testing.T) {
	repo := initE2ERepo(t)

	// Run the whole loop under a deterministic session id (as a real agent always
	// would), so selection/feedback share one scope and the feedback event carries
	// a session_id for `gap list` provenance to surface. Without this the detected
	// session is whatever the host env happens to expose — present locally, empty
	// on a clean CI runner — making the gap-row session_id non-deterministic.
	// AUTO_SESSION_ID is first in events.DetectSessionID precedence, so it wins
	// over any inherited CLAUDE_*/CODEX_* var.
	const e2eSessionID = "e2e-loop-session"
	t.Setenv("AUTO_SESSION_ID", e2eSessionID)

	// Event git-provenance / shard naming needs a resolvable repo; the happy path
	// exercises the committed-repo case. Seed an initial commit (matching the
	// legacy TestE2EFeedbackAddList pattern).
	writeE2EFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	// init sets up the events dir + snapshot (project setup runs inside a git repo).
	if stdout, stderr, err := runBinary(repo, "init"); err != nil {
		t.Fatalf("init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// Author a rule (rule_created event + refold).
	ruleID := e2eCreateRule(t, repo,
		"--use-when", "writing flaky end-to-end tests",
		"--content", "Keep passing test logs short so failing E2E tests are easy to debug",
		"--causal-note", "noisy passing logs hid the real failure during a debug session",
		"--domain", "testing",
		"--type", "soft",
	)

	// Retrieve predicates (no content). Thread the minted retrieval_id.
	retrievalID := e2eRetrieve(t, repo, "debugging flaky end-to-end tests")

	// Select reveals content and mints a feedback_id.
	feedbackID := e2eSelect(t, repo, retrievalID)

	// Gate must be OPEN (non-zero) before feedback is submitted (AC-7).
	stdout, _, err := runBinary(repo, "gate", "check")
	if err == nil {
		t.Fatalf("gate check should exit non-zero before feedback; stdout:\n%s", stdout)
	}
	var gateBefore map[string]any
	if jerr := json.Unmarshal([]byte(stdout), &gateBefore); jerr != nil {
		t.Fatalf("gate check (before) stdout not JSON: %v\nraw:\n%s", jerr, stdout)
	}
	if gateBefore["clean"] != false {
		t.Fatalf("gate should report clean=false before feedback, got %#v", gateBefore)
	}

	// Close the loop with a complete feedback payload ranking the outstanding fb-id.
	// Include a grounded gap so `gap list` (below) has something to surface.
	payload := map[string]any{
		"outcome": "success",
		"summary": "shipped the fix",
		"rankings": []map[string]any{
			{"feedback_id": feedbackID, "rank": 1, "reason": "told me to keep passing logs short"},
		},
		"gap": map[string]any{
			"report": "no rule on trimming flaky assertion output",
			"moment": "while triaging the failing end-to-end test",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal feedback payload: %v", err)
	}
	fbStdout, fbStderr, fbErr := runBinary(repo, "feedback", string(raw))
	if fbErr != nil {
		t.Fatalf("feedback failed: %v\nstdout:\n%s\nstderr:\n%s", fbErr, fbStdout, fbStderr)
	}
	// Uniform envelope: feedback echoes the acted-on ids at top-level .id / .ids.
	var fbResp struct {
		ID  string   `json:"id"`
		IDs []string `json:"ids"`
	}
	if jerr := json.Unmarshal([]byte(fbStdout), &fbResp); jerr != nil {
		t.Fatalf("feedback stdout not JSON: %v\nraw:\n%s", jerr, fbStdout)
	}
	if fbResp.ID != feedbackID || len(fbResp.IDs) != 1 || fbResp.IDs[0] != feedbackID {
		t.Fatalf("feedback envelope should echo the acted-on feedback id %q, got id=%q ids=%v", feedbackID, fbResp.ID, fbResp.IDs)
	}

	// Gate must now be CLEAN (exit 0) after feedback (AC-7).
	gateStdout, gateStderr, gateErr := runBinary(repo, "gate", "check")
	if gateErr != nil {
		t.Fatalf("gate check should exit 0 after feedback: %v\nstdout:\n%s\nstderr:\n%s", gateErr, gateStdout, gateStderr)
	}
	var gateAfter map[string]any
	if jerr := json.Unmarshal([]byte(gateStdout), &gateAfter); jerr != nil {
		t.Fatalf("gate check (after) stdout not JSON: %v\nraw:\n%s", jerr, gateStdout)
	}
	if gateAfter["clean"] != true {
		t.Fatalf("gate should report clean=true after feedback, got %#v", gateAfter)
	}

	// Events files exist on disk under .auto/reflect/events/ with the expected
	// <host>-<date>-<wt8>.jsonl name shape.
	eventsDir := filepath.Join(repo, ".auto", "reflect", "events")
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		t.Fatalf("read events dir %s: %v", eventsDir, err)
	}
	var shardFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !shardNameShape.MatchString(e.Name()) {
			t.Errorf("event file %q does not match shard name shape <host>-<date>-<wt8>.jsonl", e.Name())
			continue
		}
		shardFiles = append(shardFiles, e.Name())
	}
	if len(shardFiles) == 0 {
		t.Fatalf("expected at least one event shard under %s, found %v", eventsDir, namesOf(entries))
	}

	// The log should carry the full loop: rule_created, retrieval, selection, feedback.
	shardBody := readShards(t, eventsDir, shardFiles)
	for _, typ := range []string{`"rule_created"`, `"retrieval"`, `"selection"`, `"feedback"`} {
		if !strings.Contains(shardBody, typ) {
			t.Errorf("event log missing %s event type; shard body:\n%s", typ, shardBody)
		}
	}

	// stats should list the rule we created.
	statsStdout, statsStderr, statsErr := runBinary(repo, "stats")
	if statsErr != nil {
		t.Fatalf("stats failed: %v\nstderr:\n%s", statsErr, statsStderr)
	}
	var statsReport struct {
		UnconsolidatedObservations int              `json:"unconsolidated_observations"`
		Rules                      []map[string]any `json:"rules"`
	}
	if jerr := json.Unmarshal([]byte(statsStdout), &statsReport); jerr != nil {
		t.Fatalf("stats stdout not JSON: %v\nraw:\n%s", jerr, statsStdout)
	}
	if len(statsReport.Rules) != 1 || statsReport.Rules[0]["rule_id"] != ruleID {
		t.Fatalf("expected stats for rule %s, got %#v", ruleID, statsReport)
	}

	// gap list surfaces the feedback gap submitted above as one row carrying the
	// uniform top-level `id` (the ev- feedback event id), session, ts, and the
	// grounded report/moment (F4/AC-5).
	gapStdout, gapStderr, gapErr := runBinary(repo, "gap", "list")
	if gapErr != nil {
		t.Fatalf("gap list failed: %v\nstdout:\n%s\nstderr:\n%s", gapErr, gapStdout, gapStderr)
	}
	var gaps []map[string]any
	if jerr := json.Unmarshal([]byte(gapStdout), &gaps); jerr != nil {
		t.Fatalf("gap list stdout not JSON: %v\nraw:\n%s", jerr, gapStdout)
	}
	if len(gaps) != 1 {
		t.Fatalf("expected exactly one feedback gap, got %d: %#v", len(gaps), gaps)
	}
	gapRow := gaps[0]
	requireFields(t, gapRow, "id", "session_id", "ts", "report", "moment")
	if id, _ := gapRow["id"].(string); !strings.HasPrefix(id, "ev-") {
		t.Fatalf("gap row id should be the ev- feedback event id, got %q", gapRow["id"])
	}
	if gapRow["session_id"] != e2eSessionID {
		t.Fatalf("gap row session_id should be the loop session %q, got %#v", e2eSessionID, gapRow["session_id"])
	}
	if gapRow["report"] != "no rule on trimming flaky assertion output" {
		t.Fatalf("unexpected gap report: %#v", gapRow["report"])
	}
	if gapRow["moment"] != "while triaging the failing end-to-end test" {
		t.Fatalf("unexpected gap moment: %#v", gapRow["moment"])
	}

	// --domain is fail-fast on gap list (feedback gaps carry no domain).
	if dStdout, _, dErr := runBinary(repo, "gap", "list", "--domain", "testing"); dErr == nil {
		t.Fatalf("gap list --domain should fail fast, got success:\n%s", dStdout)
	}
}

// TestE2ERuleCreateNoCommit covers the degraded path: rule create in a git-init-only
// repo (unborn HEAD, no commits) succeeds with empty git provenance.
func TestE2ERuleCreateNoCommit(t *testing.T) {
	repo := initE2ERepo(t) // git init + config + remote, but NO commit.

	// init runs but skips project setup on an unborn HEAD (strict DetectRepo); the
	// degraded path is that rule create still works via the lenient detector.
	if stdout, stderr, err := runBinary(repo, "init"); err != nil {
		t.Fatalf("init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	ruleID := e2eCreateRule(t, repo,
		"--use-when", "starting a brand new repo",
		"--content", "Commit early so provenance resolves",
		"--causal-note", "event provenance was empty on an unborn HEAD",
		"--domain", "git",
		"--type", "soft",
	)
	if ruleID == "" {
		t.Fatal("expected a rule id from rule create on an unborn HEAD")
	}

	// The event must have landed with empty git provenance (hash empty).
	eventsDir := filepath.Join(repo, ".auto", "reflect", "events")
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		t.Fatalf("read events dir %s: %v", eventsDir, err)
	}
	var shardFiles []string
	for _, e := range entries {
		if !e.IsDir() && shardNameShape.MatchString(e.Name()) {
			shardFiles = append(shardFiles, e.Name())
		}
	}
	if len(shardFiles) == 0 {
		t.Fatalf("expected an event shard under %s, found %v", eventsDir, namesOf(entries))
	}
	body := readShards(t, eventsDir, shardFiles)
	if !strings.Contains(body, `"rule_created"`) {
		t.Errorf("expected a rule_created event in the no-commit repo; body:\n%s", body)
	}

	// Parse the first event and confirm git.hash is empty (degraded provenance).
	first := strings.SplitN(strings.TrimSpace(body), "\n", 2)[0]
	var ev struct {
		Git struct {
			Hash string `json:"hash"`
		} `json:"git"`
	}
	if jerr := json.Unmarshal([]byte(first), &ev); jerr != nil {
		t.Fatalf("decode event: %v\nraw:\n%s", jerr, first)
	}
	if ev.Git.Hash != "" {
		t.Errorf("expected empty git.hash on an unborn HEAD, got %q", ev.Git.Hash)
	}
}

func e2eCreateRule(t *testing.T, repo string, args ...string) string {
	t.Helper()
	full := append([]string{"rule", "create"}, args...)
	stdout, stderr, err := runBinary(repo, full...)
	if err != nil {
		t.Fatalf("rule create failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var resp struct {
		ID   string `json:"id"` // uniform envelope top-level id (AC-4)
		Rule struct {
			ID string `json:"id"`
		} `json:"rule"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &resp); jerr != nil {
		t.Fatalf("decode rule create json: %v\nraw:\n%s", jerr, stdout)
	}
	if resp.Rule.ID == "" {
		t.Fatalf("rule create returned no id\nraw:\n%s", stdout)
	}
	if resp.ID != resp.Rule.ID {
		t.Fatalf("rule create top-level .id %q != .rule.id %q", resp.ID, resp.Rule.ID)
	}
	return resp.Rule.ID
}

func e2eRetrieve(t *testing.T, repo, intent string) string {
	t.Helper()
	stdout, stderr, err := runBinary(repo, "retrieve", intent)
	if err != nil {
		t.Fatalf("retrieve failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var results []map[string]any
	if jerr := json.Unmarshal([]byte(stdout), &results); jerr != nil {
		t.Fatalf("decode retrieve json: %v\nraw:\n%s", jerr, stdout)
	}
	if len(results) == 0 {
		t.Fatalf("retrieve returned no results\nraw:\n%s", stdout)
	}
	row := results[0]
	// Uniform envelope: each collection row carries a top-level `id` alongside its
	// descriptive id, and the two agree.
	requireFields(t, row, "id", "retrieval_id", "use_when", "rule_type")
	if row["id"] != row["retrieval_id"] {
		t.Fatalf("retrieve row .id %v != .retrieval_id %v", row["id"], row["retrieval_id"])
	}
	return row["id"].(string)
}

func e2eSelect(t *testing.T, repo, retrievalID string) string {
	t.Helper()
	stdout, stderr, err := runBinary(repo, "select", retrievalID)
	if err != nil {
		t.Fatalf("select failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var results []map[string]any
	if jerr := json.Unmarshal([]byte(stdout), &results); jerr != nil {
		t.Fatalf("decode select json: %v\nraw:\n%s", jerr, stdout)
	}
	if len(results) == 0 {
		t.Fatalf("select returned no results\nraw:\n%s", stdout)
	}
	row := results[0]
	// Uniform envelope: top-level `id` mirrors the descriptive `feedback_id`.
	requireFields(t, row, "id", "feedback_id", "content")
	if row["id"] != row["feedback_id"] {
		t.Fatalf("select row .id %v != .feedback_id %v", row["id"], row["feedback_id"])
	}
	return row["id"].(string)
}

func readShards(t *testing.T, dir string, names []string) string {
	t.Helper()
	var b strings.Builder
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read shard %s: %v", name, err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

func namesOf(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
