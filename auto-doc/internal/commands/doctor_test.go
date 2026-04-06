package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestDoctorAllPass(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Create global config
	globalDir := filepath.Join(tmpHome, ".auto", "doc")
	os.MkdirAll(globalDir, 0o755)
	os.WriteFile(filepath.Join(globalDir, "settings.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(tmpHome, ".auto", "host.json"), []byte(`{"hostId":"test"}`), 0o644)

	// Create project
	ws := testutil.NewWorkspace(t)
	ws.WriteConfig("docs")
	os.MkdirAll(ws.Path("docs"), 0o755)
	os.MkdirAll(ws.Path(".auto/doc/index"), 0o755)

	var buf bytes.Buffer
	allPass, err := Doctor(&buf, ws.Dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if !allPass {
		t.Errorf("expected all pass, got failures:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "✓") {
		t.Error("expected pass markers in output")
	}
}

func TestDoctorFreshDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	ws := testutil.NewWorkspace(t)

	var buf bytes.Buffer
	allPass, err := Doctor(&buf, ws.Dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if allPass {
		t.Error("expected failures on fresh dir")
	}
	output := buf.String()
	if !strings.Contains(output, "✗") {
		t.Error("expected fail markers")
	}
	if !strings.Contains(output, "autodoc init") {
		t.Error("expected remediation hint for autodoc init")
	}
}

func TestDoctorJSON(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	ws := testutil.NewWorkspace(t)

	var buf bytes.Buffer
	_, err := Doctor(&buf, ws.Dir, true)
	if err != nil {
		t.Fatal(err)
	}

	var checks []DoctorCheck
	if err := json.Unmarshal(buf.Bytes(), &checks); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(checks) == 0 {
		t.Error("expected checks in output")
	}

	// All should be fail on fresh dir
	for _, c := range checks {
		if c.Status != "fail" {
			t.Errorf("check %q = %q, want fail", c.Check, c.Status)
		}
	}
}

func TestDoctorInvalidProjectConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Create global config
	globalDir := filepath.Join(tmpHome, ".auto", "doc")
	os.MkdirAll(globalDir, 0o755)
	os.WriteFile(filepath.Join(globalDir, "settings.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(tmpHome, ".auto", "host.json"), []byte(`{"hostId":"test"}`), 0o644)

	ws := testutil.NewWorkspace(t)
	// Write invalid JSON as project config
	os.MkdirAll(ws.Path(".auto/doc"), 0o755)
	os.WriteFile(ws.Path(".auto/doc/settings.json"), []byte("{invalid"), 0o644)
	os.MkdirAll(ws.Path("docs"), 0o755)

	var buf bytes.Buffer
	allPass, err := Doctor(&buf, ws.Dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if allPass {
		t.Error("expected failure for invalid JSON")
	}
	if !strings.Contains(buf.String(), "not valid JSON") {
		t.Errorf("expected JSON validation error, got:\n%s", buf.String())
	}
}

func TestDoctorInvalidHostConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Create global config
	globalDir := filepath.Join(tmpHome, ".auto", "doc")
	os.MkdirAll(globalDir, 0o755)
	os.WriteFile(filepath.Join(globalDir, "settings.json"), []byte("{}"), 0o644)
	// Host config exists but does not include required hostId
	os.WriteFile(filepath.Join(tmpHome, ".auto", "host.json"), []byte(`{"host":"legacy"}`), 0o644)

	// Create project
	ws := testutil.NewWorkspace(t)
	ws.WriteConfig("docs")
	os.MkdirAll(ws.Path("docs"), 0o755)
	os.MkdirAll(ws.Path(".auto/doc/index"), 0o755)

	var buf bytes.Buffer
	allPass, err := Doctor(&buf, ws.Dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if allPass {
		t.Error("expected failure for invalid host config")
	}
	if !strings.Contains(buf.String(), "hostId is required") {
		t.Errorf("expected hostId validation error, got:\n%s", buf.String())
	}
}
