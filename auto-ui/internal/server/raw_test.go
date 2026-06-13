package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-ui/internal/server"
)

// rawTestServer builds an httptest.Server backed by a real on-disk docs/ tree
// rooted at root, with a fixture registry pointing project "test-proj" at it.
// resolveRoot reads from the real OS filesystem (not the embedded asset FS), so
// the raw route needs a real temp dir, not an in-memory fstest.MapFS.
func rawTestServer(t *testing.T, reg config.ProjectsConfig) *httptest.Server {
	t.Helper()
	handler := server.New(newTestFS(), "test", server.WithRegistryProvider(func() config.ProjectsConfig {
		return reg
	}))
	return httptest.NewServer(handler)
}

const rawHTMLBody = "<!doctype html><h1>Page</h1><p>verbatim</p>"

// TestDocRawServesHTML verifies GET /api/doc/raw returns a .html doc verbatim
// with Content-Type text/html.
func TestDocRawServesHTML(t *testing.T) {
	root := setupDocsFixture(t)
	// Overwrite the fixture .html with known content to assert verbatim bytes.
	htmlPath := filepath.Join(root, "docs", "tasks", "page.html")
	if err := os.WriteFile(htmlPath, []byte(rawHTMLBody), 0644); err != nil {
		t.Fatal(err)
	}

	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{{ID: "test-proj", Path: root}},
	}
	srv := rawTestServer(t, reg)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/doc/raw?project=test-proj&path=docs/tasks/page.html")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != rawHTMLBody {
		t.Errorf("body = %q, want verbatim %q", string(body), rawHTMLBody)
	}
}

// TestDocRawRejectsMarkdown verifies a .md path is rejected — HTML rides this
// route exclusively; markdown must go through doc.get.
func TestDocRawRejectsMarkdown(t *testing.T) {
	root := setupDocsFixture(t)
	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{{ID: "test-proj", Path: root}},
	}
	srv := rawTestServer(t, reg)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/doc/raw?project=test-proj&path=docs/readme.md")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatalf("status = 200 for .md path, want a rejection (4xx)")
	}
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Errorf("status = %d, want 4xx for .md path", resp.StatusCode)
	}
}

// TestDocRawTraversalRejected verifies path traversal attempts are rejected.
func TestDocRawTraversalRejected(t *testing.T) {
	root := setupDocsFixture(t)
	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{{ID: "test-proj", Path: root}},
	}
	srv := rawTestServer(t, reg)
	defer srv.Close()

	cases := []struct {
		name string
		path string
	}{
		{"parent traversal", "../etc/passwd"},
		{"docs traversal", "docs/../../.env"},
		{"outside docs", "src/main.go"},
		{"abs path", "/etc/passwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + "/api/doc/raw?project=test-proj&path=" + tc.path)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Errorf("status = 200 for %q, want a rejection", tc.path)
			}
		})
	}
}

// TestDocRawMissingPath verifies a missing path param returns 4xx.
func TestDocRawMissingPath(t *testing.T) {
	root := setupDocsFixture(t)
	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{{ID: "test-proj", Path: root}},
	}
	srv := rawTestServer(t, reg)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/doc/raw?project=test-proj")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Errorf("status = %d, want 4xx for missing path", resp.StatusCode)
	}
}

// TestDocRawRejectsNonGet verifies a non-GET method is rejected with 405.
func TestDocRawRejectsNonGet(t *testing.T) {
	root := setupDocsFixture(t)
	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{{ID: "test-proj", Path: root}},
	}
	srv := rawTestServer(t, reg)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/doc/raw?project=test-proj&path=docs/tasks/page.html", "text/plain", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405 for POST", resp.StatusCode)
	}
}

// TestDocRawWorktreeResolves verifies the worktree param resolves to a
// registered worktree root and serves a .html doc from there.
func TestDocRawWorktreeResolves(t *testing.T) {
	root := setupDocsFixture(t)

	wt := t.TempDir()
	wtDocs := filepath.Join(wt, "docs")
	if err := os.MkdirAll(wtDocs, 0755); err != nil {
		t.Fatal(err)
	}
	const wtBody = "<!doctype html><h1>WT</h1>"
	if err := os.WriteFile(filepath.Join(wtDocs, "wt.html"), []byte(wtBody), 0644); err != nil {
		t.Fatal(err)
	}

	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{
			{ID: "test-proj", Path: root},
			{ID: "test-proj-wt", Path: wt},
		},
	}
	srv := rawTestServer(t, reg)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/doc/raw?worktree=" + wt + "&path=docs/wt.html")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != wtBody {
		t.Errorf("body = %q, want %q", string(body), wtBody)
	}
}
