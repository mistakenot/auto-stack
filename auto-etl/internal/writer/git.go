package writer

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/mistakenot/auto-etl/internal/model"
)

// WriteGit outputs git history data as partitioned parquet files.
//
// Partition scheme:
//   - git_repositories: unpartitioned (read-merge-write by RepoID)
//   - git_refs:         unpartitioned (append-only, dedupe by ID)
//   - commits:          year=YYYY/month=MM/commits.parquet
//   - commit_files:     year=YYYY/month=MM/commit_files.parquet
//   - commit_hunks:     year=YYYY/month=MM/commit_hunks.parquet
func WriteGit(outputDir string, result *model.GitETLResult) error {
	if len(result.Repositories) == 0 && len(result.Refs) == 0 &&
		len(result.Commits) == 0 && len(result.Files) == 0 && len(result.Hunks) == 0 {
		return nil
	}

	// Write git_repositories (unpartitioned, read-merge-write by RepoID)
	if len(result.Repositories) > 0 {
		path := filepath.Join(outputDir, "git_repositories", "git_repositories.parquet")

		existing, err := readExistingParquet[model.GitRepository](path)
		if err != nil {
			log.Printf("warning: could not read existing %s: %v (overwriting)", path, err)
		}

		merged := mergeGitRepos(existing, result.Repositories)
		if err := writeParquet(path, merged); err != nil {
			return fmt.Errorf("write git_repositories: %w", err)
		}
		log.Printf("wrote %s (%d rows)", path, len(merged))
	}

	// Write git_refs (unpartitioned, append-only with dedupe by ID)
	if len(result.Refs) > 0 {
		path := filepath.Join(outputDir, "git_refs", "git_refs.parquet")

		existing, err := readExistingParquet[model.GitRef](path)
		if err != nil {
			log.Printf("warning: could not read existing %s: %v (overwriting)", path, err)
		}

		merged := mergeByID(existing, result.Refs, func(r *model.GitRef) string { return r.ID })
		if err := writeParquet(path, merged); err != nil {
			return fmt.Errorf("write git_refs: %w", err)
		}
		log.Printf("wrote %s (%d rows)", path, len(merged))
	}

	// Write commits (monthly partitions, read-merge-write by ID)
	if len(result.Commits) > 0 {
		commitPartitions := groupCommitsByMonth(result.Commits)
		for key, commits := range commitPartitions {
			dir := filepath.Join(outputDir, "commits", fmt.Sprintf("year=%d", key.Year), fmt.Sprintf("month=%02d", key.Month))
			path := filepath.Join(dir, "commits.parquet")

			existing, err := readExistingParquet[model.Commit](path)
			if err != nil {
				log.Printf("warning: could not read existing %s: %v (overwriting)", path, err)
			}

			merged := mergeByID(existing, commits, func(c *model.Commit) string { return c.ID })
			if err := writeParquet(path, merged); err != nil {
				return fmt.Errorf("write commits year=%d/month=%02d: %w", key.Year, key.Month, err)
			}
			log.Printf("wrote %s (%d rows)", path, len(merged))
		}
	}

	// Write commit_files (monthly partitions, read-merge-write by ID)
	if len(result.Files) > 0 {
		filePartitions := groupCommitFilesByMonth(result.Files)
		for key, files := range filePartitions {
			dir := filepath.Join(outputDir, "commit_files", fmt.Sprintf("year=%d", key.Year), fmt.Sprintf("month=%02d", key.Month))
			path := filepath.Join(dir, "commit_files.parquet")

			existing, err := readExistingParquet[model.CommitFile](path)
			if err != nil {
				log.Printf("warning: could not read existing %s: %v (overwriting)", path, err)
			}

			merged := mergeByID(existing, files, func(f *model.CommitFile) string { return f.ID })
			if err := writeParquet(path, merged); err != nil {
				return fmt.Errorf("write commit_files year=%d/month=%02d: %w", key.Year, key.Month, err)
			}
			log.Printf("wrote %s (%d rows)", path, len(merged))
		}
	}

	// Write commit_hunks (monthly partitions, read-merge-write by ID)
	if len(result.Hunks) > 0 {
		hunkPartitions := groupCommitHunksByMonth(result.Hunks)
		for key, hunks := range hunkPartitions {
			dir := filepath.Join(outputDir, "commit_hunks", fmt.Sprintf("year=%d", key.Year), fmt.Sprintf("month=%02d", key.Month))
			path := filepath.Join(dir, "commit_hunks.parquet")

			existing, err := readExistingParquet[model.CommitHunk](path)
			if err != nil {
				log.Printf("warning: could not read existing %s: %v (overwriting)", path, err)
			}

			merged := mergeByID(existing, hunks, func(h *model.CommitHunk) string { return h.ID })
			if err := writeParquet(path, merged); err != nil {
				return fmt.Errorf("write commit_hunks year=%d/month=%02d: %w", key.Year, key.Month, err)
			}
			log.Printf("wrote %s (%d rows)", path, len(merged))
		}
	}

	return nil
}

// mergeByID merges existing and incoming rows, using the idFunc to extract the unique ID.
// Incoming rows win on duplicate IDs.
func mergeByID[T any](existing, incoming []T, idFunc func(*T) string) []T {
	if len(existing) == 0 {
		return incoming
	}

	// Build set of incoming IDs
	incomingIDs := make(map[string]bool, len(incoming))
	for i := range incoming {
		incomingIDs[idFunc(&incoming[i])] = true
	}

	// Keep existing rows not in incoming set
	result := make([]T, 0, len(existing)+len(incoming))
	for i := range existing {
		if !incomingIDs[idFunc(&existing[i])] {
			result = append(result, existing[i])
		}
	}

	// Append all incoming
	result = append(result, incoming...)
	return result
}

// mergeGitRepos merges repositories by RepoID, preserving the earlier FirstSeenAt
// from existing rows while updating all other fields from incoming rows.
func mergeGitRepos(existing, incoming []model.GitRepository) []model.GitRepository {
	if len(existing) == 0 {
		return incoming
	}

	// Index existing repos by RepoID for FirstSeenAt lookup
	existingByID := make(map[string]*model.GitRepository, len(existing))
	for i := range existing {
		existingByID[existing[i].RepoID] = &existing[i]
	}

	// Build set of incoming RepoIDs
	incomingIDs := make(map[string]bool, len(incoming))
	for i := range incoming {
		incomingIDs[incoming[i].RepoID] = true
	}

	// Start with existing rows not being updated
	result := make([]model.GitRepository, 0, len(existing)+len(incoming))
	for i := range existing {
		if !incomingIDs[existing[i].RepoID] {
			result = append(result, existing[i])
		}
	}

	// Append incoming rows, preserving earlier FirstSeenAt from existing
	for i := range incoming {
		repo := incoming[i]
		if prev, ok := existingByID[repo.RepoID]; ok {
			if prev.FirstSeenAt > 0 && (repo.FirstSeenAt == 0 || prev.FirstSeenAt < repo.FirstSeenAt) {
				repo.FirstSeenAt = prev.FirstSeenAt
			}
		}
		result = append(result, repo)
	}

	return result
}

func groupCommitsByMonth(commits []model.Commit) map[partKey][]model.Commit {
	m := make(map[partKey][]model.Commit)
	for i := range commits {
		k := partKey{Year: int(commits[i].Year), Month: int(commits[i].Month)}
		m[k] = append(m[k], commits[i])
	}
	return m
}

func groupCommitFilesByMonth(files []model.CommitFile) map[partKey][]model.CommitFile {
	m := make(map[partKey][]model.CommitFile)
	for i := range files {
		k := partKey{Year: int(files[i].Year), Month: int(files[i].Month)}
		m[k] = append(m[k], files[i])
	}
	return m
}

func groupCommitHunksByMonth(hunks []model.CommitHunk) map[partKey][]model.CommitHunk {
	m := make(map[partKey][]model.CommitHunk)
	for i := range hunks {
		k := partKey{Year: int(hunks[i].Year), Month: int(hunks[i].Month)}
		m[k] = append(m[k], hunks[i])
	}
	return m
}
