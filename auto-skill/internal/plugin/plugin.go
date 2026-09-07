// Package plugin implements the Agent Plugins standard
// (https://agent-plugins.org/specification, v1.0.0) as far as auto-skill
// consumes it: a plugin is a directory with a root plugin.json manifest and an
// optional skills/ directory whose immediate children are Agent Skills. Other
// component types (mcp.json, client extension directories) are ignored, which
// the spec permits — a conformant client supports at least one component type.
package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// ManifestFile is the fixed manifest location at the plugin root.
	ManifestFile = "plugin.json"
	// SkillsDir is the fixed skills component location under the plugin root.
	SkillsDir = "skills"
	// SchemaURL is the only $schema value spec v1.0.0 accepts.
	SchemaURL = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
)

// nameRE encodes the spec's name grammar: 1–64 chars of lowercase alphanumerics,
// hyphens and periods; first and last char alphanumeric. "--" and ".." are
// rejected separately (a regex for that is unreadable).
var nameRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,62}[a-z0-9])?$`)

// skillNameRE mirrors the auto-skill canonical skill-name grammar. A plugin skill
// whose declared name fails it is skipped (spec: invalid skills are skipped).
var skillNameRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// knownTopLevel is the closed set of permitted top-level manifest fields. Unknown
// fields are reported and ignored (spec), never fatal.
var knownTopLevel = map[string]bool{
	"$schema": true, "name": true, "version": true, "description": true,
	"author": true, "homepage": true, "repository": true, "license": true,
	"keywords": true, "extensions": true,
}

// Manifest is the typed plugin.json.
type Manifest struct {
	Schema      string   `json:"$schema"`
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Repository  string   `json:"repository,omitempty"`
	License     string   `json:"license,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
}

// ManifestError is a fatal manifest violation. Per the spec's failure
// boundaries an invalid plugin.json rejects the entire plugin.
type ManifestError struct {
	Code    string
	Message string
}

func (e *ManifestError) Error() string { return e.Message }

const (
	CodeManifestMissing = "plugin_manifest_missing"
	CodeManifestInvalid = "plugin_manifest_invalid"
	CodeInvalidName     = "invalid_plugin_name"
	CodeNotFound        = "plugin_not_found"
	CodeAmbiguous       = "plugin_ambiguous"
)

// ValidateName enforces the spec's plugin name grammar.
func ValidateName(name string) error {
	if name == "" {
		return &ManifestError{Code: CodeInvalidName, Message: "plugin name is required"}
	}
	if !nameRE.MatchString(name) || strings.Contains(name, "--") || strings.Contains(name, "..") {
		return &ManifestError{
			Code:    CodeInvalidName,
			Message: fmt.Sprintf("plugin name %q is invalid: 1–64 lowercase alphanumerics, hyphens or periods, alphanumeric at both ends, no \"--\" or \"..\"", name),
		}
	}
	return nil
}

// ParseManifest strictly decodes plugin.json. It returns the manifest, a list of
// ignored unknown top-level fields (advisory), and a fatal error for a schema
// violation: unparseable JSON, a non-object document, a wrong or missing
// $schema, or an invalid name.
func ParseManifest(data []byte) (Manifest, []string, error) {
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return Manifest{}, nil, &ManifestError{Code: CodeManifestInvalid, Message: fmt.Sprintf("plugin.json is not a JSON object: %v", err)}
	}
	if raw == nil {
		return Manifest{}, nil, &ManifestError{Code: CodeManifestInvalid, Message: "plugin.json is not a JSON object"}
	}

	var unknown []string
	for k := range raw {
		if !knownTopLevel[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, unknown, &ManifestError{Code: CodeManifestInvalid, Message: fmt.Sprintf("plugin.json has a mistyped field: %v", err)}
	}
	if m.Schema != SchemaURL {
		return Manifest{}, unknown, &ManifestError{
			Code:    CodeManifestInvalid,
			Message: fmt.Sprintf("plugin.json $schema must be %q (got %q)", SchemaURL, m.Schema),
		}
	}
	if err := ValidateName(m.Name); err != nil {
		return Manifest{}, unknown, err
	}
	return m, unknown, nil
}

// Skill is one skill discovered under <plugin>/skills/.
type Skill struct {
	Name    string // declared frontmatter name (falls back to the directory name)
	Dir     string // directory name under skills/
	Subpath string // plugin-root-relative path, e.g. "skills/new-task"
}

// Skipped records a skills/ child the spec says to skip rather than fail on.
type Skipped struct {
	Dir    string
	Reason string
}

// DiscoverSkills applies the spec's discovery rule to an on-disk plugin root:
// immediate child directories of skills/ that contain a regular SKILL.md file.
// No recursion. A missing skills/ dir is not an error (empty result). A skills/
// path that is not a directory disables the component (empty result, one
// Skipped). A child without SKILL.md, or with a name auto-skill cannot manage,
// is skipped and reported.
func DiscoverSkills(root string) ([]Skill, []Skipped, error) {
	skillsDir := filepath.Join(root, SkillsDir)
	info, err := os.Stat(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("stat %s: %w", skillsDir, err)
	}
	if !info.IsDir() {
		return nil, []Skipped{{Dir: SkillsDir, Reason: "skills is not a directory; skills component disabled"}}, nil
	}
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", skillsDir, err)
	}
	var (
		skills  []Skill
		skipped []Skipped
		seen    = map[string]string{}
	)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := e.Name()
		md := filepath.Join(skillsDir, dir, "SKILL.md")
		st, err := os.Stat(md)
		if err != nil || !st.Mode().IsRegular() {
			skipped = append(skipped, Skipped{Dir: dir, Reason: "no regular SKILL.md"})
			continue
		}
		data, err := os.ReadFile(md)
		if err != nil {
			skipped = append(skipped, Skipped{Dir: dir, Reason: "unreadable SKILL.md: " + err.Error()})
			continue
		}
		name := FrontmatterName(string(data))
		if name == "" {
			name = dir
		}
		if !skillNameRE.MatchString(name) {
			skipped = append(skipped, Skipped{Dir: dir, Reason: fmt.Sprintf("skill name %q is not lowercase kebab-case", name)})
			continue
		}
		if prev, dup := seen[name]; dup {
			skipped = append(skipped, Skipped{Dir: dir, Reason: fmt.Sprintf("duplicate skill name %q (already declared by %s)", name, prev)})
			continue
		}
		seen[name] = dir
		skills = append(skills, Skill{Name: name, Dir: dir, Subpath: path.Join(SkillsDir, dir)})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, skipped, nil
}

// FrontmatterName extracts the `name:` field from a SKILL.md YAML frontmatter
// block with a minimal line scanner (no YAML dependency). Empty when absent.
func FrontmatterName(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "---" {
			break
		}
		if val, ok := strings.CutPrefix(line, "name:"); ok {
			return strings.Trim(strings.TrimSpace(val), `"'`)
		}
	}
	return ""
}

// ── Locating a plugin inside a tree ─────────────────────────────────────

// Tree abstracts a read-only file tree (a git commit or a working directory) so
// plugin resolution works identically for a cached remote and a local checkout.
type Tree interface {
	// Files lists every regular file path, slash-separated, relative to the root.
	Files() ([]string, error)
	// ReadFile returns the content of one slash-separated relative path.
	ReadFile(rel string) ([]byte, error)
}

// Located is a resolved plugin: its root directory (slash-separated, relative to
// the tree root, "" for the tree root itself) and its parsed manifest.
type Located struct {
	Root     string
	Manifest Manifest
	Unknown  []string // ignored unknown manifest fields (advisory)
}

// IsPathRef reports whether a --plugin argument is a path (contains a slash,
// or is "." / starts with "./") rather than a bare plugin name.
func IsPathRef(arg string) bool {
	return arg == "." || strings.Contains(arg, "/") || strings.HasPrefix(arg, "./")
}

// Locate resolves a --plugin argument against a tree. A path argument must name
// a directory holding plugin.json; a bare name is matched against the `name` of
// every plugin.json in the tree (exactly one must match). ErrNotFound-class
// errors carry the candidates the tree does hold so the user can pick.
func Locate(tree Tree, arg string) (Located, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return Located{}, &ManifestError{Code: CodeNotFound, Message: "--plugin requires a plugin path or name"}
	}
	if IsPathRef(arg) {
		root := cleanRoot(arg)
		if root == ".." || strings.HasPrefix(root, "../") {
			return Located{}, &ManifestError{Code: CodeNotFound, Message: fmt.Sprintf("plugin path %q escapes the source root", arg)}
		}
		data, err := tree.ReadFile(path.Join(root, ManifestFile))
		if err != nil {
			names, _ := listManifests(tree)
			return Located{}, &ManifestError{
				Code:    CodeManifestMissing,
				Message: fmt.Sprintf("no %s at %q in the source%s", ManifestFile, displayRoot(root), availableHint(names)),
			}
		}
		m, unknown, err := ParseManifest(data)
		if err != nil {
			return Located{}, fmt.Errorf("plugin at %q rejected: %w", displayRoot(root), err)
		}
		return Located{Root: root, Manifest: m, Unknown: unknown}, nil
	}

	if err := ValidateName(arg); err != nil {
		return Located{}, err
	}
	manifests, err := listManifests(tree)
	if err != nil {
		return Located{}, err
	}
	var matches []Located
	for _, mp := range manifests {
		data, err := tree.ReadFile(mp)
		if err != nil {
			continue
		}
		m, unknown, err := ParseManifest(data)
		if err != nil {
			continue // an invalid manifest cannot be the one we are looking for
		}
		if m.Name == arg {
			matches = append(matches, Located{Root: cleanRoot(path.Dir(mp)), Manifest: m, Unknown: unknown})
		}
	}
	switch len(matches) {
	case 0:
		return Located{}, &ManifestError{
			Code:    CodeNotFound,
			Message: fmt.Sprintf("no plugin named %q in the source%s", arg, availableHint(manifests)),
		}
	case 1:
		return matches[0], nil
	default:
		roots := make([]string, 0, len(matches))
		for i := range matches {
			roots = append(roots, displayRoot(matches[i].Root))
		}
		return Located{}, &ManifestError{
			Code:    CodeAmbiguous,
			Message: fmt.Sprintf("plugin name %q matches %d manifests (%s); pass the plugin path instead", arg, len(matches), strings.Join(roots, ", ")),
		}
	}
}

// listManifests returns the slash-separated paths of every plugin.json in the
// tree, sorted.
func listManifests(tree Tree) ([]string, error) {
	files, err := tree.Files()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		if path.Base(f) == ManifestFile {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out, nil
}

func availableHint(manifests []string) string {
	if len(manifests) == 0 {
		return "; it contains no plugin.json manifests"
	}
	roots := make([]string, 0, len(manifests))
	for _, m := range manifests {
		roots = append(roots, displayRoot(cleanRoot(path.Dir(m))))
	}
	return "; available plugins: " + strings.Join(roots, ", ")
}

func cleanRoot(p string) string {
	p = path.Clean(strings.TrimPrefix(strings.TrimSpace(p), "./"))
	if p == "." {
		return ""
	}
	return strings.TrimSuffix(p, "/")
}

func displayRoot(root string) string {
	if root == "" {
		return "."
	}
	return root
}

// ── Tree implementations ────────────────────────────────────────────────

// DirTree is a Tree over an on-disk directory.
type DirTree struct{ Root string }

// Files walks the directory, skipping .git, and lists regular files.
func (d DirTree) Files() ([]string, error) {
	var out []string
	err := filepath.WalkDir(d.Root, func(p string, e os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if e.IsDir() {
			if e.Name() == ".git" && p != d.Root {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(d.Root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

// ReadFile reads a relative path, refusing escapes from the root.
func (d DirTree) ReadFile(rel string) ([]byte, error) {
	clean := path.Clean("/" + rel)
	full := filepath.Join(d.Root, filepath.FromSlash(clean))
	if !strings.HasPrefix(full, filepath.Clean(d.Root)+string(filepath.Separator)) {
		return nil, errors.New("path escapes root")
	}
	return os.ReadFile(full)
}

// FuncTree is a Tree built from two callbacks (used to adapt a git repo).
type FuncTree struct {
	FilesFn    func() ([]string, error)
	ReadFileFn func(rel string) ([]byte, error)
}

func (f FuncTree) Files() ([]string, error)            { return f.FilesFn() }
func (f FuncTree) ReadFile(rel string) ([]byte, error) { return f.ReadFileFn(rel) }

// DiscoverSkillsTree applies the same discovery rule as DiscoverSkills to a
// Tree (a git commit) rooted at pluginRoot: it parses the manifest, then lists
// skills/<dir>/SKILL.md one level deep and reads each frontmatter name. It is
// what sync/update use to re-derive plugin membership at a new commit without
// extracting the tree.
func DiscoverSkillsTree(tree Tree, pluginRoot string) (Manifest, []Skill, []Skipped, error) {
	root := cleanRoot(pluginRoot)
	data, err := tree.ReadFile(path.Join(root, ManifestFile))
	if err != nil {
		return Manifest{}, nil, nil, &ManifestError{
			Code:    CodeManifestMissing,
			Message: fmt.Sprintf("no %s at %q", ManifestFile, displayRoot(root)),
		}
	}
	m, _, err := ParseManifest(data)
	if err != nil {
		return Manifest{}, nil, nil, err
	}
	files, err := tree.Files()
	if err != nil {
		return m, nil, nil, err
	}
	prefix := path.Join(root, SkillsDir) + "/"
	var (
		skills  []Skill
		skipped []Skipped
		seen    = map[string]string{}
	)
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) {
			continue
		}
		rest := strings.TrimPrefix(f, prefix)
		dir, file, ok := strings.Cut(rest, "/")
		if !ok || file != "SKILL.md" || dir == "" {
			continue // deeper than one level, or not a SKILL.md
		}
		content, err := tree.ReadFile(f)
		if err != nil {
			skipped = append(skipped, Skipped{Dir: dir, Reason: "unreadable SKILL.md: " + err.Error()})
			continue
		}
		name := FrontmatterName(string(content))
		if name == "" {
			name = dir
		}
		if !skillNameRE.MatchString(name) {
			skipped = append(skipped, Skipped{Dir: dir, Reason: fmt.Sprintf("skill name %q is not lowercase kebab-case", name)})
			continue
		}
		if prev, dup := seen[name]; dup {
			skipped = append(skipped, Skipped{Dir: dir, Reason: fmt.Sprintf("duplicate skill name %q (already declared by %s)", name, prev)})
			continue
		}
		seen[name] = dir
		skills = append(skills, Skill{Name: name, Dir: dir, Subpath: path.Join(SkillsDir, dir)})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return m, skills, skipped, nil
}
