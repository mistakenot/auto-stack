package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/search"
)

// setupSearchIndex creates a temp Bluge index, inserts docs, and returns the index path.
func setupSearchIndex(t *testing.T, docs []struct{ path, title, summary, body string }) string {
	t.Helper()
	dir := t.TempDir()
	indexPath := dir + "/index"

	idx, err := search.OpenIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx.Close()

	for _, d := range docs {
		if err := idx.UpsertDoc(d.path, d.title, d.summary, "", d.body); err != nil {
			t.Fatalf("UpsertDoc %s: %v", d.path, err)
		}
	}

	return indexPath
}

// TestSearchKeywordReturnsValidJSON verifies the output is a valid JSON array.
func TestSearchKeywordReturnsValidJSON(t *testing.T) {
	indexPath := setupSearchIndex(t, []struct{ path, title, summary, body string }{
		{"docs/auth.md", "Authentication", "How to authenticate", "Configure authentication by setting the API key."},
	})

	var buf bytes.Buffer
	err := SearchKeyword(&buf, indexPath, "authentication")
	if err != nil {
		t.Fatalf("SearchKeyword: %v", err)
	}

	var results []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
}

// TestSearchKeywordResultsContainExpectedFields verifies score, path, title, summary, snippet fields.
func TestSearchKeywordResultsContainExpectedFields(t *testing.T) {
	indexPath := setupSearchIndex(t, []struct{ path, title, summary, body string }{
		{"docs/auth.md", "Authentication", "Auth guide", "Configure authentication by setting the API key."},
	})

	var buf bytes.Buffer
	if err := SearchKeyword(&buf, indexPath, "authentication"); err != nil {
		t.Fatalf("SearchKeyword: %v", err)
	}

	var results []search.SearchResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	r := results[0]
	if r.Score <= 0 {
		t.Errorf("Score should be > 0, got %f", r.Score)
	}
	if r.Path != "docs/auth.md" {
		t.Errorf("Path: got %q, want %q", r.Path, "docs/auth.md")
	}
	if r.Title != "Authentication" {
		t.Errorf("Title: got %q, want %q", r.Title, "Authentication")
	}
	if r.Summary != "Auth guide" {
		t.Errorf("Summary: got %q, want %q", r.Summary, "Auth guide")
	}
	if r.Snippet == "" {
		t.Error("Snippet should not be empty when body is present")
	}
}

// TestSearchKeywordReturnsEmptyArrayOnNoMatch verifies empty array (not null) when no docs match.
func TestSearchKeywordReturnsEmptyArrayOnNoMatch(t *testing.T) {
	indexPath := setupSearchIndex(t, []struct{ path, title, summary, body string }{
		{"docs/auth.md", "Authentication", "Auth guide", "Configure authentication."},
	})

	var buf bytes.Buffer
	if err := SearchKeyword(&buf, indexPath, "zzzznotfound"); err != nil {
		t.Fatalf("SearchKeyword: %v", err)
	}

	output := strings.TrimSpace(buf.String())
	if output != "[]" {
		t.Errorf("expected empty array [], got: %s", output)
	}
}

// TestSearchKeywordErrorWhenIndexMissing verifies an error containing "reindex" is returned.
func TestSearchKeywordErrorWhenIndexMissing(t *testing.T) {
	dir := t.TempDir()
	nonexistent := dir + "/does-not-exist"

	var buf bytes.Buffer
	err := SearchKeyword(&buf, nonexistent, "anything")
	if err == nil {
		t.Fatal("expected error for missing index, got nil")
	}
	if !strings.Contains(err.Error(), "reindex") {
		t.Errorf("error should mention 'reindex', got: %v", err)
	}
}

// TestSearchKeywordMultiWordQuery verifies multi-word queries work correctly.
func TestSearchKeywordMultiWordQuery(t *testing.T) {
	indexPath := setupSearchIndex(t, []struct{ path, title, summary, body string }{
		{"docs/auth.md", "Authentication Setup", "Auth setup guide", "This document covers authentication setup for new users."},
		{"docs/other.md", "Unrelated", "Completely different", "Nothing matching here at all."},
	})

	var buf bytes.Buffer
	if err := SearchKeyword(&buf, indexPath, "authentication setup"); err != nil {
		t.Fatalf("SearchKeyword: %v", err)
	}

	var results []search.SearchResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	found := false
	for _, r := range results {
		if r.Path == "docs/auth.md" {
			found = true
			break
		}
	}
	if !found {
		t.Error("multi-word query did not find docs/auth.md")
	}
}

// TestSearchKeywordOutputParseable verifies the output can always be json.Unmarshalled.
func TestSearchKeywordOutputParseable(t *testing.T) {
	indexPath := setupSearchIndex(t, []struct{ path, title, summary, body string }{
		{"docs/guide.md", "Guide", "Setup guide", "How to get started with the system."},
		{"docs/api.md", "API Reference", "API docs", "Full API reference documentation."},
	})

	var buf bytes.Buffer
	if err := SearchKeyword(&buf, indexPath, "guide"); err != nil {
		t.Fatalf("SearchKeyword: %v", err)
	}

	var results []search.SearchResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("output not parseable: %v\noutput: %s", err, buf.String())
	}
}
