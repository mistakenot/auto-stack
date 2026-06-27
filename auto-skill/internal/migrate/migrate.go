// Package migrate translates a vercel-style skills-lock.json into the native
// auto-skill lock/skills.yaml model. Parse/Plan are a pure, offline transform —
// they classify without resolving commits or touching the network. Apply is the
// additive writer: it only ever creates/extends .auto/skills/lock.json,
// .auto/skills/skills.yaml, and authored skills under ./skills/, never resolving
// commits or overwriting existing entries; sync resolves the commits later.
package migrate

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/transport"
	"gopkg.in/yaml.v3"
)

// Authored-import safety limits, mirroring internal/add (unexported there).
const (
	maxImportFiles    = 2000
	maxImportTotalMiB = 64
	maxImportFileMiB  = 8

	maxImportTotal = maxImportTotalMiB * 1024 * 1024
	maxImportFile  = maxImportFileMiB * 1024 * 1024
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
	ReasonUnsupported    = "unsupported_source_type"
	ReasonMissingPath    = "missing_path"
	ReasonInvalidSource  = "invalid_source"
	ReasonAlreadyPresent = "already_present"
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
		// A vercel local source may be a subdirectory inside a worktree (the
		// checked-in corpus points at e.g. <repo>/skills). A later sync clones
		// the repo top-level, not the subdir, so resolve URL/Source to the
		// worktree root and carry the source dir as a Subpath relative to it.
		// On any failure, fall back to the source path as given.
		url := abs
		subpath := subpathFromSkillPath(entry.SkillPath)
		if top, err := gitToplevel(abs); err == nil && top != "" {
			url = top
			// git --show-toplevel returns a symlink-resolved path; resolve abs the
			// same way before computing the relative subpath so they share a root.
			base := abs
			if real, rerr := filepath.EvalSymlinks(abs); rerr == nil {
				base = real
			}
			if rel, rerr := filepath.Rel(top, base); rerr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				subpath = filepath.ToSlash(rel)
			} else {
				subpath = ""
			}
		}
		m.Migrated = append(m.Migrated, Entry{
			Name:        name,
			Source:      url,
			URL:         localFileURL(url),
			Subpath:     subpath,
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

// localFileURL turns an absolute local repo path into a canonical file:// URL.
// A local lock entry's URL is later canonicalized and turned into a cache path by
// sync; a bare absolute path lands in transport's "bare host/path" branch and
// yields an empty Host, which the cache rejects with "invalid path component".
// transport already maps file:// to a usable _local identity, so emit that form.
func localFileURL(absPath string) string {
	if canon, _, err := transport.CanonicalizeURL("file://" + absPath); err == nil {
		return canon
	}
	return "file://" + absPath
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

// gitToplevel returns the absolute path of the worktree top-level containing
// path, via `git -C <path> rev-parse --show-toplevel`. The returned path is the
// repo root a later `git clone` can address (a vercel local source may point at
// a subdirectory inside the worktree).
func gitToplevel(path string) (string, error) {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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

// Apply additively materializes the Migration under m.ProjectRoot, returning the
// Result that actually occurred. It NEVER touches the source skills-lock.json and
// never overwrites existing entries: it merges migrated entries into
// .auto/skills/lock.json (state "unresolved", no commit) and .auto/skills/skills.yaml
// (seeded version intent, empty replacements), and copies each non-git local
// Import into ./skills/<name>/ as an authored skill. An entry whose name already
// exists is left untouched and reported as skipped. When dryRun is true, Apply
// computes the full Result (including collision detection) but writes nothing.
func (m Migration) Apply(dryRun bool) (Result, error) {
	env := skill.Env{Root: m.ProjectRoot}

	lock, err := loadOrCreateLock(env)
	if err != nil {
		return Result{}, err
	}
	syaml, err := loadOrCreateSkillsYAML(env)
	if err != nil {
		return Result{}, err
	}

	res := Result{
		Migrated: []Entry{},
		Skipped:  append([]Skip{}, m.Skipped...),
		Imported: []string{},
	}

	for _, e := range m.Migrated {
		if _, exists := lock.Skills[e.Name]; exists {
			res.Skipped = append(res.Skipped, Skip{
				Name:       e.Name,
				SourceType: localOrRemote(e.Local),
				Source:     e.Source,
				Reason:     ReasonAlreadyPresent,
				Message:    fmt.Sprintf("skill %q already present in lock.json; left unchanged (migration is additive)", e.Name),
			})
			continue
		}
		lock.Skills[e.Name] = skill.LockEntry{
			Source:      e.Source,
			URL:         e.URL,
			VersionSpec: e.VersionSpec,
			Subpath:     e.Subpath,
			Local:       e.Local,
			State:       "unresolved",
		}
		if _, ok := syaml.Skills[e.Name]; !ok {
			syaml.Skills[e.Name] = skill.SkillConfig{Version: e.VersionSpec}
		}
		res.Migrated = append(res.Migrated, e)
	}

	// Resolve authored-import collisions (stat is read-only, safe in dry-run).
	var toImport []Import
	for _, imp := range m.Imports {
		dest := filepath.Join(env.SkillsDir(), imp.Name)
		if _, statErr := os.Stat(dest); statErr == nil {
			res.Skipped = append(res.Skipped, Skip{
				Name:       imp.Name,
				SourceType: sourceTypeLocal,
				Source:     imp.SourcePath,
				Reason:     ReasonAlreadyPresent,
				Message:    fmt.Sprintf("skills/%s already exists; left unchanged (migration is additive)", imp.Name),
			})
			continue
		}
		res.Imported = append(res.Imported, imp.Name)
		toImport = append(toImport, imp)
	}

	res.Failed = len(res.Skipped) > 0

	if dryRun {
		return res, nil
	}

	if errs := skill.ValidateLock(lock); len(errs) > 0 {
		return res, fmt.Errorf("refusing to write invalid lock.json: %s", errs[0].Message)
	}
	if errs := skill.ValidateSkillsYAML(syaml); len(errs) > 0 {
		return res, fmt.Errorf("refusing to write invalid skills.yaml: %s", errs[0].Message)
	}

	if err := os.MkdirAll(env.SkillsConfigDir(), 0o755); err != nil {
		return res, fmt.Errorf("create config dir: %w", err)
	}
	if err := writeLock(env.LockPath(), lock); err != nil {
		return res, err
	}
	if err := writeSkillsYAML(env.SkillsYAMLPath(), syaml); err != nil {
		return res, err
	}

	for _, imp := range toImport {
		dest := filepath.Join(env.SkillsDir(), imp.Name)
		if err := os.MkdirAll(env.SkillsDir(), 0o755); err != nil {
			return res, fmt.Errorf("create skills dir: %w", err)
		}
		if err := safeCopyDir(imp.SourcePath, dest); err != nil {
			return res, fmt.Errorf("import skill %q: %w", imp.Name, err)
		}
	}

	return res, nil
}

// localOrRemote labels a migrated entry's origin for skip reports.
func localOrRemote(local bool) string {
	if local {
		return sourceTypeLocal
	}
	return "remote"
}

// loadOrCreateLock reads an existing lock.json or returns a fresh empty Lock,
// mirroring internal/add (unexported there).
func loadOrCreateLock(env skill.Env) (*skill.Lock, error) {
	data, err := os.ReadFile(env.LockPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &skill.Lock{Version: 1, Skills: map[string]skill.LockEntry{}}, nil
		}
		return nil, fmt.Errorf("read lock: %w", err)
	}
	lock, err := skill.ParseLock(data)
	if err != nil {
		return nil, fmt.Errorf("parse lock: %w", err)
	}
	if lock.Version == 0 {
		lock.Version = 1
	}
	if lock.Skills == nil {
		lock.Skills = map[string]skill.LockEntry{}
	}
	return lock, nil
}

// loadOrCreateSkillsYAML reads an existing skills.yaml or returns a fresh empty
// one, mirroring internal/add (unexported there).
func loadOrCreateSkillsYAML(env skill.Env) (*skill.SkillsYAML, error) {
	data, err := os.ReadFile(env.SkillsYAMLPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &skill.SkillsYAML{Skills: map[string]skill.SkillConfig{}}, nil
		}
		return nil, fmt.Errorf("read skills.yaml: %w", err)
	}
	cfg, err := skill.ParseSkillsYAML(data)
	if err != nil {
		return nil, fmt.Errorf("parse skills.yaml: %w", err)
	}
	if cfg.Skills == nil {
		cfg.Skills = make(map[string]skill.SkillConfig)
	}
	return cfg, nil
}

// writeLock writes lock.json with the lock.go indentation convention.
func writeLock(path string, lock *skill.Lock) error {
	data, err := skill.EncodeJSON(lock)
	if err != nil {
		return fmt.Errorf("marshal lock: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// writeSkillsYAML marshals and writes skills.yaml. A SkillConfig with no
// replacements omits the "replacements:" key entirely (the named-map field is
// omitempty).
func writeSkillsYAML(path string, cfg *skill.SkillsYAML) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal skills.yaml: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// safeCopyDir copies src to dest with the same archive-safety predicates as
// internal/add's importer: no symlinks, no special files, file count/size
// limits, and containment within dest.
func safeCopyDir(src, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}

	var fileCount int
	var totalSize int64

	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(src, p)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dest, rel)

		cleanTarget := filepath.Clean(target)
		cleanDest := filepath.Clean(dest) + string(filepath.Separator)
		if cleanTarget != filepath.Clean(dest) && !strings.HasPrefix(cleanTarget, cleanDest) {
			return fmt.Errorf("path escapes destination: %s", rel)
		}

		info, lstatErr := os.Lstat(p)
		if lstatErr != nil {
			return lstatErr
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not allowed in skill trees: %s", rel)
		}
		if !info.Mode().IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("special file not allowed in skill trees: %s", rel)
		}

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		fileCount++
		if fileCount > maxImportFiles {
			return fmt.Errorf("source exceeds %d file limit", maxImportFiles)
		}
		if info.Size() > maxImportFile {
			return fmt.Errorf("file %s is %d bytes, exceeding %d MiB limit", rel, info.Size(), maxImportFileMiB)
		}
		totalSize += info.Size()
		if totalSize > maxImportTotal {
			return fmt.Errorf("source total size exceeds %d MiB limit", maxImportTotalMiB)
		}

		return copyFile(p, target)
	})
}

// copyFile copies a single regular file, preserving execute bits.
func copyFile(src, dest string) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sf.Close() }()

	info, err := sf.Stat()
	if err != nil {
		return err
	}

	df, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode()&0o755|0o644)
	if err != nil {
		return err
	}
	defer func() { _ = df.Close() }()

	_, err = io.Copy(df, sf)
	return err
}
