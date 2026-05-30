package git

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mistakenot/auto-etl/internal/model"
	sharedgit "github.com/mistakenot/auto-shared/git"
	"github.com/parquet-go/parquet-go"
)

// messageRow reads only the columns needed for session linking.
type messageRow struct {
	SessionID   string `parquet:"session_id"`
	BashCommand string `parquet:"bash_command"`
	Content     string `parquet:"content"`
	GitRemote   string `parquet:"git_remote"`
}

var commitOutputRe = regexp.MustCompile(`\[[\w/.-]+ ([0-9a-f]{7,})\]`)

var commitCommands = []string{"git commit", "git merge", "git cherry-pick"}

func isCommitCommand(bashCmd string) bool {
	lower := strings.ToLower(bashCmd)
	for _, cmd := range commitCommands {
		if strings.Contains(lower, cmd) {
			return true
		}
	}
	return false
}

// LinkSessionIDs enriches commits that lack a SessionID by matching against
// messages parquet data. Only commit-creating bash commands are considered.
// The match is scoped to the same repo via normalized remote URL.
func LinkSessionIDs(commits []model.Commit, messagesDir string, repoRemoteNormalized string) error {
	// Check if any commits need linking
	needsLink := false
	for i := range commits {
		if commits[i].SessionID == "" {
			needsLink = true
			break
		}
	}
	if !needsLink {
		return nil
	}

	// Discover messages parquet files (recursive — messages are partitioned by year=YYYY/week=WW)
	var allFiles []string
	err := filepath.WalkDir(messagesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipAll
			}
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".parquet") {
			allFiles = append(allFiles, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(allFiles) == 0 {
		return nil // No messages data yet — skip silently (AC-5)
	}

	// Build index: captured_short_sha → session_id
	shaIndex := make(map[string]string)

	for _, file := range allFiles {
		rows, err := readParquet[messageRow](file)
		if err != nil {
			continue // Skip unreadable files
		}
		for _, row := range rows {
			if !isCommitCommand(row.BashCommand) {
				continue // AC-3: only commit-creating commands
			}
			if repoRemoteNormalized != "" && row.GitRemote != "" &&
				sharedgit.NormalizeRemoteURL(row.GitRemote) != repoRemoteNormalized {
				continue // Repo scoping
			}
			matches := commitOutputRe.FindAllStringSubmatch(row.Content, -1)
			for _, m := range matches {
				shortSHA := m[1]
				if _, exists := shaIndex[shortSHA]; !exists {
					shaIndex[shortSHA] = row.SessionID
				}
			}
		}
	}

	if len(shaIndex) == 0 {
		return nil
	}

	// Match commits to session IDs via prefix matching
	for i := range commits {
		if commits[i].SessionID != "" {
			continue // AC-4: trailer takes precedence
		}
		// Extract full SHA from Commit.ID (format: "repoID-fullsha")
		fullSHA := strings.TrimPrefix(commits[i].ID, commits[i].RepoID+"-")
		if fullSHA == commits[i].ID {
			continue // Could not extract SHA
		}

		var matchedSessionID string
		matchCount := 0
		for capturedSHA, sessionID := range shaIndex {
			if strings.HasPrefix(fullSHA, capturedSHA) || strings.HasPrefix(capturedSHA, fullSHA) {
				matchedSessionID = sessionID
				matchCount++
			}
		}
		if matchCount == 1 {
			commits[i].SessionID = matchedSessionID
		}
		// matchCount > 1 = ambiguous, skip per AC-5
	}

	return nil
}

// readParquet reads all rows from a parquet file into the given struct type.
// Returns nil, nil if the file doesn't exist.
func readParquet[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	reader := parquet.NewGenericReader[T](f)
	defer func() { _ = reader.Close() }()

	rows := make([]T, reader.NumRows())
	n, err := reader.Read(rows)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return rows[:n], nil
}
