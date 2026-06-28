package artifact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// UploadRecord is one line in ~/.auto/artifact/uploads.jsonl — a local history
// of uploads so users can see what they have shipped without querying (and
// without being able to list) the bucket.
type UploadRecord struct {
	Key          string `json:"key"`
	URL          string `json:"url"`
	OriginalPath string `json:"original_path"`
	Timestamp    string `json:"timestamp"`
	Retention    string `json:"retention"`
	SizeBytes    int64  `json:"size_bytes"`
	ContentType  string `json:"content_type"`
}

// AppendUploadLog appends rec as one JSON line to the log at path, creating the
// parent directory and file as needed.
func AppendUploadLog(path string, rec UploadRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal upload record: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open upload log: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write upload log: %w", err)
	}
	return nil
}
