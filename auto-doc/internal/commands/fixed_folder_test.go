package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/linkscan"
	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestFolderLinkLifecycle(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.InitGitRepo()
	docHash := createDocWithID(t, ws, "docs/cache.md", "deadbeef")
	ws.WriteSourceFile("pkg/cache/lru.go", "package cache\n\nfunc get() {}\n")
	ws.WriteSourceFile("pkg/cache/ttl.go", "package cache\n\nfunc ttl() {}\n")
	linkFile := ws.WriteSourceFile("pkg/cache/.autodoc", "# cache package docs\n[autodoc(deadbeef@00000000, 00000000)]\n")
	ws.GitAddAll()

	// 1. Placeholder hashes: fix reports the folder link with folder instructions.
	var buf bytes.Buffer
	if err := Fix(&buf, ws.Dir, "docs", 2, nil, nil); err == nil {
		t.Fatal("expected stale folder link")
	}
	out := buf.String()
	for _, want := range []string{
		"LINK STALE: both folder files and doc changed since last sync",
		"location:  pkg/cache/.autodoc:2",
		"every code file under pkg/cache/ (recursive)",
		"auto doc fixed pkg/cache/.autodoc",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("fix output missing %q:\n%s", want, out)
		}
	}

	// 2. fixed rewrites the tag in place, preserving the comment.
	updates, err := FixedFolderLink(linkFile, ws.Dir, "docs", nil)
	if err != nil {
		t.Fatalf("FixedFolderLink: %v", err)
	}
	if len(updates) != 1 || updates[0].Line != 2 || !strings.Contains(updates[0].NewTag, "deadbeef@"+docHash) {
		t.Fatalf("updates = %+v", updates)
	}
	data, _ := os.ReadFile(linkFile)
	if !strings.HasPrefix(string(data), "# cache package docs\n[autodoc(deadbeef@"+docHash+", ") {
		t.Fatalf("link file = %q", data)
	}

	result, err := FixCollect(ws.Dir, "docs", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LinkIssues) != 0 {
		t.Fatalf("expected no link issues after fixed, got %+v", result.LinkIssues)
	}

	// 3. Re-running fixed is a no-op.
	updates, err = FixedFolderLink(linkFile, ws.Dir, "docs", nil)
	if err != nil || len(updates) != 0 {
		t.Fatalf("second fixed: updates=%+v err=%v", updates, err)
	}

	// 4. Editing any covered file stales the one folder line only.
	ws.WriteSourceFile("pkg/cache/ttl.go", "package cache\n\nfunc ttl() int { return 1 }\n")
	buf.Reset()
	if err := Fix(&buf, ws.Dir, "docs", 2, nil, nil); err == nil {
		t.Fatal("expected stale after code edit")
	}
	if !strings.Contains(buf.String(), "LINK STALE: files under a folder link changed, doc may need updating") {
		t.Fatalf("missing scope stale block:\n%s", buf.String())
	}
}

func TestFixedFolderLinkReportsUnknownDoc(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.InitGitRepo()
	createDocWithID(t, ws, "docs/cache.md", "deadbeef")
	ws.WriteSourceFile("pkg/cache/lru.go", "package cache\n")
	linkFile := ws.WriteSourceFile("pkg/cache/.autodoc", "[autodoc(deadbeef@00000000, 00000000)]\n[autodoc(0badf00d@00000000, 00000000)]\n")
	ws.GitAddAll()

	updates, err := FixedFolderLink(linkFile, ws.Dir, "docs", nil)
	if err == nil || !strings.Contains(err.Error(), "doc id 0badf00d not found") {
		t.Fatalf("err = %v, want unknown doc error", err)
	}
	if len(updates) != 1 {
		t.Fatalf("known doc line should still be refreshed, updates = %+v", updates)
	}
}

func TestFixSuggestsFolderLinks(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.InitGitRepo()
	createDocWithID(t, ws, "docs/cache.md", "deadbeef")
	for _, f := range []string{"pkg/cache/lru.go", "pkg/cache/ttl.go", "pkg/single/one.go"} {
		ws.WriteSourceFile(f, "// [autodoc(deadbeef@00000000, 00000000)]\npackage x\n")
	}
	ws.GitAddAll()

	result, err := FixCollect(ws.Dir, "docs", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FolderSuggestions) != 1 {
		t.Fatalf("suggestions = %+v, want one for pkg/cache", result.FolderSuggestions)
	}
	s := result.FolderSuggestions[0]
	if s.Dir != "pkg/cache" || s.DocID != "deadbeef" || len(s.Files) != 2 {
		t.Fatalf("suggestion = %+v", s)
	}

	var buf bytes.Buffer
	_ = Fix(&buf, ws.Dir, "docs", 2, nil, nil)
	if !strings.Contains(buf.String(), "FOLDER LINK: 2 files in pkg/cache/ link doc deadbeef") {
		t.Fatalf("missing suggestion text:\n%s", buf.String())
	}

	buf.Reset()
	if err := FixOutputJSON(&buf, result.DocIssues, result.LinkIssues, result.FolderSuggestions); err != nil {
		t.Fatal(err)
	}
	var issues []FixIssueJSON
	if err := json.Unmarshal(buf.Bytes(), &issues); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, iss := range issues {
		if iss.Type == "folder_link_suggestion" && iss.Path == "pkg/cache" {
			found = true
		}
	}
	if !found {
		t.Fatalf("JSON missing folder_link_suggestion: %s", buf.String())
	}

	// An ancestor .autodoc for the same doc silences the hint (coexistence is allowed).
	ws.WriteSourceFile("pkg/.autodoc", "[autodoc(deadbeef@00000000, 00000000)]\n")
	ws.GitAddAll()
	result, err = FixCollect(ws.Dir, "docs", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FolderSuggestions) != 0 {
		t.Fatalf("covered folder still suggested: %+v", result.FolderSuggestions)
	}
}

func TestSuggestFolderLinksSiblingSpread(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  []string // "dir:folderCount:fileCount"
	}{
		{
			name:  "one file per sibling package collapses to parent",
			files: []string{"svc/internal/a/a.go", "svc/internal/b/b.go", "svc/internal/c/c.go"},
			want:  []string{"svc/internal:3:3"},
		},
		{
			name:  "parent with its own tagged file plus a child",
			files: []string{"svc/pkg/root.go", "svc/pkg/sub/sub.go"},
			want:  []string{"svc/pkg:2:2"},
		},
		{
			name:  "tightest folder wins before the parent",
			files: []string{"svc/a/one.go", "svc/a/two.go", "svc/b/three.go"},
			want:  []string{"svc/a:1:2"},
		},
		{
			name:  "top-level siblings never suggest the repo root",
			files: []string{"alpha/a.go", "beta/b.go"},
			want:  nil,
		},
		{
			name:  "cousins do not group",
			files: []string{"svc/a/x/one.go", "svc/b/y/two.go"},
			want:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws := testutil.NewWorkspace(t)
			ws.InitGitRepo()
			tags := make([]linkscan.Tag, 0, len(tc.files))
			for _, f := range tc.files {
				tags = append(tags, linkscan.Tag{FilePath: ws.Path(f), DocId: "deadbeef", ScopeKind: linkscan.ScopeKindIndent})
			}
			got := make([]string, 0)
			for _, s := range suggestFolderLinks(ws.Dir, tags) {
				got = append(got, fmt.Sprintf("%s:%d:%d", s.Dir, len(s.Folders), len(s.Files)))
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("suggestions = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFixSuggestsSiblingSpreadText(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.InitGitRepo()
	createDocWithID(t, ws, "docs/cache.md", "deadbeef")
	for _, f := range []string{"svc/internal/a/a.go", "svc/internal/b/b.go"} {
		ws.WriteSourceFile(f, "// [autodoc(deadbeef@00000000, 00000000)]\npackage x\n")
	}
	ws.GitAddAll()

	var buf bytes.Buffer
	_ = Fix(&buf, ws.Dir, "docs", 2, nil, nil)
	out := buf.String()
	for _, want := range []string{
		"FOLDER LINK: 2 files across 2 folders under svc/internal/ link doc deadbeef",
		"svc/internal/.autodoc would also cover untagged files and folders under svc/internal/",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}
