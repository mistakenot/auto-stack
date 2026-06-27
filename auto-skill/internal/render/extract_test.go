package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fencedFixture exercises code-fence awareness, multi-match collisions, nested
// path disambiguation, and extent boundaries.
const fencedFixture = `# Top

intro line

## Setup

setup body

` + "```" + `bash
# this is a comment inside a fence, NOT a heading
## neither is this
` + "```" + `

more setup text

### Nested

nested body

## Setup

second setup section (collides with the first)

# Other

## Install

install under Other

### Nested

other-nested body
`

func writeFixture(t *testing.T, body string) FileRefResolver {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "doc.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewFileRefResolver(root)
}

func TestExtractFenceAwareAndExtent(t *testing.T) {
	r := writeFixture(t, fencedFixture)
	// "Setup" is ambiguous (two of them); the first runs to the next level-≤2
	// heading. The `#`/`##` lines inside the fence must NOT be treated as
	// headings, so the extent does not stop early at them, and "Nested" (level 3)
	// does not stop a level-2 section.
	res, err := r.Resolve(FileRef{File: "doc.md", Section: []string{"Setup"}, IncludeHeading: true})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.HasPrefix(res.Content, "## Setup\n") {
		t.Fatalf("expected content to start with the matched heading, got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "comment inside a fence") {
		t.Fatalf("fenced code block should be inside the extent:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "### Nested") {
		t.Fatalf("level-3 subheading should be inside the level-2 extent:\n%s", res.Content)
	}
	// Extent stops at the SECOND "## Setup" (level 2), before "# Other".
	if strings.Contains(res.Content, "second setup section") {
		t.Fatalf("extent should stop at the next level-≤2 heading:\n%s", res.Content)
	}
	if res.MatchedHeading != "Setup" {
		t.Fatalf("MatchedHeading = %q, want Setup", res.MatchedHeading)
	}
	// A `#` inside the fence was not parsed as a heading; if it had been, the
	// level-1 "# this is a comment" would have truncated the section.
	if !strings.Contains(res.Content, "more setup text") {
		t.Fatalf("text after the fence should be in the extent:\n%s", res.Content)
	}
}

func TestExtractMultiMatchWarns(t *testing.T) {
	r := writeFixture(t, fencedFixture)
	res, err := r.Resolve(FileRef{File: "doc.md", Section: []string{"Setup"}, IncludeHeading: true})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatalf("expected an ambiguity warning for the duplicate Setup heading")
	}
	if !strings.Contains(res.Warnings[0], "ambiguous section") {
		t.Fatalf("warning should name the collision, got %q", res.Warnings[0])
	}
	// First-in-document-order: the first Setup, not the second.
	if strings.Contains(res.Content, "second setup section") {
		t.Fatalf("multi-match must take the first in document order")
	}
	if !strings.Contains(res.Content, "setup body") {
		t.Fatalf("expected the first Setup section body")
	}
}

func TestExtractZeroMatchIsHardError(t *testing.T) {
	r := writeFixture(t, fencedFixture)
	_, err := r.Resolve(FileRef{File: "doc.md", Section: []string{"Nonexistent"}})
	if got := codeOf(t, err); got != CodeSectionNotFound {
		t.Fatalf("got code %q, want %q (err=%v)", got, CodeSectionNotFound, err)
	}
}

func TestExtractSlugCascade(t *testing.T) {
	body := "# API Reference (v2)\n\nbody under api\n\n# Other\n\nx\n"
	r := writeFixture(t, body)
	// No exact normalized match for "api-reference-v2", but GitHub slug matches
	// "API Reference (v2)" -> "api-reference-v2".
	res, err := r.Resolve(FileRef{File: "doc.md", Section: []string{"api-reference-v2"}})
	if err != nil {
		t.Fatalf("slug match should resolve: %v", err)
	}
	if !strings.Contains(res.Content, "body under api") {
		t.Fatalf("expected the slug-matched section body, got:\n%s", res.Content)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("single slug match should not warn, got %v", res.Warnings)
	}
}

func TestExtractPathDisambiguation(t *testing.T) {
	r := writeFixture(t, fencedFixture)
	// Bare "Nested" is ambiguous (under Setup and under Install). The path
	// ["Install","Nested"] disambiguates to the Nested under Install.
	res, err := r.Resolve(FileRef{File: "doc.md", Section: []string{"Install", "Nested"}, IncludeHeading: true})
	if err != nil {
		t.Fatalf("path resolve: %v", err)
	}
	if !strings.Contains(res.Content, "other-nested body") {
		t.Fatalf("path form should select the Nested under Install, got:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "nested body\n") && !strings.Contains(res.Content, "other-nested body") {
		t.Fatalf("path form selected the wrong Nested")
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("disambiguated path should not warn, got %v", res.Warnings)
	}
}

func TestExtractIncludeHeadingToggle(t *testing.T) {
	body := "# A\n\nalpha\n\n# B\n\nbeta\n"
	r := writeFixture(t, body)
	with, err := r.Resolve(FileRef{File: "doc.md", Section: []string{"A"}, IncludeHeading: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(with.Content, "# A\n") {
		t.Fatalf("include_heading=true should keep the heading line, got:\n%s", with.Content)
	}
	without, err := r.Resolve(FileRef{File: "doc.md", Section: []string{"A"}, IncludeHeading: false})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(without.Content, "# A") {
		t.Fatalf("include_heading=false should drop the heading line, got:\n%s", without.Content)
	}
	if !strings.Contains(without.Content, "alpha") {
		t.Fatalf("body should remain when heading dropped, got:\n%s", without.Content)
	}
}

func TestExtractContentHashScoping(t *testing.T) {
	base := "# Keep\n\nstable body\n\n# Edit Me\n\noriginal\n"
	r1 := writeFixture(t, base)
	before, err := r1.Resolve(FileRef{File: "doc.md", Section: []string{"Keep"}, IncludeHeading: true})
	if err != nil {
		t.Fatal(err)
	}

	// Editing an UNRELATED section must NOT move the matched section's hash.
	edited := "# Keep\n\nstable body\n\n# Edit Me\n\nCOMPLETELY DIFFERENT\n"
	r2 := writeFixture(t, edited)
	after, err := r2.Resolve(FileRef{File: "doc.md", Section: []string{"Keep"}, IncludeHeading: true})
	if err != nil {
		t.Fatal(err)
	}
	if after.ContentHash != before.ContentHash {
		t.Fatalf("editing an unrelated section moved the content_hash: %s -> %s", before.ContentHash, after.ContentHash)
	}

	// Renaming the matched heading must fail loud (zero match).
	renamed := "# Kept\n\nstable body\n\n# Edit Me\n\noriginal\n"
	r3 := writeFixture(t, renamed)
	if _, err := r3.Resolve(FileRef{File: "doc.md", Section: []string{"Keep"}}); codeOf(t, err) != CodeSectionNotFound {
		t.Fatalf("renaming the matched heading should fail with %q", CodeSectionNotFound)
	}
}
