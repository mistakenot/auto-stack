package server_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-ui/internal/server"
)

// setupDocsFixture creates a temp directory with a docs/ subtree for testing.
// Returns the root path and a cleanup function.
func setupDocsFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Create docs structure.
	dirs := []string{
		filepath.Join(root, "docs"),
		filepath.Join(root, "docs", "tasks"),
		filepath.Join(root, "src"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	files := map[string]string{
		filepath.Join(root, "docs", "readme.md"):          "# Readme\nTop-level doc.",
		filepath.Join(root, "docs", "tasks", "plan.md"):   "# Plan\nTask plan.",
		filepath.Join(root, "docs", "tasks", "notes.txt"): "not a markdown file",
		filepath.Join(root, "src", "main.go"):             "package main",
		filepath.Join(root, ".env"):                       "SECRET=hunter2",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	return root
}

// docsTestServer creates an httptest.Server with a registry pointing at the fixture root.
func docsTestServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{{
			ID:   "test-proj",
			Path: root,
		}},
	}
	handler := server.New(newTestFS(), "test", server.WithRegistryProvider(func() config.ProjectsConfig {
		return reg
	}))
	return httptest.NewServer(handler)
}

// rpcCall sends a JSON-RPC request over WS and reads the correlated response.
func rpcCall(ctx context.Context, t *testing.T, c *websocket.Conn, id int, method string, params any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  json.RawMessage(raw),
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal req: %v", err)
	}
	if err := c.Write(ctx, websocket.MessageText, reqBytes); err != nil {
		t.Fatalf("write %s: %v", method, err)
	}

	return readUntil(ctx, t, c, func(m map[string]any) bool {
		mid, ok := m["id"].(float64)
		return ok && mid == float64(id)
	})
}

// TestDocListHappy verifies doc.list returns docs/**/*.md files only.
func TestDocListHappy(t *testing.T) {
	root := setupDocsFixture(t)
	srv := docsTestServer(t, root)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "doc.list", map[string]string{"project": "test-proj"})

	if resp["error"] != nil {
		t.Fatalf("doc.list error: %v", resp["error"])
	}

	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("doc.list result not an array: %T %v", resp["result"], resp["result"])
	}

	// Should have exactly 2 .md files: docs/readme.md and docs/tasks/plan.md.
	if len(result) != 2 {
		t.Fatalf("doc.list returned %d entries, want 2: %v", len(result), result)
	}

	paths := map[string]bool{}
	for _, entry := range result {
		e, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("entry not a map: %v", entry)
		}
		p, _ := e["path"].(string)
		paths[p] = true
	}

	if !paths["docs/readme.md"] {
		t.Errorf("doc.list missing docs/readme.md, got %v", paths)
	}
	if !paths["docs/tasks/plan.md"] {
		t.Errorf("doc.list missing docs/tasks/plan.md, got %v", paths)
	}
}

// TestDocListWorktree verifies doc.list with a worktree param reads from that root.
func TestDocListWorktree(t *testing.T) {
	root := setupDocsFixture(t)

	// Create a second worktree dir that is also under the same project root.
	wt := t.TempDir()
	wtDocs := filepath.Join(wt, "docs")
	if err := os.MkdirAll(wtDocs, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDocs, "wt-doc.md"), []byte("# WT"), 0644); err != nil {
		t.Fatal(err)
	}

	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{
			{ID: "test-proj", Path: root},
			{ID: "test-proj-wt", Path: wt},
		},
	}
	handler := server.New(newTestFS(), "test", server.WithRegistryProvider(func() config.ProjectsConfig {
		return reg
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Request docs from the worktree path.
	resp := rpcCall(ctx, t, c, 1, "doc.list", map[string]string{"worktree": wt})

	if resp["error"] != nil {
		t.Fatalf("doc.list error: %v", resp["error"])
	}

	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("doc.list result not an array: %v", resp["result"])
	}

	if len(result) != 1 {
		t.Fatalf("doc.list returned %d entries, want 1", len(result))
	}

	entry, _ := result[0].(map[string]any)
	if p, _ := entry["path"].(string); p != "docs/wt-doc.md" {
		t.Errorf("path = %q, want docs/wt-doc.md", p)
	}
}

// TestDocGetHappy verifies doc.get returns raw markdown for a valid doc path.
func TestDocGetHappy(t *testing.T) {
	root := setupDocsFixture(t)
	srv := docsTestServer(t, root)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "doc.get", map[string]string{
		"project": "test-proj",
		"path":    "docs/readme.md",
	})

	if resp["error"] != nil {
		t.Fatalf("doc.get error: %v", resp["error"])
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("doc.get result not a map: %v", resp["result"])
	}

	if result["path"] != "docs/readme.md" {
		t.Errorf("path = %v, want docs/readme.md", result["path"])
	}

	md, _ := result["markdown"].(string)
	if md != "# Readme\nTop-level doc." {
		t.Errorf("markdown = %q, want readme content", md)
	}
}

// TestDocGetTraversalRejected verifies that path traversal attempts are rejected.
func TestDocGetTraversalRejected(t *testing.T) {
	root := setupDocsFixture(t)
	srv := docsTestServer(t, root)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	cases := []struct {
		name string
		path string
	}{
		{"parent traversal", "../etc/passwd"},
		{"docs traversal", "docs/../../.env"},
		{"outside docs", "src/main.go"},
		{"non-md in docs", "docs/tasks/notes.txt"},
		{"abs path", "/etc/passwd"},
		{"dotenv", ".env"},
	}

	for i, tc := range cases {
		resp := rpcCall(ctx, t, c, i+10, "doc.get", map[string]string{
			"project": "test-proj",
			"path":    tc.path,
		})

		if resp["error"] == nil {
			t.Errorf("%s: expected error for path %q, got result %v", tc.name, tc.path, resp["result"])
		}
	}
}

// TestDocGetMissingPath verifies doc.get errors when path param is empty.
func TestDocGetMissingPath(t *testing.T) {
	root := setupDocsFixture(t)
	srv := docsTestServer(t, root)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "doc.get", map[string]string{
		"project": "test-proj",
	})

	if resp["error"] == nil {
		t.Fatal("expected error for missing path")
	}
}

// TestDocListUnknownProject verifies doc.list errors for an unregistered project.
func TestDocListUnknownProject(t *testing.T) {
	handler := server.New(newTestFS(), "test") // no registry provider — empty registry
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "doc.list", map[string]string{"project": "nonexistent"})

	if resp["error"] == nil {
		t.Fatal("expected error for unknown project")
	}
}
