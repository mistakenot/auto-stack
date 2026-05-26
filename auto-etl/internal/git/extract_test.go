package git

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseForEachRef(t *testing.T) {
	output := strings.Join([]string{
		"refs/heads/main\x00abc123def456\x00commit\x00",
		"refs/heads/feature-x\x00def789abc012\x00commit\x00",
		"refs/remotes/origin/main\x00abc123def456\x00commit\x00",
		"refs/tags/v1.0\x00111222333444\x00tag\x00",
	}, "\n")

	refs := parseForEachRef(output, "repo1", "run1", 1716681600000, "refs/remotes/origin/main")

	if len(refs) != 4 {
		t.Fatalf("expected 4 refs, got %d", len(refs))
	}

	// Check branch ref.
	r := refs[0]
	if r.RefName != "refs/heads/main" {
		t.Errorf("expected refs/heads/main, got %s", r.RefName)
	}
	if r.RefType != "branch" {
		t.Errorf("expected branch, got %s", r.RefType)
	}
	if r.IsRemote {
		t.Error("branch should not be remote")
	}
	if r.IsDefault {
		t.Error("refs/heads/main should not be default (default is refs/remotes/origin/main)")
	}
	if r.CommitID != "abc123def456" {
		t.Errorf("expected abc123def456, got %s", r.CommitID)
	}
	if r.RepoID != "repo1" {
		t.Errorf("expected repo1, got %s", r.RepoID)
	}

	// Check remote ref is default.
	r = refs[2]
	if r.RefType != "remote_branch" {
		t.Errorf("expected remote_branch, got %s", r.RefType)
	}
	if !r.IsRemote {
		t.Error("remote ref should be remote")
	}
	if !r.IsDefault {
		t.Error("refs/remotes/origin/main should be default")
	}

	// Check tag.
	r = refs[3]
	if r.RefType != "tag" {
		t.Errorf("expected tag, got %s", r.RefType)
	}
	if r.IsRemote {
		t.Error("tag should not be remote")
	}

	// Check ID format.
	expectedID := "repo1-refs/heads/main-1716681600000"
	if refs[0].ID != expectedID {
		t.Errorf("expected ID %s, got %s", expectedID, refs[0].ID)
	}
}

func TestParseForEachRefEmpty(t *testing.T) {
	refs := parseForEachRef("", "repo1", "run1", 1000, "")
	if refs != nil {
		t.Errorf("expected nil, got %d refs", len(refs))
	}
}

func TestParseCommitLog(t *testing.T) {
	// Build a record: fields separated by NUL, records by double-NUL.
	record1 := strings.Join([]string{
		"aaaa1111bbbb2222cccc3333dddd4444eeee5555", // SHA
		"aaaa1111", // short SHA
		"tttt1111bbbb2222cccc3333dddd4444eeee5555", // tree SHA
		"Alice",                     // author name
		"alice@example.com",         // author email
		"2024-05-25T12:30:00+00:00", // author date
		"Bob",                       // committer name
		"bob@example.com",           // committer email
		"2024-05-25T12:35:00+00:00", // committer date
		"pppp1111bbbb2222cccc3333dddd4444eeee5555",                                                  // parents
		"feat: add something\n\nThis is the body.\n\nCo-Authored-By: Charlie <charlie@example.com>", // body
	}, "\x00")

	output := record1 + "\x00\x00"

	commits := parseCommitLog(output, "repo1", "run1", 1716681600000)

	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}

	c := commits[0]
	if c.ID != "repo1-aaaa1111bbbb2222cccc3333dddd4444eeee5555" {
		t.Errorf("unexpected ID: %s", c.ID)
	}
	if c.ShortID != "aaaa1111" {
		t.Errorf("expected short ID aaaa1111, got %s", c.ShortID)
	}
	if c.AuthorName != "Alice" {
		t.Errorf("expected Alice, got %s", c.AuthorName)
	}
	if c.AuthorEmail != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %s", c.AuthorEmail)
	}
	if c.CommitterName != "Bob" {
		t.Errorf("expected Bob, got %s", c.CommitterName)
	}
	if c.IsMerge {
		t.Error("single parent should not be merge")
	}
	if c.ParentCount != 1 {
		t.Errorf("expected parent_count=1, got %d", c.ParentCount)
	}
	if c.ParentSHAs != "pppp1111bbbb2222cccc3333dddd4444eeee5555" {
		t.Errorf("unexpected parent_shas: %s", c.ParentSHAs)
	}
	if c.AuthorDate == 0 {
		t.Error("author_date should not be zero")
	}
	if c.AuthorDateOffset != "+00:00" {
		t.Errorf("expected +00:00 offset, got %s", c.AuthorDateOffset)
	}
	if c.Year != 2024 || c.Month != 5 {
		t.Errorf("expected year=2024 month=5, got year=%d month=%d", c.Year, c.Month)
	}
	if c.PatchID != "" {
		t.Errorf("patch_id should be empty for v1, got %s", c.PatchID)
	}

	// Check trailers.
	var trailers map[string][]string
	if err := json.Unmarshal([]byte(c.TrailersJSON), &trailers); err != nil {
		t.Fatalf("failed to parse trailers JSON: %v", err)
	}
	if coAuthored, ok := trailers["Co-Authored-By"]; !ok || len(coAuthored) != 1 {
		t.Errorf("expected Co-Authored-By trailer, got %v", trailers)
	} else if coAuthored[0] != "Charlie <charlie@example.com>" {
		t.Errorf("unexpected Co-Authored-By value: %s", coAuthored[0])
	}
}

func TestParseCommitLogMerge(t *testing.T) {
	record := strings.Join([]string{
		"aaaa1111bbbb2222cccc3333dddd4444eeee5555",
		"aaaa1111",
		"tttt1111bbbb2222cccc3333dddd4444eeee5555",
		"Alice",
		"alice@example.com",
		"2024-05-25T12:30:00+00:00",
		"Alice",
		"alice@example.com",
		"2024-05-25T12:30:00+00:00",
		"pppp1111bbbb2222cccc3333dddd4444eeee5555 pppp2222bbbb3333cccc4444dddd5555eeee6666",
		"Merge branch 'feature' into main",
	}, "\x00")

	output := record + "\x00\x00"
	commits := parseCommitLog(output, "repo1", "run1", 1000)

	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}

	c := commits[0]
	if !c.IsMerge {
		t.Error("expected is_merge=true")
	}
	if c.ParentCount != 2 {
		t.Errorf("expected parent_count=2, got %d", c.ParentCount)
	}
	expected := "pppp1111bbbb2222cccc3333dddd4444eeee5555,pppp2222bbbb3333cccc4444dddd5555eeee6666"
	if c.ParentSHAs != expected {
		t.Errorf("expected parent_shas=%s, got %s", expected, c.ParentSHAs)
	}
}

func TestParseCommitLogEmpty(t *testing.T) {
	commits := parseCommitLog("", "repo1", "run1", 1000)
	if commits != nil {
		t.Errorf("expected nil, got %d commits", len(commits))
	}
}

func TestParseDiffTreeRaw(t *testing.T) {
	// Simulate: :100644 100644 abc123 def456 M\0path/to/file.go\0
	output := ":100644 100644 abc1234 def5678 M\x00path/to/file.go\x00"

	entries := parseDiffTreeRaw(output)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.oldMode != "100644" {
		t.Errorf("expected oldMode 100644, got %s", e.oldMode)
	}
	if e.newMode != "100644" {
		t.Errorf("expected newMode 100644, got %s", e.newMode)
	}
	if e.oldBlob != "abc1234" {
		t.Errorf("expected oldBlob abc1234, got %s", e.oldBlob)
	}
	if e.newBlob != "def5678" {
		t.Errorf("expected newBlob def5678, got %s", e.newBlob)
	}
	if e.changeType != "M" {
		t.Errorf("expected changeType M, got %s", e.changeType)
	}
	if e.path != "path/to/file.go" {
		t.Errorf("expected path/to/file.go, got %s", e.path)
	}
	if e.oldPath != "" {
		t.Errorf("expected empty oldPath, got %s", e.oldPath)
	}
}

func TestParseDiffTreeRawRename(t *testing.T) {
	// Rename: :100644 100644 abc123 def456 R100\0old/path.go\0new/path.go\0
	output := ":100644 100644 abc1234 def5678 R100\x00old/path.go\x00new/path.go\x00"

	entries := parseDiffTreeRaw(output)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.changeType != "R" {
		t.Errorf("expected changeType R, got %s", e.changeType)
	}
	if e.oldPath != "old/path.go" {
		t.Errorf("expected oldPath old/path.go, got %s", e.oldPath)
	}
	if e.path != "new/path.go" {
		t.Errorf("expected path new/path.go, got %s", e.path)
	}
}

func TestParseDiffTreeRawWithSHAHeader(t *testing.T) {
	// diff-tree output starts with the commit SHA on the first line.
	output := "aaaa1111bbbb2222cccc3333dddd4444eeee5555\x00:100644 100644 abc1234 def5678 M\x00file.go\x00"

	entries := parseDiffTreeRaw(output)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].path != "file.go" {
		t.Errorf("expected file.go, got %s", entries[0].path)
	}
}

func TestParseDiffTreeNumstat(t *testing.T) {
	// Standard numstat: "10\t5\tpath/to/file.go"
	output := "10\t5\tpath/to/file.go\x00"

	entries := parseDiffTreeNumstat(output)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.insertions != 10 {
		t.Errorf("expected 10 insertions, got %d", e.insertions)
	}
	if e.deletions != 5 {
		t.Errorf("expected 5 deletions, got %d", e.deletions)
	}
	if e.isBinary {
		t.Error("should not be binary")
	}
}

func TestParseDiffTreeNumstatBinary(t *testing.T) {
	output := "-\t-\timage.png\x00"

	entries := parseDiffTreeNumstat(output)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if !entries[0].isBinary {
		t.Error("expected binary=true")
	}
	if entries[0].insertions != 0 {
		t.Error("binary insertions should be 0")
	}
}

func TestParseUnifiedDiff(t *testing.T) {
	output := `diff --git a/file1.go b/file1.go
index abc..def 100644
--- a/file1.go
+++ b/file1.go
@@ -1,3 +1,4 @@
 line1
+added
 line2
 line3
diff --git a/file2.go b/file2.go
index 111..222 100644
--- a/file2.go
+++ b/file2.go
@@ -10,3 +10,3 @@
 unchanged
-removed
+replaced
 unchanged
`

	result := parseUnifiedDiff(output)

	if len(result) != 2 {
		t.Fatalf("expected 2 files, got %d", len(result))
	}

	if _, ok := result["file1.go"]; !ok {
		t.Error("expected file1.go in result")
	}
	if _, ok := result["file2.go"]; !ok {
		t.Error("expected file2.go in result")
	}

	// Check that the diff text starts with "diff --git".
	if !strings.HasPrefix(result["file1.go"], "diff --git") {
		t.Errorf("expected diff to start with 'diff --git', got: %s", result["file1.go"][:30])
	}
}

func TestParseHunks(t *testing.T) {
	diffText := `diff --git a/file.go b/file.go
index abc..def 100644
--- a/file.go
+++ b/file.go
@@ -1,3 +1,4 @@
 line1
+added
 line2
 line3
@@ -10,2 +11,3 @@
 unchanged
+new1
+new2
`

	hunks := parseHunks(diffText)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}

	h1 := hunks[0]
	if h1.oldStart != 1 || h1.oldLines != 3 || h1.newStart != 1 || h1.newLines != 4 {
		t.Errorf("hunk 1 range: old=%d,%d new=%d,%d", h1.oldStart, h1.oldLines, h1.newStart, h1.newLines)
	}
	if !strings.HasPrefix(h1.header, "@@ -1,3 +1,4 @@") {
		t.Errorf("unexpected header: %s", h1.header)
	}
	if !strings.Contains(h1.text, "+added") {
		t.Error("hunk 1 text should contain +added")
	}

	h2 := hunks[1]
	if h2.oldStart != 10 || h2.oldLines != 2 || h2.newStart != 11 || h2.newLines != 3 {
		t.Errorf("hunk 2 range: old=%d,%d new=%d,%d", h2.oldStart, h2.oldLines, h2.newStart, h2.newLines)
	}
}

func TestParseHunkHeader(t *testing.T) {
	tests := []struct {
		header                                 string
		oldStart, oldLines, newStart, newLines int32
	}{
		{"@@ -1,3 +1,4 @@", 1, 3, 1, 4},
		{"@@ -10,2 +11,3 @@ func foo()", 10, 2, 11, 3},
		{"@@ -1 +1 @@", 1, 1, 1, 1}, // single-line hunks (no comma)
		{"@@ -0,0 +1,5 @@", 0, 0, 1, 5},
		{"not a hunk header", 0, 0, 0, 0},
	}

	for _, tt := range tests {
		os, ol, ns, nl := parseHunkHeader(tt.header)
		if os != tt.oldStart || ol != tt.oldLines || ns != tt.newStart || nl != tt.newLines {
			t.Errorf("parseHunkHeader(%q) = %d,%d,%d,%d; want %d,%d,%d,%d",
				tt.header, os, ol, ns, nl, tt.oldStart, tt.oldLines, tt.newStart, tt.newLines)
		}
	}
}

func TestParseTrailers(t *testing.T) {
	body := `feat: add something

This is the body.

Co-Authored-By: Alice <alice@example.com>
Co-Authored-By: Bob <bob@example.com>
Signed-off-by: Charlie <charlie@example.com>`

	result := parseTrailers(body)

	var trailers map[string][]string
	if err := json.Unmarshal([]byte(result), &trailers); err != nil {
		t.Fatalf("failed to parse trailers JSON: %v", err)
	}

	coAuthored := trailers["Co-Authored-By"]
	if len(coAuthored) != 2 {
		t.Fatalf("expected 2 Co-Authored-By, got %d", len(coAuthored))
	}
	if coAuthored[0] != "Alice <alice@example.com>" {
		t.Errorf("unexpected first Co-Authored-By: %s", coAuthored[0])
	}
	if coAuthored[1] != "Bob <bob@example.com>" {
		t.Errorf("unexpected second Co-Authored-By: %s", coAuthored[1])
	}

	signedOff := trailers["Signed-off-by"]
	if len(signedOff) != 1 || signedOff[0] != "Charlie <charlie@example.com>" {
		t.Errorf("unexpected Signed-off-by: %v", signedOff)
	}
}

func TestParseTrailersEmpty(t *testing.T) {
	result := parseTrailers("just a message with no trailers")
	if result != "{}" {
		t.Errorf("expected {}, got %s", result)
	}
}

func TestHunkHashDeterministic(t *testing.T) {
	text1 := "@@ -1,3 +1,4 @@\n line1\n+added\n line2\n line3"
	text2 := "@@ -1,3 +1,4 @@\n line1\n+added\n line2\n line3"

	h1 := computeHunkHash(text1)
	h2 := computeHunkHash(text2)

	if h1 != h2 {
		t.Errorf("same input should produce same hash: %s != %s", h1, h2)
	}

	// Different text should produce different hash.
	h3 := computeHunkHash("@@ -1,3 +1,4 @@\n line1\n+different\n line2")
	if h1 == h3 {
		t.Error("different input should produce different hash")
	}
}

func TestHunkHashWhitespaceNormalization(t *testing.T) {
	text1 := "+  added   line"
	text2 := "+ added line"

	h1 := computeHunkHash(text1)
	h2 := computeHunkHash(text2)

	if h1 != h2 {
		t.Errorf("whitespace-normalized text should produce same hash: %s != %s", h1, h2)
	}
}

func TestConvertSinceToGit(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"5m", "5.minutes.ago"},
		{"2h", "2.hours.ago"},
		{"5d", "5.days.ago"},
		{"3w", "3.weeks.ago"},
		{"6mo", "6.months.ago"},
		{"1y", "1.years.ago"},
		{"", ""},
		{"invalid", "invalid"},
	}

	for _, tt := range tests {
		got := convertSinceToGit(tt.input)
		if got != tt.expected {
			t.Errorf("convertSinceToGit(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestParseISO8601(t *testing.T) {
	ms, offset := parseISO8601("2024-05-25T12:30:00+00:00")
	if ms == 0 {
		t.Error("expected non-zero milliseconds")
	}
	if offset != "+00:00" {
		t.Errorf("expected +00:00, got %s", offset)
	}

	// With timezone offset.
	ms2, offset2 := parseISO8601("2024-05-25T14:30:00+02:00")
	if ms2 == 0 {
		t.Error("expected non-zero milliseconds")
	}
	if offset2 != "+02:00" {
		t.Errorf("expected +02:00, got %s", offset2)
	}

	// Same instant should have the same milliseconds regardless of offset.
	if ms != ms2 {
		t.Errorf("same instant should produce same ms: %d != %d", ms, ms2)
	}

	// Empty string.
	ms3, offset3 := parseISO8601("")
	if ms3 != 0 || offset3 != "" {
		t.Error("empty string should return zero values")
	}
}

func TestExtractBPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"a/file.go b/file.go", "file.go"},
		{"a/path/to/file.go b/path/to/file.go", "path/to/file.go"},
		{"a/old name.go b/new name.go", "new name.go"},
		{"no b path", ""},
	}

	for _, tt := range tests {
		got := extractBPath(tt.input)
		if got != tt.expected {
			t.Errorf("extractBPath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestParseDiffTreeRawMultipleEntries(t *testing.T) {
	// Two modified files.
	output := ":100644 100644 aaa1111 bbb2222 M\x00file1.go\x00:100644 100644 ccc3333 ddd4444 A\x00file2.go\x00"

	entries := parseDiffTreeRaw(output)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].changeType != "M" || entries[0].path != "file1.go" {
		t.Errorf("entry 0: type=%s path=%s", entries[0].changeType, entries[0].path)
	}
	if entries[1].changeType != "A" || entries[1].path != "file2.go" {
		t.Errorf("entry 1: type=%s path=%s", entries[1].changeType, entries[1].path)
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  hello   world  ", "hello world"},
		{"\t\nhello\t\tworld\n", "hello world"},
		{"no extra spaces", "no extra spaces"},
		{"", ""},
	}

	for _, tt := range tests {
		got := normalizeWhitespace(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeWhitespace(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExtractSessionID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with Session-Id trailer",
			input:    `{"Session-Id":["550e8400-e29b-41d4-a716-446655440000"]}`,
			expected: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:     "empty trailers",
			input:    "{}",
			expected: "",
		},
		{
			name:     "multiple Session-Id values returns first",
			input:    `{"Session-Id":["first-uuid","second-uuid"]}`,
			expected: "first-uuid",
		},
		{
			name:     "invalid JSON",
			input:    "not json",
			expected: "",
		},
		{
			name:     "other trailers but no Session-Id",
			input:    `{"Co-Authored-By":["Alice <alice@example.com>"]}`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSessionID(tt.input)
			if got != tt.expected {
				t.Errorf("extractSessionID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseCommitLogSessionID(t *testing.T) {
	// Commit with Session-Id trailer.
	recordWithSession := strings.Join([]string{
		"aaaa1111bbbb2222cccc3333dddd4444eeee5555", // SHA
		"aaaa1111",                                 // short SHA
		"tttt1111bbbb2222cccc3333dddd4444eeee5555", // tree SHA
		"Alice",                                    // author name
		"alice@example.com",                        // author email
		"2024-05-25T12:30:00+00:00",                // author date
		"Bob",                                      // committer name
		"bob@example.com",                          // committer email
		"2024-05-25T12:35:00+00:00",                // committer date
		"pppp1111bbbb2222cccc3333dddd4444eeee5555",  // parents
		"feat: add something\n\nThis is the body.\n\nSession-Id: test-uuid-123", // body with Session-Id trailer
	}, "\x00")

	// Commit without Session-Id trailer.
	recordWithout := strings.Join([]string{
		"bbbb2222cccc3333dddd4444eeee5555ffff6666", // SHA
		"bbbb2222",                                 // short SHA
		"uuuu2222cccc3333dddd4444eeee5555ffff6666", // tree SHA
		"Charlie",                                  // author name
		"charlie@example.com",                      // author email
		"2024-05-25T13:00:00+00:00",                // author date
		"Charlie",                                  // committer name
		"charlie@example.com",                      // committer email
		"2024-05-25T13:00:00+00:00",                // committer date
		"aaaa1111bbbb2222cccc3333dddd4444eeee5555",  // parents
		"fix: plain commit with no trailers",        // body without trailers
	}, "\x00")

	output := recordWithSession + "\x00\x00" + recordWithout + "\x00\x00"
	commits := parseCommitLog(output, "repo1", "run1", 1716681600000)

	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}

	// AC-1: Commit with Session-Id trailer should have SessionID populated.
	if commits[0].SessionID != "test-uuid-123" {
		t.Errorf("expected SessionID 'test-uuid-123', got %q", commits[0].SessionID)
	}

	// AC-5: Commit without Session-Id trailer should have empty SessionID.
	if commits[1].SessionID != "" {
		t.Errorf("expected empty SessionID, got %q", commits[1].SessionID)
	}
}

func TestParseCommitLogMultipleRecords(t *testing.T) {
	makeRecord := func(sha, msg string) string {
		return strings.Join([]string{
			sha,
			sha[:8],
			"tree" + sha[:36],
			"Author",
			"a@example.com",
			"2024-01-15T10:00:00+00:00",
			"Author",
			"a@example.com",
			"2024-01-15T10:00:00+00:00",
			"parentsha1234567890123456789012345678",
			msg,
		}, "\x00")
	}

	output := makeRecord("aaaa1111bbbb2222cccc3333dddd4444eeee5555", "first commit") +
		"\x00\x00" +
		makeRecord("ffff6666gggg7777hhhh8888iiii9999jjjj0000", "second commit") +
		"\x00\x00"

	commits := parseCommitLog(output, "repo1", "run1", 1000)
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].Message != "first commit" {
		t.Errorf("expected 'first commit', got %q", commits[0].Message)
	}
	if commits[1].Message != "second commit" {
		t.Errorf("expected 'second commit', got %q", commits[1].Message)
	}
}
