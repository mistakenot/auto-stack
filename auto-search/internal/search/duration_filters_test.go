package search_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/mistakenot/auto-search/internal/search"
)

// buildSyntheticDB creates a fresh indexdb with no parquet ingest, then
// inserts a handful of messages with controlled duration_ms / interrupted /
// tool_name values. This lets the duration-filter tests assert exact
// subsets without depending on the etl-output fixture.
func buildSyntheticDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")
	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// One session row to satisfy any joins.
	if _, err := db.Exec(`
		INSERT INTO sessions (partition_source_path, session_id, parent_session_id, host_id, agent, subagent_name, is_subagent, workspace, git_remote, model, source_path, first_message_at, last_message_at, total_input_tokens, total_output_tokens, total_tokens, total_bytes, total_output_bytes, total_input_bytes, transcript_truncated, schema_version)
		VALUES ('/p.parquet', 'sess-A', '', 'host1', 'claude', '', 0, '/w', '', 'opus', '/p', 1000, 9000, 0, 0, 0, 0, 0, 0, '', 1)
	`); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	// Insert a mix of tool messages with controlled duration_ms / interrupted.
	// We give them shared FTS-searchable content "lookforme" so a single
	// query selects the full set, then we filter further by flags.
	type row struct {
		messageID   string
		idx         int
		toolName    string
		durationMs  int64
		interrupted int
		content     string
	}
	rows := []row{
		{"sess-A-1", 1, "Bash", 500, 0, "lookforme fast bash"},
		{"sess-A-2", 2, "Bash", 2000, 0, "lookforme slow bash"},
		{"sess-A-3", 3, "Bash", 90_000, 0, "lookforme very slow bash"},
		{"sess-A-4", 4, "Read", 1500, 0, "lookforme slow read"},
		{"sess-A-5", 5, "Bash", 100, 1, "lookforme stuck bash"},
	}
	for _, r := range rows {
		if _, err := db.Exec(`
			INSERT INTO messages (partition_source_path, message_id, session_id, host_id, message_index, role, content, content_truncated, timestamp, tool_name, tool_input, tool_file_path, tool_file_start_line, tool_file_num_lines, tool_file_total_lines, bash_command, bash_exit_code, skill_name, tool_use_id, duration_ms, interrupted, input_tokens, cache_input_tokens, output_tokens, workspace, git_remote, git_branch, model, parent_session_id, is_subagent, source_line_index, schema_version)
			VALUES ('/p.parquet', ?, 'sess-A', 'host1', ?, 'tool', ?, ?, ?, ?, '', '', 0, 0, 0, '', 0, '', '', ?, ?, 0, 0, 0, '/w', '', '', 'opus', '', 0, ?, 1)
		`, r.messageID, r.idx, r.content, r.content, int64(1000+r.idx*100), r.toolName, r.durationMs, r.interrupted, r.idx); err != nil {
			t.Fatalf("insert message %s: %v", r.messageID, err)
		}
	}
	return db
}

func TestSearchMessagesMinToolDuration(t *testing.T) {
	db := buildSyntheticDB(t)

	min := int64(60_000)
	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:                db,
		Query:             "lookforme",
		MinToolDurationMs: &min,
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalMatches != 1 {
		t.Fatalf("total = %d, want 1 (only the 90s bash call)", result.Meta.TotalMatches)
	}
	if len(result.Hits) != 1 || result.Hits[0].MessageID != "sess-A-3" {
		t.Fatalf("unexpected hits: %+v", result.Hits)
	}
	if result.Hits[0].DurationMs != 90_000 {
		t.Fatalf("hit duration_ms = %d, want 90000", result.Hits[0].DurationMs)
	}
}

func TestSearchMessagesMinToolDurationLow(t *testing.T) {
	db := buildSyntheticDB(t)

	// 1s threshold: rows with 2000, 90000, 1500 pass (3 rows); 500 / 100 fail.
	min := int64(1000)
	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:                db,
		Query:             "lookforme",
		MinToolDurationMs: &min,
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalMatches != 3 {
		t.Fatalf("total = %d, want 3", result.Meta.TotalMatches)
	}
}

func TestSearchMessagesInterrupted(t *testing.T) {
	db := buildSyntheticDB(t)

	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:              db,
		Query:           "lookforme",
		OnlyInterrupted: true,
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalMatches != 1 {
		t.Fatalf("total = %d, want 1", result.Meta.TotalMatches)
	}
	if !result.Hits[0].Interrupted {
		t.Fatalf("expected hit.Interrupted = true")
	}
}

func TestSearchMessagesToolNameAndDurationCombine(t *testing.T) {
	db := buildSyntheticDB(t)

	// --tool-name Bash AND --min-tool-duration 1s = the 2 slow bash rows
	// (durations 2000 and 90000). The Read row with 1500ms is excluded.
	min := int64(1000)
	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:                db,
		Query:             "lookforme",
		ToolName:          "Bash",
		MinToolDurationMs: &min,
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalMatches != 2 {
		t.Fatalf("total = %d, want 2", result.Meta.TotalMatches)
	}
	for _, h := range result.Hits {
		if h.ToolName != "Bash" {
			t.Fatalf("hit toolName = %q, want Bash", h.ToolName)
		}
	}
}

func TestSearchMessagesNoQueryStructuredFilter(t *testing.T) {
	db := buildSyntheticDB(t)

	// Empty query + --min-tool-duration 60s should return only the 90s row.
	min := int64(60_000)
	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:                db,
		Query:             "",
		MinToolDurationMs: &min,
	})
	if err != nil {
		t.Fatalf("SearchMessages (no query): %v", err)
	}
	if result.Meta.TotalMatches != 1 {
		t.Fatalf("total = %d, want 1", result.Meta.TotalMatches)
	}
	if result.Hits[0].MessageID != "sess-A-3" {
		t.Fatalf("hit = %s, want sess-A-3", result.Hits[0].MessageID)
	}
	if result.Hits[0].DurationMs != 90_000 {
		t.Fatalf("hit duration_ms = %d, want 90000", result.Hits[0].DurationMs)
	}
}

func TestSearchMessagesNoQueryNoFiltersRejected(t *testing.T) {
	db := buildSyntheticDB(t)

	_, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:    db,
		Query: "",
	})
	if err == nil {
		t.Fatal("expected error when both query and structured filters are empty")
	}
}

func TestSearchMessagesNoQueryInterrupted(t *testing.T) {
	db := buildSyntheticDB(t)

	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:              db,
		Query:           "",
		OnlyInterrupted: true,
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalMatches != 1 {
		t.Fatalf("total = %d, want 1", result.Meta.TotalMatches)
	}
	if !result.Hits[0].Interrupted {
		t.Fatal("expected hit.Interrupted = true")
	}
}

func TestSearchMessagesSessionIDFilter(t *testing.T) {
	db := buildSyntheticDB(t)

	result, err := search.SearchMessages(&search.MessageSearchOpts{
		DB:        db,
		Query:     "lookforme",
		SessionID: "no-such-session",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalMatches != 0 {
		t.Fatalf("total = %d, want 0", result.Meta.TotalMatches)
	}

	result, err = search.SearchMessages(&search.MessageSearchOpts{
		DB:        db,
		Query:     "lookforme",
		SessionID: "sess-A",
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if result.Meta.TotalMatches != 5 {
		t.Fatalf("total = %d, want 5", result.Meta.TotalMatches)
	}
}
