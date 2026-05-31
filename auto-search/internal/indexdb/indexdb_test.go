package indexdb_test

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-search/internal/indexdb"
)

func TestCreateAppliesSchemaAndVersion(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer db.Close()

	// Verify the file exists.
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("db file does not exist: %v", err)
	}

	// Verify schema version.
	version, err := indexdb.ReadSchemaVersion(db)
	if err != nil {
		t.Fatalf("ReadSchemaVersion: %v", err)
	}
	if version != indexdb.SchemaVersion {
		t.Fatalf("schema_version = %d, want %d", version, indexdb.SchemaVersion)
	}

	// Verify required tables exist.
	for _, table := range []string{
		"schema_info", "index_state", "sessions", "messages",
		"sessions_fts", "messages_fts",
	} {
		exists, err := indexdb.TableExists(db, table)
		if err != nil {
			t.Fatalf("TableExists(%s): %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s should exist", table)
		}
	}
}

func TestOpenFailsOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := indexdb.Open(filepath.Join(dir, "nonexistent.sqlite"))
	if err == nil {
		t.Fatal("Open should fail on missing file")
	}
}

func TestOpenSucceedsOnExistingDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	db.Close()

	db2, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db2.Close()

	version, err := indexdb.ReadSchemaVersion(db2)
	if err != nil {
		t.Fatalf("ReadSchemaVersion: %v", err)
	}
	if version != indexdb.SchemaVersion {
		t.Fatalf("schema_version = %d, want %d", version, indexdb.SchemaVersion)
	}
}

func TestNeedsRebuildOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	rebuild, err := indexdb.NeedsRebuild(filepath.Join(dir, "missing.sqlite"))
	if err != nil {
		t.Fatalf("NeedsRebuild: %v", err)
	}
	if !rebuild {
		t.Fatal("NeedsRebuild should return true for missing file")
	}
}

func TestNeedsRebuildOnCurrentSchema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	db.Close()

	rebuild, err := indexdb.NeedsRebuild(dbPath)
	if err != nil {
		t.Fatalf("NeedsRebuild: %v", err)
	}
	if rebuild {
		t.Fatal("NeedsRebuild should return false for current schema version")
	}
}

func TestNeedsRebuildOnStaleSchema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Tamper with schema version to simulate a stale DB.
	if _, err := db.Exec("UPDATE schema_info SET schema_version = ?", indexdb.SchemaVersion-1); err != nil {
		t.Fatalf("update schema_version: %v", err)
	}
	db.Close()

	rebuild, err := indexdb.NeedsRebuild(dbPath)
	if err != nil {
		t.Fatalf("NeedsRebuild: %v", err)
	}
	if !rebuild {
		t.Fatal("NeedsRebuild should return true for stale schema version")
	}
}

func TestOpenOrCreateFreshBuild(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, rebuilt, err := indexdb.OpenOrCreate(dbPath)
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	defer db.Close()

	if !rebuilt {
		t.Fatal("expected full rebuild on fresh path")
	}

	version, err := indexdb.ReadSchemaVersion(db)
	if err != nil {
		t.Fatalf("ReadSchemaVersion: %v", err)
	}
	if version != indexdb.SchemaVersion {
		t.Fatalf("schema_version = %d, want %d", version, indexdb.SchemaVersion)
	}
}

func TestOpenOrCreateExistingCurrent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	db.Close()

	db2, rebuilt, err := indexdb.OpenOrCreate(dbPath)
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	defer db2.Close()

	if rebuilt {
		t.Fatal("should not rebuild when schema is current")
	}
}

func TestOpenOrCreateRebuildOnStaleSchema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := db.Exec("UPDATE schema_info SET schema_version = ?", indexdb.SchemaVersion-1); err != nil {
		t.Fatalf("update schema_version: %v", err)
	}
	db.Close()

	db2, rebuilt, err := indexdb.OpenOrCreate(dbPath)
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	defer db2.Close()

	if !rebuilt {
		t.Fatal("expected full rebuild on stale schema")
	}

	version, err := indexdb.ReadSchemaVersion(db2)
	if err != nil {
		t.Fatalf("ReadSchemaVersion: %v", err)
	}
	if version != indexdb.SchemaVersion {
		t.Fatalf("schema_version after rebuild = %d, want %d", version, indexdb.SchemaVersion)
	}
}

func TestIndexStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer db.Close()

	// Initially empty.
	state, err := indexdb.LoadIndexState(db)
	if err != nil {
		t.Fatalf("LoadIndexState: %v", err)
	}
	if len(state) != 0 {
		t.Fatalf("expected empty state, got %d entries", len(state))
	}

	// Insert a state entry.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	entry := indexdb.PartitionState{
		Dataset:             "messages",
		PartitionKey:        "year=2026/week=12",
		SourcePath:          "/data/messages/year=2026/week=12/part-0.parquet",
		SourceSizeBytes:     1024,
		SourceMtimeUnixMs:   1711100000000,
		SourceSchemaVersion: 1,
		IndexedAtUnixMs:     1711200000000,
	}
	if err := indexdb.UpsertIndexState(tx, &entry); err != nil {
		t.Fatalf("UpsertIndexState: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Read it back.
	state, err = indexdb.LoadIndexState(db)
	if err != nil {
		t.Fatalf("LoadIndexState: %v", err)
	}
	if len(state) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(state))
	}
	got := state[entry.SourcePath]
	if got.Dataset != entry.Dataset {
		t.Fatalf("dataset = %q, want %q", got.Dataset, entry.Dataset)
	}
	if got.PartitionKey != entry.PartitionKey {
		t.Fatalf("partition_key = %q, want %q", got.PartitionKey, entry.PartitionKey)
	}
	if got.SourceSizeBytes != entry.SourceSizeBytes {
		t.Fatalf("source_size_bytes = %d, want %d", got.SourceSizeBytes, entry.SourceSizeBytes)
	}

	// Upsert overwrites.
	tx2, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	entry.SourceSizeBytes = 2048
	if err := indexdb.UpsertIndexState(tx2, &entry); err != nil {
		t.Fatalf("UpsertIndexState: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	state, err = indexdb.LoadIndexState(db)
	if err != nil {
		t.Fatalf("LoadIndexState: %v", err)
	}
	if len(state) != 1 {
		t.Fatalf("expected 1 entry after upsert, got %d", len(state))
	}
	if state[entry.SourcePath].SourceSizeBytes != 2048 {
		t.Fatalf("source_size_bytes after upsert = %d, want 2048", state[entry.SourcePath].SourceSizeBytes)
	}
}

func TestDeleteRowsBySource(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer db.Close()

	sourcePath := "/data/messages/year=2026/week=12/part-0.parquet"

	// Insert a session and a message from this source.
	if _, err := db.Exec(`
		INSERT INTO sessions (partition_source_path, session_id, parent_session_id, host_id, agent, subagent_name, is_subagent, workspace, git_remote, model, source_path, first_message_at, last_message_at, total_input_tokens, total_output_tokens, total_tokens, total_bytes, total_output_bytes, total_input_bytes, transcript_truncated, schema_version)
		VALUES (?, 'sess-1', '', 'host1', 'claude', '', 0, '/work', 'origin', 'opus', '/src', 1000, 2000, 100, 200, 300, 400, 200, 200, 'hello world', 1)
	`, sourcePath); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO messages (partition_source_path, message_id, session_id, host_id, message_index, role, content, content_truncated, timestamp, tool_name, tool_input, tool_file_path, tool_file_start_line, tool_file_num_lines, tool_file_total_lines, bash_command, bash_exit_code, skill_name, input_tokens, cache_input_tokens, output_tokens, workspace, git_remote, git_branch, model, parent_session_id, is_subagent, source_line_index, schema_version)
		VALUES (?, 'msg-1', 'sess-1', 'host1', 0, 'user', 'full content', 'truncated', 1000, '', '', '', 0, 0, 0, '', 0, '', 10, 0, 20, '/work', 'origin', 'main', 'opus', '', 0, 0, 1)
	`, sourcePath); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	// Insert index state.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := indexdb.UpsertIndexState(tx, &indexdb.PartitionState{
		Dataset:             "messages",
		PartitionKey:        "year=2026/week=12",
		SourcePath:          sourcePath,
		SourceSizeBytes:     1024,
		SourceMtimeUnixMs:   1711100000000,
		SourceSchemaVersion: 1,
		IndexedAtUnixMs:     1711200000000,
	}); err != nil {
		t.Fatalf("UpsertIndexState: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Verify rows exist.
	sessions, messages, indexState, err := indexdb.RowCounts(db)
	if err != nil {
		t.Fatalf("RowCounts: %v", err)
	}
	if sessions != 1 || messages != 1 || indexState != 1 {
		t.Fatalf("expected 1/1/1, got %d/%d/%d", sessions, messages, indexState)
	}

	// Delete by source.
	tx2, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := indexdb.DeleteRowsBySource(tx2, sourcePath); err != nil {
		t.Fatalf("DeleteRowsBySource: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Verify all rows are gone.
	sessions, messages, indexState, err = indexdb.RowCounts(db)
	if err != nil {
		t.Fatalf("RowCounts: %v", err)
	}
	if sessions != 0 || messages != 0 || indexState != 0 {
		t.Fatalf("expected 0/0/0, got %d/%d/%d", sessions, messages, indexState)
	}
}

func TestFTSTriggersSync(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer db.Close()

	// Insert a session.
	if _, err := db.Exec(`
		INSERT INTO sessions (partition_source_path, session_id, parent_session_id, host_id, agent, subagent_name, is_subagent, workspace, git_remote, model, source_path, first_message_at, last_message_at, total_input_tokens, total_output_tokens, total_tokens, total_bytes, total_output_bytes, total_input_bytes, transcript_truncated, schema_version)
		VALUES ('/src.parquet', 'sess-fts', '', 'host1', 'claude', '', 0, '/workspace/project', 'git@github.com:test/repo', 'opus', '/src', 1000, 2000, 100, 200, 300, 400, 200, 200, 'debugging authentication middleware', 1)
	`); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	// Insert a message.
	if _, err := db.Exec(`
		INSERT INTO messages (partition_source_path, message_id, session_id, host_id, message_index, role, content, content_truncated, timestamp, tool_name, tool_input, tool_file_path, tool_file_start_line, tool_file_num_lines, tool_file_total_lines, bash_command, bash_exit_code, skill_name, input_tokens, cache_input_tokens, output_tokens, workspace, git_remote, git_branch, model, parent_session_id, is_subagent, source_line_index, schema_version)
		VALUES ('/src.parquet', 'msg-fts', 'sess-fts', 'host1', 0, 'user', 'full content here', 'Exit code 0 from test runner', 1000, '', '', '', 0, 0, 0, '', 0, '', 10, 0, 20, '/workspace/project', 'git@github.com:test/repo', 'main', 'opus', '', 0, 0, 1)
	`); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	// FTS search on sessions should find the session.
	var sessionRowID int
	err = db.QueryRow("SELECT rowid FROM sessions_fts WHERE sessions_fts MATCH 'authentication'").Scan(&sessionRowID)
	if err != nil {
		t.Fatalf("sessions_fts search: %v", err)
	}

	// FTS search on messages should find the message.
	var msgRowID int
	err = db.QueryRow("SELECT rowid FROM messages_fts WHERE messages_fts MATCH '\"Exit code\"'").Scan(&msgRowID)
	if err != nil {
		t.Fatalf("messages_fts search: %v", err)
	}

	// Delete the session and message, verify FTS is cleaned up.
	if _, err := db.Exec("DELETE FROM sessions WHERE session_id = 'sess-fts'"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, err := db.Exec("DELETE FROM messages WHERE message_id = 'msg-fts'"); err != nil {
		t.Fatalf("delete message: %v", err)
	}

	err = db.QueryRow("SELECT rowid FROM sessions_fts WHERE sessions_fts MATCH 'authentication'").Scan(&sessionRowID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no FTS session rows after delete, got err=%v", err)
	}
	err = db.QueryRow("SELECT rowid FROM messages_fts WHERE messages_fts MATCH '\"Exit code\"'").Scan(&msgRowID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no FTS message rows after delete, got err=%v", err)
	}
}

// --- ListSessions tests ---

// createTestDB creates a fresh DB and returns it. Caller must close.
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")
	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return db
}

// insertSession inserts a minimal session row for testing.
func insertSession(t *testing.T, db *sql.DB, sessionID, workspace, remote string, firstMessageAt int64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO sessions (partition_source_path, session_id, parent_session_id, host_id, agent, subagent_name, is_subagent, workspace, git_remote, model, source_path, first_message_at, last_message_at, total_input_tokens, total_output_tokens, total_tokens, total_bytes, total_output_bytes, total_input_bytes, transcript_truncated, schema_version)
		VALUES ('/src.parquet', ?, '', 'host1', 'claude', '', 0, ?, ?, 'opus', '/src', ?, ?, 100, 200, 300, 400, 200, 200, 'transcript', 1)
	`, sessionID, workspace, remote, firstMessageAt, firstMessageAt+1000)
	if err != nil {
		t.Fatalf("insert session %s: %v", sessionID, err)
	}
}

func TestListSessionsEmptyResult(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	// Query with a filter that matches nothing.
	sessions, total, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{
		Workspace: "nonexistent-workspace",
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected total=0, got %d", total)
	}
	if sessions == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestListSessionsPagination(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	// Insert 5 sessions with descending timestamps.
	for i := range 5 {
		insertSession(t, db, fmt.Sprintf("sess-%d", i), "/work", "origin", int64(5000-i*1000))
	}

	// Page 1: limit=2, offset=0 — should get the 2 most recent.
	sessions, total, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{
		Limit:  2,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("ListSessions page 1: %v", err)
	}
	if total != 5 {
		t.Fatalf("expected total=5, got %d", total)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	// First result should be the most recent (highest first_message_at).
	if sessions[0].SessionID != "sess-0" {
		t.Fatalf("expected first session sess-0, got %s", sessions[0].SessionID)
	}

	// Page 2: limit=2, offset=2.
	sessions2, total2, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{
		Limit:  2,
		Offset: 2,
	})
	if err != nil {
		t.Fatalf("ListSessions page 2: %v", err)
	}
	if total2 != 5 {
		t.Fatalf("expected total=5, got %d", total2)
	}
	if len(sessions2) != 2 {
		t.Fatalf("expected 2 sessions on page 2, got %d", len(sessions2))
	}

	// Page 3: limit=2, offset=4 — should get 1 remaining.
	sessions3, _, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{
		Limit:  2,
		Offset: 4,
	})
	if err != nil {
		t.Fatalf("ListSessions page 3: %v", err)
	}
	if len(sessions3) != 1 {
		t.Fatalf("expected 1 session on page 3, got %d", len(sessions3))
	}
}

func TestListSessionsNegativeLimitRejected(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	_, _, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{
		Limit: -1,
	})
	if err == nil {
		t.Fatal("expected error for negative limit, got nil")
	}
}

func TestListSessionsNegativeOffsetRejected(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	_, _, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{
		Offset: -1,
	})
	if err == nil {
		t.Fatal("expected error for negative offset, got nil")
	}
}

// insertSubagentSession inserts a session with subagent fields populated.
func insertSubagentSession(t *testing.T, db *sql.DB, sessionID, parentSessionID, subagentName, workspace string, firstMessageAt, lastMessageAt int64, totalTokens int64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO sessions (partition_source_path, session_id, parent_session_id, host_id, agent, subagent_name, is_subagent, workspace, git_remote, model, source_path, first_message_at, last_message_at, total_input_tokens, total_output_tokens, total_tokens, total_bytes, total_output_bytes, total_input_bytes, transcript_truncated, schema_version)
		VALUES ('/src.parquet', ?, ?, 'host1', 'claude', ?, 1, ?, 'origin', 'opus', '/src', ?, ?, 0, 0, ?, 0, 0, 0, 'transcript', 1)
	`, sessionID, parentSessionID, subagentName, workspace, firstMessageAt, lastMessageAt, totalTokens)
	if err != nil {
		t.Fatalf("insert subagent session %s: %v", sessionID, err)
	}
}

// insertSessionFull inserts a parent session with explicit last_message_at and total_tokens.
func insertSessionFull(t *testing.T, db *sql.DB, sessionID, workspace string, firstMessageAt, lastMessageAt int64, totalTokens int64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO sessions (partition_source_path, session_id, parent_session_id, host_id, agent, subagent_name, is_subagent, workspace, git_remote, model, source_path, first_message_at, last_message_at, total_input_tokens, total_output_tokens, total_tokens, total_bytes, total_output_bytes, total_input_bytes, transcript_truncated, schema_version)
		VALUES ('/src.parquet', ?, '', 'host1', 'claude', '', 0, ?, 'origin', 'opus', '/src', ?, ?, 0, 0, ?, 0, 0, 0, 'transcript', 1)
	`, sessionID, workspace, firstMessageAt, lastMessageAt, totalTokens)
	if err != nil {
		t.Fatalf("insert session %s: %v", sessionID, err)
	}
}

func insertMessage(t *testing.T, db *sql.DB, messageID, sessionID, role string, index int, timestamp int64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO messages (partition_source_path, message_id, session_id, host_id, message_index, role, content, content_truncated, timestamp, tool_name, tool_input, tool_file_path, tool_file_start_line, tool_file_num_lines, tool_file_total_lines, bash_command, bash_exit_code, skill_name, input_tokens, cache_input_tokens, output_tokens, workspace, git_remote, git_branch, model, parent_session_id, is_subagent, source_line_index, schema_version)
		VALUES ('/src.parquet', ?, ?, 'host1', ?, ?, '', '', ?, '', '', '', 0, 0, 0, '', 0, '', 0, 0, 0, '/work', 'origin', 'main', 'opus', '', 0, 0, 1)
	`, messageID, sessionID, index, role, timestamp)
	if err != nil {
		t.Fatalf("insert message %s: %v", messageID, err)
	}
}

func TestListSessionsSubagentFilter(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	insertSessionFull(t, db, "parent-1", "/work", 5000, 6000, 100)
	insertSessionFull(t, db, "parent-2", "/work", 4000, 5000, 200)
	insertSubagentSession(t, db, "sub-1", "parent-1", "Explore", "/work", 5100, 5500, 50)

	// Filter: only subagents
	v := true
	sessions, total, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{IsSubagent: &v})
	if err != nil {
		t.Fatalf("ListSessions subagent=true: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total=1, got %d", total)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "sub-1" {
		t.Fatalf("expected sub-1, got %v", sessions)
	}
	if !sessions[0].IsSubagent {
		t.Fatal("expected IsSubagent=true")
	}
	if sessions[0].ParentSessionID != "parent-1" {
		t.Fatalf("expected ParentSessionID=parent-1, got %q", sessions[0].ParentSessionID)
	}
	if sessions[0].SubagentName != "Explore" {
		t.Fatalf("expected SubagentName=Explore, got %q", sessions[0].SubagentName)
	}

	// Filter: only parents
	f := false
	_, total, err = indexdb.ListSessions(db, &indexdb.ListSessionsOpts{IsSubagent: &f})
	if err != nil {
		t.Fatalf("ListSessions subagent=false: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total=2, got %d", total)
	}

	// No filter: all sessions
	_, total, err = indexdb.ListSessions(db, &indexdb.ListSessionsOpts{})
	if err != nil {
		t.Fatalf("ListSessions no filter: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total=3, got %d", total)
	}
}

func TestListSessionsMinDuration(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	// short: 1000ms, medium: 60000ms (1min), long: 3600000ms (1h)
	insertSessionFull(t, db, "short", "/work", 10000, 11000, 100)
	insertSessionFull(t, db, "medium", "/work", 10000, 70000, 200)
	insertSessionFull(t, db, "long", "/work", 10000, 3610000, 300)

	minMs := int64(60000)
	sessions, total, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{MinDurationMs: &minMs})
	if err != nil {
		t.Fatalf("ListSessions min-duration: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total=2, got %d", total)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Verify the short session is excluded
	for _, s := range sessions {
		if s.SessionID == "short" {
			t.Fatal("short session should have been filtered out")
		}
	}
}

func TestListSessionsSortBy(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	// Different durations, tokens
	insertSessionFull(t, db, "short-many-tokens", "/work", 10000, 11000, 9000) // dur=1000, tokens=9000
	insertSessionFull(t, db, "long-few-tokens", "/work", 10000, 3610000, 100)  // dur=3600000, tokens=100
	insertSessionFull(t, db, "medium-med-tokens", "/work", 10000, 70000, 5000) // dur=60000, tokens=5000

	// Sort by duration DESC
	sessions, _, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{SortBy: "duration"})
	if err != nil {
		t.Fatalf("sort by duration: %v", err)
	}
	if sessions[0].SessionID != "long-few-tokens" {
		t.Fatalf("expected long-few-tokens first by duration, got %s", sessions[0].SessionID)
	}
	if sessions[0].DurationMs != 3600000 {
		t.Fatalf("expected DurationMs=3600000, got %d", sessions[0].DurationMs)
	}

	// Sort by tokens DESC
	sessions, _, err = indexdb.ListSessions(db, &indexdb.ListSessionsOpts{SortBy: "tokens"})
	if err != nil {
		t.Fatalf("sort by tokens: %v", err)
	}
	if sessions[0].SessionID != "short-many-tokens" {
		t.Fatalf("expected short-many-tokens first by tokens, got %s", sessions[0].SessionID)
	}

	// Sort by messages: add messages to one session
	insertMessage(t, db, "m1", "short-many-tokens", "user", 0, 10000)
	insertMessage(t, db, "m2", "short-many-tokens", "assistant", 1, 10100)
	insertMessage(t, db, "m3", "short-many-tokens", "tool", 2, 10200)

	sessions, _, err = indexdb.ListSessions(db, &indexdb.ListSessionsOpts{SortBy: "messages"})
	if err != nil {
		t.Fatalf("sort by messages: %v", err)
	}
	if sessions[0].SessionID != "short-many-tokens" {
		t.Fatalf("expected short-many-tokens first by messages, got %s", sessions[0].SessionID)
	}
	if sessions[0].MessageCount != 3 {
		t.Fatalf("expected MessageCount=3, got %d", sessions[0].MessageCount)
	}
}

func TestListSessionsOutputFields(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	insertSubagentSession(t, db, "sub-x", "parent-x", "general-purpose", "/work", 1000, 5000, 777)

	sessions, _, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.SessionID != "sub-x" {
		t.Fatalf("expected sub-x, got %s", s.SessionID)
	}
	if !s.IsSubagent {
		t.Fatal("expected IsSubagent=true")
	}
	if s.ParentSessionID != "parent-x" {
		t.Fatalf("expected ParentSessionID=parent-x, got %q", s.ParentSessionID)
	}
	if s.SubagentName != "general-purpose" {
		t.Fatalf("expected SubagentName=general-purpose, got %q", s.SubagentName)
	}
	if s.DurationMs != 4000 {
		t.Fatalf("expected DurationMs=4000, got %d", s.DurationMs)
	}
}

// insertSessionWithToolDuration inserts a parent session with explicit
// total_turn_duration_ms set, for testing --sort-by tool_duration.
func insertSessionWithToolDuration(t *testing.T, db *sql.DB, sessionID, workspace string, firstMessageAt, lastMessageAt, totalTurnDurationMs int64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO sessions (partition_source_path, session_id, parent_session_id, host_id, agent, subagent_name, is_subagent, workspace, git_remote, model, source_path, first_message_at, last_message_at, total_turn_duration_ms, total_input_tokens, total_output_tokens, total_tokens, total_bytes, total_output_bytes, total_input_bytes, transcript_truncated, schema_version)
		VALUES ('/src.parquet', ?, '', 'host1', 'claude', '', 0, ?, 'origin', 'opus', '/src', ?, ?, ?, 0, 0, 0, 0, 0, 0, 'transcript', 1)
	`, sessionID, workspace, firstMessageAt, lastMessageAt, totalTurnDurationMs)
	if err != nil {
		t.Fatalf("insert session with tool duration %s: %v", sessionID, err)
	}
}

// insertToolMessage inserts a tool-role message with duration_ms and
// interrupted set, for testing the new EXISTS-based filters.
func insertToolMessage(t *testing.T, db *sql.DB, messageID, sessionID, toolName string, index int, timestamp, durationMs int64, interrupted bool) {
	t.Helper()
	interruptedInt := 0
	if interrupted {
		interruptedInt = 1
	}
	_, err := db.Exec(`
		INSERT INTO messages (partition_source_path, message_id, session_id, host_id, message_index, role, content, content_truncated, timestamp, tool_name, tool_input, tool_file_path, tool_file_start_line, tool_file_num_lines, tool_file_total_lines, bash_command, bash_exit_code, skill_name, tool_use_id, duration_ms, interrupted, input_tokens, cache_input_tokens, output_tokens, workspace, git_remote, git_branch, model, parent_session_id, is_subagent, source_line_index, schema_version)
		VALUES ('/src.parquet', ?, ?, 'host1', ?, 'tool', '', '', ?, ?, '', '', 0, 0, 0, '', 0, '', '', ?, ?, 0, 0, 0, '/work', 'origin', 'main', 'opus', '', 0, 0, 1)
	`, messageID, sessionID, index, timestamp, toolName, durationMs, interruptedInt)
	if err != nil {
		t.Fatalf("insert tool message %s: %v", messageID, err)
	}
}

func TestListSessionsSortByToolDuration(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	// Calendar span vs total_turn_duration_ms is intentionally different:
	// "long-calendar" has a 1-hour calendar span but only 500ms of real work
	// (overnight gap). "short-calendar" has a 1-minute calendar span but
	// 30s of real work (continuous active session). Sorting by
	// tool_duration must pick "short-calendar" first, sorting by duration
	// (calendar span) must pick "long-calendar" first.
	insertSessionWithToolDuration(t, db, "long-calendar", "/work", 10000, 3610000, 500)
	insertSessionWithToolDuration(t, db, "short-calendar", "/work", 10000, 70000, 30000)
	insertSessionWithToolDuration(t, db, "no-work", "/work", 20000, 21000, 0)

	// tool_duration sort
	sessions, _, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{SortBy: "tool_duration"})
	if err != nil {
		t.Fatalf("sort by tool_duration: %v", err)
	}
	if len(sessions) < 2 {
		t.Fatalf("expected >=2 sessions, got %d", len(sessions))
	}
	if sessions[0].SessionID != "short-calendar" {
		t.Fatalf("expected short-calendar first by tool_duration, got %s", sessions[0].SessionID)
	}
	if sessions[0].ToolDurationMs != 30000 {
		t.Fatalf("expected ToolDurationMs=30000, got %d", sessions[0].ToolDurationMs)
	}

	// duration (calendar) sort must NOT have been broken — long-calendar wins.
	sessions, _, err = indexdb.ListSessions(db, &indexdb.ListSessionsOpts{SortBy: "duration"})
	if err != nil {
		t.Fatalf("sort by duration: %v", err)
	}
	if sessions[0].SessionID != "long-calendar" {
		t.Fatalf("expected long-calendar first by duration (calendar), got %s", sessions[0].SessionID)
	}
}

func TestListSessionsMinToolDuration(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	insertSessionFull(t, db, "fast", "/work", 1000, 2000, 100)
	insertSessionFull(t, db, "slow", "/work", 1000, 2000, 100)

	// fast session: only short tool calls
	insertToolMessage(t, db, "fast-m1", "fast", "Bash", 0, 1000, 500, false)
	insertToolMessage(t, db, "fast-m2", "fast", "Read", 1, 1500, 1000, false)
	// slow session: one tool call > 60s
	insertToolMessage(t, db, "slow-m1", "slow", "Bash", 0, 1000, 100, false)
	insertToolMessage(t, db, "slow-m2", "slow", "Bash", 1, 1500, 90_000, false)

	min := int64(60_000)
	sessions, total, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{MinToolDurationMs: &min})
	if err != nil {
		t.Fatalf("ListSessions min-tool-duration: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total=1, got %d", total)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "slow" {
		t.Fatalf("expected only slow session, got %v", sessions)
	}
}

func TestListSessionsInterruptedFilter(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	insertSessionFull(t, db, "clean", "/work", 1000, 2000, 100)
	insertSessionFull(t, db, "stuck", "/work", 1000, 2000, 100)

	insertToolMessage(t, db, "clean-m1", "clean", "Bash", 0, 1000, 500, false)
	insertToolMessage(t, db, "stuck-m1", "stuck", "Bash", 0, 1000, 1000, false)
	insertToolMessage(t, db, "stuck-m2", "stuck", "Bash", 1, 1500, 2000, true)

	sessions, total, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{OnlyInterrupted: true})
	if err != nil {
		t.Fatalf("ListSessions interrupted: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total=1, got %d", total)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "stuck" {
		t.Fatalf("expected only stuck session, got %v", sessions)
	}
}

func TestCountSessionMessagesUserCount(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	insertSession(t, db, "sess-1", "/work", "origin", 1000)
	insertMessage(t, db, "m1", "sess-1", "user", 0, 1000)
	insertMessage(t, db, "m2", "sess-1", "assistant", 1, 1100)
	insertMessage(t, db, "m3", "sess-1", "user", 2, 1200)
	insertMessage(t, db, "m4", "sess-1", "tool", 3, 1300)

	counts, err := indexdb.CountSessionMessages(db, "sess-1")
	if err != nil {
		t.Fatalf("CountSessionMessages: %v", err)
	}
	if counts.Total != 4 {
		t.Fatalf("expected Total=4, got %d", counts.Total)
	}
	if counts.User != 2 {
		t.Fatalf("expected User=2, got %d", counts.User)
	}
	if counts.Tool != 1 {
		t.Fatalf("expected Tool=1, got %d", counts.Tool)
	}
}

func TestRowCountsEmpty(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer db.Close()

	sessions, messages, indexState, err := indexdb.RowCounts(db)
	if err != nil {
		t.Fatalf("RowCounts: %v", err)
	}
	if sessions != 0 || messages != 0 || indexState != 0 {
		t.Fatalf("expected 0/0/0, got %d/%d/%d", sessions, messages, indexState)
	}
}
