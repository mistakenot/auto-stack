package linkscan

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/datadyne-io/autodoc/internal/doctree"
)

var markdownTagLineRegex = regexp.MustCompile(`^\s*<!--\s*(\[autodoc\([^)]*\)\])\s*-->\s*$`)
var markdownTagStripLineRegex = regexp.MustCompile(`^\s*<!--\s*\[autodoc\([^)]*\)\]\s*-->\s*$`)
var markdownHeadingRegex = regexp.MustCompile(`^(#{1,6})[ \t]+.*$`)

func ScanMarkdownDocs(entries []doctree.Entry) (ScanResult, error) {
	var result ScanResult

	for _, entry := range entries {
		if entry.AbsPath == "" {
			continue
		}

		data, err := os.ReadFile(entry.AbsPath)
		if err != nil {
			return result, fmt.Errorf("read %s: %w", entry.RepoRelPath, err)
		}

		lines := normalizeLines(string(data))
		parser := newMarkdownParser(lines)

		for i, line := range lines {
			lineNo := i + 1
			parser.updateFenceState(line)

			if parser.inFence || !strings.Contains(line, "<!--") || !strings.Contains(line, "[autodoc(") {
				continue
			}

			if !strings.Contains(line, "@") || !strings.Contains(line, "-->") {
				continue
			}

			if parser.inFrontmatter(lineNo) {
				result.Malformed = append(result.Malformed, MalformedTag{
					FilePath: entry.AbsPath,
					Line:     lineNo,
					RawText:  strings.TrimRight(line, "\r"),
				})
				continue
			}

			match := markdownTagLineRegex.FindStringSubmatch(strings.TrimRight(line, "\r"))
			if len(match) == 2 {
				inner := match[1]
				strict := strictTagRegex.FindStringSubmatch(inner)
				if len(strict) == 4 {
					result.Tags = append(result.Tags, Tag{
						FilePath:  entry.AbsPath,
						Line:      lineNo,
						DocId:     strict[1],
						DocHash:   strict[2],
						ScopeHash: strict[3],
						RawTag:    inner,
						ScopeKind: ScopeKindMarkdown,
					})
					continue
				}
			}

			if strings.Contains(line, "@") {
				result.Malformed = append(result.Malformed, MalformedTag{
					FilePath: entry.AbsPath,
					Line:     lineNo,
					RawText:  strings.TrimRight(line, "\r"),
				})
			}
		}
	}

	return result, nil
}

func computeMarkdownScopeHashFromContent(content string, tagLine int) (string, error) {
	if tagLine <= 0 {
		return "", errors.New("tag line must be >= 1")
	}

	lines := normalizeLines(content)
	if tagLine > len(lines) {
		return "", fmt.Errorf("tag line %d out of range (max %d)", tagLine, len(lines))
	}

	parser := newMarkdownParser(lines)
	anchorLine, anchorDepth, err := parser.findAnchor(tagLine)
	if err != nil {
		return "", err
	}

	start := parser.bodyStartLine
	if anchorLine != 0 {
		start = anchorLine
	}
	end := len(lines) + 1
	if anchorLine != 0 {
		end = parser.findScopeEnd(anchorLine, anchorDepth)
	}

	collected := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		line := lines[i-1]
		trimmed := strings.TrimRight(line, "\r")
		if markdownTagStripLineRegex.MatchString(strings.TrimSpace(trimmed)) {
			continue
		}
		collected = append(collected, anyAutodocTagRegex.ReplaceAllString(trimmed, ""))
	}

	scope := strings.Join(collected, "\n")
	sum := md5.Sum([]byte(scope))
	return hex.EncodeToString(sum[:])[:8], nil
}

type markdownHeading struct {
	line  int
	depth int
}

type markdownParser struct {
	lines         []string
	headings      []markdownHeading
	bodyStartLine int
	inFence       bool
	frontmatterEnd int
}

func newMarkdownParser(lines []string) *markdownParser {
	p := &markdownParser{
		lines:         lines,
		bodyStartLine: 1,
		frontmatterEnd: -1,
	}

	if len(lines) > 0 && lines[0] == "---" {
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				p.frontmatterEnd = i + 1
				p.bodyStartLine = i + 2
				break
			}
		}
	}

	inFence := false
	for i, line := range lines {
		if toggleFence(line, inFence) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if i+1 < p.bodyStartLine {
			continue
		}
		match := markdownHeadingRegex.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if len(match) == 2 {
			p.headings = append(p.headings, markdownHeading{
				line:  i + 1,
				depth: len(match[1]),
			})
		}
	}

	return p
}

func (p *markdownParser) updateFenceState(line string) {
	if toggleFence(line, p.inFence) {
		p.inFence = !p.inFence
	}
}

func (p *markdownParser) inFrontmatter(lineNo int) bool {
	return p.frontmatterEnd != -1 && lineNo <= p.frontmatterEnd
}

func (p *markdownParser) findAnchor(tagLine int) (int, int, error) {
	if tagLine < p.bodyStartLine {
		return 0, 0, errors.New("markdown autodoc tag cannot appear in frontmatter")
	}

	idx := slices.IndexFunc(p.headings, func(h markdownHeading) bool {
		return h.line >= tagLine
	})
	if idx == -1 {
		if len(p.headings) == 0 {
			return 0, 0, nil
		}
		last := p.headings[len(p.headings)-1]
		if last.line < tagLine {
			return last.line, last.depth, nil
		}
	}
	if idx == 0 {
		return 0, 0, nil
	}
	anchor := p.headings[idx-1]
	return anchor.line, anchor.depth, nil
}

func (p *markdownParser) findScopeEnd(anchorLine int, anchorDepth int) int {
	for _, heading := range p.headings {
		if heading.line <= anchorLine {
			continue
		}
		if heading.depth <= anchorDepth {
			return heading.line
		}
	}
	return len(p.lines) + 1
}

func normalizeLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.Split(content, "\n")
}

func toggleFence(line string, inFence bool) bool {
	trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
	if !strings.HasPrefix(trimmed, "```") {
		return false
	}
	return true
}

func IsMarkdownPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}
