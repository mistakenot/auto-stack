package indexdb_test

import (
	"database/sql"
	"errors"
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
		INSERT INTO messages (partition_source_path, message_id, session_id, host_id, message_index, role, content, content_truncated, timestamp, tool_name, tool_input, tool_file_path, tool_file_start_line, tool_file_num_lines, tool_file_total_lines, bash_command, skill_name, input_tokens, cache_input_tokens, output_tokens, workspace, git_remote, git_branch, model, parent_session_id, is_subagent, source_line_index, schema_version)
		VALUES (?, 'msg-1', 'sess-1', 'host1', 0, 'user', 'full content', 'truncated', 1000, '', '', '', 0, 0, 0, '', '', 10, 0, 20, '/work', 'origin', 'main', 'opus', '', 0, 0, 1)
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
		INSERT INTO messages (partition_source_path, message_id, session_id, host_id, message_index, role, content, content_truncated, timestamp, tool_name, tool_input, tool_file_path, tool_file_start_line, tool_file_num_lines, tool_file_total_lines, bash_command, skill_name, input_tokens, cache_input_tokens, output_tokens, workspace, git_remote, git_branch, model, parent_session_id, is_subagent, source_line_index, schema_version)
		VALUES ('/src.parquet', 'msg-fts', 'sess-fts', 'host1', 0, 'user', 'full content here', 'Exit code 0 from test runner', 1000, '', '', '', 0, 0, 0, '', '', 10, 0, 20, '/workspace/project', 'git@github.com:test/repo', 'main', 'opus', '', 0, 0, 1)
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
