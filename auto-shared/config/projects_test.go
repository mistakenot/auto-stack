package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpsertProjectReplacesByPath(t *testing.T) {
	cfg := ProjectsConfig{}
	UpsertProject(&cfg, ProjectRef{ID: "alpha", Path: "/repos/alpha"})
	UpsertProject(&cfg, ProjectRef{ID: "alpha-renamed", Path: "/repos/alpha", Remote: "git@x:alpha.git"})

	if len(cfg.Projects) != 1 {
		t.Fatalf("expected 1 project after upsert by same path, got %d", len(cfg.Projects))
	}
	if cfg.Projects[0].ID != "alpha-renamed" || cfg.Projects[0].Remote != "git@x:alpha.git" {
		t.Fatalf("expected entry replaced in place, got %+v", cfg.Projects[0])
	}
}

func TestUpsertProjectAppendsDistinctPaths(t *testing.T) {
	cfg := ProjectsConfig{}
	UpsertProject(&cfg, ProjectRef{ID: "alpha", Path: "/repos/alpha"})
	UpsertProject(&cfg, ProjectRef{ID: "beta", Path: "/repos/beta"})
	if len(cfg.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(cfg.Projects))
	}
}

func TestFindProjectByPathLongestPrefix(t *testing.T) {
	cfg := ProjectsConfig{Projects: []ProjectRef{
		{ID: "outer", Path: "/repos"},
		{ID: "inner", Path: "/repos/alpha"},
	}}
	got := cfg.FindProjectByPath("/repos/alpha/cmd/main")
	if got == nil || got.ID != "inner" {
		t.Fatalf("expected longest-prefix match 'inner', got %+v", got)
	}
	if got := cfg.FindProjectByPath("/elsewhere"); got != nil {
		t.Fatalf("expected no match for unrelated dir, got %+v", got)
	}
	if got := cfg.FindProjectByPath("/repos"); got == nil || got.ID != "outer" {
		t.Fatalf("expected exact root match 'outer', got %+v", got)
	}
}

func TestValidateProjectsCatchesBadIDAndDupes(t *testing.T) {
	cfg := ProjectsConfig{Projects: []ProjectRef{
		{ID: "Bad_ID", Path: "/repos/a"},
		{ID: "dup", Path: "/repos/b"},
		{ID: "dup", Path: "/repos/c"},
	}}
	errs := ValidateProjects(cfg)
	codes := map[string]bool{}
	for _, e := range errs {
		codes[e.Code] = true
	}
	if !codes["invalid_project_id"] {
		t.Errorf("expected invalid_project_id error, got %+v", errs)
	}
	if !codes["duplicate_project_id"] {
		t.Errorf("expected duplicate_project_id error, got %+v", errs)
	}
}

func TestSaveLoadProjectsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	want := ProjectsConfig{Projects: []ProjectRef{
		{ID: "alpha", Path: "/repos/alpha", Remote: "git@x:alpha.git", Name: "Alpha", Tools: []string{"watch", "ui"}},
	}}
	if err := SaveProjects(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadProjects(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Projects) != 1 || got.Projects[0].ID != "alpha" || got.Projects[0].Name != "Alpha" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if len(got.Projects[0].Tools) != 2 {
		t.Fatalf("expected tools preserved, got %+v", got.Projects[0].Tools)
	}
}

func TestUpsertProjectDefaultsRegisteredAt(t *testing.T) {
	cfg := ProjectsConfig{}
	UpsertProject(&cfg, ProjectRef{ID: "alpha", Path: "/repos/alpha"})
	if cfg.Projects[0].RegisteredAt == "" {
		t.Fatalf("expected RegisteredAt to be defaulted when blank")
	}
	// An explicit value must be preserved, not overwritten.
	UpsertProject(&cfg, ProjectRef{ID: "beta", Path: "/repos/beta", RegisteredAt: "2020-01-01T00:00:00Z"})
	if got := cfg.Projects[1].RegisteredAt; got != "2020-01-01T00:00:00Z" {
		t.Fatalf("expected explicit RegisteredAt preserved, got %q", got)
	}
}

func TestSaveProjectsIsAtomicAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	if err := SaveProjects(path, ProjectsConfig{Projects: []ProjectRef{{ID: "alpha", Path: "/a"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "projects.json" {
		t.Fatalf("expected only projects.json (no temp left behind), got %v", names(entries))
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

func TestLoadProjectsLenientUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	// A future field the current schema doesn't know about must not break loading.
	if err := WriteJSONFile(path, map[string]any{
		"projects":  []map[string]any{{"id": "alpha", "path": "/repos/alpha", "futureField": 1}},
		"topLevelX": true,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := LoadProjects(path)
	if err != nil {
		t.Fatalf("expected lenient load to succeed, got %v", err)
	}
	if len(got.Projects) != 1 || got.Projects[0].ID != "alpha" {
		t.Fatalf("expected alpha loaded, got %+v", got)
	}
}
