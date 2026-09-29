// Package scan provides public access to autodoc's link-scanning functionality.
package scan

import "github.com/datadyne-io/autodoc/internal/linkscan"

// Tag represents a single parsed [autodoc()] tag found in a source file.
type Tag = linkscan.Tag

// ScanResult holds all tag findings across scanned files.
type ScanResult = linkscan.ScanResult

// MalformedTag records a marker-shaped autodoc reference that failed strict parsing.
type MalformedTag = linkscan.MalformedTag

// ScanFiles scans git-tracked and untracked-but-not-ignored files under rootDir
// for autodoc tags.
func ScanFiles(rootDir string) (ScanResult, error) {
	return linkscan.ScanFiles(rootDir)
}

// ScopeKindFolder marks a tag read from a .autodoc folder link file; it covers
// every code file in that folder's subtree (see FolderScopeFiles).
const ScopeKindFolder = linkscan.ScopeKindFolder

// FolderScopeFiles returns the folder-relative paths covered by a .autodoc
// folder link file in dir.
func FolderScopeFiles(dir string) ([]string, error) {
	return linkscan.FolderScopeFiles(dir)
}
