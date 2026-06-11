package etlread

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	sharedmodel "github.com/mistakenot/auto-shared/model"
	"github.com/parquet-go/parquet-go"
)

// MsgSignalRow is a narrow projection over AgentMessage — never reads the large content column.
type MsgSignalRow struct {
	SessionID        string `parquet:"session_id,dict"`
	Role             string `parquet:"role,dict"`
	ContentTruncated string `parquet:"content_truncated"`
	ToolName         string `parquet:"tool_name,dict"`
	IsError          bool   `parquet:"is_error"`
	Workspace        string `parquet:"workspace,dict"`
	GitRemote        string `parquet:"git_remote,dict"`
	IsSubagent       bool   `parquet:"is_subagent"`
	ParentSessionID  string `parquet:"parent_session_id,dict"`
}

// SourceState describes the ETL output directory state.
type SourceState int

const (
	SourceOK      SourceState = iota
	SourceEmpty               // directory exists but no parquet files
	SourceMissing             // directory does not exist
)

// ResolveSource checks whether the ETL output root exists and contains parquet files.
func ResolveSource(etlRoot string) (SourceState, error) {
	info, err := os.Stat(etlRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return SourceMissing, nil
		}
		return SourceMissing, fmt.Errorf("stat etl root %s: %w", etlRoot, err)
	}
	if !info.IsDir() {
		return SourceMissing, fmt.Errorf("etl root is not a directory: %s", etlRoot)
	}
	// Check for sessions subdirectory with at least one parquet file
	sessDir := filepath.Join(etlRoot, "sessions")
	if _, err := os.Stat(sessDir); os.IsNotExist(err) {
		return SourceEmpty, nil
	}
	found := false
	_ = filepath.Walk(sessDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".parquet") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if !found {
		return SourceEmpty, nil
	}
	return SourceOK, nil
}

// ReadSessions reads all AgentSession rows from parquet files under etlRoot/sessions/.
func ReadSessions(etlRoot string) ([]sharedmodel.AgentSession, error) {
	return readAll[sharedmodel.AgentSession](filepath.Join(etlRoot, "sessions"))
}

// ReadMessageSignals reads the narrow MsgSignalRow projection from parquet files under etlRoot/messages/.
func ReadMessageSignals(etlRoot string) ([]MsgSignalRow, error) {
	return readAll[MsgSignalRow](filepath.Join(etlRoot, "messages"))
}

func readAll[T any](dir string) ([]T, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}
	var all []T
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".parquet") {
			return nil
		}
		rows, err := readParquetFile[T](path)
		if err != nil {
			return err
		}
		all = append(all, rows...)
		return nil
	})
	return all, err
}

func readParquetFile[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	pf, err := parquet.OpenFile(f, stat.Size())
	if err != nil {
		return nil, fmt.Errorf("open parquet %s: %w", path, err)
	}

	reader := parquet.NewGenericReader[T](pf)
	defer func() { _ = reader.Close() }()

	var all []T
	batch := make([]T, 1024)
	for {
		n, err := reader.Read(batch)
		if n > 0 {
			all = append(all, batch[:n]...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
	}
	return all, nil
}
