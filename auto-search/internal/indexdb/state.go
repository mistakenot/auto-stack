package indexdb

import (
	"database/sql"
	"fmt"
	"time"
)

// PartitionState represents one row in the index_state table, tracking which
// parquet files have been indexed and their metadata at index time.
type PartitionState struct {
	Dataset             string
	PartitionKey        string
	SourcePath          string
	SourceSizeBytes     int64
	SourceMtimeUnixMs   int64
	SourceSchemaVersion int
	IndexedAtUnixMs     int64
}

// LoadIndexState reads all index_state rows into a map keyed by source_path.
func LoadIndexState(db *sql.DB) (map[string]PartitionState, error) {
	rows, err := db.Query(`
		SELECT dataset, partition_key, source_path, source_size_bytes,
		       source_mtime_unix_ms, source_schema_version, indexed_at_unix_ms
		FROM index_state
	`)
	if err != nil {
		return nil, fmt.Errorf("query index_state: %w", err)
	}
	defer func() { _ = rows.Close() }()

	state := make(map[string]PartitionState)
	for rows.Next() {
		var s PartitionState
		if err := rows.Scan(
			&s.Dataset, &s.PartitionKey, &s.SourcePath,
			&s.SourceSizeBytes, &s.SourceMtimeUnixMs,
			&s.SourceSchemaVersion, &s.IndexedAtUnixMs,
		); err != nil {
			return nil, fmt.Errorf("scan index_state row: %w", err)
		}
		state[s.SourcePath] = s
	}
	return state, rows.Err()
}

// UpsertIndexState inserts or replaces an index_state row for the given source.
func UpsertIndexState(tx *sql.Tx, s *PartitionState) error {
	_, err := tx.Exec(`
		INSERT OR REPLACE INTO index_state
		  (dataset, partition_key, source_path, source_size_bytes,
		   source_mtime_unix_ms, source_schema_version, indexed_at_unix_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, s.Dataset, s.PartitionKey, s.SourcePath,
		s.SourceSizeBytes, s.SourceMtimeUnixMs,
		s.SourceSchemaVersion, s.IndexedAtUnixMs,
	)
	if err != nil {
		return fmt.Errorf("upsert index_state %s: %w", s.SourcePath, err)
	}
	return nil
}

// DeleteRowsBySource removes all session or message rows contributed by the
// given parquet source path. This is used during incremental reindex to
// replace dirty partitions.
func DeleteRowsBySource(tx *sql.Tx, sourcePath string) error {
	if _, err := tx.Exec("DELETE FROM sessions WHERE partition_source_path = ?", sourcePath); err != nil {
		return fmt.Errorf("delete sessions for source %s: %w", sourcePath, err)
	}
	if _, err := tx.Exec("DELETE FROM messages WHERE partition_source_path = ?", sourcePath); err != nil {
		return fmt.Errorf("delete messages for source %s: %w", sourcePath, err)
	}
	if _, err := tx.Exec("DELETE FROM index_state WHERE source_path = ?", sourcePath); err != nil {
		return fmt.Errorf("delete index_state for source %s: %w", sourcePath, err)
	}
	return nil
}

// NowUnixMs returns the current time in milliseconds since epoch.
func NowUnixMs() int64 {
	return time.Now().UnixMilli()
}

// RowCounts returns the number of rows in sessions, messages, and index_state tables.
func RowCounts(db *sql.DB) (sessions, messages, indexState int, err error) {
	for _, q := range []struct {
		table string
		dest  *int
	}{
		{"sessions", &sessions},
		{"messages", &messages},
		{"index_state", &indexState},
	} {
		if err = db.QueryRow("SELECT COUNT(*) FROM " + q.table).Scan(q.dest); err != nil {
			return 0, 0, 0, fmt.Errorf("count %s: %w", q.table, err)
		}
	}
	return
}
