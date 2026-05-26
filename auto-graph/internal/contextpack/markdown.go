package contextpack

import (
	"fmt"
	"path/filepath"
	"strings"
)

// RenderMarkdown renders the Pack as compact markdown suitable for LLM context
// windows. It includes budget, omitted total, seeds, reading order, watch
// guidance, file content with fenced blocks, and omitted candidates.
// No generic command tutorial, API reference, or boilerplate is included.
func RenderMarkdown(p *Pack) string {
	var b strings.Builder

	b.WriteString("# Context Pack\n\n")

	// Budget line.
	b.WriteString(fmt.Sprintf("Budget: %d/%d tokens\n", p.EstimatedTokens, p.TokenLimit))

	// Omitted total.
	b.WriteString(fmt.Sprintf("Omitted: %d tokens\n", p.OmittedTokens))

	// Seeds.
	b.WriteString(fmt.Sprintf("Seeds: %s\n", strings.Join(p.SeedFiles, ", ")))

	// Read First section.
	if len(p.ReadingOrder) > 0 {
		b.WriteString("\n## Read First\n")
		for i, item := range p.ReadingOrder {
			b.WriteString(fmt.Sprintf("%d. %s - %s\n", i+1, item.Path, item.Reason))
		}
	}

	// Watch section.
	if len(p.Guidance.Watch) > 0 {
		b.WriteString("\n## Watch\n")
		for _, w := range p.Guidance.Watch {
			b.WriteString(fmt.Sprintf("- %s\n", w))
		}
	}

	// Files section.
	if len(p.Files) > 0 {
		b.WriteString("\n## Files\n")
		for _, f := range p.Files {
			b.WriteString(fmt.Sprintf("### %s\n", f.Path))
			b.WriteString(fmt.Sprintf("Role: %s. Tokens: %d.\n", f.Role, f.EstimatedTokens))
			if len(f.Flags) > 0 {
				b.WriteString(fmt.Sprintf("Flags: %s.\n", strings.Join(f.Flags, ", ")))
			}
			b.WriteString("\n")
			lang := inferFenceLanguage(f.Path)
			fence := fenceForContent(f.Content)
			b.WriteString(fmt.Sprintf("%s%s\n", fence, lang))
			b.WriteString(f.Content)
			if !strings.HasSuffix(f.Content, "\n") {
				b.WriteString("\n")
			}
			b.WriteString(fence)
			b.WriteString("\n\n")
		}
	}

	// Omitted section.
	if len(p.OmittedCandidates) > 0 {
		b.WriteString("## Omitted\n")
		for _, oc := range p.OmittedCandidates {
			b.WriteString(fmt.Sprintf("- %s - %s, %d tokens\n", oc.Path, oc.Reason, oc.EstimatedTokens))
		}
	}

	return b.String()
}

// MarkdownEstimator returns a FormatEstimator that estimates tokens based on
// rendering the pack as markdown.
func MarkdownEstimator() FormatEstimator {
	return func(p *Pack) int {
		rendered := RenderMarkdown(p)
		return EstimateTokens(rendered)
	}
}

// fenceForContent returns a backtick fence string that won't collide with
// backtick runs inside content. Per CommonMark, the closing fence must be at
// least as long as the opening fence, so we use max(3, longest_run+1).
func fenceForContent(content string) string {
	maxRun := 0
	cur := 0
	for _, ch := range content {
		if ch == '`' {
			cur++
			if cur > maxRun {
				maxRun = cur
			}
		} else {
			cur = 0
		}
	}
	n := 3
	if maxRun >= 3 {
		n = maxRun + 1
	}
	return strings.Repeat("`", n)
}

// inferFenceLanguage infers the fenced code block language from a file path.
func inferFenceLanguage(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".ts":
		return "ts"
	case ".tsx":
		return "tsx"
	case ".js":
		return "js"
	case ".jsx":
		return "jsx"
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".md":
		return "markdown"
	default:
		return ""
	}
}
