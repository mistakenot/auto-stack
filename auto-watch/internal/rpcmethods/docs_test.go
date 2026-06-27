package rpcmethods

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-shared/rpc/conformance"
)

// seedDocFixture creates a temp project with a docs/ tree for doc.* testing.
// Returns (root dir, cleanup func).
func seedDocFixture(t *testing.T) (string, config.ProjectsConfig) {
	t.Helper()

	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	tasksDir := filepath.Join(docsDir, "tasks", "001-test-task")
	os.MkdirAll(tasksDir, 0o755)

	// A markdown doc.
	os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Hello\n"), 0o644)

	// An HTML planning doc with pd-meta.
	htmlContent := `<!doctype html>
<html><head>
<script type="application/json" id="pd-meta">
{"status":"executing","epic":"001-test","created":"2026-01-01"}
</script>
</head><body>
<pd-doc status="executing">
</pd-doc>
</body></html>`
	os.WriteFile(filepath.Join(tasksDir, "plan.html"), []byte(htmlContent), 0o644)

	// A non-doc file (should be ignored).
	os.WriteFile(filepath.Join(docsDir, "notes.txt"), []byte("ignored"), 0o644)

	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{
			{ID: "test-project", Name: "Test", Path: root},
		},
	}
	return root, reg
}

// setupWithReg creates a test RPC environment with a specific registry.
func setupWithReg(t *testing.T, reg func() config.ProjectsConfig) (*conformance.PeerClient, *Handlers, func()) {
	t.Helper()

	hub := bus.NewHub()
	h := New("test-host", "1.2.3", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, reg)

	sConn, cConn := net.Pipe()
	serverPeer := rpc.NewPeer(sConn)
	h.Register(serverPeer)

	client := conformance.NewPeerClient(cConn)

	ctx, cancel := context.WithCancel(context.Background())
	sErr := make(chan error, 1)
	cErr := make(chan error, 1)
	go func() { sErr <- serverPeer.Serve(ctx) }()
	go func() { cErr <- client.Peer().Serve(ctx) }()

	cleanup := func() {
		cancel()
		<-sErr
		<-cErr
	}
	return client, h, cleanup
}

func callRPC(t *testing.T, client *conformance.PeerClient, method string, params any) (json.RawMessage, *rpc.Error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Call(ctx, method, params)
	if err != nil {
		return nil, &rpc.Error{Message: err.Error()}
	}
	return result, nil
}

// AC-1: doc.list parity
func TestDocList_MdAndHtmlEntries(t *testing.T) {
	root, reg := seedDocFixture(t)
	_ = root
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	result, rpcErr := callRPC(t, client, "doc.list", map[string]string{"project": "test-project"})
	if rpcErr != nil {
		t.Fatalf("doc.list error: %v", rpcErr)
	}

	var entries []docEntry
	if err := json.Unmarshal(result, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should have 2 entries: readme.md and tasks/001-test-task/plan.html
	// notes.txt should be skipped
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}

	mdFound, htmlFound := false, false
	for _, e := range entries {
		if e.Path == "docs/readme.md" {
			mdFound = true
			if e.Type != "markdown" {
				t.Errorf("readme.md type = %q, want markdown", e.Type)
			}
			if e.ID != e.Path {
				t.Errorf("ID = %q, want = Path %q", e.ID, e.Path)
			}
			if e.Meta != nil {
				t.Errorf("markdown file should not have meta")
			}
		}
		if e.Path == "docs/tasks/001-test-task/plan.html" {
			htmlFound = true
			if e.Type != "html" {
				t.Errorf("plan.html type = %q, want html", e.Type)
			}
			if e.Meta == nil {
				t.Fatalf("plan.html should have meta")
			}
			if e.Meta.Status != "executing" {
				t.Errorf("meta.status = %q, want executing", e.Meta.Status)
			}
			if e.Meta.Epic != "001-test" {
				t.Errorf("meta.epic = %q, want 001-test", e.Meta.Epic)
			}
			if e.Meta.ReviewState != "executing" {
				t.Errorf("meta.reviewState = %q, want executing", e.Meta.ReviewState)
			}
		}
	}
	if !mdFound {
		t.Error("readme.md not found in entries")
	}
	if !htmlFound {
		t.Error("plan.html not found in entries")
	}
}

func TestDocList_EmptyDocsDir(t *testing.T) {
	root := t.TempDir()
	// No docs/ dir at all
	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{{ID: "empty-project", Path: root}},
	}
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	result, rpcErr := callRPC(t, client, "doc.list", map[string]string{"project": "empty-project"})
	if rpcErr != nil {
		t.Fatalf("doc.list error: %v", rpcErr)
	}

	var entries []docEntry
	if err := json.Unmarshal(result, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty list for missing docs/, got %d entries", len(entries))
	}
}

func TestDocList_UnknownProject(t *testing.T) {
	reg := config.ProjectsConfig{Projects: []config.ProjectRef{}}
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	_, rpcErr := callRPC(t, client, "doc.list", map[string]string{"project": "no-such-project"})
	if rpcErr == nil {
		t.Fatal("expected error for unknown project")
	}
}

func TestDocList_WithWorktree(t *testing.T) {
	root, reg := seedDocFixture(t)
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	result, rpcErr := callRPC(t, client, "doc.list", map[string]string{"worktree": root})
	if rpcErr != nil {
		t.Fatalf("doc.list with worktree: %v", rpcErr)
	}

	var entries []docEntry
	json.Unmarshal(result, &entries)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries via worktree, got %d", len(entries))
	}
}

// AC-2: doc.get parity
func TestDocGet_MarkdownFile(t *testing.T) {
	_, reg := seedDocFixture(t)
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	result, rpcErr := callRPC(t, client, "doc.get", map[string]string{
		"project": "test-project",
		"path":    "docs/readme.md",
	})
	if rpcErr != nil {
		t.Fatalf("doc.get error: %v", rpcErr)
	}

	var got map[string]string
	json.Unmarshal(result, &got)
	if got["path"] != "docs/readme.md" {
		t.Errorf("path = %q, want docs/readme.md", got["path"])
	}
	if got["markdown"] != "# Hello\n" {
		t.Errorf("markdown = %q, want '# Hello\\n'", got["markdown"])
	}
}

func TestDocGet_RejectsHTML(t *testing.T) {
	_, reg := seedDocFixture(t)
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	_, rpcErr := callRPC(t, client, "doc.get", map[string]string{
		"project": "test-project",
		"path":    "docs/tasks/001-test-task/plan.html",
	})
	if rpcErr == nil {
		t.Fatal("doc.get should reject HTML paths")
	}
}

func TestDocGet_EmptyPath(t *testing.T) {
	_, reg := seedDocFixture(t)
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	_, rpcErr := callRPC(t, client, "doc.get", map[string]string{
		"project": "test-project",
		"path":    "",
	})
	if rpcErr == nil {
		t.Fatal("doc.get should reject empty path")
	}
}

func TestDocGet_NotFound(t *testing.T) {
	_, reg := seedDocFixture(t)
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	_, rpcErr := callRPC(t, client, "doc.get", map[string]string{
		"project": "test-project",
		"path":    "docs/nonexistent.md",
	})
	if rpcErr == nil {
		t.Fatal("doc.get should return error for missing file")
	}
}

// AC-3: doc.raw parity
func TestDocRaw_HTMLFile(t *testing.T) {
	_, reg := seedDocFixture(t)
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	result, rpcErr := callRPC(t, client, "doc.raw", map[string]string{
		"project": "test-project",
		"path":    "docs/tasks/001-test-task/plan.html",
	})
	if rpcErr != nil {
		t.Fatalf("doc.raw error: %v", rpcErr)
	}

	var got DocRawResult
	json.Unmarshal(result, &got)

	if got.Path != "docs/tasks/001-test-task/plan.html" {
		t.Errorf("path = %q", got.Path)
	}
	if got.ContentType != "text/html; charset=utf-8" {
		t.Errorf("contentType = %q", got.ContentType)
	}

	decoded, err := base64.StdEncoding.DecodeString(got.ContentBase64)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if len(decoded) == 0 {
		t.Error("decoded bytes are empty")
	}
	// Verify the bytes match the file on disk
	expected, _ := os.ReadFile(filepath.Join(reg.Projects[0].Path, "docs/tasks/001-test-task/plan.html"))
	if !bytes.Equal(decoded, expected) {
		t.Errorf("decoded bytes don't match file on disk")
	}
}

func TestDocRaw_RejectsMarkdown(t *testing.T) {
	_, reg := seedDocFixture(t)
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	_, rpcErr := callRPC(t, client, "doc.raw", map[string]string{
		"project": "test-project",
		"path":    "docs/readme.md",
	})
	if rpcErr == nil {
		t.Fatal("doc.raw should reject markdown paths")
	}
}

// Traversal guard corpus — shared across doc.get and doc.raw
func TestDocTraversalGuards(t *testing.T) {
	_, reg := seedDocFixture(t)
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	traversalCases := []struct {
		name   string
		path   string
		method string
	}{
		{"dotdot prefix via doc.get", "../etc/passwd", "doc.get"},
		{"dotdot embedded via doc.get", "docs/../../x.md", "doc.get"},
		{"absolute path via doc.get", "/etc/passwd", "doc.get"},
		{"outside docs via doc.get", "notdocs/x.md", "doc.get"},
		{"wrong ext (txt) via doc.get", "docs/x.txt", "doc.get"},
		{"html via doc.get", "docs/a.html", "doc.get"},

		{"dotdot prefix via doc.raw", "../etc/passwd", "doc.raw"},
		{"dotdot embedded via doc.raw", "docs/../../x.html", "doc.raw"},
		{"absolute path via doc.raw", "/etc/passwd", "doc.raw"},
		{"outside docs via doc.raw", "notdocs/x.html", "doc.raw"},
		{"wrong ext (txt) via doc.raw", "docs/x.txt", "doc.raw"},
		{"md via doc.raw", "docs/a.md", "doc.raw"},

		{"empty path via doc.get", "", "doc.get"},
		{"empty path via doc.raw", "", "doc.raw"},
	}

	for _, tc := range traversalCases {
		t.Run(tc.name, func(t *testing.T) {
			_, rpcErr := callRPC(t, client, tc.method, map[string]string{
				"project": "test-project",
				"path":    tc.path,
			})
			if rpcErr == nil {
				t.Errorf("expected rejection for %s path %q via %s", tc.name, tc.path, tc.method)
			}
		})
	}
}

func TestDocRaw_NotFound(t *testing.T) {
	_, reg := seedDocFixture(t)
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	_, rpcErr := callRPC(t, client, "doc.raw", map[string]string{
		"project": "test-project",
		"path":    "docs/nonexistent.html",
	})
	if rpcErr == nil {
		t.Fatal("doc.raw should return error for missing file")
	}
}
