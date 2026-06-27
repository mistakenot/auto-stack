package skill

import (
	"testing"

	"github.com/mistakenot/auto-shared/config"
)

const validSkillsYAML = `
auto_update: true
commit_targets: true
targets:
  - .claude
  - .codex
trusted_hosts:
  - github.com
shared:
  version: latest
  replacements:
    greeting: "literal value"
    intro:
      file: snippets/header.md
      section: Intro
      include_heading: true
skills:
  remote-skill:
    version: tag:v1.0
    replacements:
      farewell: "another literal"
`

func TestParseSkillsYAMLStrict(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		cfg, err := ParseSkillsYAML([]byte(validSkillsYAML))
		if err != nil {
			t.Fatalf("ParseSkillsYAML returned error: %v", err)
		}
		if !cfg.AutoUpdate {
			t.Error("auto_update = false, want true")
		}
		if len(cfg.Targets) != 2 {
			t.Errorf("len(targets) = %d, want 2", len(cfg.Targets))
		}
		if _, ok := cfg.Skills["remote-skill"]; !ok {
			t.Error("skills missing remote-skill")
		}
	})

	t.Run("unknown key rejected", func(t *testing.T) {
		_, err := ParseSkillsYAML([]byte("auto_update: true\nbogus_field: 1\n"))
		if err == nil {
			t.Fatal("ParseSkillsYAML accepted unknown key, want error")
		}
	})
}

func TestValidateSkillsYAML(t *testing.T) {
	codes := func(errs []config.ValidationError) map[string]bool {
		m := make(map[string]bool)
		for _, e := range errs {
			m[e.Code] = true
		}
		return m
	}

	t.Run("valid baseline has no errors", func(t *testing.T) {
		cfg, err := ParseSkillsYAML([]byte(validSkillsYAML))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if errs := ValidateSkillsYAML(cfg); len(errs) != 0 {
			t.Fatalf("ValidateSkillsYAML = %+v, want none", errs)
		}
	})

	t.Run("bad skill name", func(t *testing.T) {
		cfg := &SkillsYAML{Skills: map[string]SkillConfig{"Bad_Name": {}}}
		if !codes(ValidateSkillsYAML(cfg))[CodeInvalidSkillName] {
			t.Error("expected invalid_skill_name")
		}
	})

	t.Run("bad version spec", func(t *testing.T) {
		cfg := &SkillsYAML{Skills: map[string]SkillConfig{"ok-name": {Version: "foo:bar"}}}
		if !codes(ValidateSkillsYAML(cfg))[CodeInvalidVersionSpec] {
			t.Error("expected invalid_version_spec")
		}
	})

	t.Run("bad shared version spec", func(t *testing.T) {
		cfg := &SkillsYAML{Shared: SharedConfig{Version: "commit:zz"}}
		if !codes(ValidateSkillsYAML(cfg))[CodeInvalidVersionSpec] {
			t.Error("expected invalid_version_spec on shared.version")
		}
	})

	t.Run("non-string literal replacement", func(t *testing.T) {
		cfg, err := ParseSkillsYAML([]byte("shared:\n  replacements:\n    count: 123\n"))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if !codes(ValidateSkillsYAML(cfg))[CodeInvalidLiteral] {
			t.Error("expected invalid_literal for numeric replacement")
		}
	})

	t.Run("file-ref missing file key", func(t *testing.T) {
		cfg, err := ParseSkillsYAML([]byte("shared:\n  replacements:\n    intro:\n      section: Intro\n"))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if !codes(ValidateSkillsYAML(cfg))[CodeInvalidFileRef] {
			t.Error("expected invalid_file_ref for mapping without file key")
		}
	})

	t.Run("file-ref unknown key", func(t *testing.T) {
		cfg, err := ParseSkillsYAML([]byte("shared:\n  replacements:\n    intro:\n      file: a.md\n      bogus: true\n"))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if !codes(ValidateSkillsYAML(cfg))[CodeUnknownField] {
			t.Error("expected unknown_field for unknown file-ref key")
		}
	})

	t.Run("invalid var name", func(t *testing.T) {
		cfg, err := ParseSkillsYAML([]byte("shared:\n  replacements:\n    \"bad-name\": value\n"))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if !codes(ValidateSkillsYAML(cfg))[CodeInvalidVarName] {
			t.Error("expected invalid_var_name for non-identifier replacement key")
		}
	})

	t.Run("valid named replacements bind end-to-end", func(t *testing.T) {
		cfg, err := ParseSkillsYAML([]byte("shared:\n  replacements:\n    greeting: hello\n    intro:\n      file: a.md\n"))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if errs := ValidateSkillsYAML(cfg); len(errs) != 0 {
			t.Fatalf("ValidateSkillsYAML = %+v, want none", errs)
		}
		if _, ok := cfg.Shared.Replacements["greeting"]; !ok {
			t.Error("greeting var not bound in parsed map")
		}
		if _, ok := cfg.Shared.Replacements["intro"]; !ok {
			t.Error("intro var not bound in parsed map")
		}
	})

	t.Run("duplicate target", func(t *testing.T) {
		cfg := &SkillsYAML{Targets: []string{".claude", ".claude"}}
		if !codes(ValidateSkillsYAML(cfg))[CodeDuplicateValue] {
			t.Error("expected duplicate_value for repeated target")
		}
	})

	t.Run("duplicate trusted host", func(t *testing.T) {
		cfg := &SkillsYAML{TrustedHosts: []string{"github.com", "github.com"}}
		if !codes(ValidateSkillsYAML(cfg))[CodeDuplicateValue] {
			t.Error("expected duplicate_value for repeated trusted_host")
		}
	})
}
