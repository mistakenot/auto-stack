// Package tree parses show-me notation: the indented structured text a `tree`
// node carries in its body (a call, file, dependency or pseudocode tree).
// Lint uses Parse for shape errors (`tree-syntax`); the renderers reuse the
// parsed Tree.
//
// # Grammar
//
// A body is a sequence of lines. Blank (or whitespace-only) lines are skipped.
// Trailing whitespace is ignored. Every other line is
//
//	line    = [gutter] indent body
//	gutter  = marker " "                     (two columns)
//	marker  = "+" | "-" | "~" | " "          (added, removed, changed, unchanged)
//	indent  = { "  " }                       (indent style: depth = spaces / 2)
//	        | { "│   " | "    " } branch     (file-tree style)
//	branch  = "├── " | "└── "                (depth = groups + 1)
//	body    = name [ sep comment ] | "#" comment
//	sep     = two or more spaces | " #"
//
// The gutter is optional per tree, but all-or-nothing: a tree uses one when any
// line starts with a marker-like character (ASCII punctuation other than
// `# . / _ ( [ { " ' ` + "`" + ` @ $ :`) followed by a space or the end of the
// line. Then every line must start with a two-column gutter, and depth-0 names
// begin in column 3. Without a gutter, depth-0 names begin in column 1.
//
// A tree is file-tree style when any line holds a `├` or `└` branch glyph;
// otherwise it is indent style. Within one tree the style does not mix.
//
// A comment starts at the first run of two or more spaces, or at " #", after
// the name; a leading "#" on the comment is dropped. A body that starts with
// "#" is a comment-only line: it has no name and takes no part in nesting.
//
// Parse reports, as errors, a gutter marker other than `+ - ~` or blank, a
// marker not followed by a space, a missing gutter column, a marker written
// after the indentation instead of in the gutter, indentation that is
// not a multiple of 2 (or uses tabs), a child with no parent (a depth more than
// one below the line before it), and a malformed `├──` / `└──` file tree.
package tree

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Style is the indentation style of a tree.
type Style string

const (
	// StyleIndent nests lines by 2-space indentation.
	StyleIndent Style = "indent"
	// StyleFile nests lines with `│   ` groups and `├── ` / `└── ` branches.
	StyleFile Style = "file"
)

// Markers are the valid gutter markers. A blank gutter is Marker "".
const (
	MarkerAdd    = "+"
	MarkerRemove = "-"
	MarkerChange = "~"
)

// Line is one non-blank line of a tree.
type Line struct {
	// N is the 1-based line number in the body.
	N int `json:"n"`
	// Depth is the nesting level; depth-0 lines are roots.
	Depth int `json:"depth"`
	// Marker is the gutter marker: "+", "-", "~", or "" for blank or no gutter.
	Marker string `json:"marker"`
	// Name is the line's content before the comment; "" for a comment-only line.
	Name string `json:"name"`
	// Comment is the right-hand comment, without its leading "#".
	Comment string `json:"comment"`
}

// Tree is a parsed show-me notation body.
type Tree struct {
	// Gutter reports whether the tree carries a gutter column.
	Gutter bool   `json:"gutter"`
	Style  Style  `json:"style"`
	Lines  []Line `json:"lines"`
}

// Error is one syntax problem at a body line.
type Error struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

func (e Error) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Message) }

const (
	branchMid  = "├── "
	branchLast = "└── "
	groupPipe  = "│   "
	groupBlank = "    "
)

// Parse parses body. It always returns a best-effort Tree (lines that fail
// are kept where their structure can still be read) together with every
// syntax error, in line order. Errors is never nil.
func Parse(body string) (Tree, []Error) {
	p := parser{errs: []Error{}, last: map[int]bool{}}
	raw := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i := range raw {
		raw[i] = strings.TrimRightFunc(raw[i], unicode.IsSpace)
	}
	t := Tree{Style: StyleIndent, Lines: []Line{}}
	for _, l := range raw {
		if l == "" {
			continue
		}
		if hasMarkerCandidate(l) {
			t.Gutter = true
		}
		if strings.ContainsAny(l, "├└") {
			t.Style = StyleFile
		}
	}

	for i, l := range raw {
		if l == "" {
			continue
		}
		line, ok := p.line(i+1, l, t)
		if ok {
			t.Lines = append(t.Lines, line)
		}
	}
	p.structure(t)
	slices.SortStableFunc(p.errs, func(a, b Error) int { return a.Line - b.Line })
	return t, p.errs
}

type parser struct {
	errs []Error
	// last records the lines (by number) whose branch is `└──`.
	last map[int]bool
}

func (p *parser) fail(n int, format string, args ...any) {
	p.errs = append(p.errs, Error{Line: n, Message: fmt.Sprintf(format, args...)})
}

// line parses one non-blank line. ok is false when the line cannot be placed
// in the tree at all.
func (p *parser) line(n int, l string, t Tree) (Line, bool) {
	out := Line{N: n}
	rest := l
	if t.Gutter {
		r, size := utf8.DecodeRuneInString(rest)
		switch {
		case r == ' ':
		case strings.ContainsRune("+-~", r):
			out.Marker = string(r)
		case isNameStart(r):
			p.fail(n, "line has no gutter column: start it with + - ~ or a space, like the other lines")
			return p.body(out, rest, t)
		default:
			p.fail(n, "gutter marker %q is not one of + - ~ or blank", string(r))
		}
		rest = rest[size:]
		if rest == "" {
			if out.Marker == "" {
				return out, false
			}
			p.fail(n, "gutter marker %q has no name after it", out.Marker)
			return out, false
		}
		if rest[0] != ' ' {
			p.fail(n, "gutter marker must be followed by a space")
			return p.body(out, rest, t)
		}
		rest = rest[1:]
	}
	return p.body(out, rest, t)
}

// body parses the indentation, name and comment after the gutter.
func (p *parser) body(out Line, rest string, t Tree) (Line, bool) {
	if lead := rest[:len(rest)-len(strings.TrimLeft(rest, " \t"))]; strings.Contains(lead, "\t") {
		p.fail(out.N, "indentation uses a tab; indent with 2 spaces")
		rest = strings.ReplaceAll(lead, "\t", "  ") + rest[len(lead):]
	}
	var ok bool
	if t.Style == StyleFile {
		rest, ok = p.fileIndent(&out, rest)
	} else {
		rest, ok = p.spaceIndent(&out, rest)
	}
	if !ok {
		return out, false
	}
	if t.Gutter && len(rest) > 1 && strings.ContainsRune("+-~", rune(rest[0])) && rest[1] == ' ' {
		p.fail(out.N, "marker %q belongs in the gutter column (column 1), not after the indentation", rest[:1])
	}
	if c, ok := strings.CutPrefix(rest, "#"); ok {
		out.Comment = strings.TrimSpace(c)
		return out, true
	}
	out.Name, out.Comment = splitComment(rest)
	return out, true
}

func (p *parser) spaceIndent(out *Line, rest string) (string, bool) {
	trimmed := strings.TrimLeft(rest, " ")
	spaces := len(rest) - len(trimmed)
	if spaces%2 != 0 {
		p.fail(out.N, "indentation of %d spaces is not a multiple of 2", spaces)
	}
	out.Depth = spaces / 2
	return trimmed, true
}

// fileIndent reads `│   ` / `    ` groups and an optional branch. A root line
// (no branch) must start at column 0 of the body.
func (p *parser) fileIndent(out *Line, rest string) (string, bool) {
	groups := 0
	for {
		switch {
		case strings.HasPrefix(rest, groupPipe):
			rest = rest[len(groupPipe):]
			groups++
			continue
		case strings.HasPrefix(rest, groupBlank):
			rest = rest[len(groupBlank):]
			groups++
			continue
		}
		break
	}
	r, _ := utf8.DecodeRuneInString(rest)
	switch {
	case strings.HasPrefix(rest, branchMid) || strings.HasPrefix(rest, branchLast):
		p.last[out.N] = strings.HasPrefix(rest, branchLast)
		rest = rest[len(branchMid):]
		out.Depth = groups + 1
		if strings.HasPrefix(rest, " ") {
			p.fail(out.N, "malformed file tree: one space goes between the branch and the name")
			rest = strings.TrimLeft(rest, " ")
		}
		if rest == "" {
			p.fail(out.N, "malformed file tree: the branch has no name after it")
			return rest, false
		}
		return rest, true
	case r == '├' || r == '└':
		p.fail(out.N, "malformed file tree: a branch is written %q or %q", strings.TrimSpace(branchMid), strings.TrimSpace(branchLast))
		return rest, false
	case r == '│' || r == ' ':
		p.fail(out.N, "malformed file tree: indent in groups of 4 (%q or 4 spaces) ending in a branch", strings.TrimSpace(groupPipe))
		return rest, false
	case groups > 0:
		p.fail(out.N, "malformed file tree: an indented line needs a %q or %q branch", strings.TrimSpace(branchMid), strings.TrimSpace(branchLast))
		return rest, false
	}
	return rest, true
}

// structure checks nesting across lines: every child has a parent, and in a
// file tree `└──` really is the last child of its parent.
func (p *parser) structure(t Tree) {
	prev := -1
	// closed[d] is set once a `└──` branch at depth d has ended its parent's
	// children; a shallower line starts a new parent and clears it.
	closed := map[int]bool{}
	for _, l := range t.Lines {
		if l.Name == "" {
			continue // comment-only lines take no part in nesting
		}
		if l.Depth > prev+1 {
			if prev < 0 {
				p.fail(l.N, "%q is indented to depth %d but has no parent line", l.Name, l.Depth)
			} else {
				p.fail(l.N, "%q is at depth %d but the line before it is at depth %d, so it has no parent", l.Name, l.Depth, prev)
			}
		}
		for d := range closed {
			if d > l.Depth {
				delete(closed, d)
			}
		}
		if t.Style == StyleFile && l.Depth > 0 {
			if closed[l.Depth] {
				p.fail(l.N, "malformed file tree: %q follows a %q branch, which marks its parent's last child", l.Name, strings.TrimSpace(branchLast))
			}
			if p.last[l.N] {
				closed[l.Depth] = true
			}
		}
		prev = l.Depth
	}
}

// hasMarkerCandidate reports whether l starts like a gutter marker.
func hasMarkerCandidate(l string) bool {
	r, size := utf8.DecodeRuneInString(l)
	if r >= utf8.RuneSelf || r == ' ' || isNameStart(r) || !unicode.IsPrint(r) {
		return false
	}
	return len(l) == size || l[size] == ' '
}

// isNameStart reports whether r can begin a depth-0 name in a tree without a
// gutter: letters, digits, box-drawing glyphs and the punctuation names start
// with (`.gitignore`, `/usr`, `_x`, `(a)`, `"s"`, `@dec`, `$var`).
func isNameStart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r >= utf8.RuneSelf || strings.ContainsRune("#./_([{\"'`@$:", r)
}

// splitComment splits s into a name and a comment at the first run of two or
// more spaces or at " #".
func splitComment(s string) (name, comment string) {
	cut := -1
	if i := strings.Index(s, "  "); i >= 0 {
		cut = i
	}
	if i := strings.Index(s, " #"); i >= 0 && (cut < 0 || i < cut) {
		cut = i
	}
	if cut < 0 {
		return s, ""
	}
	comment = strings.TrimSpace(s[cut:])
	comment = strings.TrimSpace(strings.TrimPrefix(comment, "#"))
	return strings.TrimSpace(s[:cut]), comment
}
