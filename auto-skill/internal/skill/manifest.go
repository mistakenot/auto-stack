package skill

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/mistakenot/auto-shared/config"
)

// Manifest is the typed representation of .auto/skills/manifest.json — derived
// render state. T1 defines and validates the schema; hashing/rendering is later.
type Manifest struct {
	Version       int                       `json:"version"`
	RenderVersion string                    `json:"render_version"`
	Skills        map[string]ManifestSkill  `json:"skills"`
	Targets       map[string]ManifestTarget `json:"targets"`
}

// ManifestSkill records the rendered state of a single skill.
type ManifestSkill struct {
	TemplateHash  string            `json:"template_hash"`
	Replacements  map[string]string `json:"replacements"`
	FileRefs      []ManifestFileRef `json:"file_refs"`
	SkillVersion  string            `json:"skill_version"`
	RenderVersion string            `json:"render_version"`
}

// ManifestFileRef records the resolved content of a file-ref replacement.
type ManifestFileRef struct {
	Path           string `json:"path"`
	ContentHash    string `json:"content_hash"`
	MatchedHeading string `json:"matched_heading"`
}

// ManifestTarget records, per target, the managed skills and the skill_version
// expected to be installed.
type ManifestTarget struct {
	ManagedSkills map[string]string `json:"managed_skills"`
}

// manifestHashRE matches a non-empty lowercase hex hash string.
var manifestHashRE = regexp.MustCompile(`^[0-9a-f]+$`)

// ParseManifest strictly decodes manifest.json, rejecting unknown keys.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ValidateManifest checks structural rules only — no hashing or rendering.
func ValidateManifest(m *Manifest) []config.ValidationError {
	var errs []config.ValidationError
	if m == nil {
		return errs
	}

	for name, ms := range m.Skills {
		path := "skills." + name
		errs = append(errs, checkHash(ms.TemplateHash, path+".template_hash", "template_hash")...)
		errs = append(errs, checkHash(ms.SkillVersion, path+".skill_version", "skill_version")...)
		for i, fr := range ms.FileRefs {
			frPath := fmt.Sprintf("%s.file_refs[%d].content_hash", path, i)
			errs = append(errs, checkHash(fr.ContentHash, frPath, "content_hash")...)
		}
	}

	for target, mt := range m.Targets {
		for skillName := range mt.ManagedSkills {
			if _, ok := m.Skills[skillName]; !ok {
				errs = append(errs, config.ValidationError{
					Code:    CodeUnknownSkillRef,
					Path:    fmt.Sprintf("targets.%s.managed_skills.%s", target, skillName),
					Field:   skillName,
					Message: fmt.Sprintf("target %q references skill %q which is not in the skills map; add the skill or remove the reference", target, skillName),
					Value:   skillName,
				})
			}
		}
	}

	return errs
}

// checkHash reports an invalid_hash error when a hash field is empty or not
// lowercase hex.
func checkHash(value, path, field string) []config.ValidationError {
	if manifestHashRE.MatchString(value) {
		return nil
	}
	return []config.ValidationError{{
		Code:    CodeInvalidHash,
		Path:    path,
		Field:   field,
		Message: field + " must be a non-empty lowercase hex hash; recompute the hash via auto skill sync",
		Value:   value,
	}}
}
