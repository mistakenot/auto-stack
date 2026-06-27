package rpcmethods

import (
	"encoding/json"
	"testing"

	"github.com/mistakenot/auto-shared/config"
)

// AC-4: project.list parity + host stamp + credential stripping
func TestProjectList_Shape(t *testing.T) {
	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{
			{ID: "proj-a", Name: "Project A", Path: "/home/user/proj-a", Remote: "git@github.com:user/proj-a.git"},
			{ID: "proj-b", Name: "Project B", Path: "/home/user/proj-b", Remote: "https://token:x-oauth@github.com/user/proj-b.git"},
		},
	}
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	result, rpcErr := callRPC(t, client, "project.list", nil)
	if rpcErr != nil {
		t.Fatalf("project.list error: %v", rpcErr)
	}

	var entries []projectEntry
	if err := json.Unmarshal(result, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Check first project
	if entries[0].ID != "proj-a" {
		t.Errorf("entries[0].ID = %q", entries[0].ID)
	}
	if entries[0].Name != "Project A" {
		t.Errorf("entries[0].Name = %q", entries[0].Name)
	}
	if entries[0].Path != "/home/user/proj-a" {
		t.Errorf("entries[0].Path = %q", entries[0].Path)
	}

	// Host should be stamped on every entry
	for i, e := range entries {
		if e.Host != "test-host" {
			t.Errorf("entries[%d].Host = %q, want test-host", i, e.Host)
		}
	}

	// Credential stripping: the second project has a credentialed remote
	if entries[1].Remote == "https://token:x-oauth@github.com/user/proj-b.git" {
		t.Errorf("credentialed remote was not stripped: %q", entries[1].Remote)
	}
}

func TestProjectList_EmptyRegistry(t *testing.T) {
	reg := config.ProjectsConfig{Projects: []config.ProjectRef{}}
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	result, rpcErr := callRPC(t, client, "project.list", nil)
	if rpcErr != nil {
		t.Fatalf("project.list error: %v", rpcErr)
	}

	var entries []projectEntry
	json.Unmarshal(result, &entries)
	if len(entries) != 0 {
		t.Errorf("expected empty array, got %d entries", len(entries))
	}
	// Should be [] not null
	if string(result) == "null" {
		t.Error("result is null, should be []")
	}
}
