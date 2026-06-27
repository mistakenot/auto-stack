package render

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// heading is a parsed ATX heading: its line index, level (number of leading #),
// and cleaned heading text.
type heading struct {
	line  int
	level int
	text  string
}

// extractSection extracts the heading-bounded section selected by path from an
// LF-normalized, frontmatter-stripped markdown body.
//
// Matching is a best-effort cascade per path element: exact normalized
// (casefold(collapse_ws(trim))) heading text, then GitHub-style slug. A
// multi-element path walks parent → child headings to disambiguate. Multiple
// final matches take the FIRST in document order and emit a warning naming the
// collision; zero matches is a hard error (CodeSectionNotFound).
//
// Extraction itself is fixed and code-fence-aware: a `#` inside a ``` or ~~~
// fenced block is not a heading. The extent runs from the matched heading line
// to the next heading with level ≤ the matched level (or EOF). includeHeading
// keeps or drops the matched heading line. The returned content is the raw
// joined extent; the caller canonicalizes and hashes it.
func extractSection(body string, path []string, includeHeading bool) (content, matchedHeading string, warnings []string, err error) {
	lines := strings.Split(body, "\n")
	headings := parseHeadings(lines)

	matches := findMatches(headings, path)
	matches = dedupeSortHeadings(matches)
	target := strings.Join(path, " > ")
	if len(matches) == 0 {
		return "", "", nil, &FileRefError{
			ErrCode: CodeSectionNotFound,
			File:    "",
			Message: fmt.Sprintf("%s: section %q matched no heading; check the section name (matching is exact-normalized then GitHub-slug) or remove the selector", CodeSectionNotFound, target),
		}
	}

	chosen := matches[0]
	if len(matches) > 1 {
		var locs []string
		for _, m := range matches {
			locs = append(locs, fmt.Sprintf("line %d", m.line+1))
		}
		warnings = append(warnings, fmt.Sprintf(
			"ambiguous section %q: %d headings match (%s); using the first at line %d",
			target, len(matches), strings.Join(locs, ", "), chosen.line+1))
	}

	// Extent: matched heading line → next heading with level ≤ matched level.
	end := len(lines)
	for _, h := range headings {
		if h.line > chosen.line && h.level <= chosen.level {
			end = h.line
			break
		}
	}
	start := chosen.line
	if !includeHeading {
		start = chosen.line + 1
	}
	if start > end {
		start = end
	}

	content = strings.Join(lines[start:end], "\n")
	return content, chosen.text, warnings, nil
}

// findMatches returns the headings matching path, narrowing scope by parent
// subtree for each non-final element. Order roughly follows document order;
// the caller sorts and dedupes.
func findMatches(headings []heading, path []string) []heading {
	if len(path) == 0 {
		return nil
	}
	cur := matchHeads(headings, path[0])
	if len(path) == 1 {
		return cur
	}
	var out []heading
	for _, parent := range cur {
		out = append(out, findMatches(subtreeOf(headings, parent), path[1:])...)
	}
	return out
}

// matchHeads returns headings matching name via the exact-normalized cascade,
// falling back to GitHub-slug matching only when no exact match exists.
func matchHeads(headings []heading, name string) []heading {
	norm := normalizeHeading(name)
	var exact []heading
	for _, h := range headings {
		if normalizeHeading(h.text) == norm {
			exact = append(exact, h)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	sl := slug(name)
	if sl == "" {
		return nil
	}
	var bySlug []heading
	for _, h := range headings {
		if slug(h.text) == sl {
			bySlug = append(bySlug, h)
		}
	}
	return bySlug
}

// subtreeOf returns the headings nested under parent: those following it with a
// strictly greater level, up to (but excluding) the first heading with level ≤
// parent's level. headings must be in document order and contain parent.
func subtreeOf(headings []heading, parent heading) []heading {
	var sub []heading
	started := false
	for _, h := range headings {
		if !started {
			if h.line == parent.line {
				started = true
			}
			continue
		}
		if h.level <= parent.level {
			break
		}
		sub = append(sub, h)
	}
	return sub
}

// dedupeSortHeadings sorts headings by line and removes duplicates by line.
func dedupeSortHeadings(hs []heading) []heading {
	sort.Slice(hs, func(i, j int) bool { return hs[i].line < hs[j].line })
	out := hs[:0]
	last := -1
	for _, h := range hs {
		if h.line == last {
			continue
		}
		out = append(out, h)
		last = h.line
	}
	return out
}

// parseHeadings scans lines for ATX headings, skipping any line inside a fenced
// code block (``` or ~~~). A `#` inside a fence is never a heading.
func parseHeadings(lines []string) []heading {
	var hs []heading
	fence := "" // "" outside a fence, else the active fence marker
	for i, line := range lines {
		s := strings.TrimLeft(line, " ")
		if fence != "" {
			if isFenceClose(s, fence) {
				fence = ""
			}
			continue
		}
		if m := fenceMarker(s); m != "" {
			fence = m
			continue
		}
		if lvl, text, ok := atxHeading(s); ok {
			hs = append(hs, heading{line: i, level: lvl, text: text})
		}
	}
	return hs
}

// fenceMarker returns the opening code-fence marker for a line, or "".
func fenceMarker(s string) string {
	switch {
	case strings.HasPrefix(s, "```"):
		return "```"
	case strings.HasPrefix(s, "~~~"):
		return "~~~"
	default:
		return ""
	}
}

// isFenceClose reports whether s is a closing fence for marker: a run of the
// marker character with no trailing info string.
func isFenceClose(s, marker string) bool {
	if !strings.HasPrefix(s, marker) {
		return false
	}
	ch := marker[0]
	i := 0
	for i < len(s) && s[i] == ch {
		i++
	}
	return strings.TrimSpace(s[i:]) == ""
}

// atxHeading parses an ATX heading line (1–6 leading #s followed by whitespace
// or end of line), returning its level and cleaned text with any closing # run
// removed. Returns ok=false for non-headings.
func atxHeading(s string) (level int, text string, ok bool) {
	i := 0
	for i < len(s) && s[i] == '#' {
		i++
	}
	if i == 0 || i > 6 {
		return 0, "", false
	}
	if i < len(s) && s[i] != ' ' && s[i] != '\t' {
		return 0, "", false
	}
	t := strings.TrimSpace(s[i:])
	// Strip a trailing closing-# run, but only when it is preceded by whitespace
	// (CommonMark) so an identifier like "C#" keeps its #.
	n := len(t)
	k := n
	for k > 0 && t[k-1] == '#' {
		k--
	}
	if k < n {
		if k == 0 {
			t = ""
		} else if t[k-1] == ' ' || t[k-1] == '\t' {
			t = strings.TrimRight(t[:k], " \t")
		}
	}
	return i, t, true
}

// normalizeHeading is the exact-match normalization: casefold(collapse_ws(trim)).
func normalizeHeading(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// slug renders a GitHub-style heading slug: lowercase, whitespace collapsed to
// single hyphens, punctuation dropped, keeping [a-z0-9-].
func slug(s string) string {
	s = strings.Join(strings.Fields(strings.ToLower(s)), " ")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ' ' || unicode.IsSpace(r):
			b.WriteByte('-')
		}
	}
	return b.String()
}
