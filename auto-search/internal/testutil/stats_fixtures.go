package testutil

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-search/internal/model"
)

// GenerateStatsEdgeCaseFixtures writes deterministic parquet fixtures tailored
// for stats edge cases such as bash normalization, empty bucket handling, and
// deterministic sample tie-breaks.
func GenerateStatsEdgeCaseFixtures(outputDir string) error {
	if err := generateSessions(outputDir); err != nil {
		return fmt.Errorf("generate sessions: %w", err)
	}

	p := MessagesFixturePath(outputDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}

	rows := []model.ParquetMessageRow{
		{
			ID:               "stats-msg-001",
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            0,
			Role:             "tool",
			Content:          "Exit code 1 from build",
			ContentTruncated: "Exit code 1 from build",
			Timestamp:        1711000000000,
			ToolName:         "Bash",
			BashCommand:      "cd auto-search && go build ./...",
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			Model:            "opus",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "stats-msg-002",
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            1,
			Role:             "tool",
			Content:          "Exit code 1 from tests",
			ContentTruncated: "Exit code 1 from tests",
			Timestamp:        1711000000000,
			ToolName:         "Bash",
			BashCommand:      "FOO=1 BAR=2 go test ./...",
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			Model:            "opus",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "stats-msg-003",
			SessionID:        "test-session-2",
			HostID:           "test-host",
			Index:            0,
			Role:             "tool",
			Content:          "Exit code 1 from vet",
			ContentTruncated: "Exit code 1 from vet",
			Timestamp:        1711001000000,
			ToolName:         "Bash",
			BashCommand:      "bash -lc 'go vet ./...'",
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			Model:            "sonnet",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "stats-msg-004",
			SessionID:        "test-session-3",
			HostID:           "test-host",
			Index:            0,
			Role:             "assistant",
			Content:          "No tool command",
			ContentTruncated: "No tool command",
			Timestamp:        1711010000000,
			Workspace:        "/workspace/project-b",
			GitRemote:        "git@github.com:test/other-repo",
			Model:            "sonnet",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
	}

	if err := writeParquet(p, rows); err != nil {
		return fmt.Errorf("write stats messages fixture: %w", err)
	}
	return nil
}
