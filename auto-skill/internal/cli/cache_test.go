package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

func TestLoadReferencedIDsMultiProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Set up two projects with distinct lock files referencing different repos.
	projectA := filepath.Join(home, "project-a")
	projectB := filepath.Join(home, "project-b")

	lockA := skill.Lock{
		Version: 1,
		Skills: map[string]skill.LockEntry{
			"skill-a": {
				Source:  "github",
				URL:     "https://github.com/org/repo-a",
				Commit:  "abc123",
				Subpath: "skills/a",
				State:   "resolved",
			},
		},
	}
	lockB := skill.Lock{
		Version: 1,
		Skills: map[string]skill.LockEntry{
			"skill-b": {
				Source:  "github",
				URL:     "https://github.com/org/repo-b",
				Commit:  "def456",
				Subpath: "skills/b",
				State:   "resolved",
			},
		},
	}

	writeLock(t, projectA, lockA)
	writeLock(t, projectB, lockB)

	// Register both projects in ~/.auto/projects.json.
	projectsDir := filepath.Join(home, ".auto")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := map[string]any{
		"projects": []map[string]any{
			{"id": "project-a", "path": projectA},
			{"id": "project-b", "path": projectB},
		},
	}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectsDir, "projects.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Call loadReferencedIDs from project A's perspective.
	env := skill.Env{Root: projectA}
	ids := loadReferencedIDs(env)

	// Both repos should be referenced, not just the current project's.
	wantA := "github.com/org/repo-a"
	wantB := "github.com/org/repo-b"
	if !ids[wantA] {
		t.Errorf("expected %q to be referenced (from project A lock), got: %v", wantA, ids)
	}
	if !ids[wantB] {
		t.Errorf("expected %q to be referenced (from project B lock), got: %v", wantB, ids)
	}
}

func writeLock(t *testing.T, projectRoot string, lock skill.Lock) {
	t.Helper()
	lockDir := filepath.Join(projectRoot, ".auto", "skills")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, "lock.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
