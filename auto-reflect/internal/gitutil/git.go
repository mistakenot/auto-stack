package gitutil

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // Git object IDs for blobs are defined as SHA-1.
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os/exec"
	"path/filepath"
	"strings"
)

type RepoInfo struct {
	Root   string
	Head   string
	Tree   string
	Remote string
	Dirty  bool
}

func DetectRepo(cwd string) (RepoInfo, error) {
	root, err := runGit(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return RepoInfo{}, errors.New("not a git repository: run this command inside a git repo")
	}

	head, err := runGit(root, "rev-parse", "HEAD")
	if err != nil {
		return RepoInfo{}, fmt.Errorf("resolve HEAD commit: %w", err)
	}
	tree, err := runGit(root, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return RepoInfo{}, fmt.Errorf("resolve HEAD tree: %w", err)
	}
	remote, err := primaryRemote(root)
	if err != nil {
		return RepoInfo{}, err
	}
	dirtyOut, err := runGit(root, "status", "--porcelain")
	if err != nil {
		return RepoInfo{}, fmt.Errorf("read git status: %w", err)
	}

	return RepoInfo{
		Root:   root,
		Head:   head,
		Tree:   tree,
		Remote: normalizeRemote(remote),
		Dirty:  strings.TrimSpace(dirtyOut) != "",
	}, nil
}

func HeadBlobSHA(repoRoot, repoRelativeFile string) (string, error) {
	value, err := runGit(repoRoot, "rev-parse", "HEAD:"+repoRelativeFile)
	if err != nil {
		return "", err
	}
	return value, nil
}

func FileDirty(repoRoot, repoRelativeFile string) (bool, error) {
	out, err := runGit(repoRoot, "status", "--porcelain", "--", repoRelativeFile)
	if err != nil {
		return false, fmt.Errorf("read file git status: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}

func ComputeGitBlobSHA(content []byte) string {
	header := fmt.Sprintf("blob %d\x00", len(content))
	hasher := newSHA1()
	_, _ = hasher.Write([]byte(header))
	_, _ = hasher.Write(content)
	return hex.EncodeToString(hasher.Sum(nil))
}

func runGit(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func primaryRemote(repoRoot string) (string, error) {
	out, err := runGit(repoRoot, "remote")
	if err != nil {
		return "", fmt.Errorf("list git remotes: %w", err)
	}
	lines := strings.Fields(strings.TrimSpace(out))
	if len(lines) == 0 {
		return "local/" + strings.ToLower(filepath.Base(repoRoot)), nil
	}
	name := lines[0]
	for _, candidate := range lines {
		if candidate == "origin" {
			name = candidate
			break
		}
	}
	url, err := runGit(repoRoot, "remote", "get-url", name)
	if err != nil {
		return "", fmt.Errorf("read git remote url: %w", err)
	}
	return strings.TrimSpace(url), nil
}

func newSHA1() hash.Hash {
	return sha1.New()
}

func normalizeRemote(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimSuffix(value, ".git")

	if trimmed, ok := strings.CutPrefix(value, "git@"); ok {
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) == 2 {
			return filepath.ToSlash(strings.ToLower(parts[0]) + "/" + strings.TrimPrefix(parts[1], "/"))
		}
	}
	if trimmed, ok := strings.CutPrefix(value, "ssh://"); ok {
		trimmed = strings.TrimPrefix(trimmed, "git@")
		idx := strings.Index(trimmed, "/")
		if idx > 0 {
			host := strings.ToLower(trimmed[:idx])
			path := strings.TrimPrefix(trimmed[idx:], "/")
			return filepath.ToSlash(host + "/" + path)
		}
	}
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		trimmed := strings.TrimPrefix(strings.TrimPrefix(value, "https://"), "http://")
		idx := strings.Index(trimmed, "/")
		if idx > 0 {
			host := strings.ToLower(trimmed[:idx])
			path := strings.TrimPrefix(trimmed[idx:], "/")
			return filepath.ToSlash(host + "/" + path)
		}
		return strings.ToLower(trimmed)
	}
	return filepath.ToSlash(value)
}
