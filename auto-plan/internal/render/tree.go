package render

import (
	"cmp"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/tree"
)

// GlyphLookup annotates tree names with the glyph of the plan node they name.
func GlyphLookup(g *graph.Graph) tree.GlyphFunc {
	ix := newIndex(g)
	return func(id string) string {
		if n, ok := ix.all[id]; ok {
			return Glyph(n.Type)
		}
		return ""
	}
}

// TreeView is one authored `tree` node, parsed and rendered in show-me
// notation. Errors holds the body's syntax errors; the tree is best effort.
type TreeView struct {
	Plan   string       `json:"plan"`
	ID     string       `json:"id"`
	Title  string       `json:"title"`
	Kind   string       `json:"kind"`
	About  []string     `json:"about"`
	Tree   tree.Tree    `json:"tree"`
	Errors []tree.Error `json:"errors"`

	glyph tree.GlyphFunc
}

// TreeNode builds the view of tree node n.
func TreeNode(planID string, g *graph.Graph, n graph.Node) TreeView {
	t, errs := tree.Parse(n.StringField("body"))
	about := []string{}
	for _, e := range g.Edges {
		if e.From == n.ID && e.Type == "about" {
			about = append(about, e.To)
		}
	}
	slices.Sort(about)
	return TreeView{
		Plan: planID, ID: n.ID, Title: n.StringField("title"), Kind: n.StringField("kind"),
		About: about, Tree: t, Errors: errs, glyph: GlyphLookup(g),
	}
}

// Text renders a header line, then the tree with its aligned gutter and
// comment column.
func (v TreeView) Text() string {
	head := Glyph("tree") + " " + v.ID + "  " + v.Title + "  (" + v.Kind
	if len(v.About) > 0 {
		head += "; about " + strings.Join(v.About, ", ")
	}
	return head + ")\n\n" + tree.Render(v.Tree, v.glyph)
}

// FileRow is one planned file change.
type FileRow struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Change string `json:"change"`
	Why    string `json:"why"`
}

// FilesView is the file tree derived from a plan's file nodes (or one
// stage's): never authored, always recomputed.
type FilesView struct {
	Plan  string    `json:"plan"`
	Stage string    `json:"stage,omitempty"`
	Files []FileRow `json:"files"`
	Tree  tree.Tree `json:"tree"`
}

// Files derives the file tree of every active file node, or — when stageID is
// set — of the files that stage touches.
func Files(planID string, g *graph.Graph, stageID string) FilesView {
	var files []graph.Node
	if stageID == "" {
		files = newIndex(g).byType("file")
	} else {
		files = StageFiles(g, stageID)
	}
	return FilesView{Plan: planID, Stage: stageID, Files: fileRows(files), Tree: tree.FileTree(files)}
}

// Text renders the derived tree, or a note when there are no files.
func (v FilesView) Text() string {
	if len(v.Files) == 0 {
		if v.Stage != "" {
			return "(stage " + v.Stage + " touches no file changes)\n"
		}
		return "(no file changes)\n"
	}
	return tree.Render(v.Tree, nil)
}

// StageFiles returns the active file nodes a stage touches, by path.
func StageFiles(g *graph.Graph, stageID string) []graph.Node {
	ix := newIndex(g)
	var out []graph.Node
	for _, e := range ix.activeEdges("touches") {
		if e.From == stageID {
			out = append(out, ix.active[e.To])
		}
	}
	return sortByPath(out)
}

func sortByPath(files []graph.Node) []graph.Node {
	slices.SortFunc(files, func(a, b graph.Node) int {
		return cmp.Or(cmp.Compare(a.StringField("path"), b.StringField("path")), cmp.Compare(a.ID, b.ID))
	})
	return slices.CompactFunc(files, func(a, b graph.Node) bool { return a.ID == b.ID })
}

func fileRows(files []graph.Node) []FileRow {
	rows := []FileRow{}
	for _, f := range sortByPath(slices.Clone(files)) {
		rows = append(rows, FileRow{ID: f.ID, Path: f.StringField("path"), Change: f.StringField("change"), Why: f.StringField("why")})
	}
	return rows
}
