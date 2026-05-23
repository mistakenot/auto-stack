package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/config"
	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestInitCreatesConfigAndDocsDir(t *testing.T) {
	ws := testutil.NewWorkspace(t)

	var buf bytes.Buffer
	if err := InitProject(&buf, ws.Dir); err != nil {
		t.Fatal(err)
	}

	// .auto/doc/ dir should exist
	if _, err := os.Stat(ws.Path(".auto/doc")); err != nil {
		t.Errorf(".auto/doc/ not created: %v", err)
	}

	// .auto/doc/settings.json should exist
	if _, err := os.Stat(ws.Path(".auto/doc/settings.json")); err != nil {
		t.Errorf(".auto/doc/settings.json not created: %v", err)
	}

	// .auto/doc/.gitignore should exist
	if _, err := os.Stat(ws.Path(".auto/doc/.gitignore")); err != nil {
		t.Errorf(".auto/doc/.gitignore not created: %v", err)
	}

	// docs/ dir should exist
	if _, err := os.Stat(ws.Path("docs")); err != nil {
		t.Errorf("docs/ not created: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Created .auto/doc/settings.json") {
		t.Error("missing creation message")
	}
}

func TestInitDoesNotOverwriteExistingConfig(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteConfig("custom-docs")

	var buf bytes.Buffer
	if err := InitProject(&buf, ws.Dir); err != nil {
		t.Fatal(err)
	}

	// Read config back - should still have custom-docs
	data, err := os.ReadFile(ws.Path(".auto/doc/settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "custom-docs") {
		t.Error("existing config was overwritten")
	}

	output := buf.String()
	if !strings.Contains(output, "already exists") {
		t.Error("missing already-exists message")
	}
}

func TestInitShowsTreeOutput(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("test.md", "Test", "A test doc", "# Test")

	var buf bytes.Buffer
	if err := InitProject(&buf, ws.Dir); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "test.md") {
		t.Error("tree output not shown")
	}
}

func TestInitAdvisesFixWhenStale(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("test.md", "Test", "A test doc", "# Test") // hash is empty, so stale

	var buf bytes.Buffer
	if err := InitProject(&buf, ws.Dir); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "autodoc fix") {
		t.Error("missing fix advice")
	}
}

func TestInitCreatesGitignore(t *testing.T) {
	ws := testutil.NewWorkspace(t)

	var buf bytes.Buffer
	if err := InitProject(&buf, ws.Dir); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(ws.Dir, ".auto", "doc", ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "index/") {
		t.Error(".gitignore missing index/ ignore")
	}
}

func TestInitDoesNotOverwriteExistingGitignore(t *testing.T) {
	ws := testutil.NewWorkspace(t)

	// Create .auto/doc/.gitignore with custom content
	ws.WriteFile(".auto/doc/.gitignore", "custom content\n")

	var buf bytes.Buffer
	if err := InitProject(&buf, ws.Dir); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(ws.Dir, ".auto", "doc", ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}

	if string(data) != "custom content\n" {
		t.Error("existing .gitignore was overwritten")
	}
}

func TestInitGlobalCreatesConfigAndHost(t *testing.T) {
	// Override HOME to a temp dir so we don't touch real home
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	var buf bytes.Buffer
	if err := InitGlobal(&buf); err != nil {
		t.Fatal(err)
	}

	// ~/.auto/doc/settings.json should exist
	cfgPath := filepath.Join(tmpHome, ".auto", "doc", "settings.json")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("global settings.json not created: %v", err)
	}

	// ~/.auto/host.json should exist
	hostPath := filepath.Join(tmpHome, ".auto", "host.json")
	if _, err := os.Stat(hostPath); err != nil {
		t.Errorf("host.json not created: %v", err)
	}
	hostCfg, err := config.LoadHost(hostPath)
	if err != nil {
		t.Fatalf("LoadHost(%q): %v", hostPath, err)
	}
	if hostCfg.HostID == "" {
		t.Fatal("created host config has empty hostId")
	}

	output := buf.String()
	if !strings.Contains(output, "Created ~/.auto/doc/settings.json") {
		t.Error("missing global config creation message")
	}
	if !strings.Contains(output, "Created ~/.auto/host.json") {
		t.Error("missing host.json creation message")
	}
}

func TestInitGlobalIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	var buf bytes.Buffer
	if err := InitGlobal(&buf); err != nil {
		t.Fatal(err)
	}

	// Run again
	buf.Reset()
	if err := InitGlobal(&buf); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "already exists") {
		t.Error("second run should report already exists")
	}
}
