package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/cli"
)

func TestInitCreatesSettingsAndStateAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)

	writeFile(t, filepath.Join(repo, "README.md"), "seed\n")
	gitAddCommit(t, repo, "seed")

	stdout, stderr, code := runCLIAt(t, repo, "init")
	if code != 0 {
		t.Fatalf("first init failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	sharedPath := filepath.Join(home, ".auto", "settings.json")
	reflectSettingsPath := filepath.Join(home, ".auto", "reflect", "settings.json")
	playbookPath := filepath.Join(repo, ".auto", "reflect", "playbook.json")
	eventsDir := filepath.Join(repo, ".auto", "reflect", "events")

	assertFileExists(t, sharedPath)
	assertFileExists(t, reflectSettingsPath)
	assertFileExists(t, playbookPath)
	assertFileExists(t, eventsDir)

	playbookBytes, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatalf("read playbook: %v", err)
	}
	var playbook map[string]any
	if err := json.Unmarshal(playbookBytes, &playbook); err != nil {
		t.Fatalf("decode playbook: %v", err)
	}
	if playbook["schema_version"] != float64(1) {
		t.Fatalf("unexpected schema_version: %v", playbook["schema_version"])
	}
	rules, ok := playbook["rules"].([]any)
	if !ok {
		t.Fatalf("playbook rules missing or wrong type: %#v", playbook["rules"])
	}
	if len(rules) != 0 {
		t.Fatalf("expected empty rules array, got %d", len(rules))
	}

	firstShared, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatalf("read first shared settings: %v", err)
	}
	firstReflect, err := os.ReadFile(reflectSettingsPath)
	if err != nil {
		t.Fatalf("read first reflect settings: %v", err)
	}
	firstPlaybook, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatalf("read first playbook: %v", err)
	}

	stdout, stderr, code = runCLIAt(t, repo, "init")
	if code != 0 {
		t.Fatalf("second init failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	secondShared, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatalf("read second shared settings: %v", err)
	}
	secondReflect, err := os.ReadFile(reflectSettingsPath)
	if err != nil {
		t.Fatalf("read second reflect settings: %v", err)
	}
	secondPlaybook, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatalf("read second playbook: %v", err)
	}

	if !bytes.Equal(firstShared, secondShared) {
		t.Fatalf("shared settings changed across repeated init runs\nfirst:\n%s\nsecond:\n%s", firstShared, secondShared)
	}
	if !bytes.Equal(firstReflect, secondReflect) {
		t.Fatalf("reflect settings changed across repeated init runs\nfirst:\n%s\nsecond:\n%s", firstReflect, secondReflect)
	}
	if !bytes.Equal(firstPlaybook, secondPlaybook) {
		t.Fatalf("playbook changed across repeated init runs\nfirst:\n%s\nsecond:\n%s", firstPlaybook, secondPlaybook)
	}
}

func TestQuickstartIncludesInitAndCoreCommands(t *testing.T) {
	cwd := t.TempDir()
	stdout, stderr, code := runCLIAt(t, cwd, "quickstart")
	if code != 0 {
		t.Fatalf("quickstart failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, needle := range []string{
		"auto reflect init",
		"auto reflect rule create",
		"auto reflect retrieve",
		"auto reflect select",
		"auto reflect feedback",
		"auto reflect gate check",
		"--use-when",
		"retrieval_id",
		"feedback_id",
		"rule graduate",
		"--lifecycle enforced",
		"--task-id",
		"--evidence-file",
		"--evidence-commit",
		"--evidence-line-range",
		`"op": "split"`,
	} {
		if !strings.Contains(stdout, needle) {
			t.Fatalf("quickstart output missing %q\noutput:\n%s", needle, stdout)
		}
	}
}

func createTestRule(t *testing.T, repo string, extraArgs ...string) string {
	t.Helper()
	args := append([]string{
		"rule", "create",
		"--use-when", "writing flaky end-to-end tests",
		"--content", "Keep passing test logs short",
		"--causal-note", "noisy logs hid the real failure",
		"--domain", "testing",
		"--type", "soft",
	}, extraArgs...)
	stdout, stderr, code := runCLIAt(t, repo, args...)
	if code != 0 {
		t.Fatalf("rule create failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	var resp struct {
		Rule struct {
			ID string `json:"id"`
		} `json:"rule"`
	}
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("decode create json: %v\nraw:\n%s", err, stdout)
	}
	if resp.Rule.ID == "" {
		t.Fatalf("create returned no rule id\nraw:\n%s", stdout)
	}
	return resp.Rule.ID
}

func TestRuleCreateListGetEditRoundTrip(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	id := createTestRule(t, repo)

	stdout, stderr, code := runCLIAt(t, repo, "rule", "list")
	if code != 0 {
		t.Fatalf("rule list failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var listResp struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal([]byte(stdout), &listResp); err != nil {
		t.Fatalf("decode list json: %v\nraw:\n%s", err, stdout)
	}
	if len(listResp.Rules) != 1 || listResp.Rules[0]["id"] != id {
		t.Fatalf("list did not return created rule: %#v", listResp.Rules)
	}

	stdout, _, code = runCLIAt(t, repo, "rule", "get", id)
	if code != 0 {
		t.Fatalf("rule get failed: code=%d", code)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode get json: %v\nraw:\n%s", err, stdout)
	}
	if got["version"] != float64(1) {
		t.Fatalf("expected version 1, got %v", got["version"])
	}

	stdout, stderr, code = runCLIAt(t, repo, "rule", "edit", id,
		"--lifecycle", "confirmed",
		"--content", "Keep passing test logs short and quiet",
	)
	if code != 0 {
		t.Fatalf("rule edit failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var editResp struct {
		Rule map[string]any `json:"rule"`
	}
	if err := json.Unmarshal([]byte(stdout), &editResp); err != nil {
		t.Fatalf("decode edit json: %v\nraw:\n%s", err, stdout)
	}
	if editResp.Rule["version"] != float64(2) {
		t.Fatalf("expected one version bump to 2, got %v", editResp.Rule["version"])
	}
	if editResp.Rule["lifecycle"] != "confirmed" {
		t.Fatalf("expected lifecycle confirmed, got %v", editResp.Rule["lifecycle"])
	}
	if editResp.Rule["content"] != "Keep passing test logs short and quiet" {
		t.Fatalf("expected edited content, got %v", editResp.Rule["content"])
	}
}

func TestRuleSnapshotDeleteRefoldsIdentical(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	id := createTestRule(t, repo)
	_, _, code := runCLIAt(t, repo, "rule", "edit", id, "--lifecycle", "confirmed")
	if code != 0 {
		t.Fatal("rule edit failed")
	}

	playbookPath := filepath.Join(repo, ".auto", "reflect", "playbook.json")
	before, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatalf("read playbook: %v", err)
	}

	if err := os.Remove(playbookPath); err != nil {
		t.Fatalf("delete playbook: %v", err)
	}

	if _, _, code = runCLIAt(t, repo, "rule", "list"); code != 0 {
		t.Fatal("rule list after delete failed")
	}
	after, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatalf("read refolded playbook: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("refold not byte-identical\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRuleCreateHardWithoutDomainFails(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	stdout, stderr, code := runCLIAt(t, repo,
		"rule", "create",
		"--use-when", "always",
		"--content", "this hard rule has no domain",
		"--causal-note", "should be rejected",
		"--type", "hard",
	)
	if code == 0 {
		t.Fatalf("expected non-zero exit for hard rule without domain\nstdout:\n%s", stdout)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("expected empty stdout on validation error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "hard rules must declare at least one domain") || !strings.Contains(stderr, "--domain") {
		t.Fatalf("expected remediation hint in stderr, got:\n%s", stderr)
	}
}

// --- Retrieval loop integration (AC-1, AC-2, AC-3, AC-3b) ---

func TestLoopHappyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_SESSION_ID", "loop-happy")
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	hardID := createTestRuleWith(t, repo,
		"--use-when", "writing go cli flags with cobra",
		"--content", "Use cobra StringSliceVar for repeatable flags",
		"--causal-note", "manual parsing dropped values",
		"--domain", "cli", "--type", "hard")
	_ = hardID

	// AC-1: retrieve returns predicates only and appends a retrieval event.
	stdout, stderr, code := runCLIAt(t, repo, "retrieve", "writing go cli flags", "--domain", "cli")
	if code != 0 {
		t.Fatalf("retrieve failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var retrieved []map[string]any
	if err := json.Unmarshal([]byte(stdout), &retrieved); err != nil {
		t.Fatalf("decode retrieve: %v\nraw:\n%s", err, stdout)
	}
	if len(retrieved) != 1 {
		t.Fatalf("expected 1 retrieved rule, got %d", len(retrieved))
	}
	if _, leaked := retrieved[0]["content"]; leaked {
		t.Fatalf("retrieve must not leak content: %#v", retrieved[0])
	}
	rtID, _ := retrieved[0]["retrieval_id"].(string)
	if !strings.HasPrefix(rtID, "rt-") {
		t.Fatalf("expected rt- id, got %q", rtID)
	}

	// AC-2: select preserves order, mints fb-ids, appends a selection event.
	stdout, stderr, code = runCLIAt(t, repo, "select", rtID)
	if code != 0 {
		t.Fatalf("select failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var selected []map[string]any
	if err := json.Unmarshal([]byte(stdout), &selected); err != nil {
		t.Fatalf("decode select: %v\nraw:\n%s", err, stdout)
	}
	if len(selected) != 1 {
		t.Fatalf("expected 1 selected, got %d", len(selected))
	}
	fbID, _ := selected[0]["feedback_id"].(string)
	if !strings.HasPrefix(fbID, "fb-") {
		t.Fatalf("expected fb- id, got %q", fbID)
	}
	if selected[0]["content"] == "" || selected[0]["content"] == nil {
		t.Fatalf("select must reveal content: %#v", selected[0])
	}

	// AC-3b: gate is open before feedback.
	_, _, code = runCLIAt(t, repo, "gate", "check")
	if code == 0 {
		t.Fatal("gate should be open (non-zero) before feedback")
	}

	// AC-3: incomplete feedback is rejected with remediation.
	incomplete := `{"outcome":"success","summary":"did it","rankings":[]}`
	stdout, stderr, code = runCLIAt(t, repo, "feedback", incomplete)
	if code == 0 {
		t.Fatalf("expected incomplete feedback to be rejected\nstdout:\n%s", stdout)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("expected empty stdout on rejected feedback, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "outstanding") && !strings.Contains(stderr, "missing") {
		t.Fatalf("expected remediation about outstanding ids, got:\n%s", stderr)
	}

	// AC-3: complete feedback is accepted.
	complete := fmt.Sprintf(`{"outcome":"success","summary":"did it","rankings":[{"feedback_id":%q,"rank":1,"reason":"used the rule"}]}`, fbID)
	_, stderr, code = runCLIAt(t, repo, "feedback", complete)
	if code != 0 {
		t.Fatalf("complete feedback rejected: code=%d\nstderr:\n%s", code, stderr)
	}

	// Gate closed after complete feedback.
	_, stderr, code = runCLIAt(t, repo, "gate", "check")
	if code != 0 {
		t.Fatalf("gate should be clean after feedback: code=%d\nstderr:\n%s", code, stderr)
	}

	// Assert events on disk with correct rt -> fb linkage.
	assertLoopEventsOnDisk(t, repo, rtID, fbID)
}

func TestLoopStatsAfterTwoSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	id := createTestRuleWith(t, repo,
		"--use-when", "go cli flags topic stats",
		"--content", "rule body for stats",
		"--causal-note", "needed for stats test",
		"--domain", "go", "--type", "soft")

	for i := range 2 {
		t.Setenv("AUTO_SESSION_ID", fmt.Sprintf("stats-session-%d", i))
		stdout, _, code := runCLIAt(t, repo, "retrieve", "go cli flags topic stats")
		if code != 0 {
			t.Fatalf("retrieve session %d failed", i)
		}
		var retrieved []map[string]any
		if err := json.Unmarshal([]byte(stdout), &retrieved); err != nil {
			t.Fatalf("decode retrieve: %v", err)
		}
		rtID := retrieved[0]["retrieval_id"].(string)
		if _, _, code := runCLIAt(t, repo, "select", rtID); code != 0 {
			t.Fatalf("select session %d failed", i)
		}
	}

	stdout, stderr, code := runCLIAt(t, repo, "stats")
	if code != 0 {
		t.Fatalf("stats failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var report struct {
		UnconsolidatedObservations int              `json:"unconsolidated_observations"`
		Rules                      []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode stats: %v\nraw:\n%s", err, stdout)
	}
	stats := report.Rules
	if len(stats) != 1 || stats[0]["rule_id"] != id {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if stats[0]["surfaced"] != float64(2) || stats[0]["selected"] != float64(2) {
		t.Fatalf("expected surfaced=2 selected=2, got %#v", stats[0])
	}
	if stats[0]["selection_rate"] != float64(1) {
		t.Fatalf("expected selection_rate 1, got %v", stats[0]["selection_rate"])
	}
}

func createTestRuleWith(t *testing.T, repo string, args ...string) string {
	t.Helper()
	full := append([]string{"rule", "create"}, args...)
	stdout, stderr, code := runCLIAt(t, repo, full...)
	if code != 0 {
		t.Fatalf("rule create failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var resp struct {
		Rule struct {
			ID string `json:"id"`
		} `json:"rule"`
	}
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("decode create: %v\nraw:\n%s", err, stdout)
	}
	return resp.Rule.ID
}

// assertLoopEventsOnDisk reads the event shards and verifies the retrieval and
// selection events link rt-id -> fb-id, and a feedback event covers the fb-id.
func assertLoopEventsOnDisk(t *testing.T, repo, rtID, fbID string) {
	t.Helper()
	eventsDir := filepath.Join(repo, ".auto", "reflect", "events")
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		t.Fatalf("read events dir: %v", err)
	}

	var types []string
	linkedRetrieval := false
	linkedSelection := false
	coveredFeedback := false
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(eventsDir, e.Name()))
		if err != nil {
			t.Fatalf("read shard: %v", err)
		}
		for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			var env struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(line), &env); err != nil {
				t.Fatalf("decode event: %v\nline:\n%s", err, line)
			}
			types = append(types, env.Type)
			switch env.Type {
			case "retrieval":
				var p struct {
					Items []struct {
						RetrievalID string `json:"retrieval_id"`
						RuleID      string `json:"rule_id"`
					} `json:"items"`
				}
				_ = json.Unmarshal(env.Payload, &p)
				for _, it := range p.Items {
					if it.RetrievalID == rtID {
						linkedRetrieval = true
					}
				}
			case "selection":
				var p struct {
					Items []struct {
						FeedbackID  string `json:"feedback_id"`
						RetrievalID string `json:"retrieval_id"`
					} `json:"items"`
				}
				_ = json.Unmarshal(env.Payload, &p)
				for _, it := range p.Items {
					if it.RetrievalID == rtID && it.FeedbackID == fbID {
						linkedSelection = true
					}
				}
			case "feedback":
				var p struct {
					Rankings []struct {
						FeedbackID string `json:"feedback_id"`
					} `json:"rankings"`
				}
				_ = json.Unmarshal(env.Payload, &p)
				for _, r := range p.Rankings {
					if r.FeedbackID == fbID {
						coveredFeedback = true
					}
				}
			}
		}
	}

	if !linkedRetrieval {
		t.Fatalf("no retrieval event minted rt-id %q; types=%v", rtID, types)
	}
	if !linkedSelection {
		t.Fatalf("no selection event linked rt-id %q to fb-id %q; types=%v", rtID, fbID, types)
	}
	if !coveredFeedback {
		t.Fatalf("no feedback event covered fb-id %q; types=%v", fbID, types)
	}
}

func TestRetrieveLifecycleFilteringAndRuleListFilter(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	// Seed one rule per lifecycle, all sharing keywords so they'd score equally if
	// lifecycle were ignored.
	seed := func(lifecycle string) string {
		return createTestRuleWith(t, repo,
			"--use-when", "lifecycle retrieval "+lifecycle+" case",
			"--content", "guidance for the "+lifecycle+" rule",
			"--causal-note", "covers the "+lifecycle+" lifecycle path",
			"--domain", "lifecycletest",
			"--type", "soft",
			"--lifecycle", lifecycle,
		)
	}
	draftID := seed("draft")
	_ = seed("confirmed")
	_ = seed("stale")

	type retrievedItem struct {
		Lifecycle string `json:"lifecycle"`
		Draft     bool   `json:"draft"`
		UseWhen   string `json:"use_when"`
	}

	// Default retrieve: draft + confirmed surface, stale never does.
	stdout, stderr, code := runCLIAt(t, repo, "retrieve", "lifecycle retrieval case")
	if code != 0 {
		t.Fatalf("retrieve failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var def []retrievedItem
	if err := json.Unmarshal([]byte(stdout), &def); err != nil {
		t.Fatalf("decode retrieve json: %v\nraw:\n%s", err, stdout)
	}
	gotLifecycles := map[string]bool{}
	for _, it := range def {
		gotLifecycles[it.Lifecycle] = true
		if it.Lifecycle == "stale" {
			t.Fatalf("stale rule surfaced in default retrieve: %#v", def)
		}
		if (it.Lifecycle == "draft") != it.Draft {
			t.Fatalf("draft flag inconsistent with lifecycle: %#v", it)
		}
	}
	if !gotLifecycles["draft"] || !gotLifecycles["confirmed"] {
		t.Fatalf("default retrieve should include draft and confirmed, got %#v", def)
	}

	// --no-drafts: confirmed only.
	stdout, stderr, code = runCLIAt(t, repo, "retrieve", "lifecycle retrieval case", "--no-drafts")
	if code != 0 {
		t.Fatalf("retrieve --no-drafts failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var noDraft []retrievedItem
	if err := json.Unmarshal([]byte(stdout), &noDraft); err != nil {
		t.Fatalf("decode --no-drafts json: %v\nraw:\n%s", err, stdout)
	}
	if len(noDraft) != 1 || noDraft[0].Lifecycle != "confirmed" {
		t.Fatalf("--no-drafts should return only the confirmed rule, got %#v", noDraft)
	}

	// rule list --lifecycle draft: only the draft rule.
	stdout, stderr, code = runCLIAt(t, repo, "rule", "list", "--lifecycle", "draft")
	if code != 0 {
		t.Fatalf("rule list --lifecycle draft failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var listResp struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal([]byte(stdout), &listResp); err != nil {
		t.Fatalf("decode rule list json: %v\nraw:\n%s", err, stdout)
	}
	if len(listResp.Rules) != 1 || listResp.Rules[0]["id"] != draftID {
		t.Fatalf("rule list --lifecycle draft should return only the draft rule, got %#v", listResp.Rules)
	}
	if listResp.Rules[0]["lifecycle"] != "draft" {
		t.Fatalf("rule list item missing/incorrect lifecycle field: %#v", listResp.Rules[0])
	}

	// Invalid --lifecycle value fails fast with a remediation hint.
	stdout, stderr, code = runCLIAt(t, repo, "rule", "list", "--lifecycle", "bogus")
	if code != 1 {
		t.Fatalf("expected exit 1 for bad --lifecycle, got %d\nstdout:\n%s", code, stdout)
	}
	if !strings.Contains(stderr, "draft") || !strings.Contains(stderr, "confirmed") || !strings.Contains(stderr, "stale") {
		t.Fatalf("bad --lifecycle error should list valid values, got stderr:\n%s", stderr)
	}
}

func TestRuleGraduateEnforcedLifecycle(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	id := createTestRuleWith(t, repo,
		"--use-when", "checking unchecked errors in go",
		"--content", "always handle returned errors",
		"--causal-note", "swallowed errors hid a real bug",
		"--domain", "go",
		"--type", "soft",
		"--lifecycle", "confirmed",
	)

	// Graduate the rule into a static lint check.
	stdout, stderr, code := runCLIAt(t, repo, "rule", "graduate", id,
		"--linter", "golangci-lint",
		"--check", "errcheck",
		"--config-path", ".golangci.yml",
		"--note", "now enforced statically",
	)
	if code != 0 {
		t.Fatalf("rule graduate failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var gradResp struct {
		Graduated bool           `json:"graduated"`
		Rule      map[string]any `json:"rule"`
	}
	if err := json.Unmarshal([]byte(stdout), &gradResp); err != nil {
		t.Fatalf("decode graduate json: %v\nraw:\n%s", err, stdout)
	}
	if !gradResp.Graduated {
		t.Fatalf("expected graduated=true, got %#v", gradResp)
	}
	if gradResp.Rule["lifecycle"] != "enforced" {
		t.Fatalf("expected lifecycle enforced, got %v", gradResp.Rule["lifecycle"])
	}
	lintRef, ok := gradResp.Rule["lint_ref"].(map[string]any)
	if !ok {
		t.Fatalf("expected lint_ref object on graduated rule, got %#v", gradResp.Rule["lint_ref"])
	}
	if lintRef["linter"] != "golangci-lint" || lintRef["check"] != "errcheck" {
		t.Fatalf("lint_ref linter/check incorrect: %#v", lintRef)
	}

	// rule list --lifecycle enforced returns it.
	stdout, stderr, code = runCLIAt(t, repo, "rule", "list", "--lifecycle", "enforced")
	if code != 0 {
		t.Fatalf("rule list --lifecycle enforced failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var listResp struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal([]byte(stdout), &listResp); err != nil {
		t.Fatalf("decode rule list json: %v\nraw:\n%s", err, stdout)
	}
	if len(listResp.Rules) != 1 || listResp.Rules[0]["id"] != id {
		t.Fatalf("rule list --lifecycle enforced should return only the graduated rule, got %#v", listResp.Rules)
	}

	// retrieve over a matching intent must NOT include the enforced rule.
	stdout, stderr, code = runCLIAt(t, repo, "retrieve", "checking unchecked errors in go", "--domain", "go")
	if code != 0 {
		t.Fatalf("retrieve failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var retrieved []map[string]any
	if err := json.Unmarshal([]byte(stdout), &retrieved); err != nil {
		t.Fatalf("decode retrieve json: %v\nraw:\n%s", err, stdout)
	}
	for _, it := range retrieved {
		if it["lifecycle"] == "enforced" {
			t.Fatalf("enforced rule surfaced in retrieve: %#v", retrieved)
		}
	}

	// graduate without --linter/--check fails fast.
	stdout, _, code = runCLIAt(t, repo, "rule", "graduate", id)
	if code == 0 {
		t.Fatalf("expected non-zero exit for graduate without required flags\nstdout:\n%s", stdout)
	}
}

func runCLIAt(t *testing.T, cwd string, args ...string) (stdout string, stderr string, code int) {
	t.Helper()

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(prev)
	}()

	var out bytes.Buffer
	var errOut bytes.Buffer

	application := app.New(&out, &errOut)
	rootCmd := cli.NewRootCmd(application)
	rootCmd.SetArgs(args)
	err = rootCmd.ExecuteContext(context.Background())
	if err != nil {
		code = 1
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
			if exitErr.Err != nil && exitErr.Err.Error() != "" {
				errOut.WriteString(exitErr.Err.Error())
				errOut.WriteByte('\n')
			}
		} else {
			errOut.WriteString(err.Error())
			errOut.WriteByte('\n')
		}
	}

	return out.String(), errOut.String(), code
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runCmd(t, repo, "git", "init")
	runCmd(t, repo, "git", "config", "user.name", "Test User")
	runCmd(t, repo, "git", "config", "user.email", "test@example.com")
	runCmd(t, repo, "git", "remote", "add", "origin", "git@github.com:example/auto-stack.git")
	return repo
}

func gitAddCommit(t *testing.T, repo, message string) {
	t.Helper()
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", message)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func runCmd(t *testing.T, cwd string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = cwd
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s failed: %v\nstderr:\n%s", name, strings.Join(args, " "), err, stderr.String())
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected %s NOT to exist", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}

// TestInitProjectScopesToRepoLocal asserts `init --project` creates only the
// repository-local state (events dir + playbook) and skips global settings.
func TestInitProjectScopesToRepoLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "seed\n")
	gitAddCommit(t, repo, "seed")

	stdout, stderr, code := runCLIAt(t, repo, "init", "--project")
	if code != 0 {
		t.Fatalf("init --project failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// Repo-local state created.
	assertFileExists(t, filepath.Join(repo, ".auto", "reflect", "playbook.json"))
	assertFileExists(t, filepath.Join(repo, ".auto", "reflect", "events"))

	// Global settings NOT created by --project.
	assertNotExists(t, filepath.Join(home, ".auto", "settings.json"))
	assertNotExists(t, filepath.Join(home, ".auto", "reflect", "settings.json"))
}

// TestInitProjectOutsideRepoFails asserts `init --project` errors when not in a
// git repo (project-local setup has no repo to target).
func TestInitProjectOutsideRepoFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir() // not a git repo

	stdout, stderr, code := runCLIAt(t, cwd, "init", "--project")
	if code == 0 {
		t.Fatalf("expected non-zero exit for init --project outside a git repo\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "git repo") {
		t.Fatalf("expected remediation mentioning git repo, got:\n%s", stderr)
	}
}

// TestFeedbackUngroundedGapRemediation asserts an ungrounded gap (gap present but
// gap.moment missing) gets the gap-specific remediation, not the ranking hint.
func TestFeedbackUngroundedGapRemediation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_SESSION_ID", "gap-remediation")
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	createTestRuleWith(t, repo,
		"--use-when", "writing go cli flags with cobra",
		"--content", "Use cobra StringSliceVar for repeatable flags",
		"--causal-note", "manual parsing dropped values",
		"--domain", "cli", "--type", "soft")

	stdout, _, code := runCLIAt(t, repo, "retrieve", "writing go cli flags", "--domain", "cli")
	if code != 0 {
		t.Fatalf("retrieve failed:\n%s", stdout)
	}
	var retrieved []map[string]any
	if err := json.Unmarshal([]byte(stdout), &retrieved); err != nil {
		t.Fatalf("decode retrieve: %v\nraw:\n%s", err, stdout)
	}
	rtID, _ := retrieved[0]["retrieval_id"].(string)

	stdout, _, code = runCLIAt(t, repo, "select", rtID)
	if code != 0 {
		t.Fatalf("select failed:\n%s", stdout)
	}
	var selected []map[string]any
	if err := json.Unmarshal([]byte(stdout), &selected); err != nil {
		t.Fatalf("decode select: %v\nraw:\n%s", err, stdout)
	}
	fbID, _ := selected[0]["feedback_id"].(string)

	// Complete rankings, but the gap is present with an empty moment (ungrounded).
	payload := fmt.Sprintf(`{"outcome":"success","summary":"did it","rankings":[{"feedback_id":%q,"rank":1,"reason":"used it"}],"gap":{"report":"needed a rule about X","moment":""}}`, fbID)
	stdout, stderr, code := runCLIAt(t, repo, "feedback", payload)
	if code == 0 {
		t.Fatalf("expected ungrounded gap to be rejected\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "gap.moment") {
		t.Fatalf("expected gap-specific remediation mentioning gap.moment, got:\n%s", stderr)
	}
	if strings.Contains(stderr, "rank exactly the outstanding feedback ids") {
		t.Fatalf("ranking remediation should NOT appear for a rankings-complete payload, got:\n%s", stderr)
	}
}
