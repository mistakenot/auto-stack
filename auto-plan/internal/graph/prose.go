package graph

import (
	"regexp"
	"strings"
)

// proseRefRE matches a `[[…]]` reference in Markdown prose.
var proseRefRE = regexp.MustCompile(`\[\[([^\[\]\n]*)\]\]`)

// ProseRef is one `[[…]]` reference in Markdown prose: its byte span
// (brackets included) and the reference between the brackets.
type ProseRef struct {
	Start, End int
	Ref        string
}

// FindProseRefs returns the `[[…]]` references in text, in order. A
// reference inside an inline code span or a fenced code block is an example,
// not a reference, and is left out — so prose can show the syntax by quoting
// it in backticks. Lint and RewritePlanRefs both read references through
// here, so what lint checks is exactly what renumber rewrites.
func FindProseRefs(text string) []ProseRef {
	code := codeSpans(text)
	var out []ProseRef
	for _, m := range proseRefRE.FindAllStringSubmatchIndex(text, -1) {
		if inSpans(code, m[0]) {
			continue
		}
		out = append(out, ProseRef{Start: m[0], End: m[1], Ref: text[m[2]:m[3]]})
	}
	return out
}

// ReplaceProseRefs returns text with each reference FindProseRefs finds
// replaced by fn's result (the full `[[…]]` replacement), or kept when fn
// returns "".
func ReplaceProseRefs(text string, fn func(ref string) string) string {
	var b strings.Builder
	last := 0
	for _, r := range FindProseRefs(text) {
		repl := fn(r.Ref)
		if repl == "" {
			continue
		}
		b.WriteString(text[last:r.Start])
		b.WriteString(repl)
		last = r.End
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}

// span is a half-open byte range [start, end).
type span struct{ start, end int }

func inSpans(spans []span, i int) bool {
	for _, s := range spans {
		if i >= s.start && i < s.end {
			return true
		}
	}
	return false
}

// codeSpans returns the byte ranges of text that Markdown renders as code:
// fenced code blocks (a line opening with three or more backticks or tildes,
// up to a closing fence of the same character at least as long, or the end
// of the text) and, outside them, inline code spans (a run of n backticks up
// to the next run of exactly n). An unmatched backtick run is literal text.
func codeSpans(text string) []span {
	var spans []span
	lineStart := 0
	proseStart := 0
	fence, fenceLen, fenceStart := byte(0), 0, 0
	for lineStart < len(text) {
		lineEnd := strings.IndexByte(text[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text)
		} else {
			lineEnd += lineStart + 1
		}
		ch, n, rest := fenceMarker(text[lineStart:lineEnd])
		switch {
		case fence == 0 && n >= 3:
			spans = append(spans, inlineCode(text, proseStart, lineStart)...)
			fence, fenceLen, fenceStart = ch, n, lineStart
		case fence != 0 && ch == fence && n >= fenceLen && strings.TrimSpace(rest) == "":
			spans = append(spans, span{fenceStart, lineEnd})
			fence, proseStart = 0, lineEnd
		}
		lineStart = lineEnd
	}
	if fence != 0 {
		return append(spans, span{fenceStart, len(text)})
	}
	return append(spans, inlineCode(text, proseStart, len(text))...)
}

// fenceMarker reports the backtick or tilde run that starts line (after up
// to three spaces of indentation) and the rest of the line after it, or a
// zero run when the line starts with neither.
func fenceMarker(line string) (ch byte, n int, rest string) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, ""
	}
	rest = strings.TrimLeft(trimmed, trimmed[:1])
	return trimmed[0], len(trimmed) - len(rest), rest
}

// inlineCode returns the inline code spans in text[from:to].
func inlineCode(text string, from, to int) []span {
	var spans []span
	for i := from; i < to; {
		if text[i] != '`' {
			i++
			continue
		}
		n := backtickRun(text, i, to)
		closeAt := -1
		for j := i + n; j < to; {
			if text[j] != '`' {
				j++
				continue
			}
			m := backtickRun(text, j, to)
			if m == n {
				closeAt = j
				break
			}
			j += m
		}
		if closeAt < 0 {
			i += n
			continue
		}
		spans = append(spans, span{i, closeAt + n})
		i = closeAt + n
	}
	return spans
}

func backtickRun(text string, i, to int) int {
	n := 0
	for i+n < to && text[i+n] == '`' {
		n++
	}
	return n
}
