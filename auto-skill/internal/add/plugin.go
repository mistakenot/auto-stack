package add

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-skill/internal/cache"
	"github.com/mistakenot/auto-skill/internal/discovery"
	"github.com/mistakenot/auto-skill/internal/plugin"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/trace"
)

// AddedPlugin records a plugin written to lock + skills.yaml as a unit. Skills
// are the member names installed at Commit; Removed lists previously locked
// members of the same plugin that the manifest at Commit no longer ships (a
// re-add of an already-installed plugin); Skipped lists skills/ children the
// spec says to skip rather than fail on.
type AddedPlugin struct {
	Name        string           `json:"name"`
	Root        string           `json:"root"`
	Commit      string           `json:"commit"`
	VersionSpec string           `json:"version_spec"`
	Description string           `json:"description,omitempty"`
	Skills      []string         `json:"skills"`
	Removed     []string         `json:"removed,omitempty"`
	Skipped     []plugin.Skipped `json:"skipped,omitempty"`
	Unknown     []string         `json:"unknown_manifest_fields,omitempty"`
	Local       bool             `json:"local,omitempty"`
}

const (
	CodePluginNoSkills  = "plugin_no_skills"
	CodePluginCollision = "plugin_collision"
	CodePluginFlags     = "plugin_flag_conflict"
)

// validatePluginFlags rejects selection flags that have no meaning for a plugin
// add: the manifest, not the caller, decides membership.
func validatePluginFlags(opts Options) error {
	if opts.Plugin == "" {
		return nil
	}
	switch {
	case len(opts.Skills) > 0:
		return &AddError{Code: CodePluginFlags, Message: "--plugin cannot be combined with --skill: a plugin installs every skill its manifest ships"}
	case len(opts.Paths) > 0:
		return &AddError{Code: CodePluginFlags, Message: "--plugin cannot be combined with --path: the plugin root is the scope"}
	case opts.As != "":
		return &AddError{Code: CodePluginFlags, Message: "--plugin cannot be combined with --as: plugin skills keep their declared names"}
	case opts.FullDepth:
		return &AddError{Code: CodePluginFlags, Message: "--plugin cannot be combined with --full-depth: plugin skills are discovered one level under skills/"}
	}
	return nil
}

// pluginSource is what the two source kinds (cached remote commit, local
// working tree) hand to the shared plugin pipeline.
type pluginSource struct {
	tree     plugin.Tree
	url      string // canonical URL recorded in the lock
	sha      string
	spec     string
	local    bool
	resultID string // Result.Source
	// materialize places the plugin's skills/ subtree under dest/skills so the
	// on-disk discovery and dry render can run; returns false when the plugin
	// ships no skills/ directory.
	materialize func(root, dest string) (bool, error)
}

// addPlugin is the shared --plugin pipeline: locate → discover members →
// (list | validate render → write lock + skills.yaml as a unit).
func addPlugin(env skill.Env, ps pluginSource, opts Options) (Result, error) {
	tr := opts.Trace
	done := trace.Spanf(tr, "add locate plugin %q", opts.Plugin)
	loc, err := plugin.Locate(ps.tree, opts.Plugin)
	if err != nil {
		done("error=%v", err)
		return Result{Source: ps.resultID}, err
	}
	done("root=%s name=%s", loc.Root, loc.Manifest.Name)

	tmpDir, err := os.MkdirTemp("", "auto-skill-plugin-*")
	if err != nil {
		return Result{Source: ps.resultID}, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	done = trace.Spanf(tr, "add materialize plugin skills")
	hasSkills, err := ps.materialize(loc.Root, tmpDir)
	if err != nil {
		done("error=%v", err)
		return Result{Source: ps.resultID}, fmt.Errorf("extract plugin skills: %w", err)
	}
	var members []plugin.Skill
	var skipped []plugin.Skipped
	if hasSkills {
		members, skipped, err = plugin.DiscoverSkills(tmpDir)
		if err != nil {
			done("error=%v", err)
			return Result{Source: ps.resultID}, fmt.Errorf("discover plugin skills: %w", err)
		}
	}
	done("members=%d skipped=%d", len(members), len(skipped))

	added := AddedPlugin{
		Name:        loc.Manifest.Name,
		Root:        loc.Root,
		Commit:      ps.sha,
		VersionSpec: ps.spec,
		Description: loc.Manifest.Description,
		Skipped:     skipped,
		Unknown:     loc.Unknown,
		Local:       ps.local,
	}
	for _, m := range members {
		added.Skills = append(added.Skills, m.Name)
	}

	if opts.List {
		listed := make([]ListedSkill, 0, len(members))
		for _, m := range members {
			listed = append(listed, ListedSkill{
				Name:      m.Name,
				Subpath:   path.Join(loc.Root, m.Subpath),
				NameValid: true,
				Container: "plugin:" + loc.Manifest.Name,
			})
		}
		return Result{Listed: listed, Plugin: &added, Source: ps.resultID}, nil
	}

	if len(members) == 0 {
		return Result{Source: ps.resultID}, &AddError{
			Code:    CodePluginNoSkills,
			Message: fmt.Sprintf("plugin %q ships no installable skills under %s/skills/; nothing to add", loc.Manifest.Name, displayRoot(loc.Root)),
		}
	}

	// Dry-render every member before any write so the add is atomic.
	disc := make([]discovery.Discovered, 0, len(members))
	for _, m := range members {
		disc = append(disc, discovery.Discovered{Name: m.Name, Subpath: m.Subpath})
	}
	done = trace.Spanf(tr, "add validate plugin renderable")
	if err := validateRenderable(tmpDir, disc); err != nil {
		done("error=%v", err)
		return Result{Source: ps.resultID}, err
	}
	done("validated=%d", len(disc))

	if err := os.MkdirAll(env.SkillsConfigDir(), 0o755); err != nil {
		return Result{Source: ps.resultID}, fmt.Errorf("create config dir: %w", err)
	}

	var addedSkills []AddedSkill
	writeErr := skill.WithFileLock(env.LockPath(), func() error {
		lock, err := loadOrCreateLock(env)
		if err != nil {
			return err
		}
		syaml, err := loadOrCreateSkillsYAML(env)
		if err != nil {
			return err
		}

		name := loc.Manifest.Name
		if existing, ok := lock.Plugins[name]; ok && !sameSource(existing, ps) {
			return &AddError{
				Code:    CodePluginCollision,
				Message: fmt.Sprintf("plugin %q already installed from %s; remove it first (auto skill remove %s --plugin)", name, existing.URL, name),
			}
		}
		for _, m := range members {
			existing, ok := lock.Skills[m.Name]
			if !ok {
				continue
			}
			if existing.Plugin != "" && existing.Plugin != name {
				return &AddError{
					Code:    CodeNameCollision,
					Message: fmt.Sprintf("skill %q is already installed by plugin %q; remove that plugin first", m.Name, existing.Plugin),
				}
			}
			if !sameSource(existing, ps) {
				return &AddError{
					Code:    CodeNameCollision,
					Message: fmt.Sprintf("skill %q already exists from %s; remove it first (auto skill remove %s)", m.Name, existing.URL, m.Name),
				}
			}
		}

		// Members the previously locked plugin had that this commit no longer
		// ships are dropped so the follow-on sync prunes their renders.
		newSet := make(map[string]bool, len(members))
		for _, m := range members {
			newSet[m.Name] = true
		}
		for _, old := range lock.PluginMembers(name) {
			if !newSet[old] {
				delete(lock.Skills, old)
				added.Removed = append(added.Removed, old)
			}
		}
		sort.Strings(added.Removed)

		if lock.Plugins == nil {
			lock.Plugins = map[string]skill.LockEntry{}
		}
		lock.Plugins[name] = skill.LockEntry{
			Source:      ps.url,
			URL:         ps.url,
			VersionSpec: ps.spec,
			Ref:         ps.sha,
			Commit:      ps.sha,
			Subpath:     loc.Root,
			Local:       ps.local,
			State:       "resolved",
		}
		addedSkills = addedSkills[:0]
		for _, m := range members {
			sub := path.Join(loc.Root, m.Subpath)
			lock.Skills[m.Name] = skill.LockEntry{
				Source:      ps.url,
				URL:         ps.url,
				VersionSpec: ps.spec,
				Ref:         ps.sha,
				Commit:      ps.sha,
				Subpath:     sub,
				Local:       ps.local,
				State:       "resolved",
				Plugin:      name,
			}
			addedSkills = append(addedSkills, AddedSkill{
				Name:        m.Name,
				Subpath:     sub,
				Commit:      ps.sha,
				VersionSpec: ps.spec,
				Local:       ps.local,
				Plugin:      name,
			})
		}

		// The plugin, not each member, carries version intent. No skills.<member>
		// stubs are written; an existing one is kept for its replacements.
		if syaml.Plugins == nil {
			syaml.Plugins = map[string]skill.PluginConfig{}
		}
		syaml.Plugins[name] = skill.PluginConfig{Version: ps.spec}

		if lockErrs := skill.ValidateLock(lock); len(lockErrs) > 0 {
			return &config.ValidationErrorsError{Path: env.LockPath(), Errors: lockErrs}
		}
		if yamlErrs := skill.ValidateSkillsYAML(syaml); len(yamlErrs) > 0 {
			return &config.ValidationErrorsError{Path: env.SkillsYAMLPath(), Errors: yamlErrs}
		}
		if err := config.WriteJSONFileAtomic(env.LockPath(), lock); err != nil {
			return fmt.Errorf("write lock: %w", err)
		}
		if err := writeSkillsYAML(env.SkillsYAMLPath(), syaml); err != nil {
			return fmt.Errorf("write skills.yaml: %w", err)
		}
		return nil
	})
	if writeErr != nil {
		return Result{Source: ps.resultID}, writeErr
	}
	return Result{Added: addedSkills, Plugin: &added, Source: ps.resultID}, nil
}

// sameSource reports whether an existing lock entry came from the source being
// added (so a re-add refreshes it rather than colliding).
func sameSource(existing skill.LockEntry, ps pluginSource) bool {
	return existing.URL == ps.url
}

// remotePluginSource adapts an opened cache repo at a realized commit.
func remotePluginSource(repo *cache.Repo, url, sha, spec string) pluginSource {
	return pluginSource{
		tree: plugin.FuncTree{
			FilesFn:    func() ([]string, error) { return repo.ListFiles(sha) },
			ReadFileFn: func(rel string) ([]byte, error) { return repo.ReadFile(sha, rel) },
		},
		url:      url,
		sha:      sha,
		spec:     spec,
		resultID: url,
		materialize: func(root, dest string) (bool, error) {
			sub := path.Join(root, plugin.SkillsDir)
			err := repo.Extract(sha, sub, filepath.Join(dest, plugin.SkillsDir))
			if err != nil {
				if errors.Is(err, cache.ErrSubpathNotFound) {
					return false, nil
				}
				return false, err
			}
			return true, nil
		},
	}
}

// localPluginSource adapts a local git working tree at HEAD.
func localPluginSource(absPath, canonicalURL, sha, spec string) pluginSource {
	return pluginSource{
		tree:     plugin.DirTree{Root: absPath},
		url:      canonicalURL,
		sha:      sha,
		spec:     spec,
		local:    true,
		resultID: absPath,
		materialize: func(root, dest string) (bool, error) {
			src := filepath.Join(absPath, filepath.FromSlash(root), plugin.SkillsDir)
			info, err := os.Stat(src)
			if err != nil {
				if os.IsNotExist(err) {
					return false, nil
				}
				return false, err
			}
			if !info.IsDir() {
				return false, nil
			}
			return true, safeCopyDir(src, filepath.Join(dest, plugin.SkillsDir))
		},
	}
}

func displayRoot(root string) string {
	if root == "" {
		return "."
	}
	return root
}
