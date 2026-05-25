package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAstGrepNotFound(t *testing.T) {
	// Set PATH to an empty directory so ast-grep cannot be found.
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)

	// Create a project dir with tsconfig.json so language detection passes.
	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "tsconfig.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newCodeGraphCmd()
	err := runCodeGraph(cmd, projDir, "json", "typescript")
	if err == nil {
		t.Fatal("expected error when ast-grep is not found, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "ast-grep not found") {
		t.Errorf("error message should mention 'ast-grep not found', got: %s", errMsg)
	}
	// Check for remediation hint.
	if !strings.Contains(errMsg, "npm") && !strings.Contains(errMsg, "brew") {
		t.Errorf("error message should contain remediation hint (npm or brew), got: %s", errMsg)
	}
}

func TestLanguageAutoDetection(t *testing.T) {
	// Create a temp dir with tsconfig.json.
	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "tsconfig.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	lang, err := detectLanguage(projDir)
	if err != nil {
		t.Fatalf("detectLanguage failed: %v", err)
	}
	if lang != "typescript" {
		t.Errorf("expected language %q, got %q", "typescript", lang)
	}
}

func TestLanguageAutoDetectionNoConfig(t *testing.T) {
	// Create a temp dir without tsconfig.json.
	projDir := t.TempDir()

	_, err := detectLanguage(projDir)
	if err == nil {
		t.Fatal("expected error when no config file found, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "could not detect project language") {
		t.Errorf("error message should mention detection failure, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "--lang=typescript") {
		t.Errorf("error message should contain remediation hint (--lang=typescript), got: %s", errMsg)
	}
}

func TestLanguageOverride(t *testing.T) {
	// Create a temp dir WITHOUT tsconfig.json.
	projDir := t.TempDir()

	// When --lang is specified, detectLanguage should not be called.
	// We verify this by checking that runCodeGraph with lang="typescript"
	// on a dir without tsconfig.json does NOT fail with a language detection error.
	// Instead it should get past detection and succeed or fail for other reasons
	// (like scanning an empty directory).

	// We need ast-grep installed for this test.
	if _, lookErr := findInPath("ast-grep"); lookErr != nil {
		t.Skip("ast-grep not installed, skipping language override test")
	}

	// Create a proper cobra command to pass to runCodeGraph.
	cmd := newCodeGraphCmd()
	cmd.SetOut(&bytes.Buffer{}) // suppress output during test
	cmd.SetArgs([]string{projDir})

	err := runCodeGraph(cmd, projDir, "json", "typescript")
	// The error should NOT be about language detection.
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "could not detect project language") {
			t.Errorf("--lang override should bypass language detection, but got: %s", errMsg)
		}
		// Other errors (e.g. no files found, scanning errors) are acceptable.
	}
}

func findInPath(name string) (string, error) {
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}
