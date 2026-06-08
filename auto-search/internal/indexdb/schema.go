package indexdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// SchemaVersion is bumped whenever the index layout changes, forcing a full rebuild.
const SchemaVersion = 9

// schemaSQL contains the DDL for all base tables, indexes, and FTS virtual tables.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS schema_info (
  schema_version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS index_state (
  dataset TEXT NOT NULL,
  partition_key TEXT NOT NULL,
  source_path TEXT NOT NULL PRIMARY KEY,
  source_size_bytes INTEGER NOT NULL,
  source_mtime_unix_ms INTEGER NOT NULL,
  source_schema_version INTEGER NOT NULL,
  indexed_at_unix_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  doc_id INTEGER PRIMARY KEY,
  partition_source_path TEXT NOT NULL,
  session_id TEXT NOT NULL UNIQUE,
  parent_session_id TEXT NOT NULL,
  host_id TEXT NOT NULL,
  agent TEXT NOT NULL,
  subagent_name TEXT NOT NULL,
  is_subagent INTEGER NOT NULL,
  workspace TEXT NOT NULL,
  git_remote TEXT NOT NULL,
  model TEXT NOT NULL,
  source_path TEXT NOT NULL,
  first_message_at INTEGER NOT NULL,
  last_message_at INTEGER NOT NULL,
  total_turn_duration_ms INTEGER NOT NULL DEFAULT 0,
  total_input_tokens INTEGER NOT NULL,
  total_output_tokens INTEGER NOT NULL,
  total_tokens INTEGER NOT NULL,
  total_bytes INTEGER NOT NULL,
  total_output_bytes INTEGER NOT NULL,
  total_input_bytes INTEGER NOT NULL,
  transcript_truncated TEXT NOT NULL,
  first_user_intent TEXT NOT NULL DEFAULT '',
  first_user_intent_truncated TEXT NOT NULL DEFAULT '',
  permission_mode TEXT NOT NULL DEFAULT '',
  version TEXT NOT NULL DEFAULT '',
  schema_version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
  doc_id INTEGER PRIMARY KEY,
  partition_source_path TEXT NOT NULL,
  message_id TEXT NOT NULL UNIQUE,
  session_id TEXT NOT NULL,
  host_id TEXT NOT NULL,
  message_index INTEGER NOT NULL,
  role TEXT NOT NULL,
  content TEXT NOT NULL,
  content_truncated TEXT NOT NULL,
  timestamp INTEGER NOT NULL,
  tool_name TEXT NOT NULL,
  tool_input TEXT NOT NULL,
  tool_file_path TEXT NOT NULL,
  tool_file_start_line INTEGER NOT NULL,
  tool_file_num_lines INTEGER NOT NULL,
  tool_file_total_lines INTEGER NOT NULL,
  bash_command TEXT NOT NULL,
  bash_exit_code INTEGER NOT NULL,
  skill_name TEXT NOT NULL,
  tool_use_result_json TEXT NOT NULL DEFAULT '',
  tool_use_id TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER NOT NULL DEFAULT 0,
  interrupted INTEGER NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL,
  cache_input_tokens INTEGER NOT NULL,
  output_tokens INTEGER NOT NULL,
  workspace TEXT NOT NULL,
  git_remote TEXT NOT NULL,
  git_branch TEXT NOT NULL,
  model TEXT NOT NULL,
  parent_session_id TEXT NOT NULL,
  is_subagent INTEGER NOT NULL,
  source_line_index INTEGER NOT NULL,
  thinking_signature TEXT NOT NULL DEFAULT '',
  stop_reason TEXT NOT NULL DEFAULT '',
  is_error BOOLEAN NOT NULL DEFAULT 0,
  cache_creation_input_tokens INTEGER NOT NULL DEFAULT 0,
  cache_read_input_tokens INTEGER NOT NULL DEFAULT 0,
  schema_version INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_session_id ON sessions(session_id);
CREATE INDEX IF NOT EXISTS idx_sessions_workspace ON sessions(workspace);
CREATE INDEX IF NOT EXISTS idx_sessions_git_remote ON sessions(git_remote);
CREATE INDEX IF NOT EXISTS idx_sessions_first_message_at ON sessions(first_message_at);

CREATE INDEX IF NOT EXISTS idx_messages_message_id ON messages(message_id);
CREATE INDEX IF NOT EXISTS idx_messages_session_id_message_index ON messages(session_id, message_index);
CREATE INDEX IF NOT EXISTS idx_messages_workspace ON messages(workspace);
CREATE INDEX IF NOT EXISTS idx_messages_git_remote ON messages(git_remote);
CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
CREATE INDEX IF NOT EXISTS idx_messages_skill_name ON messages(skill_name);
CREATE INDEX IF NOT EXISTS idx_messages_role ON messages(role);
CREATE INDEX IF NOT EXISTS idx_messages_tool_name ON messages(tool_name);
CREATE INDEX IF NOT EXISTS idx_messages_workspace_role_timestamp ON messages(workspace, role, timestamp);
CREATE INDEX IF NOT EXISTS idx_sessions_workspace_first_message_at ON sessions(workspace, first_message_at);
CREATE INDEX IF NOT EXISTS idx_sessions_parent_session_id ON sessions(parent_session_id);
CREATE INDEX IF NOT EXISTS idx_messages_bash_exit_code ON messages(bash_exit_code);
CREATE INDEX IF NOT EXISTS idx_messages_tool_use_id ON messages(tool_use_id);
CREATE INDEX IF NOT EXISTS idx_messages_duration_ms ON messages(duration_ms);
CREATE INDEX IF NOT EXISTS idx_messages_interrupted ON messages(interrupted);

CREATE VIRTUAL TABLE IF NOT EXISTS sessions_fts USING fts5(
  transcript_truncated,
  workspace,
  git_remote,
  model,
  content='sessions',
  content_rowid='doc_id',
  tokenize='unicode61'
);

CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
  content_truncated,
  workspace,
  git_remote,
  model,
  content='messages',
  content_rowid='doc_id',
  tokenize='unicode61'
);
`

// ftsTriggersSQL creates triggers to keep FTS tables in sync with base tables.
const ftsTriggersSQL = `
CREATE TRIGGER IF NOT EXISTS sessions_ai AFTER INSERT ON sessions BEGIN
  INSERT INTO sessions_fts(rowid, transcript_truncated, workspace, git_remote, model)
  VALUES (new.doc_id, new.transcript_truncated, new.workspace, new.git_remote, new.model);
END;

CREATE TRIGGER IF NOT EXISTS sessions_ad AFTER DELETE ON sessions BEGIN
  INSERT INTO sessions_fts(sessions_fts, rowid, transcript_truncated, workspace, git_remote, model)
  VALUES ('delete', old.doc_id, old.transcript_truncated, old.workspace, old.git_remote, old.model);
END;

CREATE TRIGGER IF NOT EXISTS sessions_au AFTER UPDATE ON sessions BEGIN
  INSERT INTO sessions_fts(sessions_fts, rowid, transcript_truncated, workspace, git_remote, model)
  VALUES ('delete', old.doc_id, old.transcript_truncated, old.workspace, old.git_remote, old.model);
  INSERT INTO sessions_fts(rowid, transcript_truncated, workspace, git_remote, model)
  VALUES (new.doc_id, new.transcript_truncated, new.workspace, new.git_remote, new.model);
END;

CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, content_truncated, workspace, git_remote, model)
  VALUES (new.doc_id, new.content_truncated, new.workspace, new.git_remote, new.model);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, content_truncated, workspace, git_remote, model)
  VALUES ('delete', old.doc_id, old.content_truncated, old.workspace, old.git_remote, old.model);
END;

CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, content_truncated, workspace, git_remote, model)
  VALUES ('delete', old.doc_id, old.content_truncated, old.workspace, old.git_remote, old.model);
  INSERT INTO messages_fts(rowid, content_truncated, workspace, git_remote, model)
  VALUES (new.doc_id, new.content_truncated, new.workspace, new.git_remote, new.model);
END;
`

// Open opens an existing index database at the given path.
// Returns an error if the file does not exist.
func Open(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("index database not found: %s", path)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open index database %s: %w", path, err)
	}
	if err := configurePragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Create creates a new index database at the given path, applying the full
// schema. The parent directory is created if needed. If the file already
// exists it is removed first so the caller gets a clean database.
func Create(path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create directory %s: %w", dir, err)
	}
	// Remove stale file for a clean full rebuild.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove existing index %s: %w", path, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("create index database %s: %w", path, err)
	}
	if err := configurePragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := applySchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema to %s: %w", path, err)
	}
	return db, nil
}

// applySchema runs the DDL statements, inserts the schema version, and
// creates FTS sync triggers.
func applySchema(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("create tables: %w", err)
	}
	if _, err := db.Exec(ftsTriggersSQL); err != nil {
		return fmt.Errorf("create fts triggers: %w", err)
	}
	if _, err := db.Exec("INSERT INTO schema_info (schema_version) VALUES (?)", SchemaVersion); err != nil {
		return fmt.Errorf("insert schema version: %w", err)
	}
	return nil
}

func configurePragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("exec %s: %w", p, err)
		}
	}
	return nil
}

// ReadSchemaVersion reads the schema version from an existing database.
// Returns 0 if the schema_info table is empty or does not exist.
func ReadSchemaVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRow("SELECT schema_version FROM schema_info LIMIT 1").Scan(&version)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		// Table might not exist (pre-migration DB). Treat as version 0.
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

// TableExists checks whether a table with the given name exists in the database.
func TableExists(db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
