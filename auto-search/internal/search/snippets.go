package search

import (
	"strings"
	"unicode/utf8"

	"github.com/mistakenot/auto-search/internal/query"
)

const defaultSnippetWindow = 300

// Snippet finds the best matching region in text for the given query terms,
// returning the snippet text with start and end byte offsets into the original text.
// If highlight is true, matched terms are wrapped in ** markers in the snippet
// but offsets still refer to the raw text.
func Snippet(text string, terms []string, highlight bool) (snippet string, start, end int) {
	if len(text) == 0 || len(terms) == 0 {
		limit := min(defaultSnippetWindow, len(text))
		return text[:limit], 0, limit
	}

	lowerText := strings.ToLower(text)

	// Find earliest match position.
	bestPos := len(text) // sentinel: no match found
	for _, term := range terms {
		lowerTerm := strings.ToLower(term)
		idx := strings.Index(lowerText, lowerTerm)
		if idx >= 0 && idx < bestPos {
			bestPos = idx
		}
	}
	if bestPos == len(text) {
		bestPos = 0
	}

	// Expand window around bestPos.
	halfWindow := defaultSnippetWindow / 2
	start = max(bestPos-halfWindow, 0)
	end = start + defaultSnippetWindow
	if end > len(text) {
		end = len(text)
		start = max(end-defaultSnippetWindow, 0)
	}

	// Align to UTF-8 character boundaries.
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}

	raw := text[start:end]

	if !highlight {
		return raw, start, end
	}

	// Highlight: inject ** markers.
	snippet = highlightTerms(raw, terms)
	return snippet, start, end
}

// highlightTerms wraps first occurrence of each term with ** markers.
func highlightTerms(text string, terms []string) string {
	result := text
	lowerResult := strings.ToLower(result)
	for _, term := range terms {
		lowerTerm := strings.ToLower(term)
		idx := strings.Index(lowerResult, lowerTerm)
		if idx >= 0 {
			result = result[:idx] + "**" + result[idx:idx+len(term)] + "**" + result[idx+len(term):]
			// Re-lowercase after mutation to keep positions correct for subsequent terms.
			lowerResult = strings.ToLower(result)
		}
	}
	return result
}

// TruncateAtRune trims s to at most maxBytes bytes plus, if truncation
// occurred, an ellipsis ("…"). The byte boundary is advanced forward
// to the next UTF-8 rune start so the result is always valid UTF-8 —
// callers do not have to worry about slicing through a multi-byte
// sequence and producing mojibake on terminals or U+FFFD substitution
// in json.Encoder output. The returned length may exceed maxBytes by
// up to 3 bytes (the max non-leading byte count of a UTF-8 sequence).
func TruncateAtRune(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end < len(s) && !utf8.RuneStart(s[end]) {
		end++
	}
	return s[:end] + "…"
}

// ExtractTerms collects the positive search terms from a parsed query AST
// for use in snippet generation.
func ExtractTerms(node *query.Node) []string {
	var terms []string
	extract(node, &terms, false)
	return terms
}

func extract(n *query.Node, terms *[]string, negated bool) {
	if n == nil {
		return
	}
	switch n.Type {
	case query.NodeTerm, query.NodePhrase:
		if !negated {
			// Strip quotes just in case, though parser usually handles them
			val := strings.Trim(n.Value, `"`)
			if val != "" {
				*terms = append(*terms, val)
			}
		}
	case query.NodeAnd, query.NodeOr:
		extract(n.Left, terms, negated)
		extract(n.Right, terms, negated)
	case query.NodeNot:
		// Toggle negation state for the operand?
		// "NOT (A AND B)" -> NOT A, NOT B?
		// No, standard boolean logic: NOT (A OR B) -> NOT A AND NOT B.
		// NOT (A AND B) -> NOT A OR NOT B.
		// But for highlighting, we just want to know if a term is "positive" (must appear) or "negative" (must not appear).
		// If it's negative, we don't highlight it.
		// In FTS5, NOT is unary operator.
		extract(n.Right, terms, !negated)
	}
}
