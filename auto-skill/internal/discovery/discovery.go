package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var skillNameRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const skillFile = "SKILL.md"

// Discovered represents a skill found during discovery.
type Discovered struct {
	Name      string // declared name from SKILL.md frontmatter
	Subpath   string // relative path within the repo (e.g. "skills/my-skill")
	Digest    string // stable tree digest (sha256) for dedupe
	NameValid bool   // true if name matches ^[a-z0-9]+(-[a-z0-9]+)*$
	Container string // which container found it: "root", "skills", ".claude/skills", etc.
}

// Options controls discovery behavior.
type Options struct {
	Paths     []string // explicit paths to scan (from --path or deep-link subpath)
	FullDepth bool     // force recursive scan of the whole tree
}

// DivergenceError indicates same-named skills found with different content.
type DivergenceError struct {
	Name    string
	Entries []DivergenceEntry
}

// DivergenceEntry records one instance in a divergence conflict.
type DivergenceEntry struct {
	Subpath   string
	Container string
	Digest    string
}

func (e *DivergenceError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "skill %q found in %d containers with divergent content; use --path to choose:\n", e.Name, len(e.Entries))
	for _, ent := range e.Entries {
		fmt.Fprintf(&b, "  %s (container=%s, digest=%s)\n", ent.Subpath, ent.Container, ent.Digest[:12])
	}
	return b.String()
}

// Discover scans root for skills according to the discovery-root priority.
func Discover(root string, opts Options) ([]Discovered, error) {
	root = filepath.Clean(root)

	// Mode 1: explicit paths — scan only those directories.
	if len(opts.Paths) > 0 {
		var results []Discovered
		for _, p := range opts.Paths {
			dir := filepath.Join(root, p)
			found, err := scanDir(root, dir, "explicit")
			if err != nil {
				return nil, err
			}
			results = append(results, found...)
		}
		return results, nil
	}

	// Mode 2: default container scan.
	if !opts.FullDepth {
		results, err := defaultScan(root)
		if err != nil {
			return nil, err
		}
		// If default scan found something, dedupe and return.
		if len(results) > 0 {
			return dedupeResults(results)
		}
		// Fallback to full-depth if default scan found nothing.
	}

	// Mode 3: full-depth scan (forced or fallback).
	results, err := fullDepthScan(root)
	if err != nil {
		return nil, err
	}
	return dedupeResults(results)
}

// defaultScan checks known container directories in priority order.
func defaultScan(root string) ([]Discovered, error) {
	var results []Discovered

	// Check root for single-skill repo.
	if fileExists(filepath.Join(root, skillFile)) {
		d, err := buildDiscovered(root, root, "root")
		if err != nil {
			return nil, err
		}
		results = append(results, d)
	}

	// Check skills/ directory.
	skillsDir := filepath.Join(root, "skills")
	if dirExists(skillsDir) {
		found, err := scanContainer(root, skillsDir, "skills")
		if err != nil {
			return nil, err
		}
		results = append(results, found...)
	}

	// Check agent directories.
	agentDirs := []string{".agents/skills", ".claude/skills"}
	for _, ad := range agentDirs {
		dir := filepath.Join(root, ad)
		if dirExists(dir) {
			found, err := scanContainer(root, dir, ad)
			if err != nil {
				return nil, err
			}
			results = append(results, found...)
		}
	}

	return results, nil
}

// scanContainer walks a container directory (like skills/) looking for SKILL.md files.
// It supports flat layout (container/<name>/SKILL.md) and catalog layout
// (container/<category>/<name>/SKILL.md), with shadowing: if a SKILL.md exists
// in a directory, we don't recurse into its subdirectories.
func scanContainer(root, containerDir, container string) ([]Discovered, error) {
	var results []Discovered

	entries, err := os.ReadDir(containerDir)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		childDir := filepath.Join(containerDir, e.Name())

		// Flat layout: container/<name>/SKILL.md
		if fileExists(filepath.Join(childDir, skillFile)) {
			d, err := buildDiscovered(root, childDir, container)
			if err != nil {
				return nil, err
			}
			results = append(results, d)
			// Shadowing: don't recurse into subdirectories.
			continue
		}

		// Catalog layout: container/<category>/<name>/SKILL.md
		subEntries, err := os.ReadDir(childDir)
		if err != nil {
			continue // skip unreadable
		}
		for _, se := range subEntries {
			if !se.IsDir() {
				continue
			}
			grandchildDir := filepath.Join(childDir, se.Name())
			if fileExists(filepath.Join(grandchildDir, skillFile)) {
				d, err := buildDiscovered(root, grandchildDir, container)
				if err != nil {
					return nil, err
				}
				results = append(results, d)
			}
		}
	}

	return results, nil
}

// scanDir walks a single directory for SKILL.md files, respecting shadowing.
func scanDir(root, dir string, container string) ([]Discovered, error) {
	var results []Discovered

	if !dirExists(dir) {
		return nil, nil
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != skillFile {
			return nil
		}
		skillDir := filepath.Dir(path)
		disc, err := buildDiscovered(root, skillDir, container)
		if err != nil {
			return err
		}
		results = append(results, disc)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return results, nil
}

// fullDepthScan recurses the entire tree looking for SKILL.md files,
// respecting shadowing (once a SKILL.md is found, don't recurse deeper).
func fullDepthScan(root string) ([]Discovered, error) {
	var results []Discovered
	// Track directories where we found SKILL.md to implement shadowing.
	shadowedDirs := map[string]bool{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Check if this directory is under a shadowed parent.
			for sd := range shadowedDirs {
				rel, relErr := filepath.Rel(sd, path)
				if relErr == nil && !strings.HasPrefix(rel, "..") && rel != "." {
					return fs.SkipDir
				}
			}
			return nil
		}
		if d.Name() != skillFile {
			return nil
		}
		skillDir := filepath.Dir(path)
		disc, buildErr := buildDiscovered(root, skillDir, "full-depth")
		if buildErr != nil {
			return buildErr
		}
		results = append(results, disc)
		shadowedDirs[skillDir] = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	return results, nil
}

// buildDiscovered creates a Discovered entry for a skill at skillDir.
func buildDiscovered(root, skillDir, container string) (Discovered, error) {
	relPath, err := filepath.Rel(root, skillDir)
	if err != nil {
		return Discovered{}, err
	}
	if relPath == "." {
		relPath = "."
	}

	name := extractName(filepath.Join(skillDir, skillFile), filepath.Base(skillDir))
	digest, err := treeDigest(skillDir)
	if err != nil {
		return Discovered{}, err
	}

	return Discovered{
		Name:      name,
		Subpath:   relPath,
		Digest:    digest,
		NameValid: skillNameRE.MatchString(name),
		Container: container,
	}, nil
}

// extractName reads the name from SKILL.md frontmatter. Falls back to dirName.
func extractName(skillPath, dirName string) string {
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return dirName
	}
	name := parseFrontmatterName(string(data))
	if name == "" {
		return dirName
	}
	return name
}

// parseFrontmatterName extracts the name field from YAML frontmatter.
// Minimal parser: no external YAML dependency.
func parseFrontmatterName(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			break
		}
		if val, ok := strings.CutPrefix(line, "name:"); ok {
			val = strings.TrimSpace(val)
			// Strip surrounding quotes if present.
			val = strings.Trim(val, `"'`)
			return val
		}
	}
	return ""
}

// treeDigest computes a stable sha256 digest of a skill directory tree.
func treeDigest(dir string) (string, error) {
	var entries []string

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fileHash := sha256.Sum256(data)
		entries = append(entries, fmt.Sprintf("%s:%s", rel, hex.EncodeToString(fileHash[:])))
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(entries)
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s\n", e)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// dedupeResults groups by name and collapses identical-digest duplicates.
// Returns DivergenceError for same-named skills with different digests.
func dedupeResults(results []Discovered) ([]Discovered, error) {
	if len(results) == 0 {
		return results, nil
	}

	groups := map[string][]Discovered{}
	for _, d := range results {
		groups[d.Name] = append(groups[d.Name], d)
	}

	var final []Discovered
	// Sort names for deterministic output.
	names := make([]string, 0, len(groups))
	for n := range groups {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		entries := groups[name]
		if len(entries) == 1 {
			final = append(final, entries[0])
			continue
		}

		// Check if all digests are identical.
		allSame := true
		for i := 1; i < len(entries); i++ {
			if entries[i].Digest != entries[0].Digest {
				allSame = false
				break
			}
		}

		if allSame {
			// Collapse to the highest-priority container.
			best := pickPreferred(entries)
			final = append(final, best)
		} else {
			// Divergent content — build error.
			var divEntries []DivergenceEntry
			for _, e := range entries {
				divEntries = append(divEntries, DivergenceEntry{
					Subpath:   e.Subpath,
					Container: e.Container,
					Digest:    e.Digest,
				})
			}
			return nil, &DivergenceError{
				Name:    name,
				Entries: divEntries,
			}
		}
	}

	return final, nil
}

// containerPriority returns a priority number (lower = higher priority).
func containerPriority(container string) int {
	switch container {
	case "skills":
		return 0
	case "root":
		return 1
	default:
		return 2
	}
}

// pickPreferred selects the entry from the highest-priority container.
func pickPreferred(entries []Discovered) Discovered {
	best := entries[0]
	for _, e := range entries[1:] {
		if containerPriority(e.Container) < containerPriority(best.Container) {
			best = e
		}
	}
	return best
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
