package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		"auto reflect rule list",
		"auto reflect rule get",
		"auto reflect rebuild",
		"--use-when",
	} {
		if !strings.Contains(stdout, needle) {
			t.Fatalf("quickstart output missing %q\noutput:\n%s", needle, stdout)
		}
	}
}

func TestFeedbackAddAndListJSON(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "docs", "auth.md"), "line one\nline two\nline three\n")
	gitAddCommit(t, repo, "seed docs")

	stdout, stderr, code := runCLIAt(t, repo,
		"feedback", "add",
		"--kind", "helpful",
		"--file", "docs/auth.md",
		"--start", "1",
		"--end", "2",
		"--comment", "clear guidance",
		"--effective-at", "2026-05-01T10:00:00Z",
	)
	if code != 0 {
		t.Fatalf("feedback add failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	var addResp map[string]any
	if err := json.Unmarshal([]byte(stdout), &addResp); err != nil {
		t.Fatalf("decode add response: %v\nraw:\n%s", err, stdout)
	}
	if addResp["created"] != true {
		t.Fatalf("expected created=true, got %v", addResp["created"])
	}

	feedbackPath := filepath.Join(repo, ".auto", "reflect", "feedback.jsonl")
	content, err := os.ReadFile(feedbackPath)
	if err != nil {
		t.Fatalf("read feedback log: %v", err)
	}
	if !strings.Contains(string(content), "\"git_tree_sha\"") {
		t.Fatalf("expected git_tree_sha in event, got:\n%s", content)
	}
	if !strings.Contains(string(content), "\"content_snippet\"") {
		t.Fatalf("expected content_snippet in event, got:\n%s", content)
	}

	stdout, stderr, code = runCLIAt(t, repo, "feedback", "list", "--kind", "helpful")
	if code != 0 {
		t.Fatalf("feedback list failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	var listResp map[string]any
	if err := json.Unmarshal([]byte(stdout), &listResp); err != nil {
		t.Fatalf("decode list response: %v\nraw:\n%s", err, stdout)
	}
	events, ok := listResp["events"].([]any)
	if !ok {
		t.Fatalf("events missing or wrong type: %#v", listResp["events"])
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestFeedbackAddMissingAndContextAndTextMode(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "seed\n")
	gitAddCommit(t, repo, "seed")

	stdout, stderr, code := runCLIAt(t, repo,
		"feedback", "add",
		"--kind", "missing",
		"--comment", "missing setup docs",
		"--effective-at", "2026-05-01",
		"--context", "while implementing init flow",
		"--format", "text",
	)
	if code != 0 {
		t.Fatalf("feedback add missing failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Recorded missing feedback") {
		t.Fatalf("expected text mode output, got:\n%s", stdout)
	}

	stdout, stderr, code = runCLIAt(t, repo, "feedback", "list", "--kind", "missing")
	if code != 0 {
		t.Fatalf("feedback list missing failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("decode json: %v\nraw:\n%s", err, stdout)
	}
	events := resp["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	event := events[0].(map[string]any)
	subject := event["subject"].(map[string]any)
	if subject["type"] != "missing_context" {
		t.Fatalf("expected missing_context subject, got %v", subject["type"])
	}
	if event["context"] != "while implementing init flow" {
		t.Fatalf("expected context in output, got %v", event["context"])
	}
}

func TestFeedbackListFiltersAndWarningsOutput(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "docs", "auth.md"), "a1\na2\na3\n")
	writeFile(t, filepath.Join(repo, "docs", "setup.md"), "s1\ns2\ns3\n")
	gitAddCommit(t, repo, "seed docs")

	_, _, code := runCLIAt(t, repo, "feedback", "add", "--kind", "helpful", "--file", "docs/auth.md", "--start", "1", "--comment", "auth line", "--effective-at", "2026-05-01")
	if code != 0 {
		t.Fatal("add auth event failed")
	}
	time.Sleep(1100 * time.Millisecond)
	_, _, code = runCLIAt(t, repo, "feedback", "add", "--kind", "harmful", "--file", "docs/setup.md", "--start", "2", "--comment", "setup line", "--effective-at", "2026-05-02")
	if code != 0 {
		t.Fatal("add setup event failed")
	}

	stdout, stderr, code := runCLIAt(t, repo, "feedback", "list", "--file", "docs/auth", "--limit", "1", "--since", "7d")
	if code != 0 {
		t.Fatalf("filtered list failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	var filtered map[string]any
	if err := json.Unmarshal([]byte(stdout), &filtered); err != nil {
		t.Fatalf("decode filtered json: %v\nraw:\n%s", err, stdout)
	}
	events := filtered["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("expected 1 filtered event, got %d", len(events))
	}
	event := events[0].(map[string]any)
	subject := event["subject"].(map[string]any)
	if subject["file"] != "docs/auth.md" {
		t.Fatalf("expected auth file filter, got %v", subject["file"])
	}

	after := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02")
	before := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02")
	stdout, stderr, code = runCLIAt(t, repo, "feedback", "list", "--after", after, "--before", before, "--format", "text")
	if code != 0 {
		t.Fatalf("range list failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "[") {
		t.Fatalf("expected text list rows, got:\n%s", stdout)
	}
}

func TestFeedbackJSONModeStdoutStderrSeparation(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "seed\n")
	gitAddCommit(t, repo, "seed")

	stdout, stderr, code := runCLIAt(t, repo,
		"feedback", "add",
		"--kind", "helpful",
		"--start", "1",
		"--comment", "bad",
		"--effective-at", "2026-05-01",
	)
	if code == 0 {
		t.Fatal("expected validation failure")
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("expected empty stdout on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "--file is required") {
		t.Fatalf("expected structured validation stderr, got:\n%s", stderr)
	}
}

func TestFeedbackAddInvalidSpan(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "x\n")
	gitAddCommit(t, repo, "seed")

	_, stderr, code := runCLIAt(t, repo,
		"feedback", "add",
		"--kind", "helpful",
		"--start", "1",
		"--comment", "bad flags",
		"--effective-at", "2026-05-01",
	)
	if code == 0 {
		t.Fatal("expected non-zero for invalid span flags")
	}
	if !strings.Contains(stderr, "--file is required") {
		t.Fatalf("expected remediation hint in stderr, got:\n%s", stderr)
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

	// list returns the rule with id/use_when/domain/rule_type.
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

	// get returns the full rule at version 1.
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

	// edit two fields in one invocation → single version bump to 2.
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

	// Next read refolds from the event log to a byte-identical snapshot.
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
