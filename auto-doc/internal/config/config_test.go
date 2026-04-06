package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Load from nonexistent file should return defaults
	cfg, err := Load("nonexistent-file.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DocsDir != "docs" {
		t.Errorf("DocsDir = %q, want %q", cfg.DocsDir, "docs")
	}
	if len(cfg.AgentFiles) != 2 || cfg.AgentFiles[0] != "AGENTS.md" || cfg.AgentFiles[1] != "CLAUDE.md" {
		t.Errorf("AgentFiles = %v, want [AGENTS.md CLAUDE.md]", cfg.AgentFiles)
	}
	if cfg.Parallelism != 4 {
		t.Errorf("Parallelism = %d, want 4", cfg.Parallelism)
	}
}

func TestLoadFullConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "docs.json")
	content := `{"docsDir": "documentation", "agentFiles": ["README.md"], "parallelism": 8}`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DocsDir != "documentation" {
		t.Errorf("DocsDir = %q, want %q", cfg.DocsDir, "documentation")
	}
	if len(cfg.AgentFiles) != 1 || cfg.AgentFiles[0] != "README.md" {
		t.Errorf("AgentFiles = %v, want [README.md]", cfg.AgentFiles)
	}
	if cfg.Parallelism != 8 {
		t.Errorf("Parallelism = %d, want 8", cfg.Parallelism)
	}
}

func TestLoadIgnoresUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "docs.json")
	content := `{"docsDir": "docs", "unknownKey": true, "extra": 42}`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DocsDir != "docs" {
		t.Errorf("DocsDir = %q, want %q", cfg.DocsDir, "docs")
	}
}

func TestLoadWithIgnores(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "docs.json")
	content := `{"ignores": ["draft-*.md", "internal/**"]}`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Ignores) != 2 {
		t.Fatalf("Ignores length = %d, want 2", len(cfg.Ignores))
	}
	if cfg.Ignores[0] != "draft-*.md" {
		t.Errorf("Ignores[0] = %q, want %q", cfg.Ignores[0], "draft-*.md")
	}
}

func TestLoadDefaultIgnoresEmpty(t *testing.T) {
	cfg, err := Load("nonexistent-file.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ignores != nil {
		t.Errorf("Ignores = %v, want nil", cfg.Ignores)
	}
}

func TestLoadDefaultFilename(t *testing.T) {
	if DefaultConfigFile != "settings.json" {
		t.Errorf("DefaultConfigFile = %q, want %q", DefaultConfigFile, "settings.json")
	}
	if DefaultConfigDir != ".auto/doc" {
		t.Errorf("DefaultConfigDir = %q, want %q", DefaultConfigDir, ".auto/doc")
	}
}

func TestUnionStringsEmpty(t *testing.T) {
	result := unionStrings(nil, nil)
	if result != nil {
		t.Errorf("union(nil, nil) = %v, want nil", result)
	}
}

func TestUnionStringsBothSet(t *testing.T) {
	a := []string{"draft-*.md", "internal/**"}
	b := []string{"internal/**", "vendor/**"}
	result := unionStrings(a, b)
	if len(result) != 3 {
		t.Fatalf("union length = %d, want 3, got %v", len(result), result)
	}
	// Order: a items first, then new items from b
	expected := []string{"draft-*.md", "internal/**", "vendor/**"}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("result[%d] = %q, want %q", i, result[i], v)
		}
	}
}

func TestUnionStringsEmptyGlobal(t *testing.T) {
	a := []string{"draft-*.md"}
	result := unionStrings(a, nil)
	if len(result) != 1 || result[0] != "draft-*.md" {
		t.Errorf("union with nil b = %v, want [draft-*.md]", result)
	}
}

func TestUnionStringsEmptyLocal(t *testing.T) {
	b := []string{"vendor/**"}
	result := unionStrings(nil, b)
	if len(result) != 1 || result[0] != "vendor/**" {
		t.Errorf("union with nil a = %v, want [vendor/**]", result)
	}
}

func TestHostConfigPathUsesHome(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	got, err := HostConfigPath()
	if err != nil {
		t.Fatalf("HostConfigPath() error = %v", err)
	}
	want := filepath.Join(tmpHome, ".auto", "host.json")
	if got != want {
		t.Fatalf("HostConfigPath() = %q, want %q", got, want)
	}
}

func TestLoadHostReadsHostID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "host.json")
	if err := os.WriteFile(path, []byte(`{"hostId":"docs-host"}`), 0o644); err != nil {
		t.Fatalf("write host config: %v", err)
	}

	cfg, err := LoadHost(path)
	if err != nil {
		t.Fatalf("LoadHost() error = %v", err)
	}
	if cfg.HostID != "docs-host" {
		t.Fatalf("HostID = %q, want %q", cfg.HostID, "docs-host")
	}
}

func TestLoadHostRequiresHostID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "host.json")
	if err := os.WriteFile(path, []byte(`{"host":"legacy"}`), 0o644); err != nil {
		t.Fatalf("write host config: %v", err)
	}

	if _, err := LoadHost(path); err == nil {
		t.Fatal("LoadHost() error = nil, want error")
	}
}
