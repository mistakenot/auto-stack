// Package search provides markdown normalization for BM25 indexing.
package search

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// NormalizedDoc holds the result of normalizing a markdown document
// into separate headings and body text buffers for BM25 indexing.
type NormalizedDoc struct {
	// Headings contains H1-H3 text joined by newlines.
	Headings string
	// Body contains markdown-stripped plain text.
	Body string
}

// multiSpaceRe matches runs of whitespace (spaces, tabs, newlines).
var multiSpaceRe = regexp.MustCompile(`\s+`)

// Normalize parses markdown content and extracts plain text into
// separate headings (H1-H3) and body buffers. It strips all markdown
// syntax while preserving text content and code block contents.
// The input should be the document body after frontmatter has been stripped.
func Normalize(markdown string) NormalizedDoc {
	if markdown == "" {
		return NormalizedDoc{}
	}

	src := []byte(markdown)

	md := goldmark.New(
		goldmark.WithExtensions(extension.Table),
	)

	reader := text.NewReader(src)
	parser := md.Parser()
	root := parser.Parse(reader)

	var headingsBuf bytes.Buffer
	var bodyBuf bytes.Buffer

	// inHeading tracks whether we are currently inside an H1-H3 heading.
	inHeading := false
	// skipSubtree tracks nodes whose subtrees we should skip text output for.
	// Used to skip table header alignment rows.
	skipImageChildren := false

	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n.Kind() {

		case ast.KindHeading:
			heading := n.(*ast.Heading)
			if entering {
				if heading.Level <= 3 {
					inHeading = true
				}
				// For H4+, text children will write to bodyBuf naturally
			} else {
				if heading.Level <= 3 {
					inHeading = false
					headingsBuf.WriteByte('\n')
				}
			}

		case ast.KindText:
			if entering {
				textNode := n.(*ast.Text)
				seg := textNode.Segment.Value(src)
				// Skip empty segments
				if len(seg) == 0 {
					break
				}
				if inHeading {
					headingsBuf.Write(seg)
				} else {
					// Check if parent is an Image — if so, this is alt text
					parent := n.Parent()
					if parent != nil && parent.Kind() == ast.KindImage {
						bodyBuf.Write(seg)
						skipImageChildren = false
					} else {
						bodyBuf.Write(seg)
					}
				}
				if textNode.SoftLineBreak() || textNode.HardLineBreak() {
					if !inHeading {
						bodyBuf.WriteByte(' ')
					}
				}
			}

		case ast.KindString:
			if entering {
				strNode := n.(*ast.String)
				if len(strNode.Value) == 0 {
					break
				}
				if inHeading {
					headingsBuf.Write(strNode.Value)
				} else {
					bodyBuf.Write(strNode.Value)
				}
			}

		case ast.KindImage:
			if entering {
				// Write alt text: the alt text is in the Image's Text child nodes.
				// We handle those via KindText above when parent is Image.
				// Return WalkContinue to process children (the alt text nodes).
				_ = skipImageChildren
			}
			// Do not skip children — let text nodes write alt text.
			return ast.WalkContinue, nil

		case ast.KindLink:
			// Children (Text nodes) will handle writing link text automatically.
			// Skip the destination/title by letting children run.
			return ast.WalkContinue, nil

		case ast.KindFencedCodeBlock:
			if entering {
				cb := n.(*ast.FencedCodeBlock)
				lines := cb.Lines()
				for i := range lines.Len() {
					line := lines.At(i)
					bodyBuf.Write(line.Value(src))
				}
			}
			// Skip children since we handled the content directly.
			return ast.WalkSkipChildren, nil

		case ast.KindCodeBlock:
			if entering {
				cb := n.(*ast.CodeBlock)
				lines := cb.Lines()
				for i := range lines.Len() {
					line := lines.At(i)
					bodyBuf.Write(line.Value(src))
				}
			}
			return ast.WalkSkipChildren, nil

		case ast.KindHTMLBlock:
			// Skip HTML blocks entirely.
			return ast.WalkSkipChildren, nil

		case ast.KindRawHTML:
			// Skip raw HTML entirely.
			return ast.WalkSkipChildren, nil

		case extast.KindTableCell:
			if !entering {
				bodyBuf.WriteByte(' ')
			}

		case extast.KindTableHeader:
			// Skip the alignment row content — the cells of the header contain
			// the column headings which are valid text, but GFM pipes/alignment
			// rows are handled by the parser and do not appear as text nodes.
			// Just continue normally so header cell text is written.

		case extast.KindTableRow:
			if !entering {
				bodyBuf.WriteByte(' ')
			}
		}

		return ast.WalkContinue, nil
	})

	headings := strings.TrimSpace(headingsBuf.String())
	body := strings.TrimSpace(bodyBuf.String())

	// Collapse multiple whitespace characters to a single space.
	headings = multiSpaceRe.ReplaceAllString(headings, " ")
	body = multiSpaceRe.ReplaceAllString(body, " ")

	return NormalizedDoc{
		Headings: headings,
		Body:     body,
	}
}
