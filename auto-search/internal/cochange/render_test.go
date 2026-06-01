package cochange

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// newResult is a small fixture helper that builds a normal-case *Result with a
// known seed path and the given related files.
func newResult(resolvedPath string, totalCommits int, first, last string, rels []RelatedFile) *Result {
	return &Result{
		Metadata: Metadata{
			ResolvedPath: resolvedPath,
			TotalCommits: totalCommits,
			FirstTouched: first,
			LastTouched:  last,
		},
		RelatedFiles: rels,
	}
}

func TestRenderHeader(t *testing.T) {
	t.Run("full date range", func(t *testing.T) {
		m := &Metadata{ResolvedPath: "src/a/hot.go", TotalCommits: 9, FirstTouched: "2026-04-15", LastTouched: "2026-05-08"}
		got := renderHeader(m)
		want := "src/a/hot.go\n9 commits, 2026-04-15 → 2026-05-08\n"
		if got != want {
			t.Fatalf("renderHeader =\n%q\nwant\n%q", got, want)
		}
	})
	t.Run("no dates", func(t *testing.T) {
		m := &Metadata{ResolvedPath: "src/a/hot.go", TotalCommits: 3}
		got := renderHeader(m)
		want := "src/a/hot.go\n3 commits\n"
		if got != want {
			t.Fatalf("renderHeader =\n%q\nwant\n%q", got, want)
		}
	})
	t.Run("first only, no last", func(t *testing.T) {
		m := &Metadata{ResolvedPath: "src/a/hot.go", TotalCommits: 4, FirstTouched: "2026-04-15"}
		got := renderHeader(m)
		want := "src/a/hot.go\n4 commits, 2026-04-15\n"
		if got != want {
			t.Fatalf("renderHeader =\n%q\nwant\n%q", got, want)
		}
	})
}

func TestRenderRow_D0_NoSampleCommit(t *testing.T) {
	rf := &RelatedFile{
		Path:      "src/a/sibling.go",
		CoCommits: 7,
		SampleCommits: []SampleCommitJSON{
			{SHA: "abcdef1234567", Subject: "should not appear on d0"},
		},
	}
	got := renderRow(rf, "src/a/hot.go", 1.0)
	want := "src/a/sibling.go  1.00  7×  d0"
	if got != want {
		t.Fatalf("renderRow d0 =\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(got, "[") {
		t.Fatalf("d0 row must not include the [sha \"subject\"] segment: %q", got)
	}
}

func TestRenderRow_DPositive_IncludesSampleCommit(t *testing.T) {
	rf := &RelatedFile{
		Path:      "src/b/other.go",
		CoCommits: 5,
		SampleCommits: []SampleCommitJSON{
			{SHA: "abcdef1234567", Subject: "fix coupling"},
		},
	}
	got := renderRow(rf, "src/a/hot.go", 0.65)
	want := `src/b/other.go  0.65  5×  d2  [abcdef1 "fix coupling"]`
	if got != want {
		t.Fatalf("renderRow d>0 =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderRow_DPositive_NoSampleCommit_OmitsBracket(t *testing.T) {
	rf := &RelatedFile{
		Path:          "src/b/other.go",
		CoCommits:     5,
		SampleCommits: nil,
	}
	got := renderRow(rf, "src/a/hot.go", 0.42)
	want := "src/b/other.go  0.42  5×  d2"
	if got != want {
		t.Fatalf("renderRow d>0 no-sample =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderRow_MultiLineSubject_StaysOneLine(t *testing.T) {
	// message_truncated holds the full commit message (subject + body) and may
	// carry MidTruncate's embedded "\n…[truncated]…\n" marker; the rendered row
	// must remain a single line regardless.
	rf := &RelatedFile{
		Path:      "src/b/other.go",
		CoCommits: 5,
		SampleCommits: []SampleCommitJSON{
			{SHA: "abcdef1234567", Subject: "fix coupling\n\nlong body line that would split the row\nsecond body line"},
		},
	}
	got := renderRow(rf, "src/a/hot.go", 0.65)
	if strings.Contains(got, "\n") {
		t.Fatalf("renderRow with multi-line subject must be one line, got:\n%q", got)
	}
	want := `src/b/other.go  0.65  5×  d2  [abcdef1 "fix coupling"]`
	if got != want {
		t.Fatalf("renderRow multi-line subject =\n%q\nwant\n%q", got, want)
	}
}

func TestTreeDistance(t *testing.T) {
	cases := []struct {
		name    string
		rowPath string
		seed    string
		want    int
	}{
		{"same dir", "src/a/sibling.go", "src/a/hot.go", 0},
		{"sibling dir", "src/b/x.go", "src/a/hot.go", 2},
		{"deeper child", "src/a/b/x.go", "src/a/hot.go", 1},
		{"two up two down", "pkg/util/x.go", "src/a/hot.go", 4},
		{"different top level", "infra/pipeline/runner.go", "src/a/main.go", 4},
		{"root files same dir", "main.go", "root.go", 0},
		{"row at root vs nested seed", "top.go", "src/a/hot.go", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := treeDistance(c.rowPath, c.seed); got != c.want {
				t.Fatalf("treeDistance(%q,%q)=%d want %d", c.rowPath, c.seed, got, c.want)
			}
		})
	}
}

func TestScoreNormalization_TopRowIsOne(t *testing.T) {
	rels := []RelatedFile{
		{Path: "src/a/x.go", Score: 8.0, CoCommits: 10},
		{Path: "src/a/y.go", Score: 4.0, CoCommits: 6},
		{Path: "src/a/z.go", Score: 0.32, CoCommits: 3},
	}
	r := newResult("src/a/hot.go", 9, "2026-01-01", "2026-02-01", rels)
	out := Render(r, RenderOptions{All: true})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// lines[0] = path, lines[1] = commits header, lines[2..] = rows
	if len(lines) < 5 {
		t.Fatalf("expected header + 3 rows, got %d lines:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[2], " 1.00 ") {
		t.Fatalf("top row should normalize to 1.00: %q", lines[2])
	}
	if !strings.Contains(lines[3], " 0.50 ") {
		t.Fatalf("second row should be 0.50: %q", lines[3])
	}
	if !strings.Contains(lines[4], " 0.04 ") {
		t.Fatalf("third row should be 0.04: %q", lines[4])
	}
}

func TestApproxTokensCharsDiv4(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"abcdefgh", 2},
		// multi-byte glyphs each count as one rune, not their byte length.
		{"→×—…", 1},      // 4 runes -> ceil(4/4) = 1
		{"→×—…→×—…→", 3}, // 9 runes -> ceil(9/4) = 3
	}
	for _, c := range cases {
		if got := approxTokens(c.in); got != c.want {
			t.Fatalf("approxTokens(%q)=%d want %d", c.in, got, c.want)
		}
	}
	// Sanity: the glyph string has more bytes than runes, proving rune-count.
	g := "→×—…"
	if len(g) == utf8.RuneCountInString(g) {
		t.Fatalf("expected multi-byte glyphs (bytes != runes), bytes=%d runes=%d", len(g), utf8.RuneCountInString(g))
	}
}

func TestBudget_FullFit_NoDisclosure(t *testing.T) {
	rels := []RelatedFile{
		{Path: "src/a/x.go", Score: 8.0, CoCommits: 10},
		{Path: "src/a/y.go", Score: 4.0, CoCommits: 6},
	}
	r := newResult("src/a/hot.go", 9, "2026-01-01", "2026-02-01", rels)
	out := Render(r, RenderOptions{Budget: 500})
	if strings.Contains(out, "more hidden") {
		t.Fatalf("small result should fit budget with no disclosure:\n%s", out)
	}
	if !strings.Contains(out, "src/a/x.go") || !strings.Contains(out, "src/a/y.go") {
		t.Fatalf("both rows should be present:\n%s", out)
	}
}

func TestBudget_TrimDropsD0BeforeDPositive(t *testing.T) {
	// Mix of d0 and d>0 rows; a tiny budget forces trimming. The d>0 row must
	// survive while d0 rows are dropped.
	rels := []RelatedFile{
		{Path: "src/a/s1.go", Score: 9.0, CoCommits: 9},
		{Path: "src/a/s2.go", Score: 8.0, CoCommits: 8},
		{Path: "src/a/s3.go", Score: 7.0, CoCommits: 7},
		{Path: "src/b/cross.go", Score: 1.0, CoCommits: 3,
			SampleCommits: []SampleCommitJSON{{SHA: "deadbee1234", Subject: "cross-dir coupling"}}},
	}
	r := newResult("src/a/hot.go", 20, "2026-01-01", "2026-02-01", rels)
	out := Render(r, RenderOptions{Budget: 45})
	if !strings.Contains(out, "src/b/cross.go") {
		t.Fatalf("cross-dir (d2) row must survive trimming:\n%s", out)
	}
	if strings.Contains(out, "src/a/s1.go") && strings.Contains(out, "src/a/s2.go") && strings.Contains(out, "src/a/s3.go") {
		t.Fatalf("expected some d0 rows dropped under tight budget:\n%s", out)
	}
	if !strings.Contains(out, "more hidden") {
		t.Fatalf("expected disclosure line when rows trimmed:\n%s", out)
	}
}

func TestBudget_TrimAllRows(t *testing.T) {
	rels := []RelatedFile{
		{Path: "src/a/s1.go", Score: 9.0, CoCommits: 9},
		{Path: "src/a/s2.go", Score: 8.0, CoCommits: 8},
		{Path: "src/a/s3.go", Score: 7.0, CoCommits: 7},
	}
	r := newResult("src/a/hot.go", 20, "2026-01-01", "2026-02-01", rels)
	// Budget large enough only for header + disclosure, not for any row.
	out := Render(r, RenderOptions{Budget: 12})
	if strings.Contains(out, "src/a/s1.go") || strings.Contains(out, "src/a/s2.go") || strings.Contains(out, "src/a/s3.go") {
		t.Fatalf("expected all rows trimmed:\n%s", out)
	}
	if !strings.Contains(out, "3 more hidden") {
		t.Fatalf("expected disclosure of 3 hidden rows:\n%s", out)
	}
	if !strings.HasPrefix(out, "src/a/hot.go\n") {
		t.Fatalf("header must still be present:\n%s", out)
	}
}

func TestDisclosure_AllSameDirWording(t *testing.T) {
	got := composeDisclosure(5, 0)
	want := "5 more hidden (all same-dir siblings) — run with --all"
	if got != want {
		t.Fatalf("composeDisclosure =\n%q\nwant\n%q", got, want)
	}
}

func TestDisclosure_InclCrossDirWording(t *testing.T) {
	got := composeDisclosure(6, 2)
	want := "6 more hidden (incl. 2 cross-dir) — run with --all"
	if got != want {
		t.Fatalf("composeDisclosure =\n%q\nwant\n%q", got, want)
	}
}

func TestDisclosure_OmittedWhenNothingTrimmed(t *testing.T) {
	if got := composeDisclosure(0, 0); got != "" {
		t.Fatalf("composeDisclosure(0,0) should be empty, got %q", got)
	}
}

func TestRender_AllBypassesBudget(t *testing.T) {
	rels := make([]RelatedFile, 0, 30)
	for i := range 30 {
		rels = append(rels, RelatedFile{
			Path:      "src/a/file" + strconv.Itoa(i) + ".go",
			Score:     float64(30 - i),
			CoCommits: 30 - i,
		})
	}
	r := newResult("src/a/hot.go", 50, "2026-01-01", "2026-02-01", rels)
	out := Render(r, RenderOptions{Budget: 1, All: true})
	if strings.Contains(out, "more hidden") {
		t.Fatalf("--all must not emit a disclosure line:\n%s", out)
	}
	// All 30 rows must be present.
	for i := range 30 {
		p := "src/a/file" + strconv.Itoa(i) + ".go"
		if !strings.Contains(out, p) {
			t.Fatalf("--all should emit every row; missing %q:\n%s", p, out)
		}
	}
}

func TestRender_InsufficientHistoryTextLine(t *testing.T) {
	m := Metadata{
		ResolvedPath: "src/a/rare.go",
		TotalCommits: 2,
		Warning:      "insufficient history",
	}
	r := &Result{Metadata: m}
	out := Render(r, RenderOptions{})
	want := "src/a/rare.go\n2 commits\ninsufficient history\n"
	if out != want {
		t.Fatalf("insufficient-history render =\n%q\nwant\n%q", out, want)
	}
}

func TestRender_NoHistoryTextLine(t *testing.T) {
	r := &Result{Metadata: Metadata{ResolvedPath: "src/missing.go", TotalCommits: 0}}
	out := Render(r, RenderOptions{})
	want := "src/missing.go\nno history for this file\n"
	if out != want {
		t.Fatalf("no-history render =\n%q\nwant\n%q", out, want)
	}
	if strings.Contains(out, "commits") {
		t.Fatalf("no-history output must not mention commit count:\n%s", out)
	}
}
