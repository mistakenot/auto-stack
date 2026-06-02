package indexdb

import (
	"database/sql"
	"fmt"
	"io"
	"strings"

	"github.com/mistakenot/auto-search/internal/etlscan"
	"github.com/mistakenot/auto-search/internal/model"
)

// IndexResult holds summary stats from an index build or update.
type IndexResult struct {
	SessionsIndexed   int
	MessagesIndexed   int
	SessionsSkipped   int
	MessagesSkipped   int
	FilesProcessed    int
	PartitionsSkipped int
	FullRebuild       bool
}

// isDuplicateKeyError returns true if the error is a UNIQUE constraint violation.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// FullBuild discovers parquet sources from inputRoot and indexes all rows into
// a fresh database at dbPath. The database is created from scratch (any
// existing file is replaced).
func FullBuild(dbPath, inputRoot string, stderr io.Writer) (*IndexResult, error) {
	sources, err := etlscan.Discover(inputRoot)
	if err != nil {
		return nil, fmt.Errorf("discover parquet sources: %w", err)
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no parquet files found under %s", inputRoot)
	}

	db, err := Create(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	result := &IndexResult{FullRebuild: true}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, src := range sources {
		fmt.Fprintf(stderr, "indexing %s %s\n", src.Dataset, src.PartitionKey)
		switch src.Dataset {
		case "sessions":
			n, skipped, err := indexSessionFile(tx, src, stderr)
			if err != nil {
				return nil, fmt.Errorf("index sessions from %s: %w", src.Path, err)
			}
			result.SessionsIndexed += n
			result.SessionsSkipped += skipped
		case "messages":
			n, skipped, err := indexMessageFile(tx, src, stderr)
			if err != nil {
				return nil, fmt.Errorf("index messages from %s: %w", src.Path, err)
			}
			result.MessagesIndexed += n
			result.MessagesSkipped += skipped
		}

		if err := UpsertIndexState(tx, &PartitionState{
			Dataset:             src.Dataset,
			PartitionKey:        src.PartitionKey,
			SourcePath:          src.Path,
			SourceSizeBytes:     src.SizeBytes,
			SourceMtimeUnixMs:   src.MtimeUnixMs,
			SourceSchemaVersion: SchemaVersion,
			IndexedAtUnixMs:     NowUnixMs(),
		}); err != nil {
			return nil, err
		}
		result.FilesProcessed++
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return result, nil
}

// IncrementalUpdate opens an existing index and updates only dirty partitions.
// A partition is dirty when:
//   - it is the newest partition for its dataset (always reindex)
//   - its source path, size, mtime, or schema version changed
//
// If the DB needs a full rebuild (missing or stale schema), falls back to FullBuild.
func IncrementalUpdate(dbPath, inputRoot string, stderr io.Writer) (*IndexResult, error) {
	rebuild, err := NeedsRebuild(dbPath)
	if err != nil {
		return nil, err
	}
	if rebuild {
		fmt.Fprintf(stderr, "schema changed or db missing, performing full rebuild\n")
		return FullBuild(dbPath, inputRoot, stderr)
	}

	sources, err := etlscan.Discover(inputRoot)
	if err != nil {
		return nil, fmt.Errorf("discover parquet sources: %w", err)
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no parquet files found under %s", inputRoot)
	}

	db, err := Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	existing, err := LoadIndexState(db)
	if err != nil {
		return nil, err
	}

	// Find the newest partition key per dataset.
	newest := newestPartitions(sources)

	// Determine which sources are dirty.
	var dirty []etlscan.ParquetSource
	skipped := 0
	for _, src := range sources {
		if isDirty(src, existing, newest) {
			dirty = append(dirty, src)
		} else {
			skipped++
		}
	}

	result := &IndexResult{
		FullRebuild:       false,
		PartitionsSkipped: skipped,
	}

	if len(dirty) == 0 {
		fmt.Fprintf(stderr, "index is up to date, nothing to do\n")
		return result, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, src := range dirty {
		fmt.Fprintf(stderr, "reindexing %s %s\n", src.Dataset, src.PartitionKey)

		// Delete existing rows for this source before re-inserting.
		if err := DeleteRowsBySource(tx, src.Path); err != nil {
			return nil, fmt.Errorf("delete rows for %s: %w", src.Path, err)
		}

		switch src.Dataset {
		case "sessions":
			n, skipped, err := indexSessionFile(tx, src, stderr)
			if err != nil {
				return nil, fmt.Errorf("index sessions from %s: %w", src.Path, err)
			}
			result.SessionsIndexed += n
			result.SessionsSkipped += skipped
		case "messages":
			n, skipped, err := indexMessageFile(tx, src, stderr)
			if err != nil {
				return nil, fmt.Errorf("index messages from %s: %w", src.Path, err)
			}
			result.MessagesIndexed += n
			result.MessagesSkipped += skipped
		}

		if err := UpsertIndexState(tx, &PartitionState{
			Dataset:             src.Dataset,
			PartitionKey:        src.PartitionKey,
			SourcePath:          src.Path,
			SourceSizeBytes:     src.SizeBytes,
			SourceMtimeUnixMs:   src.MtimeUnixMs,
			SourceSchemaVersion: SchemaVersion,
			IndexedAtUnixMs:     NowUnixMs(),
		}); err != nil {
			return nil, err
		}
		result.FilesProcessed++
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return result, nil
}

// newestPartitions returns a map from dataset name to the lexicographically
// greatest partition key found in the source list.
func newestPartitions(sources []etlscan.ParquetSource) map[string]string {
	newest := make(map[string]string)
	for _, src := range sources {
		if src.PartitionKey > newest[src.Dataset] {
			newest[src.Dataset] = src.PartitionKey
		}
	}
	return newest
}

// isDirty returns true if the given source needs reindexing.
func isDirty(src etlscan.ParquetSource, existing map[string]PartitionState, newest map[string]string) bool {
	// Always reindex the newest partition per dataset.
	if src.PartitionKey == newest[src.Dataset] {
		return true
	}
	prev, found := existing[src.Path]
	if !found {
		// New partition not yet indexed.
		return true
	}
	if prev.SourceSizeBytes != src.SizeBytes {
		return true
	}
	if prev.SourceMtimeUnixMs != src.MtimeUnixMs {
		return true
	}
	if prev.SourceSchemaVersion != SchemaVersion {
		return true
	}
	if prev.SourcePath != src.Path {
		return true
	}
	return false
}

func indexSessionFile(tx *sql.Tx, src etlscan.ParquetSource, stderr io.Writer) (indexed int, skipped int, err error) {
	rows, err := etlscan.ReadSessions(src.Path)
	if err != nil {
		return 0, 0, err
	}
	for i := range rows {
		if err := insertSessionFromParquet(tx, src.Path, &rows[i]); err != nil {
			if isDuplicateKeyError(err) {
				fmt.Fprintf(stderr, "WARNING: skipping duplicate session: session_id=%s source=%s\n",
					rows[i].ID, src.Path)
				skipped++
				continue
			}
			return 0, 0, err
		}
		indexed++
	}
	return indexed, skipped, nil
}

func indexMessageFile(tx *sql.Tx, src etlscan.ParquetSource, stderr io.Writer) (indexed int, skipped int, err error) {
	rows, err := etlscan.ReadMessages(src.Path)
	if err != nil {
		return 0, 0, err
	}
	for i := range rows {
		if err := insertMessageFromParquet(tx, src.Path, &rows[i]); err != nil {
			if isDuplicateKeyError(err) {
				fmt.Fprintf(stderr, "WARNING: skipping duplicate message: message_id=%s session_id=%s source=%s\n",
					rows[i].ID, rows[i].SessionID, src.Path)
				skipped++
				continue
			}
			return 0, 0, err
		}
		indexed++
	}
	return indexed, skipped, nil
}

func insertSessionFromParquet(tx *sql.Tx, sourcePath string, r *model.ParquetSessionRow) error {
	return InsertSession(tx, sourcePath,
		r.ID, r.ParentSessionID, r.HostID, r.Agent, r.SubagentName,
		r.IsSubagent,
		r.Workspace, r.GitRemote, r.Model, r.SourcePath,
		r.FirstMessageAt, r.LastMessageAt,
		r.TotalInputTokens, r.TotalOutputTokens, r.TotalTokens,
		r.TotalBytes, r.TotalOutputBytes, r.TotalInputBytes,
		r.TranscriptTruncated,
		int(r.SchemaVersion),
	)
}

func insertMessageFromParquet(tx *sql.Tx, sourcePath string, r *model.ParquetMessageRow) error {
	return InsertMessage(tx, sourcePath,
		r.ID, r.SessionID, r.HostID,
		int(r.Index),
		r.Role, r.Content, r.ContentTruncated,
		r.Timestamp,
		r.ToolName, r.ToolInput, r.ToolFilePath,
		int(r.ToolFileStartLine), int(r.ToolFileNumLines), int(r.ToolFileTotalLines),
		r.BashCommand,
		int(r.BashExitCode),
		r.SkillName,
		int(r.InputTokens), int(r.CacheInputTokens), int(r.OutputTokens),
		r.Workspace, r.GitRemote, r.GitBranch, r.Model,
		r.ParentSessionID,
		r.IsSubagent,
		int(r.SourceLineIndex), int(r.SchemaVersion),
		r.ToolUseResultJSON,
	)
}
