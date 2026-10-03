package tree

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mistakenot/auto-plan/internal/graph"
)

// GlyphFunc returns the glyph of a plan-local node ID, or "" when the ID is
// not a node of the plan. Render uses it to annotate names that mention nodes.
type GlyphFunc func(id string) string

// idToken matches a token shaped like a generated node ID (D-11).
var idToken = regexp.MustCompile(`\b[a-z]{1,4}-[0-9a-hjkmnp-tv-z]{4}\b`)

// Render prints a tree in show-me notation: the `+ - ~` gutter (when the tree
// has one), 2-space indentation or `├──` / `└──` branches by style, and the
// comments in one aligned right-hand column. Every token in a name that is a
// node ID known to glyph gets that node's glyph in front of it. Parse reads
// the output back to the same lines.
func Render(t Tree, glyph GlyphFunc) string {
	heads := make([]string, len(t.Lines))
	last := lastChildren(t.Lines)
	// more[d] reports whether the current ancestor at depth d has siblings
	// still to come, which draws a `│` through the lines below it.
	more := map[int]bool{}
	for i, l := range t.Lines {
		var b strings.Builder
		if t.Gutter {
			b.WriteString(cmp.Or(l.Marker, " ") + " ")
		}
		if l.Name == "" {
			if t.Style == StyleIndent {
				b.WriteString(strings.Repeat("  ", l.Depth))
			}
			b.WriteString("# " + l.Comment)
			heads[i] = b.String()
			continue
		}
		switch {
		case t.Style == StyleFile && l.Depth > 0:
			for d := 1; d < l.Depth; d++ {
				if more[d] {
					b.WriteString(groupPipe)
				} else {
					b.WriteString(groupBlank)
				}
			}
			if last[i] {
				b.WriteString(branchLast)
			} else {
				b.WriteString(branchMid)
			}
		case t.Style == StyleIndent:
			b.WriteString(strings.Repeat("  ", l.Depth))
		}
		more[l.Depth] = !last[i]
		b.WriteString(annotate(l.Name, glyph))
		heads[i] = b.String()
	}

	commentAt := 0
	for i, l := range t.Lines {
		if l.Name != "" && l.Comment != "" {
			commentAt = max(commentAt, utf8.RuneCountInString(heads[i]))
		}
	}
	var out strings.Builder
	for i, l := range t.Lines {
		out.WriteString(heads[i])
		if l.Name != "" && l.Comment != "" {
			out.WriteString(strings.Repeat(" ", commentAt-utf8.RuneCountInString(heads[i])+2) + l.Comment)
		}
		out.WriteString("\n")
	}
	return out.String()
}

// annotate puts the glyph of every known node ID in name in front of it.
func annotate(name string, glyph GlyphFunc) string {
	if glyph == nil {
		return name
	}
	return idToken.ReplaceAllStringFunc(name, func(id string) string {
		if g := glyph(id); g != "" {
			return g + " " + id
		}
		return id
	})
}

// lastChildren reports, per line, whether it is the last child of its parent:
// no later named line at the same depth comes before one that is shallower.
// Comment-only lines take no part.
func lastChildren(lines []Line) []bool {
	last := make([]bool, len(lines))
	for i, l := range lines {
		if l.Name == "" {
			continue
		}
		last[i] = true
		for _, next := range lines[i+1:] {
			if next.Name == "" {
				continue
			}
			if next.Depth < l.Depth {
				break
			}
			if next.Depth == l.Depth {
				last[i] = false
				break
			}
		}
	}
	return last
}

// markerOf maps a file node's change to its gutter marker.
func markerOf(change string) string {
	switch change {
	case "add":
		return MarkerAdd
	case "delete":
		return MarkerRemove
	}
	return MarkerChange
}

// fileWhyWidth is how many runes of a file change's reason the derived tree
// shows as a comment after the node ID.
const fileWhyWidth = 60

// dir is one directory (or file) in a derived file tree.
type dir struct {
	name     string
	file     *graph.Node
	children map[string]*dir
}

// FileTree derives a `├──` file tree with a `+ ~ -` gutter from file nodes:
// add is +, edit is ~, delete is -. A directory is + when everything under it
// is added, - when everything is deleted, and ~ otherwise. Directories that
// hold a single directory are joined (`auto-plan/internal/`). Siblings sort by
// name. Each file's comment is its node ID and the first line of its why.
// The tree is derived, never authored: it carries no line numbers of a body.
func FileTree(files []graph.Node) Tree {
	root := &dir{children: map[string]*dir{}}
	for i := range files {
		f := &files[i]
		parts := strings.Split(strings.Trim(f.StringField("path"), "/"), "/")
		cur := root
		for _, p := range parts {
			next, ok := cur.children[p]
			if !ok {
				next = &dir{name: p, children: map[string]*dir{}}
				cur.children[p] = next
			}
			cur = next
		}
		cur.file = f
	}

	t := Tree{Gutter: true, Style: StyleFile, Lines: []Line{}}
	var walk func(d *dir, depth int)
	walk = func(d *dir, depth int) {
		for _, name := range sortedKeys(d.children) {
			c := d.children[name]
			parts := []string{c.name}
			for c.file == nil && len(c.children) == 1 {
				only := c.children[sortedKeys(c.children)[0]]
				if len(only.children) == 0 {
					break // keep the file on its own line
				}
				parts = append(parts, only.name)
				c = only
			}
			label := strings.Join(parts, "/")
			line := Line{N: len(t.Lines) + 1, Depth: depth, Marker: c.marker()}
			if len(c.children) > 0 {
				line.Name = label + "/"
			} else {
				line.Name = label
			}
			if c.file != nil {
				line.Comment = c.file.ID
				if why := oneLine(c.file.StringField("why"), fileWhyWidth); why != "" {
					line.Comment += "  " + why
				}
			}
			t.Lines = append(t.Lines, line)
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return t
}

// marker aggregates the changes at and under d.
func (d *dir) marker() string {
	seen := map[string]bool{}
	var collect func(x *dir)
	collect = func(x *dir) {
		if x.file != nil {
			seen[markerOf(x.file.StringField("change"))] = true
		}
		for _, c := range x.children {
			collect(c)
		}
	}
	collect(d)
	if len(seen) == 1 {
		for m := range seen {
			return m
		}
	}
	return MarkerChange
}

func sortedKeys(m map[string]*dir) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// oneLine returns the first line of s, cut to width runes with "…".
func oneLine(s string, width int) string {
	first, rest, more := strings.Cut(strings.TrimSpace(s), "\n")
	first = strings.Join(strings.Fields(first), " ")
	if utf8.RuneCountInString(first) > width {
		return string([]rune(first)[:width-1]) + "…"
	}
	if more && strings.TrimSpace(rest) != "" {
		return first + " …"
	}
	return first
}
