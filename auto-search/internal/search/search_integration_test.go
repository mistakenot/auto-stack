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
	if result.MessagesIndexed != 13 {
		t.Fatalf("expected 13 messages, got %d", result.MessagesIndexed)
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

func TestMessageSearchPagination(t *testing.T) {
	db := buildTestDB(t)

	firstPage, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:       db,
		Query:    "Exit code 0",
		PageSize: 1,
	})
	if err != nil {
		t.Fatalf("SearchMessages first page: %v", err)
	}
	if firstPage.Meta.TotalHits != 2 {
		t.Fatalf("total_hits = %d, want 2", firstPage.Meta.TotalHits)
	}
	if firstPage.Meta.ReturnedHits != 1 {
		t.Fatalf("returned_hits = %d, want 1", firstPage.Meta.ReturnedHits)
	}
	if firstPage.Meta.PageSize != 1 {
		t.Fatalf("page_size = %d, want 1", firstPage.Meta.PageSize)
	}
	if firstPage.Meta.Offset != 0 {
		t.Fatalf("offset = %d, want 0", firstPage.Meta.Offset)
	}
	if !firstPage.Meta.HasMore {
		t.Fatal("has_more = false, want true")
	}
	if firstPage.Meta.NextOffset == nil || *firstPage.Meta.NextOffset != 1 {
		t.Fatalf("next_offset = %v, want 1", firstPage.Meta.NextOffset)
	}
	if len(firstPage.Hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(firstPage.Hits))
	}

	secondPage, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:       db,
		Query:    "Exit code 0",
		PageSize: 1,
		Offset:   1,
	})
	if err != nil {
		t.Fatalf("SearchMessages second page: %v", err)
	}
	if secondPage.Meta.TotalHits != 2 {
		t.Fatalf("total_hits = %d, want 2", secondPage.Meta.TotalHits)
	}
	if secondPage.Meta.ReturnedHits != 1 {
		t.Fatalf("returned_hits = %d, want 1", secondPage.Meta.ReturnedHits)
	}
	if secondPage.Meta.HasMore {
		t.Fatal("has_more = true, want false")
	}
	if secondPage.Meta.NextOffset != nil {
		t.Fatalf("next_offset = %v, want nil", *secondPage.Meta.NextOffset)
	}
	if len(secondPage.Hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(secondPage.Hits))
	}
	if firstPage.Hits[0].MessageID == secondPage.Hits[0].MessageID {
		t.Fatalf("expected different message IDs across offsets, got %q", firstPage.Hits[0].MessageID)
	}

	emptyPage, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:       db,
		Query:    "Exit code 0",
		PageSize: 1,
		Offset:   2,
	})
	if err != nil {
		t.Fatalf("SearchMessages empty page: %v", err)
	}
	if emptyPage.Meta.TotalHits != 2 {
		t.Fatalf("total_hits = %d, want 2", emptyPage.Meta.TotalHits)
	}
	if emptyPage.Meta.ReturnedHits != 0 {
		t.Fatalf("returned_hits = %d, want 0", emptyPage.Meta.ReturnedHits)
	}
	if len(emptyPage.Hits) != 0 {
		t.Fatalf("hits = %d, want 0", len(emptyPage.Hits))
	}
}

func TestMessageSearchRejectsNegativeOffset(t *testing.T) {
	db := buildTestDB(t)

	_, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:     db,
		Query:  "Exit code 0",
		Offset: -1,
	})
	if err == nil {
		t.Fatal("expected error for negative offset")
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

func TestSessionSearchBatchMessageCounts(t *testing.T) {
	db := buildTestDB(t)

	// Search that returns all 3 sessions — verifies the chunked batch IN query
	// produces correct message counts (same results as the old N+1 approach).
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

	// Every returned session must have a positive message count.
	for _, hit := range result.Hits {
		if hit.TotalMessages <= 0 {
			t.Errorf("session %s: TotalMessages = %d, want > 0", hit.SessionID, hit.TotalMessages)
		}
	}

	// Verify the sum of per-session counts equals the total indexed messages (13).
	total := 0
	for _, hit := range result.Hits {
		total += hit.TotalMessages
	}
	if total != 13 {
		t.Errorf("sum of TotalMessages across sessions = %d, want 13", total)
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

func TestSessionSearchPagination(t *testing.T) {
	db := buildTestDB(t)

	firstPage, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:       db,
		Query:    "User",
		PageSize: 1,
	})
	if err != nil {
		t.Fatalf("SearchSessions first page: %v", err)
	}
	if firstPage.Meta.TotalHits != 3 {
		t.Fatalf("total_hits = %d, want 3", firstPage.Meta.TotalHits)
	}
	if firstPage.Meta.ReturnedHits != 1 {
		t.Fatalf("returned_hits = %d, want 1", firstPage.Meta.ReturnedHits)
	}
	if !firstPage.Meta.HasMore {
		t.Fatal("has_more = false, want true")
	}
	if firstPage.Meta.NextOffset == nil || *firstPage.Meta.NextOffset != 1 {
		t.Fatalf("next_offset = %v, want 1", firstPage.Meta.NextOffset)
	}
	if len(firstPage.Hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(firstPage.Hits))
	}

	secondPage, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:       db,
		Query:    "User",
		PageSize: 1,
		Offset:   1,
	})
	if err != nil {
		t.Fatalf("SearchSessions second page: %v", err)
	}
	if secondPage.Meta.TotalHits != 3 {
		t.Fatalf("total_hits = %d, want 3", secondPage.Meta.TotalHits)
	}
	if secondPage.Meta.ReturnedHits != 1 {
		t.Fatalf("returned_hits = %d, want 1", secondPage.Meta.ReturnedHits)
	}
	if !secondPage.Meta.HasMore {
		t.Fatal("has_more = false, want true")
	}
	if secondPage.Meta.NextOffset == nil || *secondPage.Meta.NextOffset != 2 {
		t.Fatalf("next_offset = %v, want 2", secondPage.Meta.NextOffset)
	}
	if len(secondPage.Hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(secondPage.Hits))
	}

	lastPage, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:       db,
		Query:    "User",
		PageSize: 1,
		Offset:   2,
	})
	if err != nil {
		t.Fatalf("SearchSessions last page: %v", err)
	}
	if lastPage.Meta.TotalHits != 3 {
		t.Fatalf("total_hits = %d, want 3", lastPage.Meta.TotalHits)
	}
	if lastPage.Meta.ReturnedHits != 1 {
		t.Fatalf("returned_hits = %d, want 1", lastPage.Meta.ReturnedHits)
	}
	if lastPage.Meta.HasMore {
		t.Fatal("has_more = true, want false")
	}
	if lastPage.Meta.NextOffset != nil {
		t.Fatalf("next_offset = %v, want nil", *lastPage.Meta.NextOffset)
	}
	if len(lastPage.Hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(lastPage.Hits))
	}
}

func TestSessionSearchRejectsNegativeOffset(t *testing.T) {
	db := buildTestDB(t)

	_, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:     db,
		Query:  "User",
		Offset: -1,
	})
	if err == nil {
		t.Fatal("expected error for negative offset")
	}
}

func TestMessageSearchRoleFilter(t *testing.T) {
	db := buildTestDB(t)

	// "authentication middleware" appears in user (msg-002), assistant (msg-003), and tool (msg-004).
	// Filtering by role=tool should only return tool messages.
	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "authentication middleware",
		Role:  "tool",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalHits == 0 {
		t.Fatal("expected at least 1 hit for role=tool")
	}
	for _, hit := range result.Hits {
		if hit.MessageType != "tool" {
			t.Errorf("expected messageType=tool, got %q (messageId=%s)", hit.MessageType, hit.MessageID)
		}
	}

	// Filtering by role=user should return only user messages.
	userResult, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "authentication middleware",
		Role:  "user",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if userResult.Meta.TotalHits == 0 {
		t.Fatal("expected at least 1 hit for role=user")
	}
	for _, hit := range userResult.Hits {
		if hit.MessageType != "user" {
			t.Errorf("expected messageType=user, got %q (messageId=%s)", hit.MessageType, hit.MessageID)
		}
	}

	// role=tool should return fewer hits than unfiltered.
	allResult, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "authentication middleware",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalHits >= allResult.Meta.TotalHits {
		t.Errorf("role-filtered hits (%d) should be fewer than unfiltered (%d)",
			result.Meta.TotalHits, allResult.Meta.TotalHits)
	}
}

func TestMessageSearchRoleFilterHitIDChanges(t *testing.T) {
	db := buildTestDB(t)

	// Same query with different role filter should produce different hit IDs.
	unfiltered, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "authentication middleware",
	})
	if err != nil {
		t.Fatalf("SearchMessages unfiltered: %v", err)
	}
	filtered, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "authentication middleware",
		Role:  "tool",
	})
	if err != nil {
		t.Fatalf("SearchMessages filtered: %v", err)
	}
	if len(unfiltered.Hits) == 0 || len(filtered.Hits) == 0 {
		t.Fatalf("expected hits in both, got %d and %d", len(unfiltered.Hits), len(filtered.Hits))
	}

	// Find a message that appears in both result sets.
	unfilteredIDs := make(map[string]string)
	for _, h := range unfiltered.Hits {
		unfilteredIDs[h.MessageID] = h.ID
	}
	for _, h := range filtered.Hits {
		if unfilteredID, ok := unfilteredIDs[h.MessageID]; ok {
			if unfilteredID == h.ID {
				t.Errorf("hit ID for %s should differ between filtered and unfiltered", h.MessageID)
			}
			return
		}
	}
}

func TestSessionSearchRoleFilter(t *testing.T) {
	db := buildTestDB(t)

	// "authentication" appears in session transcripts. All sessions have tool
	// messages, so role=tool should still return results.
	result, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:    db,
		Query: "authentication",
		Role:  "tool",
	})
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if result.Meta.TotalHits == 0 {
		t.Fatal("expected at least 1 session hit with role=tool")
	}

	// A nonexistent role should return a validation error.
	_, err = search.SearchSessions(&search.SessionSearchOpts{
		DB:    db,
		Query: "authentication",
		Role:  "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for invalid role, got nil")
	}
}

func TestMessageSearchFieldFilter(t *testing.T) {
	db := buildTestDB(t)

	toolInput, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "contextual-commit",
		Field: "tool_input",
	})
	if err != nil {
		t.Fatalf("SearchMessages tool_input: %v", err)
	}
	if toolInput.Meta.TotalHits != 1 {
		t.Fatalf("tool_input total_hits = %d, want 1", toolInput.Meta.TotalHits)
	}
	if len(toolInput.Hits) != 1 || toolInput.Hits[0].MessageID != "msg-011" {
		t.Fatalf("unexpected tool_input hits: %+v", toolInput.Hits)
	}

	contentOnly, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "contextual-commit",
		Field: "content",
	})
	if err != nil {
		t.Fatalf("SearchMessages content: %v", err)
	}
	if contentOnly.Meta.TotalHits != 0 {
		t.Fatalf("content total_hits = %d, want 0", contentOnly.Meta.TotalHits)
	}

	toolOutput, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "Committed",
		Field: "tool_output",
	})
	if err != nil {
		t.Fatalf("SearchMessages tool_output: %v", err)
	}
	if toolOutput.Meta.TotalHits != 1 {
		t.Fatalf("tool_output total_hits = %d, want 1", toolOutput.Meta.TotalHits)
	}
	if len(toolOutput.Hits) != 1 || toolOutput.Hits[0].MessageID != "msg-012" {
		t.Fatalf("unexpected tool_output hits: %+v", toolOutput.Hits)
	}
}

func TestSessionSearchFieldFilter(t *testing.T) {
	db := buildTestDB(t)

	toolInput, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:    db,
		Query: "authentication middleware",
		Field: "tool_input",
	})
	if err != nil {
		t.Fatalf("SearchSessions tool_input: %v", err)
	}
	if toolInput.Meta.TotalHits != 1 {
		t.Fatalf("tool_input total_hits = %d, want 1", toolInput.Meta.TotalHits)
	}
	if len(toolInput.Hits) != 1 || toolInput.Hits[0].SessionID != "test-session-1" {
		t.Fatalf("unexpected tool_input session hits: %+v", toolInput.Hits)
	}

	noToolInput, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:    db,
		Query: "CI pipeline",
		Field: "tool_input",
	})
	if err != nil {
		t.Fatalf("SearchSessions noToolInput: %v", err)
	}
	if noToolInput.Meta.TotalHits != 0 {
		t.Fatalf("tool_input total_hits for CI pipeline = %d, want 0", noToolInput.Meta.TotalHits)
	}

	contentOnly, err := search.SearchSessions(&search.SessionSearchOpts{
		DB:    db,
		Query: "CI pipeline",
		Field: "content",
	})
	if err != nil {
		t.Fatalf("SearchSessions content: %v", err)
	}
	if contentOnly.Meta.TotalHits != 1 {
		t.Fatalf("content total_hits for CI pipeline = %d, want 1", contentOnly.Meta.TotalHits)
	}
}

func TestSearchRejectsInvalidField(t *testing.T) {
	db := buildTestDB(t)

	_, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "auth",
		Field: "bad_field",
	})
	if err == nil {
		t.Fatal("expected error for invalid field value in message search")
	}

	_, err = search.SearchSessions(&search.SessionSearchOpts{
		DB:    db,
		Query: "auth",
		Field: "bad_field",
	})
	if err == nil {
		t.Fatal("expected error for invalid field value in session search")
	}
}

func TestMessageSearchRejectsInvalidRole(t *testing.T) {
	db := buildTestDB(t)

	_, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "authentication",
		Role:  "bogus",
	})
	if err == nil {
		t.Fatal("expected error for invalid role in message search, got nil")
	}
}

func TestSearchRejectsInvalidLimit(t *testing.T) {
	db := buildTestDB(t)

	// Negative limit should be rejected.
	_, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:       db,
		Query:    "test",
		PageSize: -1,
	})
	if err == nil {
		t.Fatal("expected error for negative limit in message search")
	}

	_, err = search.SearchSessions(&search.SessionSearchOpts{
		DB:       db,
		Query:    "test",
		PageSize: -1,
	})
	if err == nil {
		t.Fatal("expected error for negative limit in session search")
	}

	// Over-1000 limit should be rejected.
	_, err = search.SearchMessages(&search.MessageSearchOpts{
		DB:       db,
		Query:    "test",
		PageSize: 1001,
	})
	if err == nil {
		t.Fatal("expected error for over-1000 limit in message search")
	}

	_, err = search.SearchSessions(&search.SessionSearchOpts{
		DB:       db,
		Query:    "test",
		PageSize: 1001,
	})
	if err == nil {
		t.Fatal("expected error for over-1000 limit in session search")
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

// --- Thinking message tests (Phase 3, task 016) ---

func TestMessageSearchDefaultExcludesThinking(t *testing.T) {
	db := buildTestDB(t)

	// The thinking message (msg-002t) contains "authentication middleware"
	// which also appears in non-thinking messages. Default search should
	// NOT return the thinking message.
	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "authentication middleware",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	for _, hit := range result.Hits {
		if hit.MessageType == "thinking" {
			t.Errorf("default search returned thinking message %s", hit.MessageID)
		}
	}
}

func TestMessageSearchRoleThinkingReturnsOnlyThinking(t *testing.T) {
	db := buildTestDB(t)

	// --role thinking should return only the thinking message(s).
	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "authentication middleware",
		Role:  "thinking",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalHits == 0 {
		t.Fatal("expected at least 1 hit for role=thinking")
	}
	for _, hit := range result.Hits {
		if hit.MessageType != "thinking" {
			t.Errorf("expected messageType=thinking, got %q (messageId=%s)", hit.MessageType, hit.MessageID)
		}
	}
	// Verify the specific thinking message is returned.
	found := false
	for _, hit := range result.Hits {
		if hit.MessageID == "msg-002t" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected msg-002t in role=thinking results")
	}
}

func TestMessageSearchIncludeThinkingIncludesThinking(t *testing.T) {
	db := buildTestDB(t)

	// --include-thinking should include thinking alongside other roles.
	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:              db,
		Query:           "authentication middleware",
		IncludeThinking: true,
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}

	// Should have more results than default (which excludes thinking).
	defaultResult, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "authentication middleware",
	})
	if err != nil {
		t.Fatalf("SearchMessages default: %v", err)
	}
	if result.Meta.TotalHits <= defaultResult.Meta.TotalHits {
		t.Errorf("include-thinking hits (%d) should be more than default (%d)",
			result.Meta.TotalHits, defaultResult.Meta.TotalHits)
	}

	// Verify a thinking message is present.
	foundThinking := false
	for _, hit := range result.Hits {
		if hit.MessageType == "thinking" {
			foundThinking = true
			break
		}
	}
	if !foundThinking {
		t.Error("expected at least one thinking message in include-thinking results")
	}
}

func TestMessageSearchNoFTSDefaultExcludesThinking(t *testing.T) {
	db := buildTestDB(t)

	// Structured-only search (no FTS query) should also exclude thinking
	// by default. Use --session-id to trigger the structured path.
	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:        db,
		SessionID: "test-session-1",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	for _, hit := range result.Hits {
		if hit.MessageType == "thinking" {
			t.Errorf("default structured search returned thinking message %s", hit.MessageID)
		}
	}
	// Session-1 has 9 messages but only 8 non-thinking.
	if result.Meta.TotalHits != 8 {
		t.Errorf("total hits = %d, want 8 (9 minus 1 thinking)", result.Meta.TotalHits)
	}
}

func TestMessageSearchRoleThinkingAcceptedByNormalizeRole(t *testing.T) {
	db := buildTestDB(t)

	// "thinking" should be accepted as a valid role value, not rejected.
	_, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "middleware",
		Role:  "thinking",
	})
	if err != nil {
		t.Fatalf("expected no error for role=thinking, got: %v", err)
	}
}

func TestMessageSearchSkillFilterReturnsAttributedSession(t *testing.T) {
	db := buildTestDB(t)

	// msg-010 in test-session-3 has skill_name="review-task" set via
	// attributionSkill fallback (not from Skill tool). --skill should find it.
	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "CI",
		Skill: "review-task",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalHits == 0 {
		t.Fatal("expected at least 1 hit for --skill review-task")
	}
	// All hits should be from the session with the skill attribution.
	for _, hit := range result.Hits {
		if hit.SessionID != "test-session-3" {
			t.Errorf("expected session_id=test-session-3, got %q", hit.SessionID)
		}
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
