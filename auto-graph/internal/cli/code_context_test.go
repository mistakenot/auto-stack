package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupContextTestProject creates a minimal TypeScript project fixture in a
// temp directory with tsconfig.json and some .ts files for testing the
// code context command. Since ast-grep is required for graph building,
// tests that need the full pipeline will skip if ast-grep is not available.
func setupContextTestProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// tsconfig.json for language detection.
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create src directory.
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// app.ts - seed file.
	if err := os.WriteFile(filepath.Join(srcDir, "app.ts"), []byte(`import { helper } from './helper';
export function main() { return helper(); }
`), 0644); err != nil {
		t.Fatal(err)
	}

	// helper.ts - dependency.
	if err := os.WriteFile(filepath.Join(srcDir, "helper.ts"), []byte(`export function helper() { return 'hello'; }
`), 0644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestCodeContextRequiredTokenLimit(t *testing.T) {
	// When --token-limit is not provided, cobra should report the required flag error.
	rootCmd := newCodeCmd()
	var stderr bytes.Buffer
	rootCmd.SetErr(&stderr)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"context", "/tmp", "--file", "src/app.ts"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --token-limit is missing")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "token-limit") {
		t.Errorf("error should mention 'token-limit', got: %s", errMsg)
	}
}

func TestCodeContextRequiredSeedFiles(t *testing.T) {
	// --file is required; without it, the command should fail.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newCodeContextCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := runCodeContext(cmd, dir, 10000, nil, "markdown", "typescript", true, false)
	if err == nil {
		t.Fatal("expected error when no --file is provided")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "--file") {
		t.Errorf("error should mention '--file', got: %s", errMsg)
	}
}

func TestCodeContextInvalidFormat(t *testing.T) {
	cmd := newCodeContextCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := runCodeContext(cmd, "/tmp", 10000, []string{"src/app.ts"}, "xml", "typescript", true, false)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "unknown format") {
		t.Errorf("error should mention 'unknown format', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "xml") {
		t.Errorf("error should echo the invalid format value, got: %s", errMsg)
	}
}

func TestCodeContextDefaultMarkdownOutput(t *testing.T) {
	if _, err := findInPath("ast-grep"); err != nil {
		t.Skip("ast-grep not installed, skipping integration test")
	}

	dir := setupContextTestProject(t)

	cmd := newCodeContextCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := runCodeContext(cmd, dir, 50000, []string{"src/app.ts"}, "markdown", "", true, false)
	if err != nil {
		t.Fatalf("runCodeContext failed: %v (stderr: %s)", err, stderr.String())
	}

	output := stdout.String()

	// Should start with Context Pack header.
	if !strings.HasPrefix(output, "# Context Pack\n") {
		t.Errorf("default output should start with '# Context Pack\\n', got: %q", output[:min(50, len(output))])
	}

	// Should contain budget line.
	if !strings.Contains(output, "Budget:") {
		t.Error("default markdown output should contain 'Budget:' line")
	}

	// Should contain seeds.
	if !strings.Contains(output, "src/app.ts") {
		t.Error("default markdown output should contain seed file path")
	}

	// Should contain a Files section.
	if !strings.Contains(output, "## Files") {
		t.Error("default markdown output should contain '## Files' section")
	}
}

func TestCodeContextJSONOutput(t *testing.T) {
	if _, err := findInPath("ast-grep"); err != nil {
		t.Skip("ast-grep not installed, skipping integration test")
	}

	dir := setupContextTestProject(t)

	cmd := newCodeContextCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := runCodeContext(cmd, dir, 50000, []string{"src/app.ts"}, "json", "", true, false)
	if err != nil {
		t.Fatalf("runCodeContext failed: %v (stderr: %s)", err, stderr.String())
	}

	output := stdout.String()

	// Must be parseable JSON.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("JSON output is not parseable: %v\nOutput: %s", err, output)
	}

	// Check required fields.
	if _, ok := parsed["project_root"]; !ok {
		t.Error("JSON output should have 'project_root' field")
	}
	if _, ok := parsed["token_limit"]; !ok {
		t.Error("JSON output should have 'token_limit' field")
	}
	if _, ok := parsed["estimated_tokens"]; !ok {
		t.Error("JSON output should have 'estimated_tokens' field")
	}

	// estimated_tokens must be <= token_limit.
	tokenLimit := parsed["token_limit"].(float64)
	estimatedTokens := parsed["estimated_tokens"].(float64)
	if estimatedTokens > tokenLimit {
		t.Errorf("estimated_tokens (%v) should be <= token_limit (%v)", estimatedTokens, tokenLimit)
	}
}

func TestCodeContextValidationErrorsToStderr(t *testing.T) {
	if _, err := findInPath("ast-grep"); err != nil {
		t.Skip("ast-grep not installed, skipping integration test")
	}

	dir := setupContextTestProject(t)

	cmd := newCodeContextCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	// Use a file that doesn't exist in the project.
	err := runCodeContext(cmd, dir, 50000, []string{"nonexistent.ts"}, "markdown", "", true, false)
	if err == nil {
		t.Fatal("expected error for invalid seed file")
	}

	// The error should be an ExitError.
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != 1 {
		t.Errorf("expected exit code 1, got %d", exitErr.Code)
	}

	// Stderr should contain validation error details.
	stderrOutput := stderr.String()
	if !strings.Contains(stderrOutput, "missing_file") && !strings.Contains(stderrOutput, "not_in_graph") {
		t.Errorf("stderr should contain validation error code, got: %s", stderrOutput)
	}
}

func TestCodeContextLangOverride(t *testing.T) {
	if _, err := findInPath("ast-grep"); err != nil {
		t.Skip("ast-grep not installed, skipping integration test")
	}

	// Create a directory WITHOUT tsconfig.json, but with .ts files.
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "app.ts"), []byte(`export function main() { return 'hello'; }
`), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newCodeContextCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	// Without --lang, this should fail because there's no tsconfig.json.
	err := runCodeContext(cmd, dir, 50000, []string{"src/app.ts"}, "markdown", "", true, false)
	if err == nil {
		t.Fatal("expected error without tsconfig.json and no --lang override")
	}
	if !strings.Contains(err.Error(), "could not detect project language") {
		t.Errorf("expected language detection error, got: %v", err)
	}

	// With --lang=typescript, it should succeed.
	stdout.Reset()
	stderr.Reset()
	err = runCodeContext(cmd, dir, 50000, []string{"src/app.ts"}, "markdown", "typescript", true, false)
	if err != nil {
		t.Fatalf("expected success with --lang=typescript, got: %v (stderr: %s)", err, stderr.String())
	}

	output := stdout.String()
	if !strings.HasPrefix(output, "# Context Pack\n") {
		t.Errorf("output should start with '# Context Pack', got: %q", output[:min(50, len(output))])
	}
}

func TestCodeContextNotADirectory(t *testing.T) {
	cmd := newCodeContextCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := runCodeContext(cmd, "/nonexistent/path", 10000, []string{"src/app.ts"}, "markdown", "typescript", true, false)
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error should mention 'not a directory', got: %v", err)
	}
}
