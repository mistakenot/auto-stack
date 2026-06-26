package skill

import (
	"testing"

	"github.com/mistakenot/auto-shared/config"
)

const validManifestJSON = `{
  "version": 1,
  "render_version": "1",
  "skills": {
    "remote-skill": {
      "template_hash": "abc123",
      "replacements": {"NAME": "value"},
      "file_refs": [
        {"path": "snippets/header.md", "content_hash": "deadbeef", "matched_heading": "Intro"}
      ],
      "skill_version": "cafe1234",
      "render_version": "1"
    }
  },
  "targets": {
    ".claude": {
      "managed_skills": {"remote-skill": "cafe1234"}
    }
  }
}`

func manifestCodes(errs []config.ValidationError) map[string]bool {
	m := make(map[string]bool)
	for _, e := range errs {
		m[e.Code] = true
	}
	return m
}

func TestParseManifestStrict(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		m, err := ParseManifest([]byte(validManifestJSON))
		if err != nil {
			t.Fatalf("ParseManifest: %v", err)
		}
		if m.Version != 1 {
			t.Errorf("version = %d, want 1", m.Version)
		}
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		data := `{"version":1,"bogus":true}`
		if _, err := ParseManifest([]byte(data)); err == nil {
			t.Fatal("ParseManifest accepted unknown field, want error")
		}
	})
}

func TestValidateManifest(t *testing.T) {
	t.Run("valid baseline has no errors", func(t *testing.T) {
		m, err := ParseManifest([]byte(validManifestJSON))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if errs := ValidateManifest(m); len(errs) != 0 {
			t.Fatalf("ValidateManifest = %+v, want none", errs)
		}
	})

	t.Run("invalid hash", func(t *testing.T) {
		m := &Manifest{Skills: map[string]ManifestSkill{
			"remote-skill": {TemplateHash: "NOTHEX", SkillVersion: "cafe1234"},
		}}
		if !manifestCodes(ValidateManifest(m))[CodeInvalidHash] {
			t.Error("expected invalid_hash for non-hex template_hash")
		}
	})

	t.Run("empty hash", func(t *testing.T) {
		m := &Manifest{Skills: map[string]ManifestSkill{
			"remote-skill": {TemplateHash: "", SkillVersion: "cafe1234"},
		}}
		if !manifestCodes(ValidateManifest(m))[CodeInvalidHash] {
			t.Error("expected invalid_hash for empty template_hash")
		}
	})

	t.Run("invalid file-ref content hash", func(t *testing.T) {
		m := &Manifest{Skills: map[string]ManifestSkill{
			"remote-skill": {
				TemplateHash: "abc123",
				SkillVersion: "cafe1234",
				FileRefs:     []ManifestFileRef{{Path: "p", ContentHash: "ZZZ"}},
			},
		}}
		if !manifestCodes(ValidateManifest(m))[CodeInvalidHash] {
			t.Error("expected invalid_hash for non-hex content_hash")
		}
	})

	t.Run("target references unknown skill", func(t *testing.T) {
		m := &Manifest{
			Skills: map[string]ManifestSkill{
				"remote-skill": {TemplateHash: "abc123", SkillVersion: "cafe1234"},
			},
			Targets: map[string]ManifestTarget{
				".claude": {ManagedSkills: map[string]string{"ghost-skill": "cafe1234"}},
			},
		}
		if !manifestCodes(ValidateManifest(m))[CodeUnknownSkillRef] {
			t.Error("expected unknown_skill_ref for target referencing missing skill")
		}
	})
}
