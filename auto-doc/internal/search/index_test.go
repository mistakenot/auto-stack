package search_test

import (
	"os"
	"testing"

	"github.com/datadyne-io/autodoc/internal/search"
)

// TestOpenIndexCreatesDirectory verifies that OpenIndex creates the index directory.
func TestOpenIndexCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	indexPath := dir + "/myindex"

	idx, err := search.OpenIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenIndex: unexpected error: %v", err)
	}
	defer idx.Close()

	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Error("OpenIndex did not create the index directory")
	}
}

// TestIndexExistsFalseForMissing verifies IndexExists returns false for non-existent path.
func TestIndexExistsFalseForMissing(t *testing.T) {
	dir := t.TempDir()
	if search.IndexExists(dir + "/nonexistent") {
		t.Error("IndexExists should return false for non-existent path")
	}
}

// TestIndexExistsTrueAfterOpen verifies IndexExists returns true after creating index.
func TestIndexExistsTrueAfterOpen(t *testing.T) {
	dir := t.TempDir()
	indexPath := dir + "/myindex"

	idx, err := search.OpenIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	idx.Close()

	if !search.IndexExists(indexPath) {
		t.Error("IndexExists should return true after creating index")
	}
}

// TestCloseNoError verifies Close does not return an error.
func TestCloseNoError(t *testing.T) {
	dir := t.TempDir()
	idx, err := search.OpenIndex(dir + "/idx")
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Errorf("Close returned unexpected error: %v", err)
	}
}

// TestUpsertAndSearch verifies that indexing a document makes it findable.
func TestUpsertAndSearch(t *testing.T) {
	dir := t.TempDir()
	idx, err := search.OpenIndex(dir + "/idx")
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	err = idx.UpsertDoc("docs/auth.md", "Authentication", "How to authenticate API requests", "Setup Auth Token", "Configure authentication by setting the API key.")
	if err != nil {
		t.Fatalf("UpsertDoc: %v", err)
	}

	results, err := idx.Search("authentication", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Search returned no results, expected 1")
	}

	r := results[0]
	if r.Path != "docs/auth.md" {
		t.Errorf("Path: got %q, want %q", r.Path, "docs/auth.md")
	}
	if r.Title != "Authentication" {
		t.Errorf("Title: got %q, want %q", r.Title, "Authentication")
	}
	if r.Summary != "How to authenticate API requests" {
		t.Errorf("Summary: got %q, want %q", r.Summary, "How to authenticate API requests")
	}
	if r.Score <= 0 {
		t.Errorf("Score should be positive, got %f", r.Score)
	}
}

// TestSearchNoMatchesReturnsEmptySlice verifies that no matches returns an empty (non-nil) slice.
func TestSearchNoMatchesReturnsEmptySlice(t *testing.T) {
	dir := t.TempDir()
	idx, err := search.OpenIndex(dir + "/idx")
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	results, err := idx.Search("zzzzzznotfound", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if results == nil {
		t.Error("Search returned nil, want empty slice")
	}
	if len(results) != 0 {
		t.Errorf("Search returned %d results, want 0", len(results))
	}
}

// TestSearchResultsSortedByScore verifies results are returned in descending score order.
func TestSearchResultsSortedByScore(t *testing.T) {
	dir := t.TempDir()
	idx, err := search.OpenIndex(dir + "/idx")
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	// auth.md mentions authentication many times (high score)
	err = idx.UpsertDoc("docs/auth.md", "Authentication", "Authentication guide", "Auth Overview", "Authentication is core. Authentication tokens. Authentication setup. Authentication required.")
	if err != nil {
		t.Fatalf("UpsertDoc auth.md: %v", err)
	}
	// getting-started.md mentions authentication once (lower score)
	err = idx.UpsertDoc("docs/getting-started.md", "Getting Started", "Setup instructions", "Start Here", "Welcome. Authentication is required before making API calls.")
	if err != nil {
		t.Fatalf("UpsertDoc getting-started.md: %v", err)
	}

	results, err := idx.Search("authentication", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("Expected at least 2 results, got %d", len(results))
	}

	if results[0].Score < results[1].Score {
		t.Errorf("Results not sorted by score: results[0].Score=%f < results[1].Score=%f", results[0].Score, results[1].Score)
	}
}

// TestDeleteDocRemovesFromResults verifies DeleteDoc removes a document from search results.
func TestDeleteDocRemovesFromResults(t *testing.T) {
	dir := t.TempDir()
	idx, err := search.OpenIndex(dir + "/idx")
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	err = idx.UpsertDoc("docs/auth.md", "Authentication", "How to authenticate", "", "Authentication setup guide.")
	if err != nil {
		t.Fatalf("UpsertDoc: %v", err)
	}

	// Verify it's findable before deletion
	results, err := idx.Search("authentication", 10)
	if err != nil {
		t.Fatalf("Search before delete: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Expected result before delete, got none")
	}

	// Delete
	if err := idx.DeleteDoc("docs/auth.md"); err != nil {
		t.Fatalf("DeleteDoc: %v", err)
	}

	// Should not be findable after deletion
	results, err = idx.Search("authentication", 10)
	if err != nil {
		t.Fatalf("Search after delete: %v", err)
	}
	for _, r := range results {
		if r.Path == "docs/auth.md" {
			t.Errorf("DeleteDoc did not remove the document from search results")
		}
	}
}

// TestMultiWordQueryMatchesBothTerms verifies multi-word queries match documents with both terms.
func TestMultiWordQueryMatchesBothTerms(t *testing.T) {
	dir := t.TempDir()
	idx, err := search.OpenIndex(dir + "/idx")
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	err = idx.UpsertDoc("docs/auth.md", "Authentication Setup", "Authentication setup guide", "", "This document covers authentication setup for new users.")
	if err != nil {
		t.Fatalf("UpsertDoc: %v", err)
	}
	err = idx.UpsertDoc("docs/other.md", "Unrelated", "Completely unrelated document", "", "Nothing matching here.")
	if err != nil {
		t.Fatalf("UpsertDoc other.md: %v", err)
	}

	results, err := idx.Search("authentication setup", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	found := false
	for _, r := range results {
		if r.Path == "docs/auth.md" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Multi-word query did not find docs/auth.md")
	}
}

// TestSearchRespectsLimit verifies the limit parameter is honored.
func TestSearchRespectsLimit(t *testing.T) {
	dir := t.TempDir()
	idx, err := search.OpenIndex(dir + "/idx")
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	// Index 5 documents all matching the query
	for i := range 5 {
		path := "docs/doc" + string(rune('0'+i)) + ".md"
		err = idx.UpsertDoc(path, "Config Guide", "Config setup", "Config", "Configuration settings and config options.")
		if err != nil {
			t.Fatalf("UpsertDoc %s: %v", path, err)
		}
	}

	results, err := idx.Search("config", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) > 3 {
		t.Errorf("Search returned %d results, want at most 3", len(results))
	}
}

// TestUpsertDocTwiceNoDuplicates verifies upserting the same path twice does not create duplicates.
func TestUpsertDocTwiceNoDuplicates(t *testing.T) {
	dir := t.TempDir()
	idx, err := search.OpenIndex(dir + "/idx")
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	err = idx.UpsertDoc("docs/auth.md", "Authentication v1", "Original summary", "", "Original body content.")
	if err != nil {
		t.Fatalf("UpsertDoc first: %v", err)
	}

	err = idx.UpsertDoc("docs/auth.md", "Authentication v2", "Updated summary", "", "Updated body content.")
	if err != nil {
		t.Fatalf("UpsertDoc second: %v", err)
	}

	results, err := idx.Search("authentication", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	count := 0
	for _, r := range results {
		if r.Path == "docs/auth.md" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected 1 result for docs/auth.md after upsert, got %d", count)
	}

	// The result should reflect the updated document
	for _, r := range results {
		if r.Path == "docs/auth.md" {
			if r.Title != "Authentication v2" {
				t.Errorf("Title after upsert: got %q, want %q", r.Title, "Authentication v2")
			}
		}
	}
}

// TestSnippetIsNonEmptyWhenBodyPresent verifies the snippet field is populated.
func TestSnippetIsNonEmptyWhenBodyPresent(t *testing.T) {
	dir := t.TempDir()
	idx, err := search.OpenIndex(dir + "/idx")
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	body := "Configure authentication by setting the API key in your project settings."
	err = idx.UpsertDoc("docs/auth.md", "Auth", "Auth guide", "", body)
	if err != nil {
		t.Fatalf("UpsertDoc: %v", err)
	}

	results, err := idx.Search("authentication", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Expected result, got none")
	}
	if results[0].Snippet == "" {
		t.Error("Snippet should not be empty when body is present")
	}
}

func TestListDocPathsReturnsSortedPaths(t *testing.T) {
	dir := t.TempDir()
	idx, err := search.OpenIndex(dir + "/idx")
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	if err := idx.UpsertDoc("auto-etl/docs/b.md", "B", "B", "", "B body"); err != nil {
		t.Fatalf("UpsertDoc b: %v", err)
	}
	if err := idx.UpsertDoc("docs/a.md", "A", "A", "", "A body"); err != nil {
		t.Fatalf("UpsertDoc a: %v", err)
	}

	paths, err := idx.ListDocPaths()
	if err != nil {
		t.Fatalf("ListDocPaths: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d paths, want 2", len(paths))
	}
	if paths[0] != "auto-etl/docs/b.md" || paths[1] != "docs/a.md" {
		t.Fatalf("unexpected sorted paths: %v", paths)
	}
}
