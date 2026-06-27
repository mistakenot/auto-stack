package inspect

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mistakenot/auto-skill/internal/render"
	"github.com/mistakenot/auto-skill/internal/skill"
	"gopkg.in/yaml.v3"
)

// Inspect joins authored (skill.List) ∪ vendored (lock) skills with derived state
// (manifest) and an offline stale flag (on-disk tree digest vs the manifest's
// expected skill_version). It returns the valid views plus parse-error strings
// (partial success): a malformed authored skill is reported but never hides the
// valid ones. filter scopes the result to authored-only or vendored-only.
func Inspect(env skill.Env, filter Filter) ([]SkillView, []string, error) {
	authored, parseErrors, err := skill.List(env)
	if err != nil {
		return nil, nil, err
	}
	authoredByName := make(map[string]skill.SkillSummary, len(authored))
	for _, s := range authored {
		authoredByName[s.Name] = s
	}

	lock, err := loadLock(env)
	if err != nil {
		return nil, nil, err
	}
	manifest, err := loadManifest(env)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := loadProjectConfig(env)
	if err != nil {
		return nil, nil, err
	}
	targets := resolveTargets(env, cfg)

	views := make([]SkillView, 0, len(authored))

	// Authored half (origin local). An authored skill shadows a same-named
	// vendored one: the authored row carries shadowed=true, the vendored row is
	// dropped.
	if !filter.Vendored {
		for _, s := range authored {
			shadowed := lock != nil && hasLockEntry(lock, s.Name)
			views = append(views, SkillView{
				Name:         s.Name,
				Origin:       OriginLocal,
				Description:  s.Description,
				Path:         s.Path,
				SkillVersion: manifestSkillVersion(manifest, s.Name),
				Stale:        computeStale(s.Name, manifest, targets),
				Shadowed:     shadowed,
			})
		}
	}

	// Vendored half (origin vendored), skipping any name authored locally.
	if !filter.Local && lock != nil {
		for name, entry := range lock.Skills {
			if _, isAuthored := authoredByName[name]; isAuthored {
				continue
			}
			views = append(views, SkillView{
				Name:         name,
				Origin:       OriginVendored,
				Description:  vendoredDescription(name, targets),
				Path:         lockPath(entry),
				SkillVersion: manifestSkillVersion(manifest, name),
				Stale:        computeStale(name, manifest, targets),
			})
		}
	}

	sort.Slice(views, func(i, j int) bool {
		if views[i].Name != views[j].Name {
			return views[i].Name < views[j].Name
		}
		return views[i].Origin < views[j].Origin
	})
	return views, parseErrors, nil
}

// Describe returns provenance for one skill: lock identity (source/url/ref/commit/
// version_spec) plus manifest-derived state (skill_version, resolved
// replacements). An authored skill describes with origin local and no
// source/commit. An unknown name is a hard error carrying a remediation hint.
func Describe(env skill.Env, name string) (Provenance, error) {
	authored, _, err := skill.List(env)
	if err != nil {
		return Provenance{}, err
	}
	var authoredSummary *skill.SkillSummary
	for i := range authored {
		if authored[i].Name == name {
			authoredSummary = &authored[i]
			break
		}
	}

	lock, err := loadLock(env)
	if err != nil {
		return Provenance{}, err
	}
	manifest, err := loadManifest(env)
	if err != nil {
		return Provenance{}, err
	}

	var lockEntry *skill.LockEntry
	if lock != nil {
		if e, ok := lock.Skills[name]; ok {
			lockEntry = &e
		}
	}

	if authoredSummary == nil && lockEntry == nil {
		return Provenance{}, fmt.Errorf("unknown skill %q: run auto skill list to see available skills", name)
	}

	prov := Provenance{Name: name}
	if authoredSummary != nil {
		prov.Origin = OriginLocal
		prov.Description = authoredSummary.Description
		prov.Path = authoredSummary.Path
	} else {
		prov.Origin = OriginVendored
		prov.Source = lockEntry.Source
		prov.URL = lockEntry.URL
		prov.Ref = lockEntry.Ref
		prov.Commit = lockEntry.Commit
		prov.VersionSpec = lockEntry.VersionSpec
		prov.Path = lockPath(*lockEntry)
	}
	if manifest != nil {
		if ms, ok := manifest.Skills[name]; ok {
			prov.SkillVersion = ms.SkillVersion
			if len(ms.Replacements) > 0 {
				prov.Replacements = ms.Replacements
			}
		}
	}
	return prov, nil
}

// Get returns the full rendered SKILL.md bytes for name plus the target it was
// read from. With an explicit target, it reads that target's tree (erroring if the
// skill is absent there). Otherwise it reads the first configured target holding
// the skill (deterministic order); an authored-only skill falls back to
// ./skills/<name>/SKILL.md. A skill rendered nowhere is a hard error with a
// run-sync hint.
func Get(env skill.Env, name, target string) ([]byte, string, error) {
	cfg, err := loadProjectConfig(env)
	if err != nil {
		return nil, "", err
	}
	targets := resolveTargets(env, cfg)

	if target != "" {
		for _, t := range targets {
			if t.Name == target {
				data, readErr := readSkillMD(filepath.Join(t.Dir, name))
				if readErr != nil {
					return nil, "", fmt.Errorf("skill %q not rendered in target %q: run auto skill sync", name, target)
				}
				return data, t.Name, nil
			}
		}
		return nil, "", fmt.Errorf("unknown target %q: run auto skill target list to see configured targets", target)
	}

	for _, t := range targets {
		data, readErr := readSkillMD(filepath.Join(t.Dir, name))
		if readErr == nil {
			return data, t.Name, nil
		}
	}

	// Authored-only fallback: serve the source SKILL.md directly.
	authoredDir := filepath.Join(env.SkillsDir(), name)
	if data, readErr := readSkillMD(authoredDir); readErr == nil {
		return data, OriginLocal, nil
	}

	return nil, "", fmt.Errorf("skill %q is not rendered into any target: run auto skill sync", name)
}

// hasLockEntry reports whether the lock records name.
func hasLockEntry(lock *skill.Lock, name string) bool {
	_, ok := lock.Skills[name]
	return ok
}

// manifestSkillVersion returns the manifest's expected skill_version for name, or
// "" when no manifest entry exists.
func manifestSkillVersion(manifest *skill.Manifest, name string) string {
	if manifest == nil {
		return ""
	}
	if ms, ok := manifest.Skills[name]; ok {
		return ms.SkillVersion
	}
	return ""
}

// lockPath is the display path for a vendored skill: its source repo (falling back
// to the raw URL).
func lockPath(entry skill.LockEntry) string {
	if entry.Source != "" {
		return entry.Source
	}
	return entry.URL
}

// computeStale derives the offline stale flag for name. It returns nil (unknown)
// when there is no manifest or no manifest entry — an honest "don't know" rather
// than a false "fresh". Otherwise it compares the on-disk rendered tree digest
// against the manifest's expected skill_version for every target that manages the
// skill: stale=true if any managed tree is absent or its digest differs.
func computeStale(name string, manifest *skill.Manifest, targets []targetRef) *bool {
	if manifest == nil {
		return nil
	}
	ms, ok := manifest.Skills[name]
	if !ok {
		return nil
	}
	expected := ms.SkillVersion
	hasTargetInfo := len(manifest.Targets) > 0

	stale := false
	checked := 0
	for _, t := range targets {
		want := expected
		if hasTargetInfo {
			mt, ok := manifest.Targets[t.Name]
			if !ok {
				continue
			}
			v, managed := mt.ManagedSkills[name]
			if !managed {
				continue
			}
			if v != "" {
				want = v
			}
		}
		digest, exists, err := treeDigest(filepath.Join(t.Dir, name))
		checked++
		if err != nil || !exists || digest != want {
			stale = true
		}
	}
	if checked == 0 {
		// The manifest knows the skill but no configured target manages or holds
		// it — unknown rather than a misleading false.
		return nil
	}
	return &stale
}

// vendoredDescription reads a vendored skill's description from the first rendered
// target that holds it. Returns "" when the skill is not rendered anywhere.
func vendoredDescription(name string, targets []targetRef) string {
	for _, t := range targets {
		data, err := readSkillMD(filepath.Join(t.Dir, name))
		if err != nil {
			continue
		}
		if _, desc, ok := frontmatterMeta(data); ok {
			return desc
		}
	}
	return ""
}

// readSkillMD reads the SKILL.md file directly under dir.
func readSkillMD(dir string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, render.SkillMDPath))
}

// frontmatterMeta extracts the name and description from a SKILL.md's YAML
// frontmatter. ok is false when the content has no parseable frontmatter block.
func frontmatterMeta(content []byte) (name, description string, ok bool) {
	s := strings.ReplaceAll(string(content), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return "", "", false
	}
	rest := s[len("---\n"):]
	block, _, found := strings.Cut(rest, "\n---")
	if !found {
		return "", "", false
	}
	var front struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(block), &front); err != nil {
		return "", "", false
	}
	return front.Name, front.Description, true
}
