// Package scan provides public access to autodoc's link-scanning functionality.
package scan

import "github.com/datadyne-io/autodoc/internal/linkscan"

// Tag represents a single parsed [autodoc()] tag found in a source file.
type Tag = linkscan.Tag

// ScanResult holds all tag findings across scanned files.
type ScanResult = linkscan.ScanResult

// MalformedTag records a marker-shaped autodoc reference that failed strict parsing.
type MalformedTag = linkscan.MalformedTag

// ScanFiles scans git-tracked files under rootDir for autodoc tags.
func ScanFiles(rootDir string) (ScanResult, error) {
	return linkscan.ScanFiles(rootDir)
}
