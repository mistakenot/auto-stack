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

	"github.com/mistakenot/auto-skill/internal/app"
	"github.com/mistakenot/auto-skill/internal/cli"
	"github.com/mistakenot/auto-skill/internal/skill"
	"gopkg.in/yaml.v3"
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

// --- cache + trust CLI integration tests (Phase 5/6) ---

func TestTrustListEmpty(t *testing.T) {
	root := t.TempDir()
	stdout, _, code := runCLI(t, "--root", root, "trust", "list")
	if code != 0 {
		t.Fatalf("trust list failed: code=%d, stdout=%q", code, stdout)
	}
	var tf map[string]any
	if err := json.Unmarshal([]byte(stdout), &tf); err != nil {
		t.Fatalf("expected JSON, got: %s", stdout)
	}
	eps, _ := tf["endpoints"].([]any)
	if len(eps) != 0 {
		t.Fatalf("expected empty endpoints, got %v", eps)
	}
}

func TestTrustAddAndList(t *testing.T) {
	root := t.TempDir()

	_, stderr, code := runCLI(t, "--root", root, "trust", "add", "https://github.com")
	if code != 0 {
		t.Fatalf("trust add failed: code=%d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "approved") {
		t.Fatalf("expected 'approved' message, got: %s", stderr)
	}

	stdout, _, code := runCLI(t, "--root", root, "trust", "list")
	if code != 0 {
		t.Fatal("trust list failed")
	}
	var tf map[string]any
	if err := json.Unmarshal([]byte(stdout), &tf); err != nil {
		t.Fatalf("json: %v, raw: %s", err, stdout)
	}
	eps, _ := tf["endpoints"].([]any)
	if len(eps) != 1 {
		t.Fatalf("expected 1 endpoint, got %v", eps)
	}
	if eps[0] != "https://github.com:443" {
		t.Fatalf("expected https://github.com:443, got %v", eps[0])
	}
}

func TestTrustAddIdempotent(t *testing.T) {
	root := t.TempDir()
	runCLI(t, "--root", root, "trust", "add", "https://github.com")
	runCLI(t, "--root", root, "trust", "add", "https://github.com")

	stdout, _, _ := runCLI(t, "--root", root, "trust", "list")
	var tf map[string]any
	json.Unmarshal([]byte(stdout), &tf)
	eps, _ := tf["endpoints"].([]any)
	if len(eps) != 1 {
		t.Fatalf("expected 1 endpoint after duplicate add, got %d", len(eps))
	}
}

func TestTrustRemove(t *testing.T) {
	root := t.TempDir()
	runCLI(t, "--root", root, "trust", "add", "https://github.com")
	_, stderr, code := runCLI(t, "--root", root, "trust", "remove", "https://github.com")
	if code != 0 {
		t.Fatalf("trust remove failed: code=%d, stderr=%q", code, stderr)
	}

	stdout, _, _ := runCLI(t, "--root", root, "trust", "list")
	var tf map[string]any
	json.Unmarshal([]byte(stdout), &tf)
	eps, _ := tf["endpoints"].([]any)
	if len(eps) != 0 {
		t.Fatalf("expected 0 endpoints after remove, got %d", len(eps))
	}
}

func TestTrustRemoveAbsentIsNoOp(t *testing.T) {
	root := t.TempDir()
	_, _, code := runCLI(t, "--root", root, "trust", "remove", "https://github.com")
	if code != 0 {
		t.Fatal("removing absent endpoint should succeed")
	}
}

func TestTrustListText(t *testing.T) {
	root := t.TempDir()
	runCLI(t, "--root", root, "trust", "add", "https://github.com")
	stdout, _, code := runCLI(t, "--root", root, "trust", "list", "--text")
	if code != 0 {
		t.Fatal("trust list --text failed")
	}
	if !strings.Contains(stdout, "https://github.com:443") {
		t.Fatalf("expected endpoint in text output, got: %s", stdout)
	}
}

func TestTrustAddRejectsCredentials(t *testing.T) {
	root := t.TempDir()
	_, stderr, code := runCLI(t, "--root", root, "trust", "add", "https://user:pass@github.com")
	if code == 0 {
		t.Fatal("expected error for credential-bearing endpoint")
	}
	if !strings.Contains(stderr, "credential") {
		t.Fatalf("expected credential error, got: %s", stderr)
	}
}

func TestTrustAddRejectsUnsupportedScheme(t *testing.T) {
	root := t.TempDir()
	_, _, code := runCLI(t, "--root", root, "trust", "add", "ftp://host/repo")
	if code == 0 {
		t.Fatal("expected error for unsupported scheme")
	}
}

func TestCacheListEmpty(t *testing.T) {
	root := t.TempDir()
	stdout, _, code := runCLI(t, "--root", root, "cache", "list")
	if code != 0 {
		t.Fatalf("cache list failed: code=%d", code)
	}
	if strings.TrimSpace(stdout) != "null" && strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("expected null or [] for empty cache, got: %s", stdout)
	}
}

func TestCachePathNormalizesURL(t *testing.T) {
	root := t.TempDir()

	stdout1, _, code := runCLI(t, "--root", root, "cache", "path", "https://github.com/acme/skills")
	if code != 0 {
		t.Fatalf("cache path (https) failed: code=%d", code)
	}
	stdout2, _, code := runCLI(t, "--root", root, "cache", "path", "github.com/acme/skills")
	if code != 0 {
		t.Fatalf("cache path (bare) failed: code=%d", code)
	}
	if strings.TrimSpace(stdout1) != strings.TrimSpace(stdout2) {
		t.Errorf("expected same path for https and bare forms\nhttps: %s\nbare:  %s", stdout1, stdout2)
	}
}

func TestCachePathRejectsCredentials(t *testing.T) {
	root := t.TempDir()
	_, _, code := runCLI(t, "--root", root, "cache", "path", "https://user:pass@github.com/acme/skills")
	if code == 0 {
		t.Fatal("expected error for credential-bearing URL")
	}
}

func TestCachePruneDryRunEmpty(t *testing.T) {
	root := t.TempDir()
	stdout, _, code := runCLI(t, "--root", root, "cache", "prune", "--dry-run", "--max-age", "1d")
	if code != 0 {
		t.Fatalf("cache prune --dry-run failed: code=%d", code)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("expected JSON, got: %s", stdout)
	}
}

// --- add CLI integration tests ---

// makeAddFixture creates a temp git repo with SKILL.md files.
// skills maps subpath (e.g. "skills/my-skill") to SKILL.md content.
// Returns the repo directory path.
func makeAddFixture(t *testing.T, skills map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s\n%s", strings.Join(args, " "), err, out)
		}
	}

	git("init")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "test")

	for subpath, content := range skills {
		full := filepath.Join(dir, subpath, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("add", "-A")
	git("commit", "-m", "initial")

	return dir
}

func addSkillMD(name, desc string) string {
	return "---\nname: " + name + "\ndescription: \"" + desc + "\"\n---\n\n## Workflow\n\n1. Do the thing.\n"
}

func TestAddLocalHappyPathJSON(t *testing.T) {
	fixture := makeAddFixture(t, map[string]string{
		"skills/my-skill": addSkillMD("my-skill", "Use when testing add CLI."),
	})
	root := t.TempDir()

	stdout, stderr, code := runCLI(t, "--root", root, "add", fixture)
	if code != 0 {
		t.Fatalf("add failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nraw:\n%s", err, stdout)
	}

	added, ok := result["added"].([]any)
	if !ok || len(added) != 1 {
		t.Fatalf("expected 1 added skill in JSON, got: %v", result)
	}
	first := added[0].(map[string]any)
	if first["name"] != "my-skill" {
		t.Errorf("name = %v, want my-skill", first["name"])
	}

	// Verify lock.json and skills.yaml were written.
	lockPath := filepath.Join(root, ".auto", "skills", "lock.json")
	assertExists(t, lockPath)
	yamlPath := filepath.Join(root, ".auto", "skills", "skills.yaml")
	assertExists(t, yamlPath)
}

func TestAddLocalListJSON(t *testing.T) {
	fixture := makeAddFixture(t, map[string]string{
		"skills/alpha": addSkillMD("alpha", "Use when testing alpha list."),
		"skills/beta":  addSkillMD("beta", "Use when testing beta list."),
	})
	root := t.TempDir()

	stdout, stderr, code := runCLI(t, "--root", root, "add", fixture, "--list")
	if code != 0 {
		t.Fatalf("add --list failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nraw:\n%s", err, stdout)
	}

	listed, ok := result["listed"].([]any)
	if !ok || len(listed) != 2 {
		t.Fatalf("expected 2 listed skills, got: %v", result)
	}

	// Verify nothing was written.
	lockPath := filepath.Join(root, ".auto", "skills", "lock.json")
	if _, err := os.Stat(lockPath); err == nil {
		t.Error("lock.json should not exist in list mode")
	}
	yamlPath := filepath.Join(root, ".auto", "skills", "skills.yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		t.Error("skills.yaml should not exist in list mode")
	}
}

func TestAddLocalTextOutput(t *testing.T) {
	fixture := makeAddFixture(t, map[string]string{
		"skills/my-skill": addSkillMD("my-skill", "Use when testing text output."),
	})
	root := t.TempDir()

	stdout, stderr, code := runCLI(t, "--root", root, "add", fixture, "--text")
	if code != 0 {
		t.Fatalf("add --text failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	if !strings.Contains(stdout, "Added my-skill") {
		t.Fatalf("expected 'Added my-skill' in text output, got:\n%s", stdout)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Fatalf("expected text output, got JSON:\n%s", stdout)
	}
}

func TestAddAsConflictFlagValidation(t *testing.T) {
	fixture := makeAddFixture(t, map[string]string{
		"skills/a": addSkillMD("a", "Use when testing a."),
		"skills/b": addSkillMD("b", "Use when testing b."),
	})
	root := t.TempDir()

	_, stderr, code := runCLI(t, "--root", root, "add", fixture, "--as", "my-name", "--skill", "a", "--skill", "b")
	if code == 0 {
		t.Fatal("expected error when --as combined with multiple --skill")
	}
	if !strings.Contains(stderr, "--as") {
		t.Fatalf("expected --as mentioned in error, got: %s", stderr)
	}
}

func TestAddSkillNotFound(t *testing.T) {
	fixture := makeAddFixture(t, map[string]string{
		"skills/alpha": addSkillMD("alpha", "Use when testing skill not found."),
	})
	root := t.TempDir()

	_, stderr, code := runCLI(t, "--root", root, "add", fixture, "--skill", "nonexistent")
	if code == 0 {
		t.Fatal("expected error for nonexistent skill filter")
	}
	if !strings.Contains(stderr, "alpha") {
		t.Fatalf("expected available skill 'alpha' listed in error, got: %s", stderr)
	}
}

func TestAddNoArgs(t *testing.T) {
	root := t.TempDir()

	_, stderr, code := runCLI(t, "--root", root, "add")
	if code == 0 {
		t.Fatal("expected error when no positional arg provided")
	}
	if !strings.Contains(stderr, "accepts 1 arg") {
		t.Fatalf("expected cobra arg validation error, got: %s", stderr)
	}
}

func TestAddTrustFailClosedRemote(t *testing.T) {
	root := t.TempDir()

	_, stderr, code := runCLI(t, "--root", root, "add", "github.com/acme/nonexistent-skills-repo")
	if code == 0 {
		t.Fatal("expected error for unapproved remote source")
	}
	if !strings.Contains(stderr, "not approved") && !strings.Contains(stderr, "approved") {
		t.Fatalf("expected trust-related error, got: %s", stderr)
	}
}

// --- sync CLI integration tests (Phase 6) ---

// syncFixture is a real on-disk git repo reachable over file://, mirroring the
// sync package's fixture but driven through the CLI.
type syncFixture struct {
	t   *testing.T
	dir string
	url string
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	f := &syncFixture{t: t, dir: dir, url: "file://" + dir}
	f.git("init")
	f.git("config", "user.email", "test@test.com")
	f.git("config", "user.name", "test")
	return f
}

func (f *syncFixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitSkill writes skills/<name>/SKILL.md, commits, and returns the new HEAD.
func (f *syncFixture) commitSkill(name, body string) string {
	f.t.Helper()
	full := filepath.Join(f.dir, "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		f.t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: Use when testing sync render.\n---\n\n" + body + "\n"
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
	f.git("add", "-A")
	f.git("commit", "-m", "commit "+name)
	return f.git("rev-parse", "HEAD")
}

func (f *syncFixture) remove() {
	f.t.Helper()
	if err := os.RemoveAll(f.dir); err != nil {
		f.t.Fatal(err)
	}
}

func skillsConfigDir(root string) string {
	return filepath.Join(root, ".auto", "skills")
}

func writeSyncLock(t *testing.T, root string, entries map[string]skill.LockEntry) {
	t.Helper()
	assertNoError(t, os.MkdirAll(skillsConfigDir(root), 0o755))
	data, err := skill.EncodeJSON(&skill.Lock{Version: 1, Skills: entries})
	assertNoError(t, err)
	assertNoError(t, os.WriteFile(filepath.Join(skillsConfigDir(root), "lock.json"), data, 0o644))
}

func writeSyncYAML(t *testing.T, root string, cfg *skill.SkillsYAML) {
	t.Helper()
	assertNoError(t, os.MkdirAll(skillsConfigDir(root), 0o755))
	data, err := yaml.Marshal(cfg)
	assertNoError(t, err)
	assertNoError(t, os.WriteFile(filepath.Join(skillsConfigDir(root), "skills.yaml"), data, 0o644))
}

func syncLockEntry(url, name, spec, commit string) skill.LockEntry {
	return skill.LockEntry{
		Source:      url,
		URL:         url,
		VersionSpec: spec,
		Ref:         commit,
		Commit:      commit,
		Subpath:     "skills/" + name,
		State:       "resolved",
	}
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	assertNoError(t, err)
	return data
}

// TestSyncAuthoredRendersIntoTargets proves the native render: authored skills
// land in every default target, stdout is a strictly parseable JSON payload,
// and the run needs no Node/npx at all.
func TestSyncAuthoredRendersIntoTargets(t *testing.T) {
	root := t.TempDir()
	writeSyncYAML(t, root, &skill.SkillsYAML{})
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"),
		validSkill("alpha", "Use when testing sync render.", "## Workflow\n\n1. Step.\n"))

	stdout, stderr, code := runCLI(t, "--root", root, "sync")
	if code != 0 {
		t.Fatalf("sync failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	// stdout must be parseable JSON only (payload, no diagnostics).
	res := decodeJSONMap(t, stdout)
	if res["mode"] != "sync" {
		t.Fatalf("mode = %v, want sync", res["mode"])
	}
	assertExists(t, filepath.Join(root, ".claude", "skills", "alpha", "SKILL.md"))
	assertExists(t, filepath.Join(root, ".agents", "skills", "alpha", "SKILL.md"))
}

// TestSyncCheckStaleExitsNonZero: --check is an offline dry-run that writes
// nothing and exits non-zero when a target is stale.
func TestSyncCheckStaleExitsNonZero(t *testing.T) {
	root := t.TempDir()
	writeSyncYAML(t, root, &skill.SkillsYAML{})
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"),
		validSkill("alpha", "Use when testing sync check.", "## Workflow\n\n1. Step.\n"))

	stdout, _, code := runCLI(t, "--root", root, "sync", "--check")
	if code == 0 {
		t.Fatalf("sync --check with a stale target must exit non-zero\nstdout:\n%s", stdout)
	}
	res := decodeJSONMap(t, stdout)
	if res["mode"] != "check" {
		t.Fatalf("mode = %v, want check", res["mode"])
	}
	stale, ok := res["stale"].([]any)
	if !ok || len(stale) == 0 {
		t.Fatalf("expected a stale entry, got: %v", res["stale"])
	}
	// --check writes nothing.
	if _, err := os.Stat(filepath.Join(skillsConfigDir(root), "manifest.json")); !os.IsNotExist(err) {
		t.Errorf("--check must not write the manifest (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "alpha")); !os.IsNotExist(err) {
		t.Error("--check must not write the target tree")
	}
}

// TestSyncTextMode prints a human summary instead of JSON.
func TestSyncTextMode(t *testing.T) {
	root := t.TempDir()
	writeSyncYAML(t, root, &skill.SkillsYAML{})
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"),
		validSkill("alpha", "Use when testing sync text.", "## Workflow\n\n1. Step.\n"))

	stdout, stderr, code := runCLI(t, "--root", root, "sync", "--text")
	if code != 0 {
		t.Fatalf("sync --text failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Fatalf("expected human text, got JSON:\n%s", stdout)
	}
	if !strings.Contains(stdout, "mode: sync") {
		t.Fatalf("expected 'mode: sync' in text output, got:\n%s", stdout)
	}
}

// TestSyncJobsFlag exercises the bounded worker-pool flag.
func TestSyncJobsFlag(t *testing.T) {
	root := t.TempDir()
	writeSyncYAML(t, root, &skill.SkillsYAML{})
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"),
		validSkill("alpha", "Use when testing jobs flag.", "## Workflow\n\n1. Step.\n"))

	_, stderr, code := runCLI(t, "--root", root, "sync", "--jobs", "2")
	if code != 0 {
		t.Fatalf("sync --jobs 2 failed: code=%d stderr:\n%s", code, stderr)
	}
}

// TestSyncBudgetWarnsExitsZero: an oversized SKILL.md warns on stderr but the
// run still succeeds (advisory budget; lint is the gate). stdout stays JSON.
func TestSyncBudgetWarnsExitsZero(t *testing.T) {
	root := t.TempDir()
	writeSyncYAML(t, root, &skill.SkillsYAML{})
	big := strings.Repeat("word ", 5000) // ~6k tokens > 4000 advisory budget
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"),
		validSkill("alpha", "Use when testing the advisory budget warning.", big))

	stdout, stderr, code := runCLI(t, "--root", root, "sync")
	if code != 0 {
		t.Fatalf("budget overflow must exit zero, got code=%d\nstderr:\n%s", code, stderr)
	}
	// stdout remains a clean JSON payload.
	decodeJSONMap(t, stdout)
	if !strings.Contains(stderr, "advisory budget") {
		t.Fatalf("expected an advisory budget warning on stderr, got:\n%s", stderr)
	}
}

// TestSyncLockedReproducesCommit: --locked renders the pinned commit even when
// upstream has moved, and leaves lock.json byte-identical.
func TestSyncLockedReproducesCommit(t *testing.T) {
	f := newSyncFixture(t)
	old := f.commitSkill("alpha", "v1 body")
	f.commitSkill("alpha", "v2 body") // upstream moves on

	root := t.TempDir()
	if _, _, code := runCLI(t, "--root", root, "trust", "add", f.url); code != 0 {
		t.Fatalf("trust add failed: code=%d", code)
	}
	writeSyncLock(t, root, map[string]skill.LockEntry{
		"alpha": syncLockEntry(f.url, "alpha", "latest", old),
	})
	writeSyncYAML(t, root, &skill.SkillsYAML{AutoUpdate: true})
	lockBefore := readBytes(t, filepath.Join(skillsConfigDir(root), "lock.json"))

	stdout, stderr, code := runCLI(t, "--root", root, "sync", "--locked")
	if code != 0 {
		t.Fatalf("sync --locked failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !bytes.Equal(lockBefore, readBytes(t, filepath.Join(skillsConfigDir(root), "lock.json"))) {
		t.Error("--locked must leave lock.json byte-identical")
	}
	installed := readBytes(t, filepath.Join(root, ".claude", "skills", "alpha", "SKILL.md"))
	if !strings.Contains(string(installed), "v1 body") {
		t.Errorf("--locked must render the pinned (old) commit, got:\n%s", installed)
	}
}

// TestSyncTargetImpliesLocked: --target scopes the run and implies --locked, so
// the project-wide lock is never advanced even with auto_update:true.
func TestSyncTargetImpliesLocked(t *testing.T) {
	f := newSyncFixture(t)
	old := f.commitSkill("alpha", "v1 body")
	f.commitSkill("alpha", "v2 body")

	root := t.TempDir()
	if _, _, code := runCLI(t, "--root", root, "trust", "add", f.url); code != 0 {
		t.Fatalf("trust add failed: code=%d", code)
	}
	writeSyncLock(t, root, map[string]skill.LockEntry{
		"alpha": syncLockEntry(f.url, "alpha", "latest", old),
	})
	writeSyncYAML(t, root, &skill.SkillsYAML{AutoUpdate: true})
	lockBefore := readBytes(t, filepath.Join(skillsConfigDir(root), "lock.json"))

	stdout, stderr, code := runCLI(t, "--root", root, "sync", "--target", "alpha")
	if code != 0 {
		t.Fatalf("sync --target failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	res := decodeJSONMap(t, stdout)
	if res["locked"] != true {
		t.Errorf("--target must imply locked in the result, got locked=%v", res["locked"])
	}
	if !bytes.Equal(lockBefore, readBytes(t, filepath.Join(skillsConfigDir(root), "lock.json"))) {
		t.Error("--target run must not advance lock.json")
	}
}

// TestSyncRepoFailureExitsNonZero: an unfetchable pinned commit (upstream gone)
// surfaces as a per-repo error and exits non-zero.
func TestSyncRepoFailureExitsNonZero(t *testing.T) {
	f := newSyncFixture(t)
	sha := f.commitSkill("alpha", "v1 body")

	root := t.TempDir()
	if _, _, code := runCLI(t, "--root", root, "trust", "add", f.url); code != 0 {
		t.Fatalf("trust add failed: code=%d", code)
	}
	writeSyncLock(t, root, map[string]skill.LockEntry{
		"alpha": syncLockEntry(f.url, "alpha", "latest", sha),
	})
	writeSyncYAML(t, root, &skill.SkillsYAML{})
	f.remove() // upstream disappears; the pinned commit is not cached

	stdout, stderr, code := runCLI(t, "--root", root, "sync", "--locked")
	if code == 0 {
		t.Fatalf("sync against a vanished repo must exit non-zero\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if strings.TrimSpace(stderr) == "" {
		t.Error("expected a repo-failure diagnostic on stderr")
	}
}

// TestSyncSourceHasNoNpxShellOut is a source-level guard: the npx exec and the
// os/exec import are gone from sync.go (the rewrite is native end-to-end).
func TestSyncSourceHasNoNpxShellOut(t *testing.T) {
	src, err := os.ReadFile("sync.go")
	if err != nil {
		t.Fatalf("read sync.go: %v", err)
	}
	if strings.Contains(string(src), "os/exec") {
		t.Error("sync.go must not import os/exec (npx shell-out deleted)")
	}
	if strings.Contains(string(src), "npx") {
		t.Error("sync.go must not reference npx")
	}
}

// TestSyncLockedEditRewritesManifestNotLock: a locked sync after an authored
// edit rewrites manifest.json but leaves lock.json byte-identical, and the
// installed tree reflects the edit. No Node/npx involved.
func TestSyncLockedEditRewritesManifestNotLock(t *testing.T) {
	root := t.TempDir()
	writeSyncYAML(t, root, &skill.SkillsYAML{})
	writeSyncLock(t, root, map[string]skill.LockEntry{}) // empty lock, byte-stable
	lockBefore := readBytes(t, filepath.Join(skillsConfigDir(root), "lock.json"))

	alpha := filepath.Join(root, "skills", "alpha", "SKILL.md")
	writeFile(t, alpha, validSkill("alpha", "Use when testing locked edits.", "## Workflow\n\n1. v1.\n"))

	if _, stderr, code := runCLI(t, "--root", root, "sync", "--locked"); code != 0 {
		t.Fatalf("first sync --locked failed: code=%d stderr:\n%s", code, stderr)
	}
	manifest1 := readBytes(t, filepath.Join(skillsConfigDir(root), "manifest.json"))

	// Edit the authored skill: rendered output changes.
	writeFile(t, alpha, validSkill("alpha", "Use when testing locked edits.", "## Workflow\n\n1. v2 edited.\n"))
	if _, stderr, code := runCLI(t, "--root", root, "sync", "--locked"); code != 0 {
		t.Fatalf("second sync --locked failed: code=%d stderr:\n%s", code, stderr)
	}
	manifest2 := readBytes(t, filepath.Join(skillsConfigDir(root), "manifest.json"))

	if bytes.Equal(manifest1, manifest2) {
		t.Error("manifest.json should change after the authored edit")
	}
	if !bytes.Equal(lockBefore, readBytes(t, filepath.Join(skillsConfigDir(root), "lock.json"))) {
		t.Error("lock.json must be byte-identical across a locked sync")
	}
	installed := readBytes(t, filepath.Join(root, ".claude", "skills", "alpha", "SKILL.md"))
	if !strings.Contains(string(installed), "v2 edited") {
		t.Errorf("installed tree not updated after edit:\n%s", installed)
	}
}
