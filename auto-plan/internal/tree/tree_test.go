package tree

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mistakenot/auto-plan/internal/graph"
)

var update = flag.Bool("update", false, "rewrite golden files")

// golden compares got with testdata/golden/<name>, rewriting it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if got != string(want) {
		t.Fatalf("%s mismatch (run with -update):\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func TestParseWellFormed(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Tree
	}{
		{
			name: "indent tree without a gutter",
			body: "runLint\n  lint.File  reads graph.json\n    graph.Decode # tolerant\n  lint.Graph\n\nemit",
			want: Tree{Style: StyleIndent, Lines: []Line{
				{N: 1, Depth: 0, Name: "runLint"},
				{N: 2, Depth: 1, Name: "lint.File", Comment: "reads graph.json"},
				{N: 3, Depth: 2, Name: "graph.Decode", Comment: "tolerant"},
				{N: 4, Depth: 1, Name: "lint.Graph"},
				{N: 6, Depth: 0, Name: "emit"},
			}},
		},
		{
			name: "indent tree with a gutter",
			body: "+ runLint                  new entry point\n~   lint.File              #  reads graph\n      graph.Decode\n- oldLint   \n",
			want: Tree{Gutter: true, Style: StyleIndent, Lines: []Line{
				{N: 1, Depth: 0, Marker: "+", Name: "runLint", Comment: "new entry point"},
				{N: 2, Depth: 1, Marker: "~", Name: "lint.File", Comment: "reads graph"},
				{N: 3, Depth: 2, Name: "graph.Decode"},
				{N: 4, Depth: 0, Marker: "-", Name: "oldLint"},
			}},
		},
		{
			name: "file tree with a gutter",
			body: "~ auto-plan/\n~ ├── internal/\n+ │   ├── tree/\n+ │   │   └── parse.go  show-me parser\n~ │   └── lint/rules.go\n- └── old.go",
			want: Tree{Gutter: true, Style: StyleFile, Lines: []Line{
				{N: 1, Depth: 0, Marker: "~", Name: "auto-plan/"},
				{N: 2, Depth: 1, Marker: "~", Name: "internal/"},
				{N: 3, Depth: 2, Marker: "+", Name: "tree/"},
				{N: 4, Depth: 3, Marker: "+", Name: "parse.go", Comment: "show-me parser"},
				{N: 5, Depth: 2, Marker: "~", Name: "lint/rules.go"},
				{N: 6, Depth: 1, Marker: "-", Name: "old.go"},
			}},
		},
		{
			name: "file tree without a gutter, blank continuation groups",
			body: "docs/\n└── plans/\n    ├── 001-a/\n    └── 002-b/",
			want: Tree{Style: StyleFile, Lines: []Line{
				{N: 1, Depth: 0, Name: "docs/"},
				{N: 2, Depth: 1, Name: "plans/"},
				{N: 3, Depth: 2, Name: "001-a/"},
				{N: 4, Depth: 2, Name: "002-b/"},
			}},
		},
		{
			name: "comment-only lines take no part in nesting",
			body: "# the lint path\nlint\n  # rules run here\n  rules",
			want: Tree{Style: StyleIndent, Lines: []Line{
				{N: 1, Comment: "the lint path"},
				{N: 2, Name: "lint"},
				{N: 3, Depth: 1, Comment: "rules run here"},
				{N: 4, Depth: 1, Name: "rules"},
			}},
		},
		{
			name: "names starting with punctuation are not gutter markers",
			body: ".gitignore\n(root)\n  _private\n  @decorator\n  \"quoted name\"",
			want: Tree{Style: StyleIndent, Lines: []Line{
				{N: 1, Name: ".gitignore"},
				{N: 2, Name: "(root)"},
				{N: 3, Depth: 1, Name: "_private"},
				{N: 4, Depth: 1, Name: "@decorator"},
				{N: 5, Depth: 1, Name: `"quoted name"`},
			}},
		},
		{
			name: "empty body",
			body: "  \n\n",
			want: Tree{Style: StyleIndent, Lines: []Line{}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, errs := Parse(tc.body)
			if len(errs) != 0 {
				t.Fatalf("errors: %v", errs)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("tree =\n  %+v\nwant\n  %+v", got, tc.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string // "line N: message" prefixes, in order
	}{
		{"bad gutter marker", "+ a\n* b\n  c", []string{`line 2: gutter marker "*" is not one of + - ~ or blank`}},
		{"bad marker in a tree with no valid marker", "! a\n  b", []string{`line 1: gutter marker "!" is not one of + - ~ or blank`}},
		{"marker without a space", "+ a\n~b", []string{"line 2: gutter marker must be followed by a space"}},
		{"missing gutter column", "+ a\nb", []string{"line 2: line has no gutter column"}},
		{"marker indented past the gutter", "+ a\n  ~ b", []string{`line 2: marker "~" belongs in the gutter column`}},
		{"marker with no name", "+ a\n-", []string{`line 2: gutter marker "-" has no name after it`}},
		{"odd indentation", "a\n   b", []string{"line 2: indentation of 3 spaces is not a multiple of 2"}},
		{"odd indentation in a gutter", "+ a\n+   b\n     c", []string{"line 3: indentation of 3 spaces is not a multiple of 2"}},
		{"tab indentation", "a\n\tb", []string{"line 2: indentation uses a tab"}},
		{"orphan child", "a\n    b", []string{`line 2: "b" is at depth 2 but the line before it is at depth 0, so it has no parent`}},
		{"first line indented", "  a\n  b", []string{`line 1: "a" is indented to depth 1 but has no parent line`}},
		{"file tree: branch without a space", "a/\n├──b", []string{"line 2: malformed file tree: a branch is written"}},
		{"file tree: short branch", "a/\n├─ b", []string{"line 2: malformed file tree: a branch is written"}},
		{"file tree: bad continuation group", "a/\n├── b/\n│  └── c", []string{"line 3: malformed file tree: indent in groups of 4"}},
		{"file tree: indented line without a branch", "a/\n├── b/\n│   c", []string{"line 3: malformed file tree: an indented line needs"}},
		{"file tree: sibling after └──", "a/\n└── b\n├── c", []string{`line 3: malformed file tree: "c" follows a "└──" branch`}},
		{"file tree: orphan", "a/\n│   └── b", []string{`line 2: "b" is at depth 2 but the line before it is at depth 0`}},
		{"file tree: double space after branch", "a/\n└──  b", []string{"line 2: malformed file tree: one space goes between"}},
		{"file tree: branch with no name", "a/\n└── ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := Parse(tc.body)
			if tc.want == nil {
				// Trailing whitespace is trimmed, so "└── " reads as a bare branch.
				if len(errs) != 1 {
					t.Fatalf("errors = %v, want one", errs)
				}
				return
			}
			if len(errs) != len(tc.want) {
				t.Fatalf("errors = %v, want %d", errs, len(tc.want))
			}
			for i, e := range errs {
				if !strings.HasPrefix(e.Error(), tc.want[i]) {
					t.Errorf("error %d = %q, want prefix %q", i, e.Error(), tc.want[i])
				}
			}
		})
	}
}

// TestParseBestEffort: a tree with errors still yields its readable lines.
func TestParseBestEffort(t *testing.T) {
	got, errs := Parse("+ a\n* b\n   c")
	if len(errs) != 2 {
		t.Fatalf("errors = %v", errs)
	}
	if len(got.Lines) != 3 || got.Lines[1].Name != "b" || got.Lines[1].Marker != "*" && got.Lines[1].Marker != "" {
		t.Fatalf("lines = %+v", got.Lines)
	}
}

// glyphs stands in for a plan's node lookup.
func glyphs(id string) string {
	return map[string]string{"g-k7q2": "◎", "ac-3fxm": "✓", "d-9t2w": "◆"}[id]
}

func TestRenderGoldens(t *testing.T) {
	cases := []struct{ name, body string }{
		{"render-indent.txt", "+ runBrief  new entry point\n~   render.Brief # builds the brief for ac-3fxm\n      d-9t2w   the decision\n      covers ac-3fxm and g-k7q2\n      ac-zzzz  unknown IDs stay plain\n  # a note\n- oldBrief   replaced"},
		{"render-file.txt", "~ auto-plan/\n~ ├── internal/  code\n+ │   ├── tree/\n+ │   │   └── render.go  show-me renderer\n~ │   └── lint/rules.go\n- └── old.go  gone"},
		{"render-plain.txt", "docs/\n└── plans/\n    ├── 001-a/   first\n    └── 002-b/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr, errs := Parse(tc.body)
			if len(errs) != 0 {
				t.Fatalf("errors: %v", errs)
			}
			golden(t, tc.name, Render(tr, glyphs))
		})
	}
}

// TestRenderRoundTrip: rendering a parsed tree and parsing it again gives the
// same lines (numbers aside), for every well-formed case.
func TestRenderRoundTrip(t *testing.T) {
	bodies := []string{
		"runLint\n  lint.File  reads graph.json\n    graph.Decode # tolerant\n  lint.Graph\n\nemit",
		"+ runLint                  new entry point\n~   lint.File              #  reads graph\n      graph.Decode\n- oldLint   \n",
		"~ auto-plan/\n~ ├── internal/\n+ │   ├── tree/\n+ │   │   └── parse.go  show-me parser\n~ │   └── lint/rules.go\n- └── old.go",
		"docs/\n└── plans/\n    ├── 001-a/\n    └── 002-b/",
		"# the lint path\nlint\n  # rules run here\n  rules",
	}
	for _, body := range bodies {
		want, _ := Parse(body)
		got, errs := Parse(Render(want, nil))
		if len(errs) != 0 {
			t.Fatalf("re-parse errors %v for:\n%s", errs, Render(want, nil))
		}
		if !sameLines(got, want) {
			t.Fatalf("round trip changed the tree:\n  %+v\nwant\n  %+v", got, want)
		}
	}
}

func sameLines(a, b Tree) bool {
	if a.Gutter != b.Gutter || a.Style != b.Style || len(a.Lines) != len(b.Lines) {
		return false
	}
	for i := range a.Lines {
		x, y := a.Lines[i], b.Lines[i]
		x.N, y.N = 0, 0
		if x != y {
			return false
		}
	}
	return true
}

func fileNode(id, path, change, why string) graph.Node {
	return graph.Node{ID: id, Type: "file", Status: graph.StatusActive, Fields: map[string]any{"path": path, "change": change, "why": why}}
}

func TestFileTree(t *testing.T) {
	files := []graph.Node{
		fileNode("f-0001", "auto-plan/internal/render/brief.go", "add", "Stage Brief view"),
		fileNode("f-0002", "auto-plan/internal/render/show.go", "edit", "labels, alternatives and blocks"),
		fileNode("f-0003", "auto-plan/internal/cli/read.go", "add", "list, describe, get, search"),
		fileNode("f-0004", "auto-plan/internal/cli/old.go", "delete", "replaced by read.go\nmore detail"),
		fileNode("f-0005", "go.work", "edit", "use ./auto-plan"),
		fileNode("f-0006", "docs/guide/README.md", "add", strings.Repeat("a very long reason ", 10)),
	}
	full := FileTree(files)
	golden(t, "filetree.txt", Render(full, nil))
	if !full.Gutter || full.Style != StyleFile {
		t.Fatalf("derived tree = %+v", full)
	}
	byName := map[string]Line{}
	for _, l := range full.Lines {
		byName[l.Name] = l
	}
	for name, marker := range map[string]string{
		"auto-plan/internal/": "~", "render/": "~", "cli/": "~", "brief.go": "+", "old.go": "-", "docs/guide/": "+",
	} {
		if byName[name].Marker != marker {
			t.Errorf("%s marker = %q, want %q (lines %+v)", name, byName[name].Marker, marker, full.Lines)
		}
	}
	if _, errs := Parse(Render(full, nil)); len(errs) != 0 {
		t.Fatalf("a derived tree must parse cleanly: %v", errs)
	}

	// One stage's subset: only its files, joined directories re-derived.
	staged := FileTree([]graph.Node{files[2], files[3]})
	golden(t, "filetree-stage.txt", Render(staged, nil))
	if len(staged.Lines) != 3 || staged.Lines[0].Name != "auto-plan/internal/cli/" || staged.Lines[0].Marker != "~" {
		t.Fatalf("staged tree = %+v", staged.Lines)
	}
	if empty := FileTree(nil); len(empty.Lines) != 0 || Render(empty, nil) != "" {
		t.Fatalf("empty tree = %+v", empty)
	}
}
