package git

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mistakenot/auto-etl/internal/model"
	"github.com/mistakenot/auto-etl/internal/transform"
	sharedgit "github.com/mistakenot/auto-shared/git"
)

// ExtractConfig holds configuration for extraction.
type ExtractConfig struct {
	HostID      string
	ETLRunID    string
	CollectedAt int64  // Unix ms
	Since       string // e.g. "6m", "1y" — empty means all history
	SeenSHAs    map[string]bool
}

// ExtractRepo extracts git history from a single repo.
// Returns a GitETLResult with all five dataset types populated.
func ExtractRepo(repo RepoInfo, config ExtractConfig) (*model.GitETLResult, error) {
	// Compute repo identity.
	normalized := sharedgit.NormalizeRemoteURL(repo.Remote)
	var repoID string
	if normalized != "" {
		repoID = sharedgit.ComputeRepoID(normalized)
	} else {
		repoID = sharedgit.ComputeRepoIDFromPath(repo.Path)
	}

	// Observe repo metadata.
	repoRow, defaultRef, err := observeRepo(repo.Path, repoID, normalized, repo.Remote, config)
	if err != nil {
		return nil, fmt.Errorf("observeRepo %s: %w", repo.Path, err)
	}

	// Observe refs.
	refs := observeRefs(repo.Path, repoID, config, defaultRef)

	// Extract commits.
	commits := extractCommits(repo.Path, repoID, config)

	// For each new non-merge commit, extract files and hunks.
	var allFiles []model.CommitFile
	var allHunks []model.CommitHunk

	for i := range commits {
		c := &commits[i]
		if c.IsMerge {
			continue
		}

		sha := strings.TrimPrefix(c.ID, repoID+"-")
		files, hunks := extractFilesAndHunks(repo.Path, sha, repoID, c, config)

		// Aggregate stats onto the commit.
		c.FilesChanged = int32(len(files))
		var ins, del int32
		for j := range files {
			ins += files[j].Insertions
			del += files[j].Deletions
		}
		c.Insertions = ins
		c.Deletions = del

		allFiles = append(allFiles, files...)
		allHunks = append(allHunks, hunks...)
	}

	return &model.GitETLResult{
		Repositories: []model.GitRepository{repoRow},
		Refs:         refs,
		Commits:      commits,
		Files:        allFiles,
		Hunks:        allHunks,
	}, nil
}

// observeRepo gathers repository metadata. Returns the GitRepository row and
// the default branch ref name (for matching in observeRefs).
func observeRepo(path, repoID, normalizedRemote, rawRemote string, config ExtractConfig) (model.GitRepository, string, error) {
	toplevel, err := gitToplevel(path)
	if err != nil {
		return model.GitRepository{}, "", err
	}

	// Detect worktree: if git common dir parent differs from toplevel, it's a worktree.
	var worktreePath string
	cmd := exec.Command("git", "-C", path, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err == nil {
		commonDir := strings.TrimSpace(string(out))
		// commonDir is relative or absolute; resolve relative to toplevel.
		if !strings.HasPrefix(commonDir, "/") {
			commonDir = toplevel + "/" + commonDir
		}
		// Trim trailing /.git if present to get parent.
		commonParent := strings.TrimSuffix(commonDir, "/.git")
		commonParent = strings.TrimSuffix(commonParent, "/.")
		if commonParent != toplevel {
			worktreePath = toplevel
		}
	}

	// Default branch.
	var defaultBranch string
	cmd = exec.Command("git", "-C", path, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	out, err = cmd.Output()
	if err == nil {
		defaultBranch = strings.TrimSpace(string(out))
	}

	row := model.GitRepository{
		RepoID:                repoID,
		RepoRemote:            StripCredentials(rawRemote),
		RepoRemoteNormalized:  normalizedRemote,
		RepoPath:              toplevel,
		WorktreePath:          worktreePath,
		DefaultBranchObserved: defaultBranch,
		HostID:                config.HostID,
		FirstSeenAt:           config.CollectedAt,
		LastSeenAt:            config.CollectedAt,
		ETLRunID:              config.ETLRunID,
		CollectedAt:           config.CollectedAt,
		SchemaVersion:         int32(model.SchemaVersion),
	}

	return row, defaultBranch, nil
}

// observeRefs lists all refs in a repo and returns GitRef rows.
func observeRefs(path, repoID string, config ExtractConfig, defaultRef string) []model.GitRef {
	cmd := exec.Command("git", "-C", path, "for-each-ref",
		"--format=%(refname)%00%(objectname)%00%(objecttype)%00",
		"refs/heads", "refs/remotes", "refs/tags")
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: for-each-ref failed in %s: %v\n", path, err)
		return nil
	}
	return parseForEachRef(string(out), repoID, config.ETLRunID, config.CollectedAt, defaultRef)
}

// extractCommits runs git log and parses output into Commit rows.
// Only new commits (not in config.SeenSHAs) are returned.
func extractCommits(path, repoID string, config ExtractConfig) []model.Commit {
	args := []string{"-C", path, "log", "--all",
		"--format=%H%x00%h%x00%T%x00%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%P%x00%B%x00%x00"}

	if config.Since != "" {
		gitSince := convertSinceToGit(config.Since)
		if gitSince != "" {
			args = append(args, "--since="+gitSince)
		}
	}

	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: git log failed in %s: %v\n", path, err)
		return nil
	}

	commits := parseCommitLog(string(out), repoID, config.ETLRunID, config.CollectedAt)

	// Filter to new commits only.
	if len(config.SeenSHAs) > 0 {
		var filtered []model.Commit
		for i := range commits {
			sha := strings.TrimPrefix(commits[i].ID, repoID+"-")
			if !config.SeenSHAs[sha] {
				filtered = append(filtered, commits[i])
			}
		}
		return filtered
	}
	return commits
}

// extractFilesAndHunks extracts file-level and hunk-level data for a single non-merge commit.
func extractFilesAndHunks(path, sha, repoID string, commit *model.Commit, config ExtractConfig) ([]model.CommitFile, []model.CommitHunk) {
	// 1. Get raw file metadata.
	rawCmd := exec.Command("git", "-C", path, "diff-tree", "--root", "-r", "--raw", "-z", "-M", "-C", sha)
	rawOut, err := rawCmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: diff-tree --raw failed for %s in %s: %v\n", sha, path, err)
		return nil, nil
	}
	rawEntries := parseDiffTreeRaw(string(rawOut))

	// 2. Get numstat.
	numstatCmd := exec.Command("git", "-C", path, "diff-tree", "--root", "-r", "--numstat", "-z", "-M", "-C", sha)
	numstatOut, err := numstatCmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: diff-tree --numstat failed for %s in %s: %v\n", sha, path, err)
		return nil, nil
	}
	numstatEntries := parseDiffTreeNumstat(string(numstatOut))

	// 3. Get unified diff.
	diffCmd := exec.Command("git", "-C", path, "show", sha, "--format=", "-p", "--diff-filter=AMDRCT", "-M", "-C")
	diffOut, err := diffCmd.Output()
	if err != nil {
		// Diff may fail for binary-only commits; continue without diffs.
		fmt.Fprintf(os.Stderr, "warning: git show -p failed for %s in %s: %v\n", sha, path, err)
	}
	fileDiffs := parseUnifiedDiff(string(diffOut))

	// Join raw entries with numstat by index order.
	var files []model.CommitFile
	var hunks []model.CommitHunk

	for i, raw := range rawEntries {
		filePath := raw.path
		fileID := fmt.Sprintf("%s-%s-%d", repoID, sha, i)

		var ins, del int32
		var isBinary bool
		if i < len(numstatEntries) {
			ns := numstatEntries[i]
			ins = ns.insertions
			del = ns.deletions
			isBinary = ns.isBinary
		}

		diffText := fileDiffs[filePath]
		diffTruncated := transform.MidTruncate(diffText, model.DefaultTruncateMaxChars)

		cf := model.CommitFile{
			ID:            fileID,
			CommitID:      commit.ID,
			RepoID:        repoID,
			FileIndex:     int32(i),
			FilePath:      filePath,
			ChangeType:    raw.changeType,
			OldPath:       raw.oldPath,
			Insertions:    ins,
			Deletions:     del,
			OldBlobSHA:    raw.oldBlob,
			NewBlobSHA:    raw.newBlob,
			OldMode:       raw.oldMode,
			NewMode:       raw.newMode,
			IsBinary:      isBinary,
			Diff:          diffText,
			DiffTruncated: diffTruncated,
			AuthorDate:    commit.AuthorDate,
			ETLRunID:      config.ETLRunID,
			CollectedAt:   config.CollectedAt,
			Year:          commit.Year,
			Month:         commit.Month,
			SchemaVersion: int32(model.SchemaVersion),
		}
		files = append(files, cf)

		// Parse hunks from the diff text.
		if diffText != "" {
			parsedHunks := parseHunks(diffText)
			for j, ph := range parsedHunks {
				hunkID := fmt.Sprintf("%s-%s-%d-%d", repoID, sha, i, j)
				hunkHash := computeHunkHash(ph.text)

				ch := model.CommitHunk{
					ID:                hunkID,
					CommitID:          commit.ID,
					RepoID:            repoID,
					FileIndex:         int32(i),
					HunkIndex:         int32(j),
					FilePath:          filePath,
					OldPath:           raw.oldPath,
					OldStart:          ph.oldStart,
					OldLines:          ph.oldLines,
					NewStart:          ph.newStart,
					NewLines:          ph.newLines,
					HunkHeader:        ph.header,
					HunkText:          ph.text,
					HunkTextTruncated: transform.MidTruncate(ph.text, model.DefaultTruncateMaxChars),
					HunkHash:          hunkHash,
					AuthorDate:        commit.AuthorDate,
					ETLRunID:          config.ETLRunID,
					CollectedAt:       config.CollectedAt,
					Year:              commit.Year,
					Month:             commit.Month,
					SchemaVersion:     int32(model.SchemaVersion),
				}
				hunks = append(hunks, ch)
			}
		}
	}

	return files, hunks
}

// --- Parsing functions (unexported, unit-testable) ---

// parseForEachRef parses `git for-each-ref` NUL-separated output into GitRef rows.
func parseForEachRef(output string, repoID, etlRunID string, collectedAt int64, defaultRef string) []model.GitRef {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}

	var refs []model.GitRef
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\x00")
		if len(parts) < 3 {
			continue
		}

		refName := parts[0]
		commitID := parts[1]

		var refType string
		var isRemote bool
		switch {
		case strings.HasPrefix(refName, "refs/heads/"):
			refType = "branch"
		case strings.HasPrefix(refName, "refs/remotes/"):
			refType = "remote_branch"
			isRemote = true
		case strings.HasPrefix(refName, "refs/tags/"):
			refType = "tag"
		default:
			refType = "other"
		}

		isDefault := defaultRef != "" && refName == defaultRef

		id := fmt.Sprintf("%s-%s-%d", repoID, refName, collectedAt)

		refs = append(refs, model.GitRef{
			ID:            id,
			RepoID:        repoID,
			RefName:       refName,
			RefType:       refType,
			CommitID:      commitID,
			IsDefault:     isDefault,
			IsRemote:      isRemote,
			ETLRunID:      etlRunID,
			CollectedAt:   collectedAt,
			SchemaVersion: int32(model.SchemaVersion),
		})
	}

	return refs
}

// parseCommitLog parses `git log` output with NUL-separated fields.
// Records are separated by double-NUL (%x00%x00).
func parseCommitLog(output string, repoID, etlRunID string, collectedAt int64) []model.Commit {
	if strings.TrimSpace(output) == "" {
		return nil
	}

	// Split by double-NUL record separator.
	records := strings.Split(output, "\x00\x00")

	var commits []model.Commit
	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}

		// Fields separated by single NUL:
		// 0:SHA 1:short 2:tree 3:author_name 4:author_email 5:author_date
		// 6:committer_name 7:committer_email 8:committer_date 9:parents 10:body
		fields := strings.SplitN(record, "\x00", 11)
		if len(fields) < 11 {
			continue
		}

		sha := fields[0]
		shortSHA := fields[1]
		treeSHA := fields[2]
		authorName := fields[3]
		authorEmail := fields[4]
		authorDateStr := fields[5]
		committerName := fields[6]
		committerEmail := fields[7]
		committerDateStr := fields[8]
		parentsRaw := fields[9]
		messageBody := fields[10]

		// Parse dates.
		authorDate, authorOffset := parseISO8601(authorDateStr)
		committerDate, committerOffset := parseISO8601(committerDateStr)

		// Parse parents.
		var parentSHAs string
		var parentCount int32
		if parentsRaw != "" {
			parents := strings.Fields(parentsRaw)
			parentCount = int32(len(parents))
			parentSHAs = strings.Join(parents, ",")
		}

		isMerge := parentCount > 1

		// Truncate message.
		messageTruncated := transform.MidTruncate(messageBody, model.DefaultTruncateMaxChars)

		// Parse trailers.
		trailersJSON := parseTrailers(messageBody)
		sessionID := extractSessionID(trailersJSON)

		// Partition fields from author date.
		var year, month int32
		if authorDate > 0 {
			t := time.UnixMilli(authorDate)
			year = int32(t.Year())
			month = int32(t.Month())
		}

		// Short ID: first 8 chars.
		shortID := shortSHA
		if len(sha) >= 8 {
			shortID = sha[:8]
		}

		id := fmt.Sprintf("%s-%s", repoID, sha)

		commits = append(commits, model.Commit{
			ID:                  id,
			ShortID:             shortID,
			RepoID:              repoID,
			TreeSHA:             treeSHA,
			AuthorName:          authorName,
			AuthorEmail:         authorEmail,
			AuthorDate:          authorDate,
			AuthorDateOffset:    authorOffset,
			CommitterName:       committerName,
			CommitterEmail:      committerEmail,
			CommitterDate:       committerDate,
			CommitterDateOffset: committerOffset,
			Message:             messageBody,
			MessageTruncated:    messageTruncated,
			IsMerge:             isMerge,
			ParentCount:         parentCount,
			ParentSHAs:          parentSHAs,
			TrailersJSON:        trailersJSON,
			SessionID:           sessionID,
			PatchID:             "", // Left empty for v1
			ETLRunID:            etlRunID,
			CollectedAt:         collectedAt,
			Year:                year,
			Month:               month,
			SchemaVersion:       int32(model.SchemaVersion),
		})
	}

	return commits
}

// rawFileEntry holds parsed output from `git diff-tree --raw`.
type rawFileEntry struct {
	oldMode    string
	newMode    string
	oldBlob    string
	newBlob    string
	changeType string
	path       string
	oldPath    string // set for renames/copies
}

// parseDiffTreeRaw parses NUL-terminated output from `git diff-tree --root -r --raw -z`.
// Format per entry: `:old_mode new_mode old_blob new_blob status\0path\0` (with optional second path for R/C).
func parseDiffTreeRaw(output string) []rawFileEntry {
	if output == "" {
		return nil
	}

	// Split on NUL.
	parts := strings.Split(output, "\x00")
	var entries []rawFileEntry

	i := 0
	for i < len(parts) {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			i++
			continue
		}

		// A raw entry starts with ':'.
		if !strings.HasPrefix(part, ":") {
			// Could be the SHA header line from diff-tree; skip it.
			i++
			continue
		}

		// Parse the colon-prefixed metadata.
		// Format: :old_mode new_mode old_blob new_blob status
		meta := strings.TrimPrefix(part, ":")
		fields := strings.Fields(meta)
		if len(fields) < 5 {
			i++
			continue
		}

		entry := rawFileEntry{
			oldMode: fields[0],
			newMode: fields[1],
			oldBlob: fields[2],
			newBlob: fields[3],
		}

		// Status may have a score suffix (e.g. R100, C090).
		status := fields[4]
		entry.changeType = status[:1]

		// Next NUL-separated part is the path.
		i++
		if i >= len(parts) {
			break
		}
		entry.path = parts[i]

		// For renames (R) and copies (C), there's a second path (the old/source path).
		if entry.changeType == "R" || entry.changeType == "C" {
			entry.oldPath = entry.path // First path is source
			i++
			if i >= len(parts) {
				break
			}
			entry.path = parts[i] // Second path is destination
		}

		entries = append(entries, entry)
		i++
	}

	return entries
}

// numstatEntry holds parsed output from `git diff-tree --numstat`.
type numstatEntry struct {
	insertions int32
	deletions  int32
	isBinary   bool
	path       string
}

// parseDiffTreeNumstat parses NUL-terminated output from `git diff-tree --numstat -z`.
// Format: `insertions\tdeletions\tpath\0` per file. Binary files show `-\t-\t`.
func parseDiffTreeNumstat(output string) []numstatEntry {
	if output == "" {
		return nil
	}

	// With -z, numstat uses NUL to terminate paths but tabs separate fields.
	// The format is: "ins\tdel\0old_path\0new_path\0" for renames,
	// or "ins\tdel\0path\0" for non-renames.
	// Actually with -z: "ins\tdel\t\0path\0" — the -z replaces the path with NUL termination.
	// Let's parse by splitting on NUL first.
	parts := strings.Split(output, "\x00")
	var entries []numstatEntry

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Skip the SHA header line from diff-tree output.
		if len(part) == 40 && !strings.Contains(part, "\t") {
			continue
		}

		// Each entry may be: "ins\tdel\tpath" (non-z inline) or just a stat line.
		fields := strings.SplitN(part, "\t", 3)
		if len(fields) < 2 {
			continue
		}

		entry := numstatEntry{}
		if fields[0] == "-" && fields[1] == "-" {
			entry.isBinary = true
		} else {
			ins, err := strconv.ParseInt(fields[0], 10, 32)
			if err != nil {
				continue
			}
			del, err := strconv.ParseInt(fields[1], 10, 32)
			if err != nil {
				continue
			}
			entry.insertions = int32(ins)
			entry.deletions = int32(del)
		}

		if len(fields) >= 3 {
			entry.path = fields[2]
		}

		entries = append(entries, entry)
	}

	return entries
}

// parseUnifiedDiff splits unified diff output by file.
// Returns a map of file path to the complete diff text for that file.
func parseUnifiedDiff(output string) map[string]string {
	result := make(map[string]string)
	if output == "" {
		return result
	}

	// Split on "diff --git " boundaries.
	diffPrefix := "diff --git "

	for section := range strings.SplitSeq(output, diffPrefix) {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}

		// Re-add the prefix for the full diff text.
		fullDiff := diffPrefix + section

		// Extract the b/ path from the first line: "a/path b/path"
		firstLine := section
		if before, _, ok := strings.Cut(section, "\n"); ok {
			firstLine = before
		}

		// Parse "a/old b/new" — take the b/ path.
		filePath := extractBPath(firstLine)
		if filePath != "" {
			result[filePath] = fullDiff
		}
	}

	return result
}

// extractBPath extracts the destination path from a "diff --git a/... b/..." header line.
// The input is everything after "diff --git " (i.e., "a/path b/path").
func extractBPath(headerLine string) string {
	// The format is "a/path b/path". The tricky part is paths can contain spaces.
	// We look for " b/" as the separator.
	_, after, ok := strings.Cut(headerLine, " b/")
	if !ok {
		return ""
	}
	return after
}

// parsedHunk holds a parsed diff hunk.
type parsedHunk struct {
	oldStart int32
	oldLines int32
	newStart int32
	newLines int32
	header   string
	text     string
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// parseHunks splits a file diff into individual hunks.
func parseHunks(diffText string) []parsedHunk {
	lines := strings.Split(diffText, "\n")
	var hunks []parsedHunk
	var current *parsedHunk
	var textLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "@@ ") {
			// Finalize previous hunk.
			if current != nil {
				current.text = strings.Join(textLines, "\n")
				hunks = append(hunks, *current)
			}

			h := parsedHunk{}
			h.header = line
			oldStart, oldLines, newStart, newLines := parseHunkHeader(line)
			h.oldStart = oldStart
			h.oldLines = oldLines
			h.newStart = newStart
			h.newLines = newLines

			current = &h
			textLines = []string{line}
		} else if current != nil {
			textLines = append(textLines, line)
		}
	}

	// Finalize last hunk.
	if current != nil {
		current.text = strings.Join(textLines, "\n")
		hunks = append(hunks, *current)
	}

	return hunks
}

// parseHunkHeader extracts range info from an @@ header line.
func parseHunkHeader(header string) (oldStart, oldLines, newStart, newLines int32) {
	m := hunkHeaderRe.FindStringSubmatch(header)
	if m == nil {
		return 0, 0, 0, 0
	}

	os, _ := strconv.ParseInt(m[1], 10, 32)
	oldStart = int32(os)
	if m[2] != "" {
		ol, _ := strconv.ParseInt(m[2], 10, 32)
		oldLines = int32(ol)
	} else {
		oldLines = 1
	}
	ns, _ := strconv.ParseInt(m[3], 10, 32)
	newStart = int32(ns)
	if m[4] != "" {
		nl, _ := strconv.ParseInt(m[4], 10, 32)
		newLines = int32(nl)
	} else {
		newLines = 1
	}

	return
}

// parseTrailers extracts conventional trailers from the end of a commit message body.
// Returns a JSON object mapping trailer keys to arrays of values.
// e.g. {"Co-Authored-By": ["name <email>"], "Signed-off-by": ["name <email>"]}
func parseTrailers(messageBody string) string {
	lines := strings.Split(strings.TrimSpace(messageBody), "\n")

	// Walk backwards from the end to find trailer lines.
	// Trailers are "Key: Value" lines at the end, possibly after a blank line.
	trailers := make(map[string][]string)
	trailerRe := regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*)\s*:\s*(.+)$`)

	// Scan from the end backwards, collecting trailers until we hit a non-trailer, non-blank line.
	foundTrailer := false
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			if foundTrailer {
				break // Blank line above trailers = end of trailer block
			}
			continue
		}
		m := trailerRe.FindStringSubmatch(line)
		if m == nil {
			break // Non-trailer line = end of trailer block
		}
		foundTrailer = true
		key := m[1]
		value := m[2]
		trailers[key] = append(trailers[key], value)
	}

	if len(trailers) == 0 {
		return "{}"
	}

	// Reverse each trailer value list since we scanned backwards.
	for k, v := range trailers {
		for i, j := 0, len(v)-1; i < j; i, j = i+1, j-1 {
			v[i], v[j] = v[j], v[i]
		}
		trailers[k] = v
	}

	data, err := json.Marshal(trailers)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// extractSessionID extracts the Session-Id trailer value from trailers JSON.
// Returns the first Session-Id value if present, or empty string.
func extractSessionID(trailersJSON string) string {
	var trailers map[string][]string
	if err := json.Unmarshal([]byte(trailersJSON), &trailers); err != nil {
		return ""
	}
	if vals, ok := trailers["Session-Id"]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// computeHunkHash returns a SHA256 hex hash of whitespace-normalized hunk text.
func computeHunkHash(hunkText string) string {
	// Normalize: collapse runs of whitespace, trim.
	normalized := normalizeWhitespace(hunkText)
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:])
}

// normalizeWhitespace collapses runs of whitespace into a single space and trims.
func normalizeWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// parseISO8601 parses an ISO 8601 date string and returns Unix milliseconds and the offset string.
func parseISO8601(s string) (int64, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ""
	}

	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// Try other ISO 8601 formats.
		t, err = time.Parse("2006-01-02T15:04:05-0700", s)
		if err != nil {
			return 0, ""
		}
	}

	// Extract timezone offset string.
	_, offset := t.Zone()
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	if minutes < 0 {
		minutes = -minutes
	}
	offsetStr := fmt.Sprintf("%+03d:%02d", hours, minutes)

	return t.UnixMilli(), offsetStr
}

// convertSinceToGit converts a short duration string to git's --since format.
// e.g. "6m" -> "6.months.ago", "1y" -> "1.years.ago", "3w" -> "3.weeks.ago", "5d" -> "5.days.ago"
func convertSinceToGit(since string) string {
	if since == "" {
		return ""
	}

	since = strings.TrimSpace(since)
	if len(since) < 2 {
		return since
	}

	// Check multi-char suffixes first.
	if strings.HasSuffix(since, "mo") {
		numStr := since[:len(since)-2]
		if _, err := strconv.Atoi(numStr); err == nil {
			return numStr + ".months.ago"
		}
		return since
	}

	numStr := since[:len(since)-1]
	unit := since[len(since)-1]

	if _, err := strconv.Atoi(numStr); err != nil {
		return since
	}

	switch unit {
	case 'm':
		return numStr + ".minutes.ago"
	case 'h':
		return numStr + ".hours.ago"
	case 'd':
		return numStr + ".days.ago"
	case 'w':
		return numStr + ".weeks.ago"
	case 'y':
		return numStr + ".years.ago"
	default:
		return since
	}
}
