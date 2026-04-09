package writer

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-etl/internal/model"
	"github.com/parquet-go/parquet-go"
)

// WriteGitHub outputs PR and comment data as partitioned parquet files.
//
// Unlike session/message partitions, GitHub partitions are NOT immutable.
// Retried PRs may update historical month partitions, so we do a
// read-merge-write with deduplication.
//
// Partition scheme:
//   - pull_requests: year=YYYY/month=MM/pull_requests.parquet
//   - pull_request_comments:   year=YYYY/month=MM/pull_request_comments.parquet
func WriteGitHub(outputDir string, result *model.GitHubSyncResult) error {
	if len(result.PullRequests) == 0 && len(result.Comments) == 0 {
		return nil
	}

	// Write pull_requests (monthly partitions, read-merge-write)
	prPartitions := groupPRsByMonth(result.PullRequests)
	for key, prs := range prPartitions {
		dir := filepath.Join(outputDir, "pull_requests", fmt.Sprintf("year=%d", key.Year), fmt.Sprintf("month=%02d", key.Month))
		path := filepath.Join(dir, "pull_requests.parquet")

		existing, err := readExistingParquet[model.PullRequest](path)
		if err != nil {
			log.Printf("warning: could not read existing %s: %v (overwriting)", path, err)
		}

		merged := mergePRs(existing, prs)
		if err := writeParquet(path, merged); err != nil {
			return fmt.Errorf("write pull_requests year=%d/month=%02d: %w", key.Year, key.Month, err)
		}
		log.Printf("wrote %s (%d rows)", path, len(merged))
	}

	// Write pull_request_comments (monthly partitions, read-merge-write)
	commentPartitions := groupCommentsByMonth(result.Comments)
	for key, comments := range commentPartitions {
		dir := filepath.Join(outputDir, "pull_request_comments", fmt.Sprintf("year=%d", key.Year), fmt.Sprintf("month=%02d", key.Month))
		path := filepath.Join(dir, "pull_request_comments.parquet")

		existing, err := readExistingParquet[model.PRComment](path)
		if err != nil {
			log.Printf("warning: could not read existing %s: %v (overwriting)", path, err)
		}

		// For retried PRs, delete all old comments for that PR then insert new ones
		merged := mergeComments(existing, comments)
		if err := writeParquet(path, merged); err != nil {
			return fmt.Errorf("write pull_request_comments year=%d/month=%02d: %w", key.Year, key.Month, err)
		}
		log.Printf("wrote %s (%d rows)", path, len(merged))
	}

	return nil
}

func groupPRsByMonth(prs []model.PullRequest) map[partKey][]model.PullRequest {
	m := make(map[partKey][]model.PullRequest)
	for i := range prs {
		k := partKey{Year: int(prs[i].Year), Month: int(prs[i].Month)}
		m[k] = append(m[k], prs[i])
	}
	return m
}

func groupCommentsByMonth(comments []model.PRComment) map[partKey][]model.PRComment {
	m := make(map[partKey][]model.PRComment)
	for i := range comments {
		k := partKey{Year: int(comments[i].Year), Month: int(comments[i].Month)}
		m[k] = append(m[k], comments[i])
	}
	return m
}

// mergePRs deduplicates by PR ID. New data wins.
func mergePRs(existing, incoming []model.PullRequest) []model.PullRequest {
	if len(existing) == 0 {
		return incoming
	}

	// Build set of incoming IDs
	incomingIDs := make(map[string]bool, len(incoming))
	for i := range incoming {
		incomingIDs[incoming[i].ID] = true
	}

	// Keep existing rows not in incoming set
	result := make([]model.PullRequest, 0, len(existing)+len(incoming))
	for i := range existing {
		if !incomingIDs[existing[i].ID] {
			result = append(result, existing[i])
		}
	}

	// Append all incoming
	result = append(result, incoming...)
	return result
}

// mergeComments deduplicates by comment ID. For retried PRs, all old
// comments for that PR are replaced with the fresh fetch.
func mergeComments(existing, incoming []model.PRComment) []model.PRComment {
	if len(existing) == 0 {
		return incoming
	}

	// Collect PR IDs that are being refreshed
	refreshedPRs := make(map[string]bool)
	incomingIDs := make(map[string]bool, len(incoming))
	for i := range incoming {
		refreshedPRs[incoming[i].PRID] = true
		incomingIDs[incoming[i].ID] = true
	}

	// Keep existing comments that are NOT from a refreshed PR and NOT duplicates
	result := make([]model.PRComment, 0, len(existing)+len(incoming))
	for i := range existing {
		if refreshedPRs[existing[i].PRID] {
			continue // Drop all old comments for refreshed PRs
		}
		if incomingIDs[existing[i].ID] {
			continue // Dedupe by comment ID
		}
		result = append(result, existing[i])
	}

	result = append(result, incoming...)
	return result
}

// readExistingParquet reads all rows from an existing parquet file.
// Returns nil, nil if the file doesn't exist.
func readExistingParquet[T any](path string) ([]T, error) {
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
