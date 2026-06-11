package config

import (
	"os"
	"path/filepath"
	"testing"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-watch/internal/model"
)

// TestEnsureGlobalConfigMigratesLegacy verifies that projects registered under
// the pre-registry ~/.auto/watch/settings.json are seeded into the canonical
// ~/.auto/projects.json on first ensure.
func TestEnsureGlobalConfigMigratesLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	legacyPath := filepath.Join(home, ".auto", "watch", "settings.json")
	legacy := model.GlobalConfig{Projects: []model.ProjectRef{
		{ID: "legacy", Path: "/repos/legacy", Remote: "git@x:legacy.git"},
	}}
	if err := sharedconfig.WriteJSONFile(legacyPath, legacy); err != nil {
		t.Fatalf("seed legacy file: %v", err)
	}

	path, cfg, created, err := EnsureGlobalConfig()
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !created {
		t.Fatalf("expected registry to be created on first ensure")
	}
	if want := filepath.Join(home, ".auto", "projects.json"); path != want {
		t.Fatalf("expected registry at %s, got %s", want, path)
	}
	if len(cfg.Projects) != 1 || cfg.Projects[0].ID != "legacy" {
		t.Fatalf("expected legacy project migrated, got %#v", cfg.Projects)
	}

	// The new canonical file must exist on disk with the migrated content.
	onDisk, err := LoadGlobalConfig(path)
	if err != nil {
		t.Fatalf("load migrated registry: %v", err)
	}
	if len(onDisk.Projects) != 1 || onDisk.Projects[0].Remote != "git@x:legacy.git" {
		t.Fatalf("migrated registry not persisted: %#v", onDisk.Projects)
	}

	// The legacy file must be retired so an older binary can't keep writing it.
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Errorf("expected legacy settings.json to be renamed away, stat err = %v", err)
	}
	if _, err := os.Stat(legacyPath + ".migrated"); err != nil {
		t.Errorf("expected legacy file renamed to .migrated, got %v", err)
	}
}

func TestEnsureGlobalConfigNoLegacyStartsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, cfg, created, err := EnsureGlobalConfig()
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !created {
		t.Fatalf("expected created")
	}
	if _, err := os.Stat(filepath.Join(home, ".auto", "projects.json")); err != nil {
		t.Fatalf("registry file should exist: %v", err)
	}
	if len(cfg.Projects) != 0 {
		t.Fatalf("expected empty registry, got %#v", cfg.Projects)
	}
}
