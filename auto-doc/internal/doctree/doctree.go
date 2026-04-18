// [autodoc(e8d3cf9c@34e92e15, 070f2fc4)]
package doctree

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/datadyne-io/autodoc/internal/frontmatter"
)

// Entry represents a single doc file in the tree.
type Entry struct {
	RelPath string // relative to docs dir (e.g. "api/auth.md")
	Id      string
	Title   string
	Summary string
	Hash    string
	Tags    []string
	Body    string

	DocsRootRel string // docs root relative to repo root (e.g. "auto-etl/docs")
	RepoRelPath string // file path relative to repo root (e.g. "auto-etl/docs/api/auth.md")
	AbsPath     string // absolute file path for writes
}

// Walk reads all .md files under docsDir and returns sorted entries.
// Files matching any of the ignore globs (matched against the relative path) are skipped.
func Walk(docsDir string, ignores ...string) ([]Entry, error) {
	var entries []Entry

	err := filepath.Walk(docsDir, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}

		rel, err := filepath.Rel(docsDir, filePath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if matchesAny(rel, ignores) {
			return nil
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		doc := frontmatter.Parse(string(data))
		docsRootRel := filepath.ToSlash(filepath.Base(filepath.Clean(docsDir)))
		repoRelPath := rel
		if docsRootRel != "" {
			repoRelPath = path.Join(docsRootRel, rel)
		}
		entries = append(entries, Entry{
			RelPath:     rel,
			Id:          doc.Id,
			Title:       doc.Title,
			Summary:     doc.Summary,
			Hash:        doc.Hash,
			Tags:        doc.Tags,
			Body:        doc.Body,
			DocsRootRel: docsRootRel,
			RepoRelPath: repoRelPath,
			AbsPath:     filePath,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].RelPath < entries[j].RelPath
	})

	return entries, nil
}

// WalkRepo discovers markdown docs recursively across the repository and returns merged entries.
// docsDir is treated as a compatibility include root in addition to directories named "docs".
func WalkRepo(rootDir string, docsDir string, ignores ...string) ([]Entry, error) {
	repoPaths, err := DiscoverDocsMarkdownPaths(rootDir, docsDir)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(repoPaths))
	docsCompatRoot := normalizePath(docsDir)

	for _, repoRelPath := range repoPaths {
		docsRootRel, relPath := deriveDocsRootAndRelPath(repoRelPath, docsCompatRoot)
		if matchesAnyRepo(repoRelPath, relPath, ignores) {
			continue
		}

		absPath := filepath.Join(rootDir, filepath.FromSlash(repoRelPath))
		data, err := os.ReadFile(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		doc := frontmatter.Parse(string(data))
		entries = append(entries, Entry{
			RelPath:     relPath,
			Id:          doc.Id,
			Title:       doc.Title,
			Summary:     doc.Summary,
			Hash:        doc.Hash,
			Tags:        doc.Tags,
			Body:        doc.Body,
			DocsRootRel: docsRootRel,
			RepoRelPath: repoRelPath,
			AbsPath:     absPath,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].RepoRelPath < entries[j].RepoRelPath
	})

	return entries, nil
}

// DiscoverDocsMarkdownPaths returns sorted repo-relative markdown paths under discovered docs roots.
// Discovery uses git tracked+untracked files when possible with filesystem fallback.
func DiscoverDocsMarkdownPaths(rootDir string, docsDir string) ([]string, error) {
	gitPaths, err := discoverViaGit(rootDir)
	if err == nil {
		return filterDocsMarkdownPaths(gitPaths, docsDir), nil
	}

	fsPaths, fsErr := discoverViaFilesystem(rootDir)
	if fsErr != nil {
		return nil, fmt.Errorf("discover docs paths: git error: %w; filesystem error: %w", err, fsErr)
	}
	return filterDocsMarkdownPaths(fsPaths, docsDir), nil
}

func matchesAny(rel string, patterns []string) bool {
	return matchesAnyPaths(rel, rel, patterns)
}

func matchesAnyRepo(repoRelPath, relPath string, patterns []string) bool {
	return matchesAnyPaths(repoRelPath, relPath, patterns)
}

func matchesAnyPaths(repoRelPath, relPath string, patterns []string) bool {
	repoRelPath = normalizePath(repoRelPath)
	relPath = normalizePath(relPath)
	baseName := path.Base(relPath)
	if baseName == "." {
		baseName = path.Base(repoRelPath)
	}

	for _, p := range patterns {
		p = normalizePath(p)
		if p == "" {
			continue
		}

		candidates := []string{p}
		if after, ok := strings.CutPrefix(p, "docs/"); ok {
			candidates = append(candidates, after)
		}

		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}

			if matched, _ := path.Match(candidate, repoRelPath); matched {
				return true
			}
			if matched, _ := path.Match(candidate, relPath); matched {
				return true
			}
			if matched, _ := path.Match(candidate, baseName); matched {
				return true
			}
			if matchesDirectoryWildcard(candidate, repoRelPath) || matchesDirectoryWildcard(candidate, relPath) {
				return true
			}
		}
	}
	return false
}

func matchesDirectoryWildcard(pattern, candidatePath string) bool {
	if !strings.HasSuffix(pattern, "/*") {
		return false
	}
	dirPattern := strings.TrimSuffix(pattern, "/*")
	if dirPattern == "" {
		return false
	}

	dirPath := path.Dir(candidatePath)
	if dirPath == "." || dirPath == "" {
		return false
	}
	parts := strings.Split(dirPath, "/")
	for i := 1; i <= len(parts); i++ {
		parent := strings.Join(parts[:i], "/")
		if matched, _ := path.Match(dirPattern, parent); matched {
			return true
		}
	}
	return false
}

func discoverViaGit(rootDir string) ([]string, error) {
	cmd := exec.Command("git", "-C", rootDir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("git ls-files failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}

	if len(out) == 0 {
		return nil, nil
	}

	parts := bytes.Split(out, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		paths = append(paths, normalizePath(string(part)))
	}
	return paths, nil
}

func discoverViaFilesystem(rootDir string) ([]string, error) {
	paths := make([]string, 0)
	err := filepath.Walk(rootDir, func(absPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if shouldSkipDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(rootDir, absPath)
		if err != nil {
			return err
		}
		paths = append(paths, normalizePath(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", "out", "target", "bin":
		return true
	default:
		return false
	}
}

// Directories that should be excluded from doc discovery.
var excludedSegments = map[string]bool{
	"testdata": true,
}

func filterDocsMarkdownPaths(paths []string, docsDir string) []string {
	docsCompatRoot := normalizePath(docsDir)
	uniq := make(map[string]struct{}, len(paths))

	for _, raw := range paths {
		p := normalizePath(raw)
		if p == "" {
			continue
		}
		if !strings.HasSuffix(p, ".md") {
			continue
		}
		if hasExcludedSegment(p) {
			continue
		}
		if hasDocsSegment(p) || isUnderPath(p, docsCompatRoot) {
			uniq[p] = struct{}{}
		}
	}

	out := make([]string, 0, len(uniq))
	for p := range uniq {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func hasExcludedSegment(repoRelPath string) bool {
	for seg := range strings.SplitSeq(repoRelPath, "/") {
		if excludedSegments[seg] {
			return true
		}
	}
	return false
}

func hasDocsSegment(repoRelPath string) bool {
	parts := strings.Split(repoRelPath, "/")
	if len(parts) <= 1 {
		return false
	}
	for i := range len(parts) - 1 {
		if parts[i] == "docs" {
			return true
		}
	}
	return false
}

func deriveDocsRootAndRelPath(repoRelPath string, docsCompatRoot string) (string, string) {
	parts := strings.Split(repoRelPath, "/")
	lastDocsIdx := -1
	for i := range len(parts) - 1 {
		if parts[i] == "docs" {
			lastDocsIdx = i
		}
	}
	if lastDocsIdx >= 0 {
		docsRoot := strings.Join(parts[:lastDocsIdx+1], "/")
		relPath := strings.Join(parts[lastDocsIdx+1:], "/")
		return docsRoot, relPath
	}

	if isUnderPath(repoRelPath, docsCompatRoot) {
		relPath := strings.TrimPrefix(repoRelPath, docsCompatRoot+"/")
		if relPath == "" {
			relPath = path.Base(repoRelPath)
		}
		return docsCompatRoot, relPath
	}

	return path.Dir(repoRelPath), path.Base(repoRelPath)
}

func isUnderPath(repoRelPath, root string) bool {
	if root == "" {
		return false
	}
	return repoRelPath == root || strings.HasPrefix(repoRelPath, root+"/")
}

func normalizePath(p string) string {
	p = filepath.ToSlash(p)
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	p = path.Clean(p)
	if p == "." {
		return ""
	}
	return p
}
