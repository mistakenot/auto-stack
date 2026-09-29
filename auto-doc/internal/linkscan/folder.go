package linkscan

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// FolderLinkFileName is the basename of a folder link file. Each autodoc tag
// line inside it links its doc to every code file in the folder's subtree, so
// one line changes instead of one tag per file.
const FolderLinkFileName = ".autodoc"

// IsFolderLinkPath reports whether path names a folder link file.
func IsFolderLinkPath(path string) bool {
	return filepath.Base(path) == FolderLinkFileName
}

// ScanFolderLinkFile parses a folder link file. Lines starting with # are
// comments; every strict tag becomes a ScopeKindFolder tag.
func ScanFolderLinkFile(fullPath string, data []byte, result *ScanResult) {
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		match := strictTagRegex.FindStringSubmatch(line)
		if len(match) == 4 {
			result.Tags = append(result.Tags, Tag{
				FilePath:  fullPath,
				Line:      i + 1,
				DocId:     match[1],
				DocHash:   match[2],
				ScopeHash: match[3],
				RawTag:    strictTagRegex.FindString(line),
				ScopeKind: ScopeKindFolder,
			})
			continue
		}
		// Any non-comment content in a link file is an attempted tag.
		result.Malformed = append(result.Malformed, MalformedTag{
			FilePath: fullPath,
			Line:     i + 1,
			RawText:  strings.TrimRight(line, "\r"),
		})
	}
}

// FolderScopeFiles returns the folder-relative, slash-separated paths of the
// files covered by a folder link file in dir: every tracked or untracked
// (but not git-ignored) file in the subtree that the code scanner would scan,
// excluding folder link files themselves. Sorted.
func FolderScopeFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "-C", dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", ".")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files %s: %w", dir, err)
	}

	seen := make(map[string]bool)
	files := make([]string, 0)
	for rel := range strings.SplitSeq(string(out), "\x00") {
		if rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		if IsFolderLinkPath(rel) || shouldIgnorePath(rel) {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			if os.IsNotExist(err) {
				continue // deleted but still in the index
			}
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		files = append(files, filepath.ToSlash(rel))
	}
	sort.Strings(files)
	return files, nil
}

// ComputeFolderScopeHash hashes every file covered by a folder link file in
// dir. Paths are part of the hash, so adding, removing or renaming a file
// marks the link stale. Inline autodoc tags are stripped so refreshing them
// never moves the folder hash.
func ComputeFolderScopeHash(dir string) (string, error) {
	files, err := FolderScopeFiles(dir)
	if err != nil {
		return "", err
	}

	h := md5.New()
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		content := strings.ReplaceAll(string(data), "\r\n", "\n")
		content = anyAutodocTagRegex.ReplaceAllString(content, "")
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write([]byte(content))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:8], nil
}
