package cochange

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-search/internal/etlscan"
	sharedgit "github.com/mistakenot/auto-shared/git"
)

// Typed resolution errors (AC-10). Each is distinguishable via errors.Is so the
// CLI can map it to a non-zero ExitError with a concrete remediation hint.
var (
	// ErrOutsideRepo: the input path is not inside any git repository.
	ErrOutsideRepo = errors.New("input path is not inside a git repository")
	// ErrNoOriginRemote: the repo has no origin remote and no --repo-id was given.
	ErrNoOriginRemote = errors.New("repo has no origin remote")
	// ErrNoRepoMatch: the resolved origin remote matched no row in git_repositories.
	ErrNoRepoMatch = errors.New("repo not found in indexed git_repositories")
	// ErrMissingParquet: the repo's git parquet data is missing under the input root.
	ErrMissingParquet = errors.New("git parquet data missing")
)

// ResolvedRepo carries the outcome of repo resolution.
type ResolvedRepo struct {
	RepoID       string // the matched (or overridden) repo id
	RepoRoot     string // git toplevel directory
	ResolvedPath string // input path lexically relative to RepoRoot
	Remote       string // raw origin remote URL (may be empty when --repo-id used)
}

// ResolveRepo resolves the repository for inputPath following solution.md step 1.
//
// It derives a directory to hand `git -C` (git rejects file/missing paths):
// existing file -> parent dir; existing dir -> itself; non-existent -> nearest
// existing ancestor dir, else cwd. It runs `git -C <dir> rev-parse
// --show-toplevel`, computes resolvedPath lexically relative to the toplevel
// (so unknown/untracked files still resolve, AC-9), then reads the origin remote,
// normalises it, and matches it against repos[].RepoRemoteNormalized to get the
// repo id.
//
// repoIDOverride (--repo-id) bypasses git-remote matching: the toplevel and
// resolvedPath are still resolved from the path when possible, but the repo id
// is taken verbatim and no remote lookup is performed.
//
// Errors are typed (see ErrOutsideRepo / ErrNoOriginRemote / ErrNoRepoMatch) so
// the CLI can attach remediation.
func ResolveRepo(inputPath, repoIDOverride string, repos []etlscan.GitRepoSlim) (ResolvedRepo, error) {
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return ResolvedRepo{}, fmt.Errorf("resolve absolute path for %q: %w", inputPath, err)
	}

	dir := dirForGit(abs)

	top, err := gitToplevel(dir)
	if err != nil {
		return ResolvedRepo{}, fmt.Errorf("%w: %q is not inside a git repository (cd into a repository, or pass --repo-id <id>)", ErrOutsideRepo, inputPath)
	}

	resolvedPath := lexicalRel(top, abs)

	res := ResolvedRepo{RepoRoot: top, ResolvedPath: resolvedPath}

	if strings.TrimSpace(repoIDOverride) != "" {
		res.RepoID = strings.TrimSpace(repoIDOverride)
		// Best-effort remote for the metadata `repo` field; ignore errors.
		res.Remote, _ = gitOriginRemote(top)
		return res, nil
	}

	remote, err := gitOriginRemote(top)
	if err != nil || strings.TrimSpace(remote) == "" {
		return ResolvedRepo{}, fmt.Errorf("%w for %q; pass --repo-id <id> to select the repository explicitly", ErrNoOriginRemote, top)
	}
	res.Remote = remote

	normalized := sharedgit.NormalizeRemoteURL(remote)
	for _, r := range repos {
		if r.RepoRemoteNormalized != "" && r.RepoRemoteNormalized == normalized {
			res.RepoID = r.RepoID
			return res, nil
		}
	}
	return ResolvedRepo{}, fmt.Errorf("%w: origin remote %q (normalized %q) has no match; run `auto etl run --only git`, or pass --repo-id <id>", ErrNoRepoMatch, remote, normalized)
}

// dirForGit derives an existing directory to hand `git -C` from an absolute
// input path. git rejects file paths and non-existent paths, so we map:
// existing file -> parent dir; existing dir -> itself; non-existent -> nearest
// existing ancestor dir; nothing existing -> current working directory.
func dirForGit(abs string) string {
	if info, err := os.Stat(abs); err == nil {
		if info.IsDir() {
			return abs
		}
		return filepath.Dir(abs)
	}
	// Walk up to the nearest existing ancestor.
	dir := filepath.Dir(abs)
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

// lexicalRel returns target relative to base using lexical computation only (the
// target need not exist). It falls back to the absolute target if the relative
// computation fails or escapes the base.
func lexicalRel(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return filepath.ToSlash(rel)
}

func gitToplevel(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitOriginRemote(top string) (string, error) {
	out, err := exec.Command("git", "-C", top, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
