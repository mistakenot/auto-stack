package inspect

import (
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// seedSources builds a project whose lock records three skills across two repos
// (acme provides two, beta provides one) so dedupe-by-repo can be exercised.
func seedSources(t *testing.T) skill.Env {
	t.Helper()
	root := t.TempDir()
	env := skill.Env{Root: root}
	writeYAML(t, env, &skill.SkillsYAML{Targets: []string{"claude", "agents"}})
	writeLock(t, env, map[string]skill.LockEntry{
		"deploy": {Source: "github.com/acme/skills", URL: "https://github.com/acme/skills", Ref: "main", Commit: "abc123"},
		"build":  {Source: "github.com/acme/skills", URL: "https://github.com/acme/skills", Ref: "main", Commit: "abc123"},
		"lint":   {Source: "github.com/beta/tools", URL: "https://github.com/beta/tools", Ref: "v1", Commit: "def456"},
	})
	return env
}

func TestSourceListDedupesByRepo(t *testing.T) {
	env := seedSources(t)
	sources, err := SourceList(env)
	if err != nil {
		t.Fatalf("SourceList: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("got %d sources, want 2: %+v", len(sources), sources)
	}
	// Sorted by ID: acme before beta.
	if sources[0].ID != "github.com/acme/skills" {
		t.Errorf("sources[0].ID = %q", sources[0].ID)
	}
	if len(sources[0].Skills) != 2 || sources[0].Skills[0] != "build" || sources[0].Skills[1] != "deploy" {
		t.Errorf("acme skills = %v, want [build deploy]", sources[0].Skills)
	}
	if sources[1].Commit != "def456" {
		t.Errorf("beta commit = %q", sources[1].Commit)
	}
}

func TestSourceDescribe(t *testing.T) {
	env := seedSources(t)
	s, err := SourceDescribe(env, "github.com/beta/tools")
	if err != nil {
		t.Fatalf("SourceDescribe: %v", err)
	}
	if s.Ref != "v1" || len(s.Skills) != 1 || s.Skills[0] != "lint" {
		t.Errorf("unexpected source: %+v", s)
	}
}

func TestSourceDescribeUnknown(t *testing.T) {
	env := seedSources(t)
	if _, err := SourceDescribe(env, "github.com/ghost/repo"); err == nil {
		t.Fatal("expected error for unknown source id")
	}
}

func TestSourceListEmptyProject(t *testing.T) {
	env := skill.Env{Root: t.TempDir()}
	sources, err := SourceList(env)
	if err != nil {
		t.Fatalf("SourceList: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("want empty source list, got %+v", sources)
	}
}

func TestTargetListDefaults(t *testing.T) {
	// No skills.yaml → default targets claude, agents.
	env := skill.Env{Root: t.TempDir()}
	targets, err := TargetList(env)
	if err != nil {
		t.Fatalf("TargetList: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2: %+v", len(targets), targets)
	}
	if targets[0].Name != "agents" || targets[1].Name != "claude" {
		t.Errorf("targets = %+v, want sorted [agents claude]", targets)
	}
	if targets[1].Path != filepath.Join(env.Root, ".claude", "skills") {
		t.Errorf("claude path = %q", targets[1].Path)
	}
	if targets[0].ManagedCount != 0 {
		t.Errorf("managed count = %d, want 0 (no manifest)", targets[0].ManagedCount)
	}
}

func TestTargetListManagedCount(t *testing.T) {
	env := seedProject(t)
	targets, err := TargetList(env)
	if err != nil {
		t.Fatalf("TargetList: %v", err)
	}
	for _, target := range targets {
		if target.ManagedCount != 1 {
			t.Errorf("%s managed count = %d, want 1", target.Name, target.ManagedCount)
		}
	}
}
