package skill

import (
	"testing"
)

// TestParseLockRejectsInvalidNames covers M8 (empty/path-separator names crash
// sync) and H4 (case-variant duplicates): ParseLock is the single chokepoint, so
// a structurally-invalid key never reaches the engine.
func TestParseLockRejectsInvalidNames(t *testing.T) {
	cases := map[string]string{
		"empty name":             `{"version":1,"skills":{"":{"source":"github.com/o/r","url":"https://github.com/o/r","commit":"abc1234","state":"resolved"}}}`,
		"path separator":         `{"version":1,"skills":{"a/b":{"source":"github.com/o/r","url":"https://github.com/o/r","commit":"abc1234","state":"resolved"}}}`,
		"mixed case":             `{"version":1,"skills":{"Deploy-Checklist":{"source":"github.com/o/r","url":"https://github.com/o/r","commit":"abc1234","state":"resolved"}}}`,
		"case-variant duplicate": `{"version":1,"skills":{"deploy":{"source":"github.com/o/r","url":"https://github.com/o/r","commit":"abc1234","state":"resolved"},"Deploy":{"source":"github.com/o/r","url":"https://github.com/o/r","commit":"abc1234","state":"resolved"}}}`,
	}
	for name, js := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseLock([]byte(js)); err == nil {
				t.Fatalf("expected ParseLock to reject %s", name)
			}
		})
	}
}

func TestParseLockAcceptsValidNames(t *testing.T) {
	js := `{"version":1,"skills":{"deploy-checklist":{"source":"github.com/o/r","url":"https://github.com/o/r","commit":"abc1234","state":"resolved"}}}`
	if _, err := ParseLock([]byte(js)); err != nil {
		t.Fatalf("ParseLock rejected a valid lock: %v", err)
	}
}

// TestValidateTargetNameTraversal covers H3: a target with ".." or an absolute
// path is rejected (would write outside the project root).
func TestValidateTargetNameTraversal(t *testing.T) {
	bad := []string{"../../../tmp/escape", "..", "a/../../b", "/etc/skills"}
	for _, tgt := range bad {
		if ve := ValidateTargetName(tgt); ve == nil {
			t.Errorf("ValidateTargetName(%q) = nil, want rejection", tgt)
		}
	}
	good := []string{"claude", "agents", ".codex", "sub/dir", ""}
	for _, tgt := range good {
		if ve := ValidateTargetName(tgt); ve != nil {
			t.Errorf("ValidateTargetName(%q) = %v, want nil", tgt, ve)
		}
	}
}

func TestValidateSkillsYAMLRejectsTraversalTarget(t *testing.T) {
	cfg := &SkillsYAML{Targets: []string{"../../../tmp/escape"}}
	errs := ValidateSkillsYAML(cfg)
	found := false
	for _, e := range errs {
		if e.Code == CodeInvalidTarget {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an invalid_target error, got: %v", errs)
	}
}
