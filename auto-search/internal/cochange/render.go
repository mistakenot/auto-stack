package cochange

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Compact text renderer for `autosearch co-change` (AC-2 through AC-11). The
// renderer consumes the same *Result the JSON encoder uses, so it stays
// testable in isolation against synthetic fixtures. Output is UTF-8 with a
// closed set of permitted non-ASCII glyphs only: → (header date separator),
// × (co-commit count suffix), — (disclosure em dash), and … (subject
// truncation ellipsis). No other non-ASCII characters are emitted.

// RenderOptions controls the compact text rendering.
type RenderOptions struct {
	Budget int  // approximate token budget; <= 0 means use defaultBudget
	All    bool // emit every row, bypassing the budget
}

const (
	// defaultBudget is the approximate token budget applied when Budget <= 0.
	defaultBudget = 500
	// subjectCap is the maximum number of runes of a commit subject rendered
	// in the [sha "subject"] segment before it is truncated with an ellipsis.
	subjectCap = 60
)

// renderedRow pairs a fully rendered row line with its sort/trim attributes.
type renderedRow struct {
	line  string
	d     int     // directory tree-distance from the seed (AC-3)
	score float64 // normalized score in [0, 1], used as the trim tiebreak
}

// Render produces the compact text form of a co-change result: a two-line
// header, one line per related file, and an optional trailing disclosure line
// when budget truncation hid rows. It owns its own trailing newline.
func Render(r *Result, opts RenderOptions) string {
	m := &r.Metadata

	// AC-11 case 2: unknown file (no history at all). Emit just the seed path
	// and a dedicated no-history line; do NOT call renderHeader (it would emit
	// a spurious "0 commits" line).
	if m.TotalCommits == 0 {
		return m.ResolvedPath + "\nno history for this file\n"
	}

	header := renderHeader(m)

	// AC-11 case 1: insufficient history. Full header is appropriate because
	// TotalCommits > 0; append the literal status line and stop.
	if m.Warning == "insufficient history" {
		return header + "insufficient history\n"
	}

	// Normal case (AC-3/AC-4/AC-6/AC-7).
	if len(r.RelatedFiles) == 0 {
		return header
	}

	top := r.RelatedFiles[0].Score
	rows := make([]renderedRow, 0, len(r.RelatedFiles))
	for i := range r.RelatedFiles {
		rf := &r.RelatedFiles[i]
		norm := 0.0
		if top > 0 {
			norm = rf.Score / top
		}
		d := treeDistance(rf.Path, m.ResolvedPath)
		rows = append(rows, renderedRow{
			line:  renderRow(rf, m.ResolvedPath, norm),
			d:     d,
			score: norm,
		})
	}

	if opts.All {
		var b strings.Builder
		b.WriteString(header)
		for _, row := range rows {
			b.WriteString(row.line)
			b.WriteByte('\n')
		}
		return b.String()
	}

	effectiveBudget := opts.Budget
	if effectiveBudget <= 0 {
		effectiveBudget = defaultBudget
	}

	// AC-6 trim order: drop the lowest-(d, score) row first so every d>0 row
	// survives until all d0 rows are gone, and the weakest row within a
	// d-group goes first. We pick victims out of the rendered set repeatedly.
	// kept[i] tracks whether row i is still emitted.
	kept := make([]bool, len(rows))
	for i := range kept {
		kept[i] = true
	}

	// trimOrder lists row indices in the order they would be dropped.
	trimOrder := make([]int, len(rows))
	for i := range trimOrder {
		trimOrder[i] = i
	}
	sort.SliceStable(trimOrder, func(i, j int) bool {
		a, b := rows[trimOrder[i]], rows[trimOrder[j]]
		if a.d != b.d {
			return a.d < b.d
		}
		return a.score < b.score
	})

	hiddenCount := 0
	hiddenCrossDir := 0
	nextVictim := 0
	for {
		out := composeOutput(header, rows, kept, hiddenCount, hiddenCrossDir)
		if approxTokens(out) <= effectiveBudget {
			break
		}
		if nextVictim >= len(trimOrder) {
			// Nothing left to drop; emit what we have.
			break
		}
		victim := trimOrder[nextVictim]
		nextVictim++
		kept[victim] = false
		hiddenCount++
		if rows[victim].d > 0 {
			hiddenCrossDir++
		}
	}

	return composeOutput(header, rows, kept, hiddenCount, hiddenCrossDir)
}

// composeOutput assembles the header, the kept rows, and the disclosure line
// (when any rows are hidden) into the final string.
func composeOutput(header string, rows []renderedRow, kept []bool, hiddenCount, hiddenCrossDir int) string {
	var b strings.Builder
	b.WriteString(header)
	for i, row := range rows {
		if kept[i] {
			b.WriteString(row.line)
			b.WriteByte('\n')
		}
	}
	if disc := composeDisclosure(hiddenCount, hiddenCrossDir); disc != "" {
		b.WriteString(disc)
		b.WriteByte('\n')
	}
	return b.String()
}

// renderHeader builds the AC-2 two-line header. Line 1 is the resolved seed
// path; line 2 is "<N> commits" optionally followed by ", <first> → <last>".
// The date segment is omitted entirely when FirstTouched is empty; the
// " → <last>" tail is omitted when only FirstTouched is present.
func renderHeader(m *Metadata) string {
	var b strings.Builder
	b.WriteString(m.ResolvedPath)
	b.WriteByte('\n')
	b.WriteString(strconv.Itoa(m.TotalCommits))
	b.WriteString(" commits")
	if m.FirstTouched != "" {
		b.WriteString(", ")
		b.WriteString(m.FirstTouched)
		if m.LastTouched != "" {
			b.WriteString(" → ") // " → "
			b.WriteString(m.LastTouched)
		}
	}
	b.WriteByte('\n')
	return b.String()
}

// renderRow builds one AC-3 row: "<path>  <score>  <N>×  d<n>" with an optional
// trailing "  [<sha7> \"<subject>\"]" segment. The bracket segment is included
// only when d > 0 AND the related file has at least one sample commit; d0 rows
// never carry it.
func renderRow(rf *RelatedFile, seedPath string, norm float64) string {
	d := treeDistance(rf.Path, seedPath)
	var b strings.Builder
	b.WriteString(rf.Path)
	b.WriteString("  ")
	b.WriteString(strconv.FormatFloat(norm, 'f', 2, 64))
	b.WriteString("  ")
	b.WriteString(strconv.Itoa(rf.CoCommits))
	b.WriteString("×") // ×
	b.WriteString("  d")
	b.WriteString(strconv.Itoa(d))
	if d > 0 && len(rf.SampleCommits) > 0 {
		sc := rf.SampleCommits[0]
		sha7 := sc.SHA
		if len(sha7) > 7 {
			sha7 = sha7[:7]
		}
		b.WriteString("  [")
		b.WriteString(sha7)
		b.WriteString(" \"")
		b.WriteString(truncSubject(sc.Subject))
		b.WriteString("\"]")
	}
	return b.String()
}

// treeDistance returns the directory tree-distance between two full paths
// (AC-3): the number of "up" segments from the row's directory to the lowest
// common ancestor plus "down" segments back to the seed's directory. The
// basename is dropped from each argument before computing. Same dir = 0;
// sibling dirs sharing a parent = 2.
func treeDistance(rowPath, seedPath string) int {
	rowDir := dirSegments(rowPath)
	seedDir := dirSegments(seedPath)
	p := 0
	for p < len(rowDir) && p < len(seedDir) && rowDir[p] == seedDir[p] {
		p++
	}
	return (len(rowDir) - p) + (len(seedDir) - p)
}

// dirSegments splits a forward-slash path into its directory segments, dropping
// the basename. Empty segments (from leading/trailing/double slashes) are
// dropped so paths like "src/a/" and "src/a/x.go" share segments cleanly.
func dirSegments(path string) []string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return nil
	}
	dir := path[:idx]
	parts := strings.Split(dir, "/")
	segs := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			segs = append(segs, p)
		}
	}
	return segs
}

// approxTokens approximates the token cost of s as ceil(runes / 4). It counts
// runes (NOT bytes) so the permitted multi-byte glyphs (→ × — …) each count as
// one rune per AC-8.
func approxTokens(s string) int {
	return (utf8.RuneCountInString(s) + 3) / 4
}

// truncSubject caps s at subjectCap runes, appending a single-rune ellipsis on
// overflow.
func truncSubject(s string) string {
	// sc.Subject is sourced from the message_truncated column, which holds the
	// full commit message (subject + body) and may carry MidTruncate's embedded
	// "\n…[truncated]…\n" marker. Take only the first line so the bracket segment
	// can never split a row across lines (AC-3's one-line-per-file contract).
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if utf8.RuneCountInString(s) <= subjectCap {
		return s
	}
	runes := []rune(s)
	return string(runes[:subjectCap]) + "…" // …
}

// composeDisclosure builds the AC-7 trailing disclosure line, or "" when
// nothing was hidden. characterization is "all same-dir siblings" when every
// hidden row was d0, else "incl. <K> cross-dir" where K is the number of hidden
// d>0 rows.
func composeDisclosure(hiddenCount, hiddenCrossDir int) string {
	if hiddenCount == 0 {
		return ""
	}
	var characterization string
	if hiddenCrossDir == 0 {
		characterization = "all same-dir siblings"
	} else {
		characterization = fmt.Sprintf("incl. %d cross-dir", hiddenCrossDir)
	}
	return fmt.Sprintf("%d more hidden (%s) — run with --all", hiddenCount, characterization)
}
