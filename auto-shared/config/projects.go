package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mistakenot/auto-shared/git"
)

const projectsFileName = "projects.json"

// projectIDPattern is the canonical project-id format shared across all auto
// tools: lowercase alphanumerics with single hyphen separators.
var projectIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ProjectRef is a single project (git repository) registered on this host.
// It is the canonical record every auto tool reads to learn what projects exist.
type ProjectRef struct {
	ID     string   `json:"id"`
	Path   string   `json:"path"`
	Remote string   `json:"remote,omitempty"`
	Name   string   `json:"name,omitempty"`
	Tools  []string `json:"tools,omitempty"`
	// RegisteredAt is an RFC 3339 timestamp (string so omitempty suppresses the
	// zero value, and so the JS UI can consume it directly).
	RegisteredAt string `json:"registeredAt,omitempty"`
}

// ProjectsConfig is the host-level project registry stored at ~/.auto/projects.json.
type ProjectsConfig struct {
	Projects []ProjectRef `json:"projects"`
}

// ProjectsConfigPath returns the path to ~/.auto/projects.json.
func ProjectsConfigPath() (string, error) {
	autoDir, err := AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, projectsFileName), nil
}

// NormalizeID lowercases and trims a project id to its canonical form. It does
// not coerce invalid characters — use it for user-supplied ids so that bad
// input is caught by ValidateProjects rather than silently rewritten.
func NormalizeID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// SlugifyID coerces an arbitrary string into a valid project id matching
// ^[a-z0-9]+(?:-[a-z0-9]+)*$: lowercased, with each run of other characters
// collapsed to a single hyphen and leading/trailing hyphens trimmed. Returns ""
// when nothing usable remains. Use it for ids derived from directory names.
func SlugifyID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	prevHyphen := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevHyphen = false
		case !prevHyphen:
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// LoadProjects reads the registry from path. A nil projects array is
// normalized to an empty slice. Decoding is lenient (unknown fields ignored)
// because the registry is shared and may gain fields over time.
func LoadProjects(path string) (ProjectsConfig, error) {
	var cfg ProjectsConfig
	if err := DecodeJSONFile(path, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Projects == nil {
		cfg.Projects = []ProjectRef{}
	}
	return cfg, nil
}

// SaveProjects writes the registry to path atomically (temp file + rename), so
// a concurrent reader never sees a partial write. Creates parent dirs as needed.
func SaveProjects(path string, cfg ProjectsConfig) error {
	if cfg.Projects == nil {
		cfg.Projects = []ProjectRef{}
	}
	return WriteJSONFileAtomic(path, cfg)
}

// EnsureProjects loads ~/.auto/projects.json, creating it if absent. On first
// creation it seeds the registry from the legacy ~/.auto/watch/settings.json (if
// present) and retires that file, so the migration happens no matter which
// command first touches the registry — `auto init` and `auto watch init` behave
// identically and in any order. Returns the path, config, created flag, and error.
func EnsureProjects() (string, ProjectsConfig, bool, error) {
	path, err := ProjectsConfigPath()
	if err != nil {
		return "", ProjectsConfig{}, false, err
	}
	if err := EnsureAutoDir(); err != nil {
		return "", ProjectsConfig{}, false, err
	}
	if _, err := os.Stat(path); err == nil {
		cfg, err := LoadProjects(path)
		return path, cfg, false, err
	} else if !os.IsNotExist(err) {
		return "", ProjectsConfig{}, false, err
	}

	// New registry: seed from the legacy watch-owned file if it has projects.
	cfg := ProjectsConfig{Projects: []ProjectRef{}}
	legacyPath, migrated, ok, err := migrateLegacyRegistry()
	if err != nil {
		return "", ProjectsConfig{}, false, err
	} else if ok {
		cfg = migrated
	}
	if err := SaveProjects(path, cfg); err != nil {
		return "", ProjectsConfig{}, false, err
	}
	// Retire the legacy file so an older binary still on PATH can't keep writing
	// to it and silently diverge from the canonical registry.
	if ok {
		_ = os.Rename(legacyPath, legacyPath+".migrated")
	}
	return path, cfg, true, nil
}

// legacyWatchRegistryPath returns ~/.auto/watch/settings.json — where auto-watch
// stored the project list before the registry moved to ~/.auto/projects.json.
// This shim can be removed once all hosts have migrated.
func legacyWatchRegistryPath() (string, error) {
	autoDir, err := AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, "watch", "settings.json"), nil
}

// migrateLegacyRegistry reads the legacy watch registry, if present and
// non-empty, returning its path and projects so EnsureProjects can seed the
// shared registry and then retire the legacy file. A missing or empty legacy
// file is not an error (ok=false).
func migrateLegacyRegistry() (legacyPath string, cfg ProjectsConfig, ok bool, err error) {
	legacyPath, err = legacyWatchRegistryPath()
	if err != nil {
		return "", ProjectsConfig{}, false, err
	}
	if _, statErr := os.Stat(legacyPath); statErr != nil {
		if os.IsNotExist(statErr) {
			return legacyPath, ProjectsConfig{}, false, nil
		}
		return legacyPath, ProjectsConfig{}, false, statErr
	}
	cfg, err = LoadProjects(legacyPath)
	if err != nil {
		return legacyPath, ProjectsConfig{}, false, err
	}
	if len(cfg.Projects) == 0 {
		return legacyPath, ProjectsConfig{}, false, nil
	}
	return legacyPath, cfg, true, nil
}

// UpsertProject replaces the entry whose cleaned path matches project, or
// appends project when no existing entry shares its path. RegisteredAt is
// defaulted to now (RFC 3339 UTC) when the caller leaves it blank, so every
// writer records a timestamp without having to remember to set it.
func UpsertProject(cfg *ProjectsConfig, project ProjectRef) {
	if project.RegisteredAt == "" {
		project.RegisteredAt = time.Now().UTC().Format(time.RFC3339)
	}
	for i := range cfg.Projects {
		if filepath.Clean(cfg.Projects[i].Path) == filepath.Clean(project.Path) {
			cfg.Projects[i] = project
			return
		}
	}
	cfg.Projects = append(cfg.Projects, project)
}

// FindProjectByPath returns the registered project whose path is the longest
// prefix of dir (i.e. dir is the project root or lives inside it), or nil.
func (c ProjectsConfig) FindProjectByPath(dir string) *ProjectRef {
	clean := filepath.Clean(dir)
	var best *ProjectRef
	bestLen := -1
	for i := range c.Projects {
		root := filepath.Clean(c.Projects[i].Path)
		if clean == root || strings.HasPrefix(clean, root+string(filepath.Separator)) {
			if len(root) > bestLen {
				bestLen = len(root)
				best = &c.Projects[i]
			}
		}
	}
	return best
}

// FindProjectByExactPath returns the registered project whose path equals dir
// (after cleaning), or nil. Unlike FindProjectByPath it does not match parent
// projects, so re-registration of a nested repo is not confused with its parent.
func (c ProjectsConfig) FindProjectByExactPath(dir string) *ProjectRef {
	clean := filepath.Clean(dir)
	for i := range c.Projects {
		if filepath.Clean(c.Projects[i].Path) == clean {
			return &c.Projects[i]
		}
	}
	return nil
}

// FindProjectByRemote returns the registered project whose remote URL matches
// the given remote after normalizing both sides via git.NormalizeRemoteURL, or
// nil when no entry matches. This is the primary lookup for worktree paths,
// which share a remote but not a path prefix with the registered main path.
func (c ProjectsConfig) FindProjectByRemote(remote string) *ProjectRef {
	norm := git.NormalizeRemoteURL(remote)
	if norm == "" {
		return nil
	}
	for i := range c.Projects {
		if c.Projects[i].Remote == "" {
			continue
		}
		if git.NormalizeRemoteURL(c.Projects[i].Remote) == norm {
			return &c.Projects[i]
		}
	}
	return nil
}

// FindProjectByID returns the registered project with the given (normalized) id, or nil.
func (c ProjectsConfig) FindProjectByID(id string) *ProjectRef {
	id = NormalizeID(id)
	for i := range c.Projects {
		if c.Projects[i].ID == id {
			return &c.Projects[i]
		}
	}
	return nil
}

// ValidateProjects checks every entry's id format and the uniqueness of ids and
// paths, returning structured errors (empty slice when valid).
func ValidateProjects(cfg ProjectsConfig) []ValidationError {
	errs := []ValidationError{}
	seenIDs := map[string]string{}
	seenPaths := map[string]string{}
	for i, project := range cfg.Projects {
		path := fmt.Sprintf("$.projects[%d]", i)
		if !projectIDPattern.MatchString(project.ID) {
			errs = append(errs, ValidationError{
				Code:    "invalid_project_id",
				Path:    path,
				Field:   "id",
				Message: "project id must match ^[a-z0-9]+(?:-[a-z0-9]+)*$",
				Value:   project.ID,
			})
		}
		cleanPath := filepath.Clean(strings.TrimSpace(project.Path))
		if cleanPath == "." || cleanPath == "" {
			errs = append(errs, ValidationError{
				Code:    "missing_project_path",
				Path:    path,
				Field:   "path",
				Message: "project path is required",
			})
		}
		if prior, ok := seenIDs[project.ID]; ok && filepath.Clean(prior) != cleanPath {
			errs = append(errs, ValidationError{
				Code:    "duplicate_project_id",
				Path:    path,
				Field:   "id",
				Message: "project id is already registered for a different path",
				Value:   project.ID,
			})
		}
		if priorID, ok := seenPaths[cleanPath]; ok && priorID != project.ID {
			errs = append(errs, ValidationError{
				Code:    "duplicate_project_path",
				Path:    path,
				Field:   "path",
				Message: "project path is already registered under a different project id",
				Value:   project.Path,
			})
		}
		seenIDs[project.ID] = cleanPath
		seenPaths[cleanPath] = project.ID
	}
	return errs
}

// Usable returns the subset of projects that a running tool can safely act on,
// together with a structured error for every entry it dropped. A project is
// usable when its id matches the canonical pattern, its path is present and
// exists as a directory on disk ("real"), and neither its id nor its cleaned
// path duplicates one already kept (first occurrence wins).
//
// This is the lenient counterpart to ValidateProjects: rather than failing the
// whole registry when one entry is malformed or stale, callers operate on the
// good projects and skip the rest. It keeps long-running consumers — notably the
// `auto watch` daemon startup doctor — resilient to a registry that has picked
// up a dead entry (for example a deleted temp dir left behind by a test run).
// Use ValidateProjects when strict, all-or-nothing validation is required.
func (c ProjectsConfig) Usable() (ProjectsConfig, []ValidationError) {
	kept := ProjectsConfig{Projects: []ProjectRef{}}
	skipped := []ValidationError{}
	seenIDs := map[string]bool{}
	seenPaths := map[string]bool{}
	for i, project := range c.Projects {
		jsonPath := fmt.Sprintf("$.projects[%d]", i)
		cleanPath := filepath.Clean(strings.TrimSpace(project.Path))
		switch {
		case !projectIDPattern.MatchString(project.ID):
			skipped = append(skipped, ValidationError{Code: "invalid_project_id", Path: jsonPath, Field: "id", Message: "project id must match ^[a-z0-9]+(?:-[a-z0-9]+)*$", Value: project.ID})
		case cleanPath == "." || cleanPath == "":
			skipped = append(skipped, ValidationError{Code: "missing_project_path", Path: jsonPath, Field: "path", Message: "project path is required"})
		case !dirExists(cleanPath):
			skipped = append(skipped, ValidationError{Code: "project_path_missing", Path: jsonPath, Field: "path", Message: "project path does not exist on disk", Value: project.Path})
		case seenIDs[project.ID]:
			skipped = append(skipped, ValidationError{Code: "duplicate_project_id", Path: jsonPath, Field: "id", Message: "project id is already registered", Value: project.ID})
		case seenPaths[cleanPath]:
			skipped = append(skipped, ValidationError{Code: "duplicate_project_path", Path: jsonPath, Field: "path", Message: "project path is already registered", Value: project.Path})
		default:
			seenIDs[project.ID] = true
			seenPaths[cleanPath] = true
			kept.Projects = append(kept.Projects, project)
		}
	}
	return kept, skipped
}

// dirExists reports whether path is an existing directory on disk.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
