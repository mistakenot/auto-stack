package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFile is a small helper that fails the test if the file can't be read.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestSessionExportDefault(t *testing.T) {
	setupIndexedFixtures(t)
	t.Chdir(t.TempDir()) // default --out is relative to cwd

	stdout, stderr, code := runCLI(t, "session", "export", "test-session-1")
	if code != 0 {
		t.Fatalf("export failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// AC-6: stdout stays clean; path + size land on stderr.
	if stdout != "" {
		t.Errorf("stdout should be empty, got:\n%s", stdout)
	}
	wantPath := filepath.Join("docs", "sessions", "test-session-1.html")
	if !strings.Contains(stderr, wantPath) {
		t.Errorf("stderr missing output path %q, got:\n%s", wantPath, stderr)
	}
	if !strings.Contains(stderr, "wrote ") {
		t.Errorf("stderr missing size line, got:\n%s", stderr)
	}

	// AC-1: a single self-contained HTML file at the default path.
	html := readFile(t, wantPath)
	if !strings.Contains(html, "<!DOCTYPE html>") && !strings.Contains(html, "<!doctype html>") {
		t.Error("output is not an HTML document")
	}
	for _, re := range []string{"<script src=", "<script  src=", "<link href=", "src=\"http", "href=\"http"} {
		if strings.Contains(html, re) {
			t.Errorf("output is not self-contained: found %q", re)
		}
	}
	if strings.Contains(html, "http://") || strings.Contains(html, "https://") {
		t.Error("output contains an http(s) URL; should open offline from file://")
	}

	// AC-2: the correlated sub-agent (test-session-2) is nested in the map.
	if !strings.Contains(html, "test-session-2") {
		t.Error("export missing nested sub-agent session test-session-2")
	}
	// AC-3: the renderable bash command is surfaced.
	if !strings.Contains(html, "go build ./...") {
		t.Error("export missing the bash command structural summary")
	}
}

func TestSessionExportOutOverrideAndOverwrite(t *testing.T) {
	setupIndexedFixtures(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "nested", "map.html")

	_, stderr, code := runCLI(t, "session", "export", "test-session-1", "--out", outPath)
	if code != 0 {
		t.Fatalf("export --out failed: code=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected file at --out path: %v", err)
	}
	first := readFile(t, outPath)

	// A second run overwrites silently.
	_, stderr, code = runCLI(t, "session", "export", "test-session-1", "--out", outPath)
	if code != 0 {
		t.Fatalf("second export failed: code=%d stderr=%s", code, stderr)
	}
	if got := readFile(t, outPath); got != first {
		t.Error("re-export produced different output for the same session")
	}
}

func TestSessionExportThinkingFlagsShrink(t *testing.T) {
	setupIndexedFixtures(t)
	dir := t.TempDir()

	export := func(name string, args ...string) int {
		full := append([]string{"session", "export", "test-session-1", "--out", filepath.Join(dir, name)}, args...)
		_, stderr, code := runCLI(t, full...)
		if code != 0 {
			t.Fatalf("export %s failed: code=%d stderr=%s", name, code, stderr)
		}
		return len(readFile(t, filepath.Join(dir, name)))
	}

	defaultSize := export("default.html")
	excludeSize := export("exclude.html", "--exclude-thinking")
	lightSize := export("light.html", "--light")

	// AC-5: --exclude-thinking and --light both produce strictly smaller files.
	if excludeSize >= defaultSize {
		t.Errorf("--exclude-thinking size %d not smaller than default %d", excludeSize, defaultSize)
	}
	if lightSize >= defaultSize {
		t.Errorf("--light size %d not smaller than default %d", lightSize, defaultSize)
	}

	// The default export must embed thinking content; --exclude-thinking drops it.
	full := readFile(t, filepath.Join(dir, "default.html"))
	if !strings.Contains(full, "race condition in the token refresh") {
		t.Error("default export should embed thinking content")
	}
	excluded := readFile(t, filepath.Join(dir, "exclude.html"))
	if strings.Contains(excluded, "race condition in the token refresh") {
		t.Error("--exclude-thinking should drop thinking content")
	}
}

func TestSessionExportFormatJSONReserved(t *testing.T) {
	setupIndexedFixtures(t)
	t.Chdir(t.TempDir())

	stdout, stderr, code := runCLI(t, "session", "export", "test-session-1", "--format", "json")
	if code == 0 {
		t.Fatal("expected non-zero exit for --format json")
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "reserved") {
		t.Errorf("expected 'reserved' in stderr, got:\n%s", stderr)
	}
}

func TestSessionExportUnknownFormat(t *testing.T) {
	setupIndexedFixtures(t)
	t.Chdir(t.TempDir())

	_, stderr, code := runCLI(t, "session", "export", "test-session-1", "--format", "pdf")
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown --format")
	}
	if !strings.Contains(stderr, "invalid --format") {
		t.Errorf("expected format validation error, got:\n%s", stderr)
	}
}

func TestSessionExportUnknownSession(t *testing.T) {
	setupIndexedFixtures(t)
	t.Chdir(t.TempDir())

	stdout, stderr, code := runCLI(t, "session", "export", "no-such-session")
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown session")
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "session not found") {
		t.Errorf("expected 'session not found' remediation, got:\n%s", stderr)
	}
	// No empty file should have been written.
	if _, err := os.Stat(filepath.Join("docs", "sessions", "no-such-session.html")); err == nil {
		t.Error("an output file was written for an unknown session")
	}
}

func TestSessionExportMissingIndex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	// No init/index run — the index database does not exist.
	stdout, stderr, code := runCLI(t, "session", "export", "test-session-1")
	if code == 0 {
		t.Fatal("expected non-zero exit when index is missing")
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "auto search index") {
		t.Errorf("expected index remediation hint, got:\n%s", stderr)
	}
}
