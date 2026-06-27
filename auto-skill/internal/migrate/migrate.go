// Package migrate translates a vercel-style skills-lock.json into the native
// auto-skill lock/skills.yaml model. It is a pure, offline transform: it parses,
// classifies, and plans — it never resolves commits, touches the network, or
// writes any files. Writing the planned outcome is the job of a later Apply step.
package migrate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/transport"
)

// Vercel source-type discriminators observed in skills-lock.json.
const (
	sourceTypeGitHub      = "github"
	sourceTypeGitLab      = "gitlab"
	sourceTypeLocal       = "local"
	sourceTypeNodeModules = "node_modules"
	sourceTypeWellKnown   = "well-known"
	sourceTypeHuggingFace = "huggingface"
	sourceTypeMintlify    = "mintlify"
)

// Skip reason codes carried on each skipped entry.
const (
	ReasonUnsupported   = "unsupported_source_type"
	ReasonMissingPath   = "missing_path"
	ReasonInvalidSource = "invalid_source"
)

// Validation error codes returned by ParseVercelLock.
const (
	CodeParseError = "parse_error"
	CodeEmptyLock  = "empty_lock"
)

// remoteHosts maps a remote sourceType to the host used when the vercel source
// is a bare "owner/repo" shorthand with no embedded host.
var remoteHosts = map[string]string{
	sourceTypeGitHub: "github.com",
	sourceTypeGitLab: "gitlab.com",
}

// VercelLock is the typed representation of a vercel-style skills-lock.json.
type VercelLock struct {
	Version int                    `json:"version"`
	Skills  map[string]VercelEntry `json:"skills"`
}

// VercelEntry is a single skill pin in a vercel lock. Real github/local entries
// carry no ref field; ref is present only on explicitly pinned entries.
type VercelEntry struct {
	Source       string `json:"source"`
	SourceType   string `json:"sourceType"`
	SkillPath    string `json:"skillPath"`
	ComputedHash string `json:"computedHash"`
	Ref          string `json:"ref"`
}

// Entry is a planned native lock.json + skills.yaml addition. Migration always
// emits entries in the "unresolved" state — sync resolves the commit later.
type Entry struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	URL         string `json:"url"`
	Subpath     string `json:"subpath,omitempty"`
	VersionSpec string `json:"version_spec"`
	Local       bool   `json:"local"`
	State       string `json:"state"`
}

// Import is a planned authored-import of a non-git local directory into
// ./skills/<name>/. It carries the on-disk source so Apply can copy the tree.
type Import struct {
	Name       string `json:"name"`
	SourcePath string `json:"source_path"`
}

// Skip records an entry that migration could not (or will not) translate, with
// a machine-readable reason and a human remediation message.
type Skip struct {
	Name       string `json:"name"`
	SourceType string `json:"source_type"`
	Source     string `json:"source"`
	Reason     string `json:"reason"`
	Message    string `json:"message"`
}

// Migration is the classified, write-free plan produced by Plan. It is the rich
// internal artifact a later Apply step consumes; Result is its summary view.
type Migration struct {
	ProjectRoot string   `json:"project_root"`
	Migrated    []Entry  `json:"migrated"`
	Imports     []Import `json:"imports"`
	Skipped     []Skip   `json:"skipped"`
}

// Result is the JSON-output summary of a Migration. Failed is true when any
// entry was skipped (unsupported source type or missing path), so the caller
// can return valid results yet exit non-zero.
type Result struct {
	Migrated []Entry  `json:"migrated"`
	Skipped  []Skip   `json:"skipped"`
	Imported []string `json:"imported"`
	Failed   bool     `json:"failed"`
}

// Result derives the summary Result view from a Migration.
func (m Migration) Result() Result {
	imported := make([]string, 0, len(m.Imports))
	for _, imp := range m.Imports {
		imported = append(imported, imp.Name)
	}
	migrated := m.Migrated
	if migrated == nil {
		migrated = []Entry{}
	}
	skipped := m.Skipped
	if skipped == nil {
		skipped = []Skip{}
	}
	return Result{
		Migrated: migrated,
		Skipped:  skipped,
		Imported: imported,
		Failed:   len(m.Skipped) > 0,
	}
}

// ParseVercelLock strictly decodes a vercel skills-lock.json, rejecting unknown
// keys for parity with skill.ParseLock. It returns structured validation errors
// (each with a remediation hint) on garbled input or missing required fields,
// rather than a bare error, so the CLI can render the JSON-default error shape.
func ParseVercelLock(r io.Reader) (VercelLock, []config.ValidationError) {
	var v VercelLock
	if r == nil {
		return v, []config.ValidationError{{
			Code:    CodeParseError,
			Path:    "skills-lock.json",
			Message: "no input provided; pass a vercel skills-lock.json produced by `npx skills`",
		}}
	}

	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, []config.ValidationError{{
			Code:    CodeParseError,
			Path:    "skills-lock.json",
			Message: fmt.Sprintf("cannot parse vercel skills-lock.json: %v; ensure it is valid JSON produced by `npx skills`", err),
		}}
	}

	var errs []config.ValidationError
	if v.Version == 0 {
		errs = append(errs, config.ValidationError{
			Code:    skill.CodeRequired,
			Path:    "version",
			Field:   "version",
			Message: "version is required; expected a vercel skills-lock.json with \"version\": 1",
		})
	}
	if len(v.Skills) == 0 {
		errs = append(errs, config.ValidationError{
			Code:    CodeEmptyLock,
			Path:    "skills",
			Field:   "skills",
			Message: "no skills found to migrate; ensure the vercel skills-lock.json lists skills under \"skills\"",
		})
		return v, errs
	}

	for _, name := range sortedKeys(v.Skills) {
		entry := v.Skills[name]
		base := "skills." + name
		if strings.TrimSpace(entry.SourceType) == "" {
			errs = append(errs, config.ValidationError{
				Code:    skill.CodeRequired,
				Path:    base + ".sourceType",
				Field:   "sourceType",
				Message: fmt.Sprintf("sourceType is required for skill %q; set it to one of github, gitlab, local", name),
			})
		}
		if strings.TrimSpace(entry.Source) == "" {
			errs = append(errs, config.ValidationError{
				Code:    skill.CodeRequired,
				Path:    base + ".source",
				Field:   "source",
				Message: fmt.Sprintf("source is required for skill %q; set the owner/repo or local path", name),
			})
		}
	}

	return v, errs
}

// versionFromRef maps a vercel ref onto a native version spec that is guaranteed
// to pass skill.ValidateVersionSpec:
//   - empty/absent  → "latest"            (track the default branch)
//   - "branch:<n>"  → "branch:<n>"        (pass through)
//   - 7-40 hex sha  → the bare sha        (commit pin, validates as a bare spec)
//   - any other tag → the bare value      (tag pin)
//
// Every non-empty ref is returned verbatim; only an absent ref becomes "latest".
func versionFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "latest"
	}
	return ref
}

// Plan classifies every entry in a vercel lock into a write-free Migration. It
// inspects local paths on the current machine to apply the local-source split
// but performs no writes, no commit resolution, and no network access. A nil
// error is returned for partial plans; per-entry problems land in Skipped.
func Plan(v VercelLock, projectRoot string) (Migration, error) {
	m := Migration{ProjectRoot: projectRoot}

	for _, name := range sortedKeys(v.Skills) {
		entry := v.Skills[name]
		switch entry.SourceType {
		case sourceTypeGitHub, sourceTypeGitLab:
			ent, ve := planRemote(name, entry)
			if ve != nil {
				m.Skipped = append(m.Skipped, *ve)
				continue
			}
			m.Migrated = append(m.Migrated, ent)
		case sourceTypeLocal:
			m.classifyLocal(name, entry, projectRoot)
		case sourceTypeNodeModules, sourceTypeWellKnown, sourceTypeHuggingFace, sourceTypeMintlify:
			m.Skipped = append(m.Skipped, Skip{
				Name:       name,
				SourceType: entry.SourceType,
				Source:     entry.Source,
				Reason:     ReasonUnsupported,
				Message:    fmt.Sprintf("sourceType %q is not supported; install %q manually or re-add it via `auto skill add`", entry.SourceType, name),
			})
		default:
			m.Skipped = append(m.Skipped, Skip{
				Name:       name,
				SourceType: entry.SourceType,
				Source:     entry.Source,
				Reason:     ReasonUnsupported,
				Message:    fmt.Sprintf("unknown sourceType %q for skill %q; re-add it via `auto skill add`", entry.SourceType, name),
			})
		}
	}

	return m, nil
}

// planRemote builds a lock-add Entry for a github/gitlab source, deriving a
// credential-free URL and a subpath from skillPath.
func planRemote(name string, entry VercelEntry) (Entry, *Skip) {
	url, err := deriveRemoteURL(entry.SourceType, entry.Source)
	if err != nil {
		return Entry{}, &Skip{
			Name:       name,
			SourceType: entry.SourceType,
			Source:     entry.Source,
			Reason:     ReasonInvalidSource,
			Message:    fmt.Sprintf("cannot derive a credential-free URL for skill %q: %v; re-add it via `auto skill add`", name, err),
		}
	}
	return Entry{
		Name:        name,
		Source:      entry.Source,
		URL:         url,
		Subpath:     subpathFromSkillPath(entry.SkillPath),
		VersionSpec: versionFromRef(entry.Ref),
		Local:       false,
		State:       "unresolved",
	}, nil
}

// classifyLocal applies the local-source split: a git repo becomes a
// non-portable local lock entry, a non-git directory is marked for authored
// import into ./skills/<name>/, and a missing path is reported and skipped.
func (m *Migration) classifyLocal(name string, entry VercelEntry, projectRoot string) {
	abs := entry.Source
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(projectRoot, entry.Source)
	}
	if cleaned, err := filepath.Abs(abs); err == nil {
		abs = cleaned
	}

	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		m.Skipped = append(m.Skipped, Skip{
			Name:       name,
			SourceType: entry.SourceType,
			Source:     entry.Source,
			Reason:     ReasonMissingPath,
			Message:    fmt.Sprintf("local path %q for skill %q is missing on this machine; re-add it via `auto skill add` once available", entry.Source, name),
		})
		return
	}

	if isGitRepo(abs) {
		m.Migrated = append(m.Migrated, Entry{
			Name:        name,
			Source:      abs,
			URL:         abs,
			Subpath:     subpathFromSkillPath(entry.SkillPath),
			VersionSpec: versionFromRef(entry.Ref),
			Local:       true,
			State:       "unresolved",
		})
		return
	}

	m.Imports = append(m.Imports, Import{
		Name:       name,
		SourcePath: abs,
	})
}

// deriveRemoteURL produces a canonical, credential-free https URL from a vercel
// remote source, prepending the host for bare "owner/repo" shorthand.
func deriveRemoteURL(sourceType, src string) (string, error) {
	src = strings.TrimSpace(src)
	candidate := src
	if isBareOwnerRepo(src) {
		if host, ok := remoteHosts[sourceType]; ok {
			candidate = host + "/" + src
		}
	}
	canonical, _, err := transport.CanonicalizeURL(candidate)
	if err != nil {
		return "", err
	}
	return canonical, nil
}

// subpathFromSkillPath strips the trailing /SKILL.md from a vercel skillPath to
// yield the skill directory within the repo. Returns "" when unknown.
func subpathFromSkillPath(skillPath string) string {
	skillPath = strings.TrimSpace(skillPath)
	if skillPath == "" {
		return ""
	}
	dir := path.Dir(skillPath)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}

// isBareOwnerRepo returns true for "owner/repo" inputs whose first segment is
// not a hostname (mirrors source.ParseSource's bare-shorthand detection).
func isBareOwnerRepo(input string) bool {
	if strings.Contains(input, "://") || strings.HasPrefix(input, "git@") || strings.HasPrefix(input, "-") {
		return false
	}
	first, _, ok := strings.Cut(input, "/")
	if !ok {
		return false
	}
	return !strings.Contains(first, ".")
}

// isGitRepo returns true if path is inside a git work tree. Mirrors the
// best-effort detection in internal/add (unexported there) — a fast .git stat
// with a `git rev-parse` fallback.
func isGitRepo(path string) bool {
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return true
	}
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// sortedKeys returns the skill names in deterministic order.
func sortedKeys(skills map[string]VercelEntry) []string {
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
