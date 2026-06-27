package sync

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-skill/internal/render"
	"github.com/mistakenot/auto-skill/internal/skill"
	"gopkg.in/yaml.v3"
)

// buildManifest populates the typed manifest from the staged skills and the
// per-target install decisions, then revalidates it. Each skill records its
// template_hash, resolved replacement literals, file-ref content hashes +
// matched headings, skill_version, and the engine's render_version; each target
// records its managed-skills map (name → expected skill_version). Returns the
// validation errors (empty on success) so the caller can refuse to write an
// invalid manifest.
func buildManifest(staged []*StagedSkill, targets []Target) (*skill.Manifest, []config.ValidationError) {
	rv := strconv.Itoa(render.RenderVersion)
	m := &skill.Manifest{
		Version:       1,
		RenderVersion: rv,
		Skills:        map[string]skill.ManifestSkill{},
		Targets:       map[string]skill.ManifestTarget{},
	}

	for _, st := range staged {
		refs := make([]skill.ManifestFileRef, 0, len(st.FileRefs))
		for _, fr := range st.FileRefs {
			refs = append(refs, skill.ManifestFileRef{
				Path:           fr.Path,
				ContentHash:    fr.ContentHash,
				MatchedHeading: fr.MatchedHeading,
			})
		}
		repl := st.Replacements
		if repl == nil {
			repl = map[string]string{}
		}
		m.Skills[st.Name] = skill.ManifestSkill{
			TemplateHash:  st.TemplateHash,
			Replacements:  repl,
			FileRefs:      refs,
			SkillVersion:  st.SkillVersion,
			RenderVersion: rv,
		}
	}

	// managed_skills per target: every staged skill is managed in every target
	// (sync writes the full union into each target). The expected value is the
	// skill_version; skip-vs-write does not change what `sync` *manages*.
	want := map[string]string{}
	for _, st := range staged {
		want[st.Name] = st.SkillVersion
	}
	for _, t := range targets {
		managed := map[string]string{}
		for _, st := range staged {
			managed[st.Name] = want[st.Name]
		}
		m.Targets[t.Name] = skill.ManifestTarget{ManagedSkills: managed}
	}

	return m, skill.ValidateManifest(m)
}

// joinValidation renders validation errors into a single remediation string.
func joinValidation(errs []config.ValidationError) string {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Path + ": " + e.Message
	}
	return strings.Join(msgs, "; ")
}

// hasMappingKey reports whether a YAML mapping node contains the given key.
func hasMappingKey(node *yaml.Node, key string) bool {
	if node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

// walkFiles invokes fn for every regular (non-symlink) file under root, passing
// the slash-separated path relative to root, the file bytes, and its FileInfo.
func walkFiles(root string, fn func(rel string, data []byte, info os.FileInfo)) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		fn(filepath.ToSlash(rel), data, info)
		return nil
	})
}
