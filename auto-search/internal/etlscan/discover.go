package etlscan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParquetSource represents a discovered parquet file with its metadata.
type ParquetSource struct {
	// Dataset is the dataset subdirectory name, e.g. "messages", "sessions",
	// "commits", "commit_files", "git_repositories", or "git_refs".
	Dataset string
	// PartitionKey is the Hive-style partition path, e.g. "year=2026/week=12".
	PartitionKey string
	// Path is the absolute filesystem path to the parquet file.
	Path string
	// SizeBytes is the file size.
	SizeBytes int64
	// MtimeUnixMs is the file modification time in unix milliseconds.
	MtimeUnixMs int64
}

// Discover walks the input root and finds all parquet files under the
// messages/ and sessions/ subdirectories. It returns them grouped by dataset.
//
// Discover is intentionally scoped to the messages/sessions datasets the FTS
// indexer consumes. The indexer writes index_state / FilesProcessed for any
// discovered source, so git datasets must NOT leak into it. Callers that need
// git datasets (e.g. co-change) use DiscoverDatasets directly.
func Discover(inputRoot string) ([]ParquetSource, error) {
	return DiscoverDatasets(inputRoot, []string{"messages", "sessions"})
}

// DiscoverDatasets walks the input root and finds all parquet files under each
// of the named dataset subdirectories. It returns them grouped by dataset.
// Datasets whose subdirectory does not exist are skipped silently.
func DiscoverDatasets(inputRoot string, datasets []string) ([]ParquetSource, error) {
	info, err := os.Stat(inputRoot)
	if err != nil {
		return nil, fmt.Errorf("stat input root %s: %w", inputRoot, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("input root is not a directory: %s", inputRoot)
	}

	var sources []ParquetSource
	for _, dataset := range datasets {
		datasetDir := filepath.Join(inputRoot, dataset)
		if _, err := os.Stat(datasetDir); os.IsNotExist(err) {
			continue
		}
		err := filepath.Walk(datasetDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(info.Name(), ".parquet") {
				return nil
			}
			rel, err := filepath.Rel(datasetDir, path)
			if err != nil {
				return fmt.Errorf("rel path for %s: %w", path, err)
			}
			partitionKey := filepath.Dir(rel)
			if partitionKey == "." {
				partitionKey = ""
			}
			sources = append(sources, ParquetSource{
				Dataset:      dataset,
				PartitionKey: partitionKey,
				Path:         path,
				SizeBytes:    info.Size(),
				MtimeUnixMs:  info.ModTime().UnixMilli(),
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", datasetDir, err)
		}
	}
	return sources, nil
}
