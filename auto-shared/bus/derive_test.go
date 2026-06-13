package bus

import (
	"encoding/json"
	"testing"

	"github.com/mistakenot/auto-shared/config"
)

func testRegistry() config.ProjectsConfig {
	return config.ProjectsConfig{
		Projects: []config.ProjectRef{
			{ID: "auto-stack", Path: "/repos/auto-stack", Remote: "https://github.com/mistakenot/auto-stack"},
		},
	}
}

func toolPostEvent(project string, paths []PathRef) Event {
	tp := ToolPost{
		Tool:  "Edit",
		Event: "PostToolUse",
		Paths: paths,
		Raw:   json.RawMessage(`{"tool_name":"Edit"}`),
	}
	ev, _ := NewEvent("agent.tool.post", "auto/hooks/claude", tp)
	ev.Project = project
	ev.Remote = "https://github.com/mistakenot/auto-stack"
	ev.Branch = "main"
	ev.Worktree = "/repos/auto-stack"
	ev.Commit = "abc123"
	return ev
}

func TestDeriveDocChangedFromDocsMd(t *testing.T) {
	reg := testRegistry()
	ev := toolPostEvent("auto-stack", []PathRef{
		{Rel: "docs/tasks/021/plan.md", Abs: "/repos/auto-stack/docs/tasks/021/plan.md"},
	})

	derived := DeriveDocChanged(ev, reg)
	if len(derived) != 1 {
		t.Fatalf("expected 1 derived event, got %d", len(derived))
	}
	if derived[0].Type != "doc.changed" {
		t.Errorf("type = %q, want doc.changed", derived[0].Type)
	}

	dc, err := DecodeData[DocChanged](derived[0])
	if err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if dc.Project != "auto-stack" {
		t.Errorf("project = %q, want auto-stack", dc.Project)
	}
	if dc.Path != "docs/tasks/021/plan.md" {
		t.Errorf("path = %q, want docs/tasks/021/plan.md", dc.Path)
	}
	if dc.AbsPath != "/repos/auto-stack/docs/tasks/021/plan.md" {
		t.Errorf("abs_path = %q, want /repos/auto-stack/docs/tasks/021/plan.md", dc.AbsPath)
	}
}

func TestDeriveDocChangedFromDocsHtml(t *testing.T) {
	reg := testRegistry()
	ev := toolPostEvent("auto-stack", []PathRef{
		{Rel: "docs/tasks/021/artifacts/x.html", Abs: "/repos/auto-stack/docs/tasks/021/artifacts/x.html"},
	})

	derived := DeriveDocChanged(ev, reg)
	if len(derived) != 1 {
		t.Fatalf("expected 1 derived event, got %d", len(derived))
	}
	if derived[0].Type != "doc.changed" {
		t.Errorf("type = %q, want doc.changed", derived[0].Type)
	}

	dc, err := DecodeData[DocChanged](derived[0])
	if err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if dc.Path != "docs/tasks/021/artifacts/x.html" {
		t.Errorf("path = %q, want docs/tasks/021/artifacts/x.html", dc.Path)
	}
	if dc.AbsPath != "/repos/auto-stack/docs/tasks/021/artifacts/x.html" {
		t.Errorf("abs_path = %q, want /repos/auto-stack/docs/tasks/021/artifacts/x.html", dc.AbsPath)
	}
}

func TestDeriveDocChangedOutsideDocsHtmlIgnored(t *testing.T) {
	// A non-docs/ .html path must derive nothing.
	reg := testRegistry()
	ev := toolPostEvent("auto-stack", []PathRef{
		{Rel: "web/x.html", Abs: "/repos/auto-stack/web/x.html"},
	})
	if derived := DeriveDocChanged(ev, reg); len(derived) != 0 {
		t.Errorf("expected no derived events for non-docs/ .html, got %d", len(derived))
	}
}

func TestDeriveDocChangedTopLevelDocsDir(t *testing.T) {
	// docs/foo.md (directly under docs/, no subdir) must also match.
	reg := testRegistry()
	ev := toolPostEvent("auto-stack", []PathRef{
		{Rel: "docs/foo.md", Abs: "/repos/auto-stack/docs/foo.md"},
	})

	derived := DeriveDocChanged(ev, reg)
	if len(derived) != 1 {
		t.Fatalf("expected 1 derived event for docs/foo.md, got %d", len(derived))
	}
}

func TestDeriveDocChangedProvenanceCarried(t *testing.T) {
	reg := testRegistry()
	ev := toolPostEvent("auto-stack", []PathRef{
		{Rel: "docs/plan.md", Abs: "/repos/auto-stack/docs/plan.md"},
	})

	derived := DeriveDocChanged(ev, reg)
	if len(derived) != 1 {
		t.Fatalf("expected 1 derived event, got %d", len(derived))
	}
	d := derived[0]
	if d.Project != ev.Project {
		t.Errorf("project = %q, want %q", d.Project, ev.Project)
	}
	if d.Remote != ev.Remote {
		t.Errorf("remote = %q, want %q", d.Remote, ev.Remote)
	}
	if d.Branch != ev.Branch {
		t.Errorf("branch = %q, want %q", d.Branch, ev.Branch)
	}
	if d.Worktree != ev.Worktree {
		t.Errorf("worktree = %q, want %q", d.Worktree, ev.Worktree)
	}
	if d.Commit != ev.Commit {
		t.Errorf("commit = %q, want %q", d.Commit, ev.Commit)
	}
}

func TestDeriveDocChangedNonMdIgnored(t *testing.T) {
	reg := testRegistry()
	ev := toolPostEvent("auto-stack", []PathRef{
		{Rel: "docs/notes.txt", Abs: "/repos/auto-stack/docs/notes.txt"},
	})
	if derived := DeriveDocChanged(ev, reg); len(derived) != 0 {
		t.Errorf("expected no derived events for non-md file, got %d", len(derived))
	}
}

func TestDeriveDocChangedOutsideDocsIgnored(t *testing.T) {
	reg := testRegistry()
	ev := toolPostEvent("auto-stack", []PathRef{
		{Rel: "auto-shared/bus/event.go", Abs: "/repos/auto-stack/auto-shared/bus/event.go"},
	})
	if derived := DeriveDocChanged(ev, reg); len(derived) != 0 {
		t.Errorf("expected no derived events for file outside docs/, got %d", len(derived))
	}
}

func TestDeriveDocChangedRootMdIgnored(t *testing.T) {
	reg := testRegistry()
	ev := toolPostEvent("auto-stack", []PathRef{
		{Rel: "README.md", Abs: "/repos/auto-stack/README.md"},
	})
	if derived := DeriveDocChanged(ev, reg); len(derived) != 0 {
		t.Errorf("expected no derived events for root-level .md, got %d", len(derived))
	}
}

func TestDeriveDocChangedUnregisteredProject(t *testing.T) {
	reg := testRegistry()
	ev := toolPostEvent("unknown-project", []PathRef{
		{Rel: "docs/plan.md", Abs: "/repos/unknown/docs/plan.md"},
	})
	if derived := DeriveDocChanged(ev, reg); len(derived) != 0 {
		t.Errorf("expected no derived events for unregistered project, got %d", len(derived))
	}
}

func TestDeriveDocChangedEmptyProject(t *testing.T) {
	reg := testRegistry()
	ev := toolPostEvent("", []PathRef{
		{Rel: "docs/plan.md", Abs: "/repos/auto-stack/docs/plan.md"},
	})
	if derived := DeriveDocChanged(ev, reg); len(derived) != 0 {
		t.Errorf("expected no derived events for empty project, got %d", len(derived))
	}
}

func TestDeriveDocChangedWrongType(t *testing.T) {
	reg := testRegistry()
	ev, _ := NewEvent("agent.session.start", "test", nil)
	ev.Project = "auto-stack"
	if derived := DeriveDocChanged(ev, reg); len(derived) != 0 {
		t.Errorf("expected no derived events for non-tool-post type, got %d", len(derived))
	}
}

func TestDeriveDocChangedPathTraversalRejected(t *testing.T) {
	reg := testRegistry()
	ev := toolPostEvent("auto-stack", []PathRef{
		{Rel: "../../../etc/passwd", Abs: "/etc/passwd"},
		{Rel: "docs/../../secret.md", Abs: "/repos/secret.md"},
	})
	if derived := DeriveDocChanged(ev, reg); len(derived) != 0 {
		t.Errorf("expected no derived events for path-traversal paths, got %d", len(derived))
	}
}

func TestDeriveDocChangedMultiplePaths(t *testing.T) {
	reg := testRegistry()
	ev := toolPostEvent("auto-stack", []PathRef{
		{Rel: "docs/plan.md", Abs: "/repos/auto-stack/docs/plan.md"},
		{Rel: "auto-shared/bus/event.go", Abs: "/repos/auto-stack/auto-shared/bus/event.go"}, // not docs
		{Rel: "docs/context.md", Abs: "/repos/auto-stack/docs/context.md"},
	})
	derived := DeriveDocChanged(ev, reg)
	if len(derived) != 2 {
		t.Fatalf("expected 2 derived events (2 docs .md paths), got %d", len(derived))
	}
}
