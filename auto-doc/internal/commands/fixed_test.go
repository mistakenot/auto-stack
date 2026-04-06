package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/frontmatter"
	"github.com/datadyne-io/autodoc/internal/search"
	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestFixed(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	path := ws.WriteDoc("test.md", "Test Doc", "A test document", "# Content")

	if err := Fixed(path, "", ""); err != nil {
		t.Fatalf("Fixed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	doc := frontmatter.Parse(string(data))
	expected := frontmatter.ComputeHash(&frontmatter.Doc{Title: "Test Doc", Summary: "A test document", Body: "\n# Content\n"})
	if doc.Hash != expected {
		t.Errorf("Hash = %q, want %q", doc.Hash, expected)
	}
}

func TestFixedSortsKeysAlphabetically(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	path := ws.WriteDoc("test.md", "Test", "Summary", "# Body")

	if err := Fixed(path, "", ""); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	hashIdx := strings.Index(content, "hash:")
	summaryIdx := strings.Index(content, "summary:")
	titleIdx := strings.Index(content, "title:")

	if hashIdx > summaryIdx || summaryIdx > titleIdx {
		t.Errorf("keys not alphabetical: hash@%d, summary@%d, title@%d", hashIdx, summaryIdx, titleIdx)
	}
}

func TestFixedUpdatesSearchIndex(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	path := ws.WriteDoc("indexed.md", "Indexed Doc", "A searchable document", "# Unique Quux Content")
	indexPath := ws.Path(".auto/doc/index")

	// Create an empty index first, then close it.
	idx, err := search.OpenIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := Fixed(path, indexPath, "docs/indexed.md"); err != nil {
		t.Fatalf("Fixed: %v", err)
	}

	// Reopen and search for unique content.
	idx2, err := search.OpenIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenIndex (read): %v", err)
	}
	defer idx2.Close()

	results, err := idx2.Search("Quux", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected search results, got none")
	}
	if results[0].Title != "Indexed Doc" {
		t.Errorf("Title = %q, want %q", results[0].Title, "Indexed Doc")
	}
}

func TestFixedSkipsWhenNoIndex(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	path := ws.WriteDoc("test.md", "Test", "Summary", "# Body")

	if err := Fixed(path, "/nonexistent/path/index", "docs/test.md"); err != nil {
		t.Fatalf("Fixed with nonexistent index should not error: %v", err)
	}
}

func TestFixedSkipsWhenEmptyIndexPath(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	path := ws.WriteDoc("test.md", "Test", "Summary", "# Body")

	if err := Fixed(path, "", ""); err != nil {
		t.Fatalf("Fixed with empty index path should not error: %v", err)
	}
}
