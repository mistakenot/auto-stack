package worktree

import (
	"errors"
	"fmt"
	"hash/crc32"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const maxSlots = 100

type Info struct {
	Name         string
	Branch       string
	BranchSlug   string
	IsMain       bool
	Slot         int
	RepoRoot     string
	WorktreePath string
}

func Detect(cwd string) (*Info, error) {
	repoRoot, err := gitShowToplevel(cwd)
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}

	mainWorktree, err := gitMainWorktree(cwd)
	if err != nil {
		return nil, fmt.Errorf("detect main worktree: %w", err)
	}

	branch, err := gitBranch(cwd)
	if err != nil {
		branch = "HEAD"
	}

	name := filepath.Base(repoRoot)
	isMain := filepath.Clean(repoRoot) == filepath.Clean(mainWorktree)

	slot := 0
	if !isMain {
		slot = hashSlot(name)
	}

	return &Info{
		Name:         name,
		Branch:       branch,
		BranchSlug:   Slugify(branch),
		IsMain:       isMain,
		Slot:         slot,
		RepoRoot:     repoRoot,
		WorktreePath: repoRoot,
	}, nil
}

func hashSlot(name string) int {
	h := crc32.ChecksumIEEE([]byte(name))
	return int(h%(maxSlots-1)) + 1
}

var nonSlugChar = regexp.MustCompile(`[^a-z0-9-]`)
var multiHyphen = regexp.MustCompile(`-{2,}`)

func Slugify(s string) string {
	s = strings.ToLower(s)
	s = nonSlugChar.ReplaceAllString(s, "-")
	s = multiHyphen.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = s[:63]
		s = strings.TrimRight(s, "-")
	}
	return s
}

func gitShowToplevel(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitMainWorktree(cwd string) (string, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			return path, nil
		}
	}
	return "", errors.New("no worktree found in porcelain output")
}

func gitBranch(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
