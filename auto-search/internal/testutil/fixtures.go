package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-search/internal/model"
	"github.com/parquet-go/parquet-go"
)

// SessionsFixturePath returns the relative path to the sessions fixture file
// within the given output directory.
func SessionsFixturePath(outputDir string) string {
	return filepath.Join(outputDir, "sessions", "year=2026", "month=03", "sessions.parquet")
}

// MessagesFixturePath returns the relative path to the messages fixture file
// within the given output directory.
func MessagesFixturePath(outputDir string) string {
	return filepath.Join(outputDir, "messages", "year=2026", "week=12", "messages.parquet")
}

// GenerateFixtures creates small parquet fixture files for testing.
// It writes sessions and messages parquet files under outputDir with the
// standard ETL partition layout.
func GenerateFixtures(outputDir string) error {
	if err := generateSessions(outputDir); err != nil {
		return fmt.Errorf("generate sessions: %w", err)
	}
	if err := generateMessages(outputDir); err != nil {
		return fmt.Errorf("generate messages: %w", err)
	}
	return nil
}

func generateSessions(outputDir string) error {
	p := SessionsFixturePath(outputDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}

	sessions := []model.ParquetSessionRow{
		{
			ID:                  "test-session-1",
			ParentSessionID:     "",
			HostID:              "test-host",
			Agent:               "claude",
			SubagentName:        "",
			IsSubagent:          false,
			Workspace:           "/workspace/project-a",
			GitRemote:           "git@github.com:test/repo",
			Model:               "opus",
			SourcePath:          "/tmp/sessions/session1.jsonl",
			FirstMessageAt:      1711000000000,
			LastMessageAt:       1711003600000,
			TotalInputTokens:    5000,
			TotalOutputTokens:   3000,
			TotalTokens:         8000,
			TotalBytes:          20000,
			TotalOutputBytes:    12000,
			TotalInputBytes:     8000,
			TranscriptFull:      "User: Help me debug the authentication middleware retry logic\nAssistant: I'll investigate the auth middleware...",
			TranscriptTruncated: "User: Help me debug the authentication middleware retry logic\nAssistant: I'll investigate the auth middleware. debugging authentication middleware retry logic",
			// Caveat-derived intent: the literal first user message was a
			// <local-command-caveat>/<command-name> wrapper; the heuristic
			// recovered this clean prose intent.
			FirstUserIntent:          "Help me debug the authentication middleware retry logic",
			FirstUserIntentTruncated: "Help me debug the authentication middleware retry logic",
			Year:                     2026,
			Month:                    3,
			SchemaVersion:            1,
		},
		{
			ID:                  "test-session-2",
			ParentSessionID:     "test-session-1",
			HostID:              "test-host",
			Agent:               "claude",
			SubagentName:        "Explore",
			IsSubagent:          true,
			Workspace:           "/workspace/project-a",
			GitRemote:           "git@github.com:test/repo",
			Model:               "sonnet",
			SourcePath:          "/tmp/sessions/session2.jsonl",
			FirstMessageAt:      1711001000000,
			LastMessageAt:       1711002000000,
			TotalInputTokens:    2000,
			TotalOutputTokens:   1500,
			TotalTokens:         3500,
			TotalBytes:          10000,
			TotalOutputBytes:    6000,
			TotalInputBytes:     4000,
			TranscriptFull:      "User: Explore the auth module structure\nAssistant: Looking at the auth directory...",
			TranscriptTruncated: "User: Explore the auth module structure\nAssistant: Looking at the auth directory...",
			// Subagent dispatch prompt is clean prose intent.
			FirstUserIntent:          "Explore the auth module structure",
			FirstUserIntentTruncated: "Explore the auth module structure",
			Year:                     2026,
			Month:                    3,
			SchemaVersion:            1,
		},
		{
			ID:                  "test-session-3",
			ParentSessionID:     "",
			HostID:              "test-host",
			Agent:               "claude",
			SubagentName:        "",
			IsSubagent:          false,
			Workspace:           "/workspace/project-b",
			GitRemote:           "git@github.com:test/other-repo",
			Model:               "sonnet",
			SourcePath:          "/tmp/sessions/session3.jsonl",
			FirstMessageAt:      1711010000000,
			LastMessageAt:       1711013600000,
			TotalInputTokens:    4000,
			TotalOutputTokens:   2500,
			TotalTokens:         6500,
			TotalBytes:          15000,
			TotalOutputBytes:    9000,
			TotalInputBytes:     6000,
			TranscriptFull:      "User: Set up the CI pipeline\nAssistant: I'll configure the CI...",
			TranscriptTruncated: "User: Set up the CI pipeline\nAssistant: I'll configure the CI...",
			// Slash-command fallback: no clean prose user message, so the
			// heuristic fell back to the parsed slash-command invocation.
			FirstUserIntent:          "/execute-task 014",
			FirstUserIntentTruncated: "/execute-task 014",
			Year:                     2026,
			Month:                    3,
			SchemaVersion:            1,
		},
	}

	return writeParquet(p, sessions)
}

func generateMessages(outputDir string) error {
	p := MessagesFixturePath(outputDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}

	// Build a long content string (>4096 chars) for truncation testing.
	longContent := "This is a long message about authentication middleware. " + strings.Repeat("The authentication middleware handles retry logic and session validation across multiple service boundaries. ", 50) + " End of long content."

	// Truncated version: first 2048 + " [...] " + last 2048
	var contentTruncated string
	if len(longContent) > 4096 {
		contentTruncated = longContent[:2048] + " [...] " + longContent[len(longContent)-2048:]
	} else {
		contentTruncated = longContent
	}

	messages := []model.ParquetMessageRow{
		{
			ID:               "msg-001",
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            0,
			Role:             "system",
			Content:          "You are a helpful coding assistant.",
			ContentTruncated: "You are a helpful coding assistant.",
			Timestamp:        1711000000000,
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			GitBranch:        "main",
			Model:            "opus",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "msg-002",
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            1,
			Role:             "user",
			Content:          "Help me debug the authentication middleware retry logic",
			ContentTruncated: "Help me debug the authentication middleware retry logic",
			Timestamp:        1711000100000,
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			GitBranch:        "main",
			Model:            "opus",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "msg-003",
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            2,
			Role:             "assistant",
			Content:          longContent,
			ContentTruncated: contentTruncated,
			Timestamp:        1711000200000,
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			GitBranch:        "main",
			Model:            "opus",
			InputTokens:      500,
			OutputTokens:     1200,
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:                 "msg-004",
			SessionID:          "test-session-1",
			HostID:             "test-host",
			Index:              3,
			Role:               "tool",
			Content:            "package auth\n\nfunc Middleware(next http.Handler) http.Handler {\n\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n\t\t// authentication middleware\n\t\tnext.ServeHTTP(w, r)\n\t})\n}",
			ContentTruncated:   "package auth\n\nfunc Middleware(next http.Handler) http.Handler {\n\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n\t\t// authentication middleware\n\t\tnext.ServeHTTP(w, r)\n\t})\n}",
			Timestamp:          1711000300000,
			ToolName:           "Read",
			ToolFilePath:       "/workspace/project-a/pkg/auth/middleware.go",
			ToolFileStartLine:  1,
			ToolFileNumLines:   8,
			ToolFileTotalLines: 50,
			Workspace:          "/workspace/project-a",
			GitRemote:          "git@github.com:test/repo",
			GitBranch:          "main",
			Model:              "opus",
			Year:               2026,
			Week:               12,
			Month:              3,
			SchemaVersion:      1,
		},
		{
			ID:               "msg-005",
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            4,
			Role:             "tool",
			Content:          "$ go test ./pkg/auth/...\nok  \tproject-a/pkg/auth\t0.015s\nExit code 0",
			ContentTruncated: "$ go test ./pkg/auth/...\nok  \tproject-a/pkg/auth\t0.015s\nExit code 0",
			Timestamp:        1711000400000,
			ToolName:         "Bash",
			BashCommand:      "go test ./pkg/auth/...",
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			GitBranch:        "main",
			Model:            "opus",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "msg-006",
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            5,
			Role:             "assistant",
			Content:          "The tests pass. The authentication middleware is working correctly now. The xyzzy token validation was the root cause.",
			ContentTruncated: "The tests pass. The authentication middleware is working correctly now. The xyzzy token validation was the root cause.",
			Timestamp:        1711000500000,
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			GitBranch:        "main",
			Model:            "opus",
			InputTokens:      800,
			OutputTokens:     200,
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "msg-007",
			SessionID:        "test-session-2",
			HostID:           "test-host",
			Index:            0,
			Role:             "user",
			Content:          "Explore the auth module structure",
			ContentTruncated: "Explore the auth module structure",
			Timestamp:        1711001000000,
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			GitBranch:        "main",
			Model:            "sonnet",
			ParentSessionID:  "test-session-1",
			IsSubagent:       true,
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "msg-008",
			SessionID:        "test-session-2",
			HostID:           "test-host",
			Index:            1,
			Role:             "assistant",
			Content:          "I found the auth module at /workspace/project-a/pkg/auth/. It contains middleware.go and token.go.",
			ContentTruncated: "I found the auth module at /workspace/project-a/pkg/auth/. It contains middleware.go and token.go.",
			Timestamp:        1711001100000,
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			GitBranch:        "main",
			Model:            "sonnet",
			ParentSessionID:  "test-session-1",
			IsSubagent:       true,
			InputTokens:      300,
			OutputTokens:     150,
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "msg-009",
			SessionID:        "test-session-3",
			HostID:           "test-host",
			Index:            0,
			Role:             "user",
			Content:          "Set up the CI pipeline for this project",
			ContentTruncated: "Set up the CI pipeline for this project",
			Timestamp:        1711010000000,
			Workspace:        "/workspace/project-b",
			GitRemote:        "git@github.com:test/other-repo",
			GitBranch:        "main",
			Model:            "sonnet",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "msg-010",
			SessionID:        "test-session-3",
			HostID:           "test-host",
			Index:            1,
			Role:             "assistant",
			Content:          "I'll create a GitHub Actions workflow for CI. Exit code 0 from the lint step confirms the config is valid.",
			ContentTruncated: "I'll create a GitHub Actions workflow for CI. Exit code 0 from the lint step confirms the config is valid.",
			Timestamp:        1711010100000,
			Workspace:        "/workspace/project-b",
			GitRemote:        "git@github.com:test/other-repo",
			GitBranch:        "main",
			Model:            "sonnet",
			InputTokens:      400,
			OutputTokens:     300,
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "msg-011",
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            6,
			Role:             "assistant",
			Content:          "",
			ContentTruncated: `{"skill":"contextual-commit","args":"-m fix auth"}`,
			Timestamp:        1711000600000,
			ToolName:         "Skill",
			ToolInput:        `{"skill":"contextual-commit","args":"-m fix auth"}`,
			SkillName:        "contextual-commit",
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			GitBranch:        "main",
			Model:            "opus",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "msg-012",
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            7,
			Role:             "tool",
			Content:          "Committed: fix auth retry logic",
			ContentTruncated: "Committed: fix auth retry logic",
			Timestamp:        1711000700000,
			ToolName:         "Skill",
			SkillName:        "contextual-commit",
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			GitBranch:        "main",
			Model:            "opus",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
	}

	return writeParquet(p, messages)
}

// GenerateDuplicateMessageFixtures creates a parquet file containing messages
// with a duplicate message_id, for testing duplicate-skip behavior.
func GenerateDuplicateMessageFixtures(outputDir string) error {
	p := MessagesFixturePath(outputDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}

	messages := []model.ParquetMessageRow{
		{
			ID:               "msg-001",
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            0,
			Role:             "user",
			Content:          "First message",
			ContentTruncated: "First message",
			Timestamp:        1711000000000,
			Workspace:        "/workspace/project-a",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "msg-001", // duplicate message_id
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            0,
			Role:             "user",
			Content:          "Duplicate of first message",
			ContentTruncated: "Duplicate of first message",
			Timestamp:        1711000000000,
			Workspace:        "/workspace/project-a",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
		{
			ID:               "msg-002",
			SessionID:        "test-session-1",
			HostID:           "test-host",
			Index:            1,
			Role:             "assistant",
			Content:          "Second message",
			ContentTruncated: "Second message",
			Timestamp:        1711000100000,
			Workspace:        "/workspace/project-a",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    1,
		},
	}

	return writeParquet(p, messages)
}

// GenerateDuplicateSessionFixtures creates a parquet file containing sessions
// with a duplicate session_id, for testing duplicate-skip behavior.
func GenerateDuplicateSessionFixtures(outputDir string) error {
	p := SessionsFixturePath(outputDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}

	sessions := []model.ParquetSessionRow{
		{
			ID:                  "test-session-1",
			HostID:              "test-host",
			Agent:               "claude",
			Workspace:           "/workspace/project-a",
			FirstMessageAt:      1711000000000,
			LastMessageAt:       1711003600000,
			TranscriptTruncated: "First session",
			Year:                2026,
			Month:               3,
			SchemaVersion:       1,
		},
		{
			ID:                  "test-session-1", // duplicate session_id
			HostID:              "test-host",
			Agent:               "claude",
			Workspace:           "/workspace/project-a",
			FirstMessageAt:      1711000000000,
			LastMessageAt:       1711003600000,
			TranscriptTruncated: "Duplicate session",
			Year:                2026,
			Month:               3,
			SchemaVersion:       1,
		},
		{
			ID:                  "test-session-2",
			HostID:              "test-host",
			Agent:               "claude",
			Workspace:           "/workspace/project-b",
			FirstMessageAt:      1711010000000,
			LastMessageAt:       1711013600000,
			TranscriptTruncated: "Second session",
			Year:                2026,
			Month:               3,
			SchemaVersion:       1,
		},
	}

	return writeParquet(p, sessions)
}

// AUQEnvelopeJSON is the toolUseResult envelope written onto the AUQ
// tool_result fixture row. It carries an answers map plus per-question
// annotation notes, mirroring the live JSONL shape.
const AUQEnvelopeJSON = `{"questions":[{"question":"Which database should we use?","options":[{"label":"Postgres (Recommended)"},{"label":"SQLite"}]}],"answers":{"Which database should we use?":"Postgres (Recommended)"},"annotations":{"Which database should we use?":{"notes":"prefer managed instance"}}}`

// GenerateAUQFixtures writes a sessions + messages parquet pair containing two
// AskUserQuestion messages: an assistant tool_use row (no envelope) and a tool
// tool_result row carrying the toolUseResult envelope. Used to exercise the
// tool_use_result_json round-trip through index → SQLite → message describe.
func GenerateAUQFixtures(outputDir string) error {
	sp := SessionsFixturePath(outputDir)
	if err := os.MkdirAll(filepath.Dir(sp), 0o755); err != nil {
		return err
	}
	sessions := []model.ParquetSessionRow{
		{
			ID:                  "auq-session-1",
			HostID:              "test-host",
			Agent:               "claude",
			Workspace:           "/workspace/project-a",
			GitRemote:           "git@github.com:test/repo",
			Model:               "opus",
			FirstMessageAt:      1711000000000,
			LastMessageAt:       1711000200000,
			TranscriptTruncated: "User asks a question via AskUserQuestion",
			Year:                2026,
			Month:               3,
			SchemaVersion:       1,
		},
	}
	if err := writeParquet(sp, sessions); err != nil {
		return err
	}

	mp := MessagesFixturePath(outputDir)
	if err := os.MkdirAll(filepath.Dir(mp), 0o755); err != nil {
		return err
	}
	messages := []model.ParquetMessageRow{
		{
			ID:               "auq-msg-use",
			SessionID:        "auq-session-1",
			HostID:           "test-host",
			Index:            0,
			Role:             "assistant",
			Content:          "AskUserQuestion: Which database should we use?",
			ContentTruncated: "AskUserQuestion: Which database should we use?",
			Timestamp:        1711000100000,
			ToolName:         "AskUserQuestion",
			ToolInput:        `{"questions":[{"question":"Which database should we use?","options":[{"label":"Postgres (Recommended)"},{"label":"SQLite"}]}]}`,
			Workspace:        "/workspace/project-a",
			GitRemote:        "git@github.com:test/repo",
			Model:            "opus",
			Year:             2026,
			Week:             12,
			Month:            3,
			SchemaVersion:    3,
		},
		{
			ID:                "auq-msg-result",
			SessionID:         "auq-session-1",
			HostID:            "test-host",
			Index:             1,
			Role:              "tool",
			Content:           "Postgres (Recommended)",
			ContentTruncated:  "Postgres (Recommended)",
			Timestamp:         1711000200000,
			ToolName:          "AskUserQuestion",
			ToolUseResultJSON: AUQEnvelopeJSON,
			Workspace:         "/workspace/project-a",
			GitRemote:         "git@github.com:test/repo",
			Model:             "opus",
			Year:              2026,
			Week:              12,
			Month:             3,
			SchemaVersion:     3,
		},
	}
	return writeParquet(mp, messages)
}

// AUQAcceptancePair describes one AskUserQuestion call/result pair for the
// recommended-acceptance fixture: the assistant tool_use row carries the
// questions/options in toolInput, the tool tool_result row carries the
// toolUseResult envelope (answers + optional annotations).
type AUQAcceptancePair struct {
	SessionID  string
	ToolInput  string // questions/options array on the assistant tool_use row
	ResultJSON string // toolUseResult envelope on the tool tool_result row
}

// GenerateAUQAcceptanceFixtures writes a sessions + messages parquet pair built
// from the supplied AUQ pairs. Each pair produces two messages in its own
// session: an assistant AskUserQuestion tool_use row (toolInput populated, no
// envelope) at index 0 and a tool tool_result row (toolUseResult envelope
// populated) at index 1. This drives the SQL recommended-acceptance test.
func GenerateAUQAcceptanceFixtures(outputDir string, pairs []AUQAcceptancePair) error {
	sp := SessionsFixturePath(outputDir)
	if err := os.MkdirAll(filepath.Dir(sp), 0o755); err != nil {
		return err
	}
	var sessions []model.ParquetSessionRow
	var messages []model.ParquetMessageRow
	ts := int64(1711000000000)
	for _, p := range pairs {
		sessions = append(sessions, model.ParquetSessionRow{
			ID:                  p.SessionID,
			HostID:              "test-host",
			Agent:               "claude",
			Workspace:           "/workspace/project-a",
			GitRemote:           "git@github.com:test/repo",
			Model:               "opus",
			FirstMessageAt:      ts,
			LastMessageAt:       ts + 100,
			TranscriptTruncated: "AskUserQuestion call",
			Year:                2026,
			Month:               3,
			SchemaVersion:       1,
		})
		messages = append(messages,
			model.ParquetMessageRow{
				ID:               p.SessionID + "-use",
				SessionID:        p.SessionID,
				HostID:           "test-host",
				Index:            0,
				Role:             "assistant",
				Content:          "AskUserQuestion",
				ContentTruncated: "AskUserQuestion",
				Timestamp:        ts,
				ToolName:         "AskUserQuestion",
				ToolInput:        p.ToolInput,
				Workspace:        "/workspace/project-a",
				GitRemote:        "git@github.com:test/repo",
				Model:            "opus",
				Year:             2026,
				Week:             12,
				Month:            3,
				SchemaVersion:    3,
			},
			model.ParquetMessageRow{
				ID:                p.SessionID + "-result",
				SessionID:         p.SessionID,
				HostID:            "test-host",
				Index:             1,
				Role:              "tool",
				Content:           "answer recorded",
				ContentTruncated:  "answer recorded",
				Timestamp:         ts + 100,
				ToolName:          "AskUserQuestion",
				ToolUseResultJSON: p.ResultJSON,
				Workspace:         "/workspace/project-a",
				GitRemote:         "git@github.com:test/repo",
				Model:             "opus",
				Year:              2026,
				Week:              12,
				Month:             3,
				SchemaVersion:     3,
			},
		)
		ts += 1000
	}
	if err := writeParquet(sp, sessions); err != nil {
		return err
	}

	mp := MessagesFixturePath(outputDir)
	if err := os.MkdirAll(filepath.Dir(mp), 0o755); err != nil {
		return err
	}
	return writeParquet(mp, messages)
}

func writeParquet[T any](path string, rows []T) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	writer := parquet.NewGenericWriter[T](f)
	if _, err := writer.Write(rows); err != nil {
		return fmt.Errorf("write rows to %s: %w", path, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close writer for %s: %w", path, err)
	}
	return nil
}
