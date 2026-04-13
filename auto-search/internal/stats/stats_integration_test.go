package stats_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/mistakenot/auto-search/internal/stats"
	"github.com/mistakenot/auto-search/internal/testutil"
)

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

	dbPath := filepath.Join(home, "stats-test.sqlite")
	result, err := indexdb.FullBuild(dbPath, abs, os.Stderr)
	if err != nil {
		t.Fatalf("FullBuild: %v", err)
	}
	if result.SessionsIndexed != 3 || result.MessagesIndexed != 12 {
		t.Fatalf("unexpected fixture row counts: sessions=%d messages=%d", result.SessionsIndexed, result.MessagesIndexed)
	}

	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestStatsMessagesSessionIDBaseline(t *testing.T) {
	db := buildTestDB(t)

	resp, err := stats.Run(&stats.Request{
		DB:      db,
		Scope:   "messages",
		GroupBy: "session_id",
	})
	if err != nil {
		t.Fatalf("stats.Run: %v", err)
	}

	if resp.Meta.TotalMatches != 12 {
		t.Fatalf("total_matches = %d, want 12", resp.Meta.TotalMatches)
	}
	if resp.Meta.TotalBucketsUnfiltered != 3 {
		t.Fatalf("total_buckets_unfiltered = %d, want 3", resp.Meta.TotalBucketsUnfiltered)
	}
	if resp.Meta.TotalBuckets != 3 {
		t.Fatalf("total_buckets = %d, want 3", resp.Meta.TotalBuckets)
	}
	if len(resp.Buckets) != 3 {
		t.Fatalf("len(buckets) = %d, want 3", len(resp.Buckets))
	}
	if resp.Buckets[0].Key != "test-session-1" || resp.Buckets[0].Count != 8 {
		t.Fatalf("bucket[0] = %+v, want key=test-session-1 count=8", resp.Buckets[0])
	}
	if resp.Buckets[1].Key != "test-session-2" || resp.Buckets[1].Count != 2 {
		t.Fatalf("bucket[1] = %+v, want key=test-session-2 count=2", resp.Buckets[1])
	}
	if resp.Buckets[2].Key != "test-session-3" || resp.Buckets[2].Count != 2 {
		t.Fatalf("bucket[2] = %+v, want key=test-session-3 count=2", resp.Buckets[2])
	}
	if resp.Buckets[0].SampleSnippet != "" {
		t.Fatalf("expected empty sample_snippet without query, got %q", resp.Buckets[0].SampleSnippet)
	}
}

func TestStatsMessagesRoleCounts(t *testing.T) {
	db := buildTestDB(t)

	resp, err := stats.Run(&stats.Request{
		DB:      db,
		Scope:   "messages",
		GroupBy: "role",
	})
	if err != nil {
		t.Fatalf("stats.Run: %v", err)
	}
	if len(resp.Buckets) != 4 {
		t.Fatalf("len(buckets) = %d, want 4", len(resp.Buckets))
	}

	got := map[string]int{}
	for _, b := range resp.Buckets {
		got[b.Key] = b.Count
	}
	want := map[string]int{
		"assistant": 5,
		"tool":      3,
		"user":      3,
		"system":    1,
	}
	for key, count := range want {
		if got[key] != count {
			t.Fatalf("role %q count = %d, want %d", key, got[key], count)
		}
	}
}

func TestStatsMessagesBashCommandNormalization(t *testing.T) {
	db := buildTestDB(t)

	resp, err := stats.Run(&stats.Request{
		DB:      db,
		Scope:   "messages",
		GroupBy: "bash_command",
	})
	if err != nil {
		t.Fatalf("stats.Run: %v", err)
	}
	if len(resp.Buckets) != 2 {
		t.Fatalf("len(buckets) = %d, want 2", len(resp.Buckets))
	}
	if resp.Buckets[0].Key != "(none)" || resp.Buckets[0].Count != 11 {
		t.Fatalf("bucket[0] = %+v, want key=(none) count=11", resp.Buckets[0])
	}
	if resp.Buckets[1].Key != "go test" || resp.Buckets[1].Count != 1 {
		t.Fatalf("bucket[1] = %+v, want key=go test count=1", resp.Buckets[1])
	}
}

func TestStatsSessionsWorkspaceDistinctMessages(t *testing.T) {
	db := buildTestDB(t)

	resp, err := stats.Run(&stats.Request{
		DB:      db,
		Scope:   "sessions",
		GroupBy: "workspace",
	})
	if err != nil {
		t.Fatalf("stats.Run: %v", err)
	}
	if len(resp.Buckets) != 2 {
		t.Fatalf("len(buckets) = %d, want 2", len(resp.Buckets))
	}
	if resp.Buckets[0].Key != "/workspace/project-a" || resp.Buckets[0].Count != 2 || resp.Buckets[0].DistinctMessages != 10 {
		t.Fatalf("bucket[0] = %+v, want workspace project-a count=2 distinct_messages=10", resp.Buckets[0])
	}
	if resp.Buckets[0].SampleSessionID != "test-session-2" {
		t.Fatalf("sample_session_id = %q, want test-session-2", resp.Buckets[0].SampleSessionID)
	}
	if resp.Buckets[0].SampleMessageID != "msg-008" {
		t.Fatalf("sample_message_id = %q, want msg-008", resp.Buckets[0].SampleMessageID)
	}
	if resp.Buckets[1].Key != "/workspace/project-b" || resp.Buckets[1].Count != 1 || resp.Buckets[1].DistinctMessages != 2 {
		t.Fatalf("bucket[1] = %+v, want workspace project-b count=1 distinct_messages=2", resp.Buckets[1])
	}
}

func TestStatsMessagesQueryFilterAndSnippet(t *testing.T) {
	db := buildTestDB(t)

	resp, err := stats.Run(&stats.Request{
		DB:      db,
		Scope:   "messages",
		GroupBy: "session_id",
		Query:   `"Exit code 0"`,
	})
	if err != nil {
		t.Fatalf("stats.Run: %v", err)
	}
	if resp.Meta.TotalMatches != 2 {
		t.Fatalf("total_matches = %d, want 2", resp.Meta.TotalMatches)
	}
	if len(resp.Buckets) != 2 {
		t.Fatalf("len(buckets) = %d, want 2", len(resp.Buckets))
	}
	if resp.Buckets[0].Key != "test-session-1" || resp.Buckets[1].Key != "test-session-3" {
		t.Fatalf("unexpected bucket order: %+v", resp.Buckets)
	}
	if resp.Buckets[0].SampleSnippet == "" {
		t.Fatal("expected sample_snippet with query")
	}
}

func TestStatsMessagesMinCountByMeasure(t *testing.T) {
	db := buildTestDB(t)

	byCount, err := stats.Run(&stats.Request{
		DB:       db,
		Scope:    "messages",
		GroupBy:  "role",
		Measure:  "count",
		MinCount: 3,
	})
	if err != nil {
		t.Fatalf("stats.Run by count: %v", err)
	}
	if len(byCount.Buckets) != 3 {
		t.Fatalf("count/min_count buckets = %d, want 3", len(byCount.Buckets))
	}

	byDistinctSessions, err := stats.Run(&stats.Request{
		DB:       db,
		Scope:    "messages",
		GroupBy:  "role",
		Measure:  "distinct_sessions",
		MinCount: 2,
	})
	if err != nil {
		t.Fatalf("stats.Run by distinct_sessions: %v", err)
	}
	if len(byDistinctSessions.Buckets) != 2 {
		t.Fatalf("distinct_sessions/min_count buckets = %d, want 2", len(byDistinctSessions.Buckets))
	}
	if byDistinctSessions.Buckets[0].Key != "assistant" || byDistinctSessions.Buckets[1].Key != "user" {
		t.Fatalf("unexpected buckets: %+v", byDistinctSessions.Buckets)
	}
}

func TestStatsMessagesPaginationDeterminism(t *testing.T) {
	db := buildTestDB(t)

	first, err := stats.Run(&stats.Request{
		DB:       db,
		Scope:    "messages",
		GroupBy:  "session_id",
		Query:    `"Exit code 0"`,
		PageSize: 1,
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Buckets) != 1 || first.Buckets[0].Key != "test-session-1" {
		t.Fatalf("first page buckets = %+v, want test-session-1", first.Buckets)
	}
	if !first.Meta.HasMore || first.Meta.NextOffset == nil || *first.Meta.NextOffset != 1 {
		t.Fatalf("first page meta = %+v, want has_more=true next_offset=1", first.Meta)
	}

	second, err := stats.Run(&stats.Request{
		DB:       db,
		Scope:    "messages",
		GroupBy:  "session_id",
		Query:    `"Exit code 0"`,
		PageSize: 1,
		Offset:   1,
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Buckets) != 1 || second.Buckets[0].Key != "test-session-3" {
		t.Fatalf("second page buckets = %+v, want test-session-3", second.Buckets)
	}
	if second.Meta.HasMore || second.Meta.NextOffset != nil {
		t.Fatalf("second page meta = %+v, want no next page", second.Meta)
	}
}
