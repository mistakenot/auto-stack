package search_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/mistakenot/auto-search/internal/search"
	"github.com/mistakenot/auto-search/internal/testutil"
)

// buildTestDB creates a fresh index from committed fixtures in a temp directory.
func buildTestDB(t *testing.T) *sql.DB {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	fixtureDir := filepath.Join("..", "..", "testdata", "etl-output")
	abs, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatalf("resolve fixture dir: %v", err)
	}
	if _, err := os.Stat(testutil.SessionsFixturePath(abs)); err != nil {
		t.Skipf("fixture files not found at %s: %v", abs, err)
	}

	dbPath := filepath.Join(home, "test.sqlite")
	result, err := indexdb.FullBuild(dbPath, abs, os.Stderr)
	if err != nil {
		t.Fatalf("FullBuild: %v", err)
	}
	if result.SessionsIndexed != 3 {
		t.Fatalf("expected 3 sessions, got %d", result.SessionsIndexed)
	}
	if result.MessagesIndexed != 10 {
		t.Fatalf("expected 10 messages, got %d", result.MessagesIndexed)
	}

	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMessageSearchBasic(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:        db,
		Query:     "authentication middleware",
		RequestID: "test-msg-1",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}

	if result.Meta.Scope != "messages" {
		t.Errorf("scope = %q, want messages", result.Meta.Scope)
	}
	if result.Meta.RequestID != "test-msg-1" {
		t.Errorf("request_id = %q, want test-msg-1", result.Meta.RequestID)
	}
	if result.Meta.TotalHits == 0 {
		t.Error("expected at least 1 hit")
	}

	// Verify hit fields.
	hit := result.Hits[0]
	if hit.MessageID == "" {
		t.Error("hit messageId should not be empty")
	}
	if hit.SessionID == "" {
		t.Error("hit sessionId should not be empty")
	}
	if hit.ID == "" {
		t.Error("hit id should not be empty")
	}
	if hit.Snippet == "" {
		t.Error("hit snippet should not be empty")
	}
}

func TestMessageSearchWithHighlight(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:        db,
		Query:     "authentication",
		Highlight: true,
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalHits == 0 {
		t.Fatal("expected at least 1 hit")
	}

	// Snippet should contain ** markers.
	found := false
	for _, hit := range result.Hits {
		if containsStr(hit.Snippet, "**") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one snippet with ** highlight markers")
	}
}

func TestMessageSearchExitCode(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "Exit code 0",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalHits == 0 {
		t.Fatal("expected hits for 'Exit code 0'")
	}
}

func TestMessageSearchPrefixFallback(t *testing.T) {
	db := buildTestDB(t)

	// "xyzzy" is a rare term that should trigger prefix fallback.
	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "xyzzy",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	// With the fixture data, xyzzy appears once, so we should get at least 1 hit.
	if result.Meta.TotalHits == 0 {
		t.Error("expected at least 1 hit for xyzzy")
	}
}

func TestMessageSearchNoFiltersRegressionGuard(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "Exit code 0",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalHits != 2 {
		t.Fatalf("total hits = %d, want 2", result.Meta.TotalHits)
	}
}

func TestMessageSearchSinceFilter(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "Exit code 0",
		Since: "2h",
		Now:   mustParseRFC3339(t, "2024-03-21T09:00:00Z"),
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalHits != 1 {
		t.Fatalf("total hits = %d, want 1", result.Meta.TotalHits)
	}
	if len(result.Hits) != 1 || result.Hits[0].MessageID != "msg-010" {
		t.Fatalf("unexpected hits: %+v", result.Hits)
	}
}

func TestMessageSearchAfterBeforeFilter(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:     db,
		Query:  "Exit code 0",
		After:  "2024-03-21T08:00:00Z",
		Before: "2024-03-21T09:00:00Z",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalHits != 1 {
		t.Fatalf("total hits = %d, want 1", result.Meta.TotalHits)
	}
	if len(result.Hits) != 1 || result.Hits[0].MessageID != "msg-010" {
		t.Fatalf("unexpected hits: %+v", result.Hits)
	}
}

func TestMessageSearchBeforeIsExclusive(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:     db,
		Query:  "Exit code 0",
		Before: "2024-03-21T08:35:00Z",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalHits != 1 {
		t.Fatalf("total hits = %d, want 1", result.Meta.TotalHits)
	}
	if len(result.Hits) != 1 || result.Hits[0].MessageID != "msg-005" {
		t.Fatalf("unexpected hits: %+v", result.Hits)
	}
}

func TestMessageSearchFallbackWithDateFilter(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:     db,
		Query:  "xyzzy",
		After:  "2024-03-21T05:54:00Z",
		Before: "2024-03-21T06:00:00Z",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalHits != 1 {
		t.Fatalf("total hits = %d, want 1", result.Meta.TotalHits)
	}
	if len(result.Hits) != 1 || result.Hits[0].MessageID != "msg-006" {
		t.Fatalf("unexpected hits: %+v", result.Hits)
	}
}

func TestMessageSearchHitIDChangesWithDateFilter(t *testing.T) {
	db := buildTestDB(t)

	unfiltered, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "lint step confirms",
	})
	if err != nil {
		t.Fatalf("SearchMessages unfiltered: %v", err)
	}
	filtered, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:     db,
		Query:  "lint step confirms",
		After:  "2024-03-21T08:00:00Z",
		Before: "2024-03-21T09:00:00Z",
	})
	if err != nil {
		t.Fatalf("SearchMessages filtered: %v", err)
	}
	if len(unfiltered.Hits) != 1 || len(filtered.Hits) != 1 {
		t.Fatalf("expected one hit each, got %d and %d", len(unfiltered.Hits), len(filtered.Hits))
	}
	if unfiltered.Hits[0].MessageID != "msg-010" || filtered.Hits[0].MessageID != "msg-010" {
		t.Fatalf("unexpected message IDs: %q and %q", unfiltered.Hits[0].MessageID, filtered.Hits[0].MessageID)
	}
	if unfiltered.Hits[0].ID == filtered.Hits[0].ID {
		t.Fatalf("expected different hit IDs when date filters change, got %q", unfiltered.Hits[0].ID)
	}
}

func TestSessionSearchBasic(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:        db,
		Query:     "authentication middleware",
		RequestID: "test-sess-1",
	})
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}

	if result.Meta.Scope != "sessions" {
		t.Errorf("scope = %q, want sessions", result.Meta.Scope)
	}
	if result.Meta.RequestID != "test-sess-1" {
		t.Errorf("request_id = %q, want test-sess-1", result.Meta.RequestID)
	}
	if result.Meta.TotalHits == 0 {
		t.Error("expected at least 1 session hit")
	}

	// Verify hit fields.
	hit := result.Hits[0]
	if hit.SessionID == "" {
		t.Error("hit sessionId should not be empty")
	}
	if hit.Workspace == "" {
		t.Error("hit workspace should not be empty")
	}
	if hit.TotalMessages == 0 {
		t.Error("hit totalMessages should be > 0")
	}
}

func TestSessionSearchTranscriptScope(t *testing.T) {
	db := buildTestDB(t)

	// Search for something in the session transcript.
	result, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:    db,
		Query: "CI pipeline",
	})
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if result.Meta.TotalHits == 0 {
		t.Error("expected session hit for 'CI pipeline'")
	}
	// The CI session is test-session-3.
	found := false
	for _, h := range result.Hits {
		if h.SessionID == "test-session-3" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected test-session-3 in results")
	}
}

func TestSessionSearchNoFiltersRegressionGuard(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:    db,
		Query: "User",
	})
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if result.Meta.TotalHits != 3 {
		t.Fatalf("total hits = %d, want 3", result.Meta.TotalHits)
	}
}

func TestSessionSearchSinceFilter(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:    db,
		Query: "User",
		Since: "2h",
		Now:   mustParseRFC3339(t, "2024-03-21T09:00:00Z"),
	})
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if result.Meta.TotalHits != 1 {
		t.Fatalf("total hits = %d, want 1", result.Meta.TotalHits)
	}
	if len(result.Hits) != 1 || result.Hits[0].SessionID != "test-session-3" {
		t.Fatalf("unexpected hits: %+v", result.Hits)
	}
}

func TestSessionSearchAfterBeforeFilter(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:     db,
		Query:  "User",
		After:  "2024-03-21T06:00:00Z",
		Before: "2024-03-21T07:00:00Z",
	})
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if result.Meta.TotalHits != 1 {
		t.Fatalf("total hits = %d, want 1", result.Meta.TotalHits)
	}
	if len(result.Hits) != 1 || result.Hits[0].SessionID != "test-session-2" {
		t.Fatalf("unexpected hits: %+v", result.Hits)
	}
}

func TestSessionSearchBeforeIsExclusive(t *testing.T) {
	db := buildTestDB(t)

	result, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:     db,
		Query:  "User",
		Before: "2024-03-21T06:03:20Z",
	})
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if result.Meta.TotalHits != 1 {
		t.Fatalf("total hits = %d, want 1", result.Meta.TotalHits)
	}
	if len(result.Hits) != 1 || result.Hits[0].SessionID != "test-session-1" {
		t.Fatalf("unexpected hits: %+v", result.Hits)
	}
}

func TestCWDAndRemoteMutuallyExclusive(t *testing.T) {
	db := buildTestDB(t)

	_, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:     db,
		Query:  "test",
		CWD:    "/some/path",
		Remote: "git@example.com",
	})
	if err == nil {
		t.Error("expected error for --cwd + --remote")
	}

	_, err = search.SearchSessions(&search.SessionSearchOpts{
		DB:     db,
		Query:  "test",
		CWD:    "/some/path",
		Remote: "git@example.com",
	})
	if err == nil {
		t.Error("expected error for --cwd + --remote")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func mustParseRFC3339(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse time %q: %v", raw, err)
	}
	return parsed
}
