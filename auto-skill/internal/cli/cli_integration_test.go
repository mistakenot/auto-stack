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

	stdout, stderr, code := runCLI(t, "--root", root, "init", "--project")
	if code != 0 {
		t.Fatalf("init --project failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	projectSettings := filepath.Join(root, ".auto", "skill", "settings.json")
	skillsDir := filepath.Join(root, "skills")
	assertExists(t, projectSettings)
	assertExists(t, skillsDir)

	firstSettings, err := os.ReadFile(projectSettings)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	stdout, stderr, code = runCLI(t, "--root", root, "init", "--project")
	if code != 0 {
		t.Fatalf("second init --project failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	secondSettings, err := os.ReadFile(projectSettings)
	if err != nil {
		t.Fatalf("read settings after rerun: %v", err)
	}
	if !bytes.Equal(firstSettings, secondSettings) {
		t.Fatalf("settings changed across idempotent runs\nfirst:\n%s\nsecond:\n%s", firstSettings, secondSettings)
	}
}

func TestCreateHappyWithDirs(t *testing.T) {
	root := t.TempDir()
	_, _, code := runCLI(t, "--root", root, "init", "--project")
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
	_, _, code := runCLI(t, "--root", root, "init", "--project")
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
	_, _, code = runCLI(t, "--root", root, "init", "--project")
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

func TestQuickstartAndDocs(t *testing.T) {
	root := t.TempDir()

	stdout, stderr, code := runCLI(t, "--root", root, "quickstart")
	if code != 0 {
		t.Fatalf("quickstart failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "autoskill init") {
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

func validSkillWithImportantIf(name, description, body, importantIf, importantIfBody string) string {
	var meta strings.Builder
	meta.WriteString("metadata:\n")
	meta.WriteString("    short-description: \"\"\n")
	if importantIf != "" {
		meta.WriteString(fmt.Sprintf("    important_if: \"%s\"\n", importantIf))
	}
	if importantIfBody != "" {
		meta.WriteString(fmt.Sprintf("    important_if_body: \"%s\"\n", importantIfBody))
	}
	return fmt.Sprintf("---\nname: %s\ndescription: \"%s\"\n%s---\n%s", name, description, meta.String(), body)
}

func TestAgentsFencedSectionCreation(t *testing.T) {
	root := t.TempDir()
	// Create a skill with important_if
	writeFile(t, filepath.Join(root, "skills", "ctx-commit", "SKILL.md"),
		validSkillWithImportantIf("ctx-commit",
			"Use when committing code. Trigger when the user asks to commit.",
			"## Workflow\n\n1. Commit.\n",
			"you are committing code or the user asks you to commit",
			""))

	// Run agents command
	stdout, stderr, code := runCLI(t, "--root", root, "agents")
	if code != 0 {
		t.Fatalf("agents failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "CLAUDE.md: updated") {
		t.Fatalf("expected CLAUDE.md updated, got:\n%s", stdout)
	}

	// Verify fenced section in CLAUDE.md
	claudeMD, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(claudeMD)

	if !strings.Contains(content, "<!-- autoskill: start -->") {
		t.Fatalf("missing fenced start marker in CLAUDE.md:\n%s", content)
	}
	if !strings.Contains(content, "<!-- autoskill: end -->") {
		t.Fatalf("missing fenced end marker in CLAUDE.md:\n%s", content)
	}
	if !strings.Contains(content, `<important if="you are committing code or the user asks you to commit">`) {
		t.Fatalf("missing important_if block in CLAUDE.md:\n%s", content)
	}
	if !strings.Contains(content, "Use the **ctx-commit** skill.") {
		t.Fatalf("missing default important_if body in CLAUDE.md:\n%s", content)
	}
}

func TestAgentsFencedSectionWithCustomBody(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "release", "SKILL.md"),
		validSkillWithImportantIf("release",
			"Use when releasing. Trigger when the user asks to release.",
			"## Workflow\n\n1. Release.\n",
			"the user asks to release, tag, or create a new version",
			"Use the **release** skill. Do not manually run git tag."))

	stdout, stderr, code := runCLI(t, "--root", root, "agents")
	if code != 0 {
		t.Fatalf("agents failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	claudeMD, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(claudeMD)

	if !strings.Contains(content, "Use the **release** skill. Do not manually run git tag.") {
		t.Fatalf("missing custom important_if_body in CLAUDE.md:\n%s", content)
	}
}

func TestAgentsFencedSectionReplacement(t *testing.T) {
	root := t.TempDir()

	// First run: create with one skill
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"),
		validSkillWithImportantIf("alpha",
			"Use when alpha. Trigger when alpha.",
			"## Workflow\n\n1. Alpha.\n",
			"you are doing alpha",
			""))

	_, _, code := runCLI(t, "--root", root, "agents")
	if code != 0 {
		t.Fatal("first agents run failed")
	}

	// Verify first state
	data1, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if !strings.Contains(string(data1), "Use the **alpha** skill.") {
		t.Fatalf("first run missing alpha block:\n%s", string(data1))
	}

	// Add a second skill
	writeFile(t, filepath.Join(root, "skills", "beta", "SKILL.md"),
		validSkillWithImportantIf("beta",
			"Use when beta. Trigger when beta.",
			"## Workflow\n\n1. Beta.\n",
			"you are doing beta",
			""))

	stdout, _, code := runCLI(t, "--root", root, "agents")
	if code != 0 {
		t.Fatal("second agents run failed")
	}
	if !strings.Contains(stdout, "CLAUDE.md: updated") {
		t.Fatalf("expected update on second run, got:\n%s", stdout)
	}

	// Verify both skills present
	data2, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	content := string(data2)
	if !strings.Contains(content, "Use the **alpha** skill.") {
		t.Fatalf("missing alpha after update:\n%s", content)
	}
	if !strings.Contains(content, "Use the **beta** skill.") {
		t.Fatalf("missing beta after update:\n%s", content)
	}

	// Verify only one pair of markers
	if strings.Count(content, "<!-- autoskill: start -->") != 1 {
		t.Fatalf("expected exactly one start marker, got:\n%s", content)
	}
	if strings.Count(content, "<!-- autoskill: end -->") != 1 {
		t.Fatalf("expected exactly one end marker, got:\n%s", content)
	}
}

func TestAgentsFencedSectionIdempotent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"),
		validSkillWithImportantIf("alpha",
			"Use when alpha. Trigger when alpha.",
			"## Workflow\n\n1. Alpha.\n",
			"you are doing alpha",
			""))

	_, _, code := runCLI(t, "--root", root, "agents")
	if code != 0 {
		t.Fatal("first agents run failed")
	}

	stdout, _, code := runCLI(t, "--root", root, "agents")
	if code != 0 {
		t.Fatal("second agents run failed")
	}
	if !strings.Contains(stdout, "CLAUDE.md: already present") {
		t.Fatalf("expected idempotent 'already present', got:\n%s", stdout)
	}
}

func TestAgentsMigrationFromUnfenced(t *testing.T) {
	root := t.TempDir()

	// Create a CLAUDE.md with old un-fenced snippet
	existingContent := "# My Project\n\nSome docs here.\n\n**autoskill** — Author and lint reusable agent skills. Run `autoskill quickstart` to learn more.\n\n## More stuff\n"
	writeFile(t, filepath.Join(root, "CLAUDE.md"), existingContent)

	// Create a skill with important_if
	writeFile(t, filepath.Join(root, "skills", "ctx-commit", "SKILL.md"),
		validSkillWithImportantIf("ctx-commit",
			"Use when committing code. Trigger when the user asks to commit.",
			"## Workflow\n\n1. Commit.\n",
			"you are committing code",
			""))

	stdout, stderr, code := runCLI(t, "--root", root, "agents")
	if code != 0 {
		t.Fatalf("agents failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	claudeMD, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	content := string(claudeMD)

	// Should have migrated to fenced section
	if !strings.Contains(content, "<!-- autoskill: start -->") {
		t.Fatalf("missing fenced start marker after migration:\n%s", content)
	}
	if !strings.Contains(content, "<!-- autoskill: end -->") {
		t.Fatalf("missing fenced end marker after migration:\n%s", content)
	}

	// Original content should be preserved
	if !strings.Contains(content, "# My Project") {
		t.Fatalf("original content lost during migration:\n%s", content)
	}
	if !strings.Contains(content, "## More stuff") {
		t.Fatalf("trailing content lost during migration:\n%s", content)
	}

	// Old un-fenced snippet should not exist outside markers
	startIdx := strings.Index(content, "<!-- autoskill: start -->")
	endIdx := strings.Index(content, "<!-- autoskill: end -->")
	before := content[:startIdx]
	after := content[endIdx+len("<!-- autoskill: end -->"):]
	if strings.Contains(before, "autoskill quickstart") || strings.Contains(after, "autoskill quickstart") {
		t.Fatalf("old un-fenced snippet still present outside markers:\n%s", content)
	}
}

func TestAgentsUninstall(t *testing.T) {
	root := t.TempDir()

	// Create fenced section first
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"),
		validSkillWithImportantIf("alpha",
			"Use when alpha. Trigger when alpha.",
			"## Workflow\n\n1. Alpha.\n",
			"you are doing alpha",
			""))

	_, _, code := runCLI(t, "--root", root, "agents")
	if code != 0 {
		t.Fatal("agents create failed")
	}

	// Verify it exists
	data, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if !strings.Contains(string(data), "<!-- autoskill: start -->") {
		t.Fatal("fenced section not created")
	}

	// Uninstall
	stdout, stderr, code := runCLI(t, "--root", root, "agents", "--uninstall")
	if code != 0 {
		t.Fatalf("uninstall failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "CLAUDE.md: uninstalled") {
		t.Fatalf("expected uninstall message, got:\n%s", stdout)
	}

	// Verify section removed
	data, _ = os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	content := string(data)
	if strings.Contains(content, "<!-- autoskill: start -->") {
		t.Fatalf("fenced section still present after uninstall:\n%s", content)
	}
	if strings.Contains(content, "<!-- autoskill: end -->") {
		t.Fatalf("fenced end marker still present after uninstall:\n%s", content)
	}
	if strings.Contains(content, "autoskill") {
		t.Fatalf("autoskill reference still present after uninstall:\n%s", content)
	}
}

func TestAgentsUninstallOldSnippet(t *testing.T) {
	root := t.TempDir()

	// Create a CLAUDE.md with old un-fenced snippet
	existingContent := "# My Project\n\n**autoskill** — Author and lint reusable agent skills. Run `autoskill quickstart` to learn more.\n\n## More stuff\n"
	writeFile(t, filepath.Join(root, "CLAUDE.md"), existingContent)

	stdout, stderr, code := runCLI(t, "--root", root, "agents", "--uninstall")
	if code != 0 {
		t.Fatalf("uninstall failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "CLAUDE.md: uninstalled") {
		t.Fatalf("expected uninstall message, got:\n%s", stdout)
	}

	data, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	content := string(data)
	if strings.Contains(content, "autoskill") {
		t.Fatalf("old snippet still present after uninstall:\n%s", content)
	}
	if !strings.Contains(content, "# My Project") {
		t.Fatalf("original content lost during uninstall:\n%s", content)
	}
}

func TestAgentsNoImportantIfSkills(t *testing.T) {
	root := t.TempDir()
	// Skill without important_if
	writeFile(t, filepath.Join(root, "skills", "plain", "SKILL.md"),
		validSkill("plain", "Use when doing plain things. Prefer for simple tasks.", "## Workflow\n\n1. Do it.\n"))

	stdout, stderr, code := runCLI(t, "--root", root, "agents")
	if code != 0 {
		t.Fatalf("agents failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	data, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	content := string(data)

	// Should have fenced section but no important_if blocks
	if !strings.Contains(content, "<!-- autoskill: start -->") {
		t.Fatalf("missing fenced section:\n%s", content)
	}
	if strings.Contains(content, "<important if=") {
		t.Fatalf("unexpected important_if block for skill without important_if:\n%s", content)
	}
}

func TestLintImportantIfRules(t *testing.T) {
	root := t.TempDir()

	// Skill with empty important_if (whitespace only)
	writeFile(t, filepath.Join(root, "skills", "empty-trigger", "SKILL.md"),
		`---
name: empty-trigger
description: "Use when testing empty triggers. Trigger when testing."
metadata:
    important_if: "   "
---
## Workflow

1. Step.
`)

	stdout, _, code := runCLI(t, "--root", root, "lint")
	// Warnings don't cause non-zero exit
	if code != 0 {
		t.Fatalf("lint should succeed with only warnings, code=%d\nstdout:\n%s", code, stdout)
	}
	diags := decodeDiagnostics(t, stdout)
	assertHasDiag(t, diags, "empty_important_if")
}

func TestLintImportantIfTooLong(t *testing.T) {
	root := t.TempDir()

	longTrigger := strings.Repeat("a", 201)
	writeFile(t, filepath.Join(root, "skills", "long-trigger", "SKILL.md"),
		fmt.Sprintf(`---
name: long-trigger
description: "Use when testing long triggers. Trigger when testing."
metadata:
    important_if: "%s"
---
## Workflow

1. Step.
`, longTrigger))

	stdout, _, code := runCLI(t, "--root", root, "lint")
	if code != 0 {
		t.Fatalf("lint should succeed with only warnings, code=%d\nstdout:\n%s", code, stdout)
	}
	diags := decodeDiagnostics(t, stdout)
	assertHasDiag(t, diags, "important_if_too_long")
}

func TestLintImportantIfBodyWithoutTrigger(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "skills", "orphan-body", "SKILL.md"),
		`---
name: orphan-body
description: "Use when testing orphan body. Trigger when testing."
metadata:
    important_if_body: "Use the **orphan-body** skill."
---
## Workflow

1. Step.
`)

	stdout, _, code := runCLI(t, "--root", root, "lint")
	if code != 0 {
		t.Fatalf("lint should succeed with only warnings, code=%d\nstdout:\n%s", code, stdout)
	}
	diags := decodeDiagnostics(t, stdout)
	assertHasDiag(t, diags, "important_if_body_without_trigger")
}

func TestAgentsPreservesExistingContent(t *testing.T) {
	root := t.TempDir()

	// Create existing CLAUDE.md with content before and after where section will be
	existingContent := "# Project\n\nSome important instructions.\n\n## Section 2\n\nMore content here.\n"
	writeFile(t, filepath.Join(root, "CLAUDE.md"), existingContent)

	writeFile(t, filepath.Join(root, "skills", "test-skill", "SKILL.md"),
		validSkillWithImportantIf("test-skill",
			"Use when testing. Trigger when testing.",
			"## Workflow\n\n1. Test.\n",
			"you are testing",
			""))

	_, _, code := runCLI(t, "--root", root, "agents")
	if code != 0 {
		t.Fatal("agents failed")
	}

	data, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	content := string(data)

	// All original content should be preserved
	if !strings.Contains(content, "# Project") {
		t.Fatalf("lost header:\n%s", content)
	}
	if !strings.Contains(content, "Some important instructions.") {
		t.Fatalf("lost instructions:\n%s", content)
	}
	if !strings.Contains(content, "## Section 2") {
		t.Fatalf("lost section 2:\n%s", content)
	}
	if !strings.Contains(content, "More content here.") {
		t.Fatalf("lost trailing content:\n%s", content)
	}
}
