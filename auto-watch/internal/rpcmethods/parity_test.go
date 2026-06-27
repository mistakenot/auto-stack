package rpcmethods

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-shared/config"
)

// parityCase is a golden test case asserting behaviour parity with auto-ui.
type parityCase struct {
	name    string
	method  string
	params  map[string]string
	wantOK  bool
	checkFn func(t *testing.T, result json.RawMessage)
}

// seedParityFixture creates a fixture with known content for parity assertions.
func seedParityFixture(t *testing.T) (string, config.ProjectsConfig) {
	t.Helper()

	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	tasksDir := filepath.Join(docsDir, "tasks", "042-sample")
	os.MkdirAll(tasksDir, 0o755)

	os.WriteFile(filepath.Join(docsDir, "overview.md"), []byte("# Overview\nThis is a test."), 0o644)

	htmlBody := `<!doctype html>
<html><head>
<script type="application/json" id="pd-meta">
{"id":"042","name":"sample","status":"planning","epic":"001","created":"2026-01-15"}
</script>
</head><body>
<pd-doc title="042: sample" status="pending">
</pd-doc>
</body></html>`
	os.WriteFile(filepath.Join(tasksDir, "plan.html"), []byte(htmlBody), 0o644)

	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{
			{ID: "parity-proj", Name: "Parity", Path: root, Remote: "git@github.com:user/parity.git"},
		},
	}
	return root, reg
}

func TestParityTable(t *testing.T) {
	root, reg := seedParityFixture(t)
	client, _, cleanup := setupWithReg(t, func() config.ProjectsConfig { return reg })
	defer cleanup()

	htmlBytes, _ := os.ReadFile(filepath.Join(root, "docs/tasks/042-sample/plan.html"))

	cases := []parityCase{
		// doc.list: returns md + html entries with correct types and meta
		{
			name:   "doc.list: md and html entries",
			method: "doc.list",
			params: map[string]string{"project": "parity-proj"},
			wantOK: true,
			checkFn: func(t *testing.T, result json.RawMessage) {
				var entries []docEntry
				json.Unmarshal(result, &entries)
				if len(entries) != 2 {
					t.Fatalf("expected 2 entries, got %d", len(entries))
				}
				// Find the HTML entry and verify meta
				for _, e := range entries {
					if e.Type == "html" {
						if e.Meta == nil {
							t.Fatal("html entry missing meta")
						}
						if e.Meta.Status != "planning" {
							t.Errorf("meta.status = %q, want planning", e.Meta.Status)
						}
						if e.Meta.ReviewState != "pending" {
							t.Errorf("meta.reviewState = %q, want pending", e.Meta.ReviewState)
						}
					}
					if e.Type == "markdown" {
						if e.Meta != nil {
							t.Error("md entry should not have meta")
						}
					}
					// auto-ui parity: ID == Path
					if e.ID != e.Path {
						t.Errorf("ID (%q) != Path (%q)", e.ID, e.Path)
					}
				}
			},
		},
		// doc.list: empty docs dir returns []
		{
			name:   "doc.list: missing docs dir → empty array",
			method: "doc.list",
			params: map[string]string{"worktree": t.TempDir()},
			wantOK: false, // worktree not in registry
		},
		// doc.get: returns markdown content
		{
			name:   "doc.get: markdown file content",
			method: "doc.get",
			params: map[string]string{"project": "parity-proj", "path": "docs/overview.md"},
			wantOK: true,
			checkFn: func(t *testing.T, result json.RawMessage) {
				var got map[string]string
				json.Unmarshal(result, &got)
				if got["path"] != "docs/overview.md" {
					t.Errorf("path = %q", got["path"])
				}
				if got["markdown"] != "# Overview\nThis is a test." {
					t.Errorf("markdown content mismatch: %q", got["markdown"])
				}
			},
		},
		// doc.get: rejects HTML
		{
			name:   "doc.get: rejects .html",
			method: "doc.get",
			params: map[string]string{"project": "parity-proj", "path": "docs/tasks/042-sample/plan.html"},
			wantOK: false,
		},
		// doc.get: traversal rejected
		{
			name:   "doc.get: traversal rejected",
			method: "doc.get",
			params: map[string]string{"project": "parity-proj", "path": "../etc/passwd"},
			wantOK: false,
		},
		// doc.raw: returns verbatim bytes + content type
		{
			name:   "doc.raw: html file bytes",
			method: "doc.raw",
			params: map[string]string{"project": "parity-proj", "path": "docs/tasks/042-sample/plan.html"},
			wantOK: true,
			checkFn: func(t *testing.T, result json.RawMessage) {
				var got DocRawResult
				json.Unmarshal(result, &got)
				if got.ContentType != "text/html; charset=utf-8" {
					t.Errorf("contentType = %q", got.ContentType)
				}
				decoded, err := base64.StdEncoding.DecodeString(got.ContentBase64)
				if err != nil {
					t.Fatalf("base64 decode: %v", err)
				}
				if !bytes.Equal(decoded, htmlBytes) {
					t.Error("decoded bytes don't match file")
				}
			},
		},
		// doc.raw: rejects .md
		{
			name:   "doc.raw: rejects .md",
			method: "doc.raw",
			params: map[string]string{"project": "parity-proj", "path": "docs/overview.md"},
			wantOK: false,
		},
		// project.list: shape + host stamp
		{
			name:   "project.list: shape + host stamp",
			method: "project.list",
			params: nil,
			wantOK: true,
			checkFn: func(t *testing.T, result json.RawMessage) {
				var entries []projectEntry
				json.Unmarshal(result, &entries)
				if len(entries) != 1 {
					t.Fatalf("expected 1 entry, got %d", len(entries))
				}
				e := entries[0]
				if e.ID != "parity-proj" {
					t.Errorf("id = %q", e.ID)
				}
				if e.Host != "test-host" {
					t.Errorf("host = %q, want test-host", e.Host)
				}
				// remote should be normalized (no credentials)
				if strings.Contains(e.Remote, "token") || strings.Contains(e.Remote, "oauth") {
					t.Errorf("remote not normalized: %q", e.Remote)
				}
			},
		},
		// error class: unknown project → error (InvalidParams class)
		{
			name:   "doc.get: unknown project → error",
			method: "doc.get",
			params: map[string]string{"project": "nonexistent", "path": "docs/x.md"},
			wantOK: false,
		},
		// error class: valid project, missing file → error
		{
			name:   "doc.get: missing file → error",
			method: "doc.get",
			params: map[string]string{"project": "parity-proj", "path": "docs/nope.md"},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, rpcErr := callRPC(t, client, tc.method, tc.params)
			if tc.wantOK {
				if rpcErr != nil {
					t.Fatalf("expected success, got error: %v", rpcErr)
				}
				if tc.checkFn != nil {
					tc.checkFn(t, result)
				}
			} else {
				if rpcErr == nil {
					t.Fatalf("expected error, got success: %s", string(result))
				}
			}
		})
	}
}
