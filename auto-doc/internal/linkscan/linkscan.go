// [autodoc(e8d3cf9c@34e92e15, f407732a)]
package linkscan

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var strictTagRegex = regexp.MustCompile(`\[autodoc\(([0-9a-f]{8})@([0-9a-f]{8}),\s*([0-9a-f]{8})\)\]`)
var anyAutodocTagRegex = regexp.MustCompile(`\[autodoc\([^\]]*\)\]`)

// malformedCandidateRegex matches [autodoc(...)] where the content contains @,
// indicating someone attempted a real tag but got the format wrong.
// This avoids flagging descriptive mentions like [autodoc()] or [autodoc(...)] in prose/comments.
var malformedCandidateRegex = regexp.MustCompile(`\[autodoc\([^)]*@[^)]*\)\]`)

var ignoredSegments = map[string]bool{
	".git":         true,
	".next":        true,
	"bin":          true,
	"build":        true,
	"dist":         true,
	"node_modules": true,
	"obj":          true,
	"out":          true,
	"target":       true,
	"testdata":     true,
	"vendor":       true,
}

// File extensions to skip — these are documentation/data files, not code.
var ignoredExtensions = map[string]bool{
	".md":       true,
	".markdown": true,
	".json":     true,
	".jsonl":    true,
	".ndjson":   true,
	".yaml":     true,
	".yml":      true,
	".toml":     true,
	".xml":      true,
	".csv":      true,
	".txt":      true,
}

// File suffixes to skip — test files contain fixture data, not real tags.
var ignoredSuffixes = []string{
	"_test.go",
}

type ScopeKind int

const (
	ScopeKindIndent ScopeKind = iota
	ScopeKindMarkdown
)

// Tag represents a single parsed [autodoc()] tag found in a source file.
type Tag struct {
	FilePath  string // absolute path to the source file
	Line      int    // 1-indexed line number where the tag appears
	DocId     string // 8-char hex doc ID
	DocHash   string // 8-char hex doc hash snapshot
	ScopeHash string // 8-char hex scope hash snapshot
	RawTag    string // the full [autodoc(...)] string as found
	ScopeKind ScopeKind
}

// MalformedTag records a marker-shaped autodoc reference that failed strict parsing.
type MalformedTag struct {
	FilePath string // absolute path
	Line     int    // 1-indexed
	RawText  string // raw line text
}

// ScanResult holds all tag findings across scanned files.
type ScanResult struct {
	Tags      []Tag
	Malformed []MalformedTag
}

// ScanFiles scans git-tracked files under rootDir for autodoc tags.
func ScanFiles(rootDir string) (ScanResult, error) {
	var result ScanResult

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return result, fmt.Errorf("abs root: %w", err)
	}

	cmd := exec.Command("git", "-C", absRoot, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		return result, fmt.Errorf("git ls-files: %w", err)
	}

	for rel := range strings.SplitSeq(string(out), "\x00") {
		if rel == "" {
			continue
		}
		if shouldIgnorePath(rel) {
			continue
		}

		fullPath := filepath.Join(absRoot, rel)
		info, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return result, fmt.Errorf("stat %s: %w", rel, err)
		}
		if info.IsDir() {
			continue
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return result, fmt.Errorf("read %s: %w", rel, err)
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if !strings.Contains(line, "[autodoc(") {
				continue
			}

			// Try strict match first — valid autodoc tag with hex8 triplet
			match := strictTagRegex.FindStringSubmatch(line)
			if len(match) == 4 {
				rawTag := strictTagRegex.FindString(line)
				result.Tags = append(result.Tags, Tag{
					FilePath:  fullPath,
					Line:      i + 1,
					DocId:     match[1],
					DocHash:   match[2],
					ScopeHash: match[3],
					RawTag:    rawTag,
					ScopeKind: ScopeKindIndent,
				})
				continue
			}

			// Only flag as malformed if it looks like an attempted real tag —
			// contains @ (suggesting docId@docHash format was tried).
			// This avoids flagging descriptive mentions like [autodoc()] or
			// [autodoc(...)] in comments and string literals.
			if malformedCandidateRegex.MatchString(line) {
				result.Malformed = append(result.Malformed, MalformedTag{
					FilePath: fullPath,
					Line:     i + 1,
					RawText:  strings.TrimRight(line, "\r"),
				})
			}
		}
	}

	return result, nil
}

// ComputeScopeHash computes the scope hash for a tag in filePath at tagLine.
func ComputeScopeHash(filePath string, tagLine int) (string, error) {
	return ComputeScopeHashForTag(Tag{
		FilePath:  filePath,
		Line:      tagLine,
		ScopeKind: ScopeKindIndent,
	})
}

// ComputeScopeHashForTag computes the scope hash for a parsed tag.
func ComputeScopeHashForTag(tag Tag) (string, error) {
	data, err := os.ReadFile(tag.FilePath)
	if err != nil {
		return "", err
	}
	return ComputeScopeHashFromContentForTag(string(data), tag)
}

// ComputeScopeHashFromContent computes a scope hash using in-memory file content.
func ComputeScopeHashFromContent(content string, tagLine int) (string, error) {
	return ComputeScopeHashFromContentForTag(content, Tag{
		Line:      tagLine,
		ScopeKind: ScopeKindIndent,
	})
}

// ComputeScopeHashFromContentForTag computes a scope hash using in-memory file content.
func ComputeScopeHashFromContentForTag(content string, tag Tag) (string, error) {
	if tag.ScopeKind == ScopeKindMarkdown {
		return computeMarkdownScopeHashFromContent(content, tag.Line)
	}
	return computeIndentedScopeHashFromContent(content, tag.Line)
}

func computeIndentedScopeHashFromContent(content string, tagLine int) (string, error) {
	if tagLine <= 0 {
		return "", errors.New("tag line must be >= 1")
	}

	lines := strings.Split(content, "\n")
	if tagLine > len(lines) {
		return "", fmt.Errorf("tag line %d out of range (max %d)", tagLine, len(lines))
	}

	tagIndent := leadingWhitespaceCount(strings.TrimRight(lines[tagLine-1], "\r"))
	collected := make([]string, 0, len(lines)-tagLine)

	for i := tagLine; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "" {
			collected = append(collected, line)
			continue
		}

		if leadingWhitespaceCount(line) < tagIndent {
			break
		}

		collected = append(collected, line)
	}

	scope := strings.Join(collected, "\n")
	scope = anyAutodocTagRegex.ReplaceAllString(scope, "")

	sum := md5.Sum([]byte(scope))
	return hex.EncodeToString(sum[:])[:8], nil
}

func leadingWhitespaceCount(line string) int {
	count := 0
	for _, r := range line {
		if r == ' ' || r == '\t' {
			count++
			continue
		}
		break
	}
	return count
}

func shouldIgnorePath(relPath string) bool {
	// Skip non-code file extensions
	ext := strings.ToLower(filepath.Ext(relPath))
	if ignoredExtensions[ext] {
		return true
	}

	// Skip test files
	base := filepath.Base(relPath)
	for _, suffix := range ignoredSuffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}

	for p := range strings.SplitSeq(filepath.ToSlash(relPath), "/") {
		if ignoredSegments[p] {
			return true
		}
	}
	return false
}
