package server_test

import (
	"testing"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
)

// testRegistry returns a ProjectsConfig fixture with one registered project.
func testRegistry() config.ProjectsConfig {
	return config.ProjectsConfig{
		Projects: []config.ProjectRef{{
			ID:     "test-proj",
			Path:   "/fake/project",
			Remote: "https://github.com/test/repo.git",
		}},
	}
}

// validToolPostEvent builds a valid agent.tool.post bus.Event targeting a doc
// path in the test-proj project.
func validToolPostEvent(t *testing.T, relPath string) bus.Event {
	t.Helper()
	tp := bus.ToolPost{
		Tool:  "Edit",
		Event: "PostToolUse",
		Paths: []bus.PathRef{{Rel: relPath, Abs: "/fake/project/" + relPath}},
	}
	ev, err := bus.NewEvent("agent.tool.post", "auto/hooks/claude", tp)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	ev.Project = "test-proj"
	ev.Worktree = "/fake/project"
	ev.Branch = "main"
	return ev
}
