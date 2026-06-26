package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/app"
	"github.com/mistakenot/auto-skill/internal/cli"
)

func TestInitProjectCreatesFilesAndIsIdempotent(t *testing.T) {
	root := t.TempDir()

	stdout, stderr, code := runCLI(t, "--root", root, "init", "--project", "-y")
	if code != 0 {
		t.Fatalf("init --project failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	skillsYAML := filepath.Join(root, ".auto", "skills", "skills.yaml")
	lockJSON := filepath.Join(root, ".auto", "skills", "lock.json")
	skillsDir := filepath.Join(root, "skills")
	assertExists(t, skillsYAML)
	assertExists(t, lockJSON)
	assertExists(t, skillsDir)

	firstYAML, err := os.ReadFile(skillsYAML)
	if err != nil {
		t.Fatalf("read skills.yaml: %v", err)
	}

	stdout, stderr, code = runCLI(t, "--root", root, "init", "--project", "-y")
	if code != 0 {
		t.Fatalf("second init --project failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	secondYAML, err := os.ReadFile(skillsYAML)
	if err != nil {
		t.Fatalf("read skills.yaml after rerun: %v", err)
	}
	if !bytes.Equal(firstYAML, secondYAML) {
		t.Fatalf("skills.yaml changed across idempotent runs\nfirst:\n%s\nsecond:\n%s", firstYAML, secondYAML)
	}
}

func TestCreateHappyWithDirs(t *testing.T) {
	root := t.TempDir()
	_, _, code := runCLI(t, "--root", root, "init", "--project", "-y")
	if code != 0 {
		t.Fatal("init failed")
	}

	desc := "Use when creating reusable workflows. Prefer for repeatable task execution."
	stdout, stderr, code := runCLI(t, "--root", root, "create", "my-skill", "--description", desc, "--with-dirs")
	if code != 0 {
		t.Fatalf("create failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if strings.Contains(stderr, "warning") || strings.Contains(stderr, "error") {
		t.Fatalf("expected no warnings/errors on create, got stderr:\n%s", stderr)
	}

	assertExists(t, filepath.Join(root, "skills", "my-skill", "SKILL.md"))
	assertExists(t, filepath.Join(root, "skills", "my-skill", "references"))
	assertExists(t, filepath.Join(root, "skills", "my-skill", "scripts"))
	assertExists(t, filepath.Join(root, "skills", "my-skill", "assets"))

	lintOut, lintErr, lintCode := runCLI(t, "--root", root, "lint")
	if lintCode != 0 {
		t.Fatalf("lint failed: code=%d\nstdout:\n%s\nstderr:\n%s", lintCode, lintOut, lintErr)
	}
	diags := decodeDiagnostics(t, lintOut)
	if containsDiagCode(diags, "missing_trigger_phrase") {
		t.Fatalf("expected no missing_trigger_phrase warning, got: %s", lintOut)
	}
}

func TestCreateRejectsBadAndLongNames(t *testing.T) {
	root := t.TempDir()
	_, _, code := runCLI(t, "--root", root, "init", "--project", "-y")
	if code != 0 {
		t.Fatal("init failed")
	}

	_, stderr, code := runCLI(t, "--root", root, "create", "My Skill", "--description", "Use when needed.")
	if code == 0 || !strings.Contains(stderr, "invalid skill name") {
		t.Fatalf("expected bad-name failure, code=%d stderr=%q", code, stderr)
	}

	longName := strings.Repeat("a", 65)
	_, stderr, code = runCLI(t, "--root", root, "create", longName, "--description", "Use when needed.")
	if code == 0 || !strings.Contains(stderr, "must be <= 64 chars") {
		t.Fatalf("expected long-name failure, code=%d stderr=%q", code, stderr)
	}
}

func TestLintFrontmatterAndBodyChecks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "bad", "SKILL.md"), `---
description: "Helps with deployment"
---
This skill is designed to do things.
`)

	stdout, stderr, code := runCLI(t, "--root", root, "lint")
	if code == 0 {
		t.Fatalf("expected lint errors\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}

	diags := decodeDiagnostics(t, stdout)
	assertHasDiag(t, diags, "missing_name")
	assertHasDiag(t, diags, "missing_trigger_phrase")
	assertHasDiag(t, diags, "weak_opening")
}

func TestLintNoFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "no-frontmatter", "SKILL.md"), `# No frontmatter
content
`)

	stdout, _, code := runCLI(t, "--root", root, "lint")
	if code == 0 {
		t.Fatalf("expected lint failure, got success:\n%s", stdout)
	}
	assertHasDiag(t, decodeDiagnostics(t, stdout), "invalid_frontmatter")
}

func TestLintLinkChecksAndSecretDetection(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "source.md"), `---
id: "deadbeef"
hash: "11111111"
summary: "x"
title: "x"
---
doc body
`)
	writeFile(t, filepath.Join(root, "skills", "checks", "SKILL.md"), `---
name: checks
description: "Use when validating links. Prefer for lint coverage."
---
<!-- [autodoc(deadbeef@22222222, aaaaaaaa)] -->
<!-- [autodoc(feedface@11111111, aaaaaaaa)] -->
[bad](../docs/missing.md)
scripts/run.sh
AKIAIOSFODNN7EXAMPLE
`)

	stdout, _, code := runCLI(t, "--root", root, "lint")
	if code == 0 {
		t.Fatalf("expected lint failure, got success:\n%s", stdout)
	}

	diags := decodeDiagnostics(t, stdout)
	assertHasDiag(t, diags, "stale_autodoc_link")
	assertHasDiag(t, diags, "broken_autodoc_link")
	assertHasDiag(t, diags, "broken_local_link")
	assertHasDiag(t, diags, "broken_side_file")
	assertHasDiag(t, diags, "secret_detected")
}

func TestLintDetectsNotADirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "standalone.md"), "not a directory")

	stdout, _, code := runCLI(t, "--root", root, "lint")
	if code == 0 {
		t.Fatalf("expected lint failure, got success:\n%s", stdout)
	}
	assertHasDiag(t, decodeDiagnostics(t, stdout), "not_a_directory")
}

func TestLintListingTokenBudgetWarningAndError(t *testing.T) {
	rootWarn := t.TempDir()
	seedManySkills(t, rootWarn, 24, strings.Repeat("a", 420))

	stdout, stderr, code := runCLI(t, "--root", rootWarn, "lint")
	if code != 0 {
		t.Fatalf("expected warning-only lint success, code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	assertHasDiag(t, decodeDiagnostics(t, stdout), "listing_too_large")

	rootErr := t.TempDir()
	seedManySkills(t, rootErr, 48, strings.Repeat("b", 420))

	stdout, _, code = runCLI(t, "--root", rootErr, "lint")
	if code == 0 {
		t.Fatalf("expected lint error for oversized listing, got success:\n%s", stdout)
	}
	assertHasDiag(t, decodeDiagnostics(t, stdout), "listing_too_large")
}

func TestLintSkipsNonFrontmatterDocs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "plain.md"), "# Just a plain doc\nNo frontmatter here.\n")
	writeFile(t, filepath.Join(root, "docs", "valid.md"), "---\nid: \"aabbccdd\"\nhash: \"11223344\"\ntitle: \"x\"\nsummary: \"x\"\n---\nDoc body.\n")
	writeFile(t, filepath.Join(root, "skills", "good", "SKILL.md"), validSkill("good", "Use when testing doc discovery resilience.", "## Workflow\n\n1. Step.\n"))

	stdout, stderr, code := runCLI(t, "--root", root, "lint")
	if code != 0 {
		t.Fatalf("expected lint success despite non-frontmatter doc: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

func TestLintTextOutput(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "bad", "SKILL.md"), "---\ndescription: \"Helps with deployment\"\n---\nThis skill is designed to do things.\n")

	stdout, _, code := runCLI(t, "--root", root, "lint", "--text")
	if code == 0 {
		t.Fatalf("expected lint errors\nstdout:\n%s", stdout)
	}

	if !strings.Contains(stdout, "error[missing_name]:") {
		t.Fatalf("expected text output with error[missing_name]:, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "skills/bad/SKILL.md") {
		t.Fatalf("expected full path in text output, got:\n%s", stdout)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout), "[") {
		t.Fatalf("expected text output, got JSON:\n%s", stdout)
	}
}

func TestLintTextAndJSONMutuallyExclusive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "ok", "SKILL.md"), validSkill("ok", "Use when testing flags.", "## Workflow\n\n1. Step.\n"))

	_, stderr, code := runCLI(t, "--root", root, "lint", "--text", "--json")
	if code == 0 {
		t.Fatal("expected error when --text and --json combined")
	}
	if !strings.Contains(stderr, "cannot be combined") {
		t.Fatalf("expected mutual exclusion error, got stderr:\n%s", stderr)
	}
}

func TestLintValueFieldsPopulated(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "bad", "SKILL.md"), "---\ndescription: \"Helps with deployment\"\n---\nThis skill is designed to do things.\n")

	stdout, _, code := runCLI(t, "--root", root, "lint")
	if code == 0 {
		t.Fatalf("expected lint errors\nstdout:\n%s", stdout)
	}

	diags := decodeDiagnostics(t, stdout)
	for _, d := range diags {
		code, _ := d["code"].(string)
		switch code {
		case "missing_trigger_phrase":
			if d["value"] == nil {
				t.Fatal("missing_trigger_phrase should have value with description snippet")
			}
		case "weak_opening":
			if d["value"] == nil {
				t.Fatal("weak_opening should have value with body snippet")
			}
		}
	}
}

func TestLsMixedAndJSON(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"), validSkill("alpha", "Use when alpha. Prefer for alpha tasks.", "## Workflow\n\n1. Run alpha.\n"))
	writeFile(t, filepath.Join(root, "skills", "beta", "SKILL.md"), validSkill("beta", "Use when beta. Prefer for beta tasks.", "## Workflow\n\n1. Run beta.\n"))
	writeFile(t, filepath.Join(root, "skills", "broken", "SKILL.md"), "no-frontmatter")

	stdout, stderr, code := runCLI(t, "--root", root, "ls")
	if code == 0 {
		t.Fatalf("expected non-zero due mixed parse errors\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("json decode failed: %v\nraw:\n%s", err, stdout)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len=%d, want 2", len(rows))
	}
	if !strings.Contains(stderr, "error:") {
		t.Fatalf("expected parse error on stderr, got:\n%s", stderr)
	}

	stdout, stderr, code = runCLI(t, "--root", root, "ls", "--text")
	if code == 0 {
		t.Fatalf("expected non-zero for mixed parse errors with --text\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "- alpha: Use when alpha. Prefer for alpha tasks.") {
		t.Fatalf("missing alpha listing, stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "- beta: Use when beta. Prefer for beta tasks.") {
		t.Fatalf("missing beta listing, stdout:\n%s", stdout)
	}
}

func TestLsEmpty(t *testing.T) {
	root := t.TempDir()
	assertNoError(t, os.MkdirAll(filepath.Join(root, "skills"), 0o755))

	stdout, stderr, code := runCLI(t, "--root", root, "ls")
	if code != 0 {
		t.Fatalf("ls empty should succeed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("expected [] JSON for empty ls, got:\n%s", stdout)
	}

	stdout, stderr, code = runCLI(t, "--root", root, "ls", "--text")
	if code != 0 {
		t.Fatalf("ls --text empty should succeed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected no output for empty ls --text, got:\n%s", stdout)
	}
}

func TestDoctorReportsMissingAndReadyState(t *testing.T) {
	root := t.TempDir()

	stdout, stderr, code := runCLI(t, "--root", root, "doctor")
	if code == 0 {
		t.Fatalf("doctor should fail before init\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	report := decodeJSONMap(t, stdout)
	if report["ok"] != false {
		t.Fatalf("doctor ok=%v, want false", report["ok"])
	}

	_, _, code = runCLI(t, "--root", root, "init")
	if code != 0 {
		t.Fatal("init failed")
	}
	_, _, code = runCLI(t, "--root", root, "init", "--project", "-y")
	if code != 0 {
		t.Fatal("init --project failed")
	}

	stdout, stderr, code = runCLI(t, "--root", root, "doctor")
	if code != 0 {
		t.Fatalf("doctor should pass after init\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	report = decodeJSONMap(t, stdout)
	if report["ok"] != true {
		t.Fatalf("doctor ok=%v, want true", report["ok"])
	}
}

func TestInitProjectFlagOverrides(t *testing.T) {
	root := t.TempDir()

	stdout, stderr, code := runCLI(t, "--root", root, "init", "--project", "-y",
		"--target", "claude",
		"--no-auto-update",
		"--default-version", "branch:main",
		"--no-commit-targets",
	)
	if code != 0 {
		t.Fatalf("init --project with flags failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	yamlPath := filepath.Join(root, ".auto", "skills", "skills.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read skills.yaml: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "auto_update: false") {
		t.Fatalf("expected auto_update: false in skills.yaml, got:\n%s", content)
	}
	if !strings.Contains(content, "commit_targets: false") {
		t.Fatalf("expected commit_targets: false in skills.yaml, got:\n%s", content)
	}
	if !strings.Contains(content, "version: branch:main") {
		t.Fatalf("expected version: branch:main in skills.yaml, got:\n%s", content)
	}
	if strings.Contains(content, "agents") {
		t.Fatalf("expected only claude target, got:\n%s", content)
	}
}

func TestInitProjectConflictingFlags(t *testing.T) {
	root := t.TempDir()

	_, stderr, code := runCLI(t, "--root", root, "init", "--project", "-y",
		"--auto-update", "--no-auto-update")
	if code == 0 {
		t.Fatal("expected error for conflicting --auto-update and --no-auto-update")
	}
	if !strings.Contains(stderr, "cannot be combined") {
		t.Fatalf("expected 'cannot be combined' in stderr, got: %s", stderr)
	}

	_, stderr, code = runCLI(t, "--root", root, "init", "--project", "-y",
		"--commit-targets", "--no-commit-targets")
	if code == 0 {
		t.Fatal("expected error for conflicting --commit-targets and --no-commit-targets")
	}
	if !strings.Contains(stderr, "cannot be combined") {
		t.Fatalf("expected 'cannot be combined' in stderr, got: %s", stderr)
	}
}

func TestInitProjectNonTTYGuard(t *testing.T) {
	fi, err := os.Stdin.Stat()
	if err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		t.Skip("stdin is a TTY — guard only triggers when stdin is a pipe")
	}

	root := t.TempDir()

	_, stderr, code := runCLI(t, "--root", root, "init", "--project")
	if code == 0 {
		t.Fatal("expected error when running init --project without -y in non-TTY")
	}
	if !strings.Contains(stderr, "not a TTY") {
		t.Fatalf("expected 'not a TTY' error, got: %s", stderr)
	}
}

func TestInitGlobalCreatesSettings(t *testing.T) {
	root := t.TempDir()

	stdout, stderr, code := runCLI(t, "--root", root, "init")
	if code != 0 {
		t.Fatalf("init (global) failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout not valid JSON: %v\nraw:\n%s", err, stdout)
	}
	if result["mode"] != "global" {
		t.Fatalf("expected mode=global, got %v", result["mode"])
	}

	globalSection, ok := result["global"].(map[string]any)
	if !ok {
		t.Fatalf("expected global section in result, got: %v", result)
	}
	if globalSection["created"] != true {
		t.Fatalf("expected created=true, got %v", globalSection["created"])
	}

	settingsPath := filepath.Join(root, ".auto", "skills", "settings.json")
	assertExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings not valid JSON: %v\nraw:\n%s", err, data)
	}
	if settings["schemaVersion"] != float64(1) {
		t.Fatalf("expected schemaVersion=1, got %v", settings["schemaVersion"])
	}
}

func TestInitProjectJSONDefault(t *testing.T) {
	root := t.TempDir()

	stdout, _, code := runCLI(t, "--root", root, "init", "--project", "-y")
	if code != 0 {
		t.Fatalf("init --project failed: code=%d", code)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout is not valid JSON (default should be JSON): %v\nraw:\n%s", err, stdout)
	}
	if result["mode"] != "project" {
		t.Fatalf("expected mode=project, got %v", result["mode"])
	}
	if _, ok := result["skills_yaml"]; !ok {
		t.Fatalf("expected skills_yaml key in JSON output, got: %v", result)
	}
	if _, ok := result["lock"]; !ok {
		t.Fatalf("expected lock key in JSON output, got: %v", result)
	}
}

func TestInitProjectTextOutput(t *testing.T) {
	root := t.TempDir()

	stdout, _, code := runCLI(t, "--root", root, "init", "--project", "-y", "--text")
	if code != 0 {
		t.Fatalf("init --project --text failed: code=%d", code)
	}

	if strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Fatalf("expected text output, got JSON:\n%s", stdout)
	}
	if !strings.Contains(stdout, "skills.yaml:") {
		t.Fatalf("expected skills.yaml mention in text output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Created.") {
		t.Fatalf("expected 'Created.' in text output, got:\n%s", stdout)
	}
}

func TestAgentsCommandRemoved(t *testing.T) {
	root := t.TempDir()

	_, stderr, code := runCLI(t, "--root", root, "agents")
	if code == 0 {
		t.Fatal("expected error — agents command should be removed")
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("expected 'unknown command' error for agents, got: %s", stderr)
	}
}

func TestInitProjectGitignoreEntries(t *testing.T) {
	root := t.TempDir()

	_, _, code := runCLI(t, "--root", root, "init", "--project", "-y")
	if code != 0 {
		t.Fatalf("init --project failed: code=%d", code)
	}

	gitignorePath := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(data), ".auto/skills/.sync-journal") {
		t.Fatalf("expected .auto/skills/.sync-journal in .gitignore, got:\n%s", data)
	}
}

func TestInitProjectNoCommitTargetsGitignore(t *testing.T) {
	root := t.TempDir()

	_, _, code := runCLI(t, "--root", root, "init", "--project", "-y", "--no-commit-targets")
	if code != 0 {
		t.Fatalf("init --project --no-commit-targets failed: code=%d", code)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "CLAUDE.md") {
		t.Fatalf("expected CLAUDE.md in .gitignore when --no-commit-targets, got:\n%s", content)
	}
	if !strings.Contains(content, "AGENTS.md") {
		t.Fatalf("expected AGENTS.md in .gitignore when --no-commit-targets, got:\n%s", content)
	}
}

func TestQuickstartAndDocs(t *testing.T) {
	root := t.TempDir()

	stdout, stderr, code := runCLI(t, "--root", root, "quickstart")
	if code != 0 {
		t.Fatalf("quickstart failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "auto skill init") {
		t.Fatalf("quickstart output missing expected command:\n%s", stdout)
	}

	stdout, stderr, code = runCLI(t, "--root", root, "docs")
	if code != 0 {
		t.Fatalf("docs failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "`doctor`") {
		t.Fatalf("docs output missing doctor command:\n%s", stdout)
	}
}

func runCLI(t *testing.T, args ...string) (stdout string, stderr string, code int) {
	t.Helper()
	var out bytes.Buffer
	var errOut bytes.Buffer

	application := app.New(&out, &errOut)
	rootCmd := cli.NewRootCmd(application)
	rootCmd.SetArgs(args)

	err := rootCmd.ExecuteContext(context.Background())
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

func decodeDiagnostics(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var diags []map[string]any
	if err := json.Unmarshal([]byte(raw), &diags); err != nil {
		t.Fatalf("failed to decode diagnostics JSON: %v\nraw:\n%s", err, raw)
	}
	return diags
}

func decodeJSONMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("failed to decode JSON map: %v\nraw:\n%s", err, raw)
	}
	return out
}

func containsDiagCode(diags []map[string]any, code string) bool {
	for _, d := range diags {
		if d["code"] == code {
			return true
		}
	}
	return false
}

func assertHasDiag(t *testing.T, diags []map[string]any, code string) {
	t.Helper()
	if !containsDiagCode(diags, code) {
		t.Fatalf("expected diagnostic code %q, got %v", code, collectCodes(diags))
	}
}

func collectCodes(diags []map[string]any) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		code, _ := d["code"].(string)
		if code != "" {
			out = append(out, code)
		}
	}
	return out
}

func validSkill(name, description, body string) string {
	return `---
name: ` + name + `
description: "` + description + `"
---
` + body
}

func seedManySkills(t *testing.T, root string, n int, descriptionPad string) {
	t.Helper()
	for i := range n {
		name := fmt.Sprintf("s%03d", i)
		desc := "Use when " + descriptionPad + ". Prefer for token budget tests."
		writeFile(t, filepath.Join(root, "skills", name, "SKILL.md"), validSkill(name, desc, "## Workflow\n\n1. Step.\n"))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	assertNoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	assertNoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
