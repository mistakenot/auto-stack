package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-artifact/internal/app"
	"github.com/mistakenot/auto-artifact/internal/cli"
)

// runCLI drives the command tree in-process under whatever $HOME the caller has
// set, returning combined stdout, stderr, and the process exit code. No network
// is involved for the paths exercised here (config load / validation fail
// before any S3 call).
func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cwd, _ := os.Getwd()
	root := cli.NewRootCmd(app.New(&outBuf, &errBuf, cwd))
	root.SetArgs(args)
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)

	err := root.ExecuteContext(context.Background())
	code = 0
	if err != nil {
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
			if exitErr.Err != nil {
				errBuf.WriteString(exitErr.Err.Error())
			}
		} else {
			code = 1
			errBuf.WriteString(err.Error())
		}
	}
	return outBuf.String(), errBuf.String(), code
}

// TestInitWritesSettings: `init` under a temp HOME writes settings.json with all
// required fields (AC-11, no network).
func TestInitWritesSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, stderr, code := runCLI(t, "init",
		"--endpoint", "https://s3.us-east-1.amazonaws.com",
		"--bucket", "test-bucket",
		"--region", "us-east-1",
		"--access-key-id", "AKIATEST",
		"--secret-access-key", "secret",
	)
	if code != 0 {
		t.Fatalf("init exit %d; stderr: %s", code, stderr)
	}

	path := filepath.Join(home, ".auto", "artifact", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	for _, f := range []string{"endpoint", "bucket", "region", "access_key_id", "secret_access_key", "default_retention"} {
		if _, ok := m[f]; !ok {
			t.Errorf("settings.json missing field %q", f)
		}
	}
	if m["default_retention"] != "90d" {
		t.Errorf("default_retention = %v, want 90d", m["default_retention"])
	}
}

// TestDoctorMissingConfig: `doctor` with no settings exits non-zero with a
// diagnostic (AC-12b, no network).
func TestDoctorMissingConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stdout, stderr, code := runCLI(t, "doctor")
	if code == 0 {
		t.Fatalf("doctor with no config should exit non-zero")
	}
	combined := strings.ToLower(stdout + stderr)
	if !strings.Contains(combined, "missing") && !strings.Contains(combined, "invalid") &&
		!strings.Contains(combined, "init") {
		t.Errorf("doctor diagnostic did not mention the problem: %s", combined)
	}
}

// TestUploadWithoutConfig: `upload` with no settings exits non-zero pointing at
// `init` (AC-14, no network — config load fails before any S3 call).
func TestUploadWithoutConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "upload", file)
	if code == 0 {
		t.Fatalf("upload without config should exit non-zero")
	}
	if !strings.Contains(strings.ToLower(stderr), "init") {
		t.Errorf("upload error did not mention init: %s", stderr)
	}
}

// TestUploadRejectsBadRetention: an invalid --retain is rejected after config
// loads but before any S3 call (AC-4 negative, no network).
func TestUploadRejectsBadRetention(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, stderr, code := runCLI(t, "init",
		"--endpoint", "https://s3.us-east-1.amazonaws.com",
		"--bucket", "b", "--region", "us-east-1",
		"--access-key-id", "k", "--secret-access-key", "s",
	); code != 0 {
		t.Fatalf("init failed: %s", stderr)
	}
	file := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "upload", file, "--retain", "60d")
	if code == 0 {
		t.Fatal("upload with --retain 60d should be rejected")
	}
	if !strings.Contains(strings.ToLower(stderr), "retention") {
		t.Errorf("error did not mention retention: %s", stderr)
	}
}

// TestUploadRejectsBadFormat: an invalid --format is rejected up front.
func TestUploadRejectsBadFormat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "upload", file, "--format", "yaml")
	if code == 0 {
		t.Fatal("upload with --format yaml should be rejected")
	}
	if !strings.Contains(strings.ToLower(stderr), "format") {
		t.Errorf("error did not mention format: %s", stderr)
	}
}
