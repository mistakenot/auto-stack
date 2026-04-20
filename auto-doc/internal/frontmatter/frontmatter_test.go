package frontmatter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	input := "---\nid: \"deadbeef\"\ntitle: \"Getting Started\"\nsummary: \"Setup instructions\"\nhash: \"a1b2c3d4\"\n---\n\n# Hello\n\nBody content.\n"
	doc := Parse(input)

	if doc.Id != "deadbeef" {
		t.Errorf("Id = %q, want %q", doc.Id, "deadbeef")
	}
	if doc.Title != "Getting Started" {
		t.Errorf("Title = %q, want %q", doc.Title, "Getting Started")
	}
	if doc.Summary != "Setup instructions" {
		t.Errorf("Summary = %q, want %q", doc.Summary, "Setup instructions")
	}
	if doc.Hash != "a1b2c3d4" {
		t.Errorf("Hash = %q, want %q", doc.Hash, "a1b2c3d4")
	}
	if !strings.Contains(doc.Body, "# Hello") {
		t.Errorf("Body missing expected content, got %q", doc.Body)
	}
}

func TestParseMissingFrontmatter(t *testing.T) {
	input := "# Just a heading\n\nNo frontmatter here.\n"
	doc := Parse(input)

	if doc.Title != "" {
		t.Errorf("Title = %q, want empty", doc.Title)
	}
	if doc.Summary != "" {
		t.Errorf("Summary = %q, want empty", doc.Summary)
	}
	if doc.Hash != "" {
		t.Errorf("Hash = %q, want empty", doc.Hash)
	}
}

func TestRoundTrip(t *testing.T) {
	original := Doc{
		Id:      "deadbeef",
		Title:   "Test Doc",
		Summary: "A test document",
		Hash:    "abcd1234",
		Body:    "\n# Content\n\nSome text.\n",
	}

	serialized := Serialize(&original)
	parsed := Parse(serialized)

	if parsed.Id != original.Id {
		t.Errorf("Id = %q, want %q", parsed.Id, original.Id)
	}
	if parsed.Title != original.Title {
		t.Errorf("Title = %q, want %q", parsed.Title, original.Title)
	}
	if parsed.Summary != original.Summary {
		t.Errorf("Summary = %q, want %q", parsed.Summary, original.Summary)
	}
	if parsed.Hash != original.Hash {
		t.Errorf("Hash = %q, want %q", parsed.Hash, original.Hash)
	}
	if parsed.Body != original.Body {
		t.Errorf("Body = %q, want %q", parsed.Body, original.Body)
	}
}

func TestComputeHash(t *testing.T) {
	doc := Doc{
		Title:   "Getting Started",
		Summary: "Setup instructions for new users",
		Body:    "\n# Hello\n\nSome content.\n",
	}
	hash := ComputeHash(&doc)

	if len(hash) != 8 {
		t.Errorf("hash length = %d, want 8", len(hash))
	}

	// Same input should produce same hash
	hash2 := ComputeHash(&doc)
	if hash != hash2 {
		t.Errorf("hash not deterministic: %q != %q", hash, hash2)
	}

	// Different input should produce different hash
	doc2 := Doc{Title: "Different Title", Summary: "Different summary", Body: "\n# Other\n"}
	hash3 := ComputeHash(&doc2)
	if hash == hash3 {
		t.Errorf("different docs produced same hash")
	}

	// Same frontmatter but different body should produce different hash
	doc3 := Doc{
		Title:   "Getting Started",
		Summary: "Setup instructions for new users",
		Body:    "\n# Hello\n\nDifferent content.\n",
	}
	hash4 := ComputeHash(&doc3)
	if hash == hash4 {
		t.Errorf("different body content produced same hash")
	}
}

func TestSerializeKeysAlphabetical(t *testing.T) {
	doc := Doc{Id: "deadbeef", Title: "Z Title", Summary: "A Summary", Hash: "12345678"}
	s := Serialize(&doc)

	hashIdx := strings.Index(s, "hash:")
	idIdx := strings.Index(s, "id:")
	summaryIdx := strings.Index(s, "summary:")
	titleIdx := strings.Index(s, "title:")

	if hashIdx > idIdx || idIdx > summaryIdx || summaryIdx > titleIdx {
		t.Errorf("keys not in alphabetical order: hash@%d, id@%d, summary@%d, title@%d", hashIdx, idIdx, summaryIdx, titleIdx)
	}
}

func TestSerializeOmitsEmptyId(t *testing.T) {
	doc := Doc{Title: "Title", Summary: "Summary", Hash: "12345678"}
	s := Serialize(&doc)
	if strings.Contains(s, "\nid:") {
		t.Fatalf("serialize should omit empty id, got:\n%s", s)
	}
}

func TestComputeHashIgnoresId(t *testing.T) {
	doc := Doc{
		Title:   "Getting Started",
		Summary: "Setup instructions for new users",
		Body:    "\n# Hello\n\nSome content.\n",
	}
	withID := doc
	withID.Id = "deadbeef"

	if got, want := ComputeHash(&withID), ComputeHash(&doc); got != want {
		t.Fatalf("ComputeHash should ignore id, got %q want %q", got, want)
	}
}

func TestParseReadWhen(t *testing.T) {
	input := "---\ntitle: \"Test\"\nsummary: \"A test\"\nread_when: \"modifying auth\"\nhash: \"12345678\"\n---\n\n# Body\n"
	doc := Parse(input)
	if doc.ReadWhen != "modifying auth" {
		t.Errorf("ReadWhen = %q, want %q", doc.ReadWhen, "modifying auth")
	}
}

func TestComputeHashIgnoresReadWhen(t *testing.T) {
	doc := Doc{Title: "Test", Summary: "A test", Body: "\n# Body\n"}
	withReadWhen := doc
	withReadWhen.ReadWhen = "modifying auth"
	if got, want := ComputeHash(&withReadWhen), ComputeHash(&doc); got != want {
		t.Fatalf("ComputeHash should ignore read_when, got %q want %q", got, want)
	}
}

func TestSerializeOmitsEmptyReadWhen(t *testing.T) {
	doc := Doc{Title: "Title", Summary: "Summary", Hash: "12345678"}
	s := Serialize(&doc)
	if strings.Contains(s, "read_when") {
		t.Fatalf("serialize should omit empty read_when, got:\n%s", s)
	}
}

func TestSerializeIncludesReadWhen(t *testing.T) {
	doc := Doc{Title: "Title", Summary: "Summary", ReadWhen: "testing", Hash: "12345678"}
	s := Serialize(&doc)
	parsed := Parse(s)
	if parsed.ReadWhen != "testing" {
		t.Fatalf("round-trip ReadWhen = %q, want %q", parsed.ReadWhen, "testing")
	}
}

func TestUpdateHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")

	content := "---\ntitle: \"Test\"\nsummary: \"A test doc\"\nhash: \"wronghash\"\n---\n\n# Content\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpdateHash(path); err != nil {
		t.Fatalf("UpdateHash: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	doc := Parse(string(data))
	expected := ComputeHash(&Doc{Title: "Test", Summary: "A test doc", Body: "\n# Content\n"})
	if doc.Hash != expected {
		t.Errorf("Hash = %q, want %q", doc.Hash, expected)
	}
}
