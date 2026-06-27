package skill

import (
	"os"
	"path/filepath"
	"testing"
)

// staleRefEnv builds an isolated env with a skills.yaml declaring the given
// skill names (version "latest"). Authored dirs and a lock are added by helpers.
func staleRefEnv(t *testing.T, declared ...string) Env {
	t.Helper()
	env := Env{Root: t.TempDir(), RootOverride: true}
	if err := os.MkdirAll(env.SkillsConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	var b []byte
	b = append(b, "skills:\n"...)
	for _, name := range declared {
		b = append(b, ("  " + name + ":\n    version: \"latest\"\n")...)
	}
	if err := os.WriteFile(env.SkillsYAMLPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return env
}

func writeAuthoredDir(t *testing.T, env Env, name string) {
	t.Helper()
	dir := filepath.Join(env.SkillsDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: Use when testing.\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLockWith(t *testing.T, env Env, names ...string) {
	t.Helper()
	var b []byte
	b = append(b, "{\n  \"version\": 1,\n  \"skills\": {\n"...)
	for i, name := range names {
		comma := ","
		if i == len(names)-1 {
			comma = ""
		}
		b = append(b, ("    \"" + name + "\": {\"source\": \"git\", \"url\": \"https://example.com/r\", \"version_spec\": \"latest\", \"ref\": \"\", \"commit\": \"deadbeef\", \"subpath\": \"skills/" + name + "\", \"private\": false, \"local\": false, \"state\": \"resolved\"}" + comma + "\n")...)
	}
	b = append(b, "  }\n}\n"...)
	if err := os.WriteFile(env.LockPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasStaleRef(diags []Diagnostic, name string) bool {
	for _, d := range diags {
		if d.Code == "stale_skill_ref" && d.Value == name {
			return true
		}
	}
	return false
}

// TestCheckStaleSkillRefsFlagsGhost: a skills.yaml entry with no authored dir and
// no lock entry is flagged.
func TestCheckStaleSkillRefsFlagsGhost(t *testing.T) {
	env := staleRefEnv(t, "ghost")

	diags, err := CheckStaleSkillRefs(env)
	if err != nil {
		t.Fatalf("CheckStaleSkillRefs: %v", err)
	}
	if !hasStaleRef(diags, "ghost") {
		t.Fatalf("expected stale_skill_ref for ghost, got %+v", diags)
	}
	for _, d := range diags {
		if d.Code == "stale_skill_ref" && d.Value == "ghost" {
			if d.Severity != SeverityWarning {
				t.Errorf("severity = %q, want warning", d.Severity)
			}
			if d.Field != "ghost" {
				t.Errorf("field = %q, want ghost", d.Field)
			}
		}
	}

	// Lint surfaces the same diagnostic.
	lintDiags, err := Lint(env, "")
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if !hasStaleRef(lintDiags, "ghost") {
		t.Fatalf("Lint did not surface stale_skill_ref: %+v", lintDiags)
	}
}

// TestCheckStaleSkillRefsAuthoredOK: an authored skill is not stale.
func TestCheckStaleSkillRefsAuthoredOK(t *testing.T) {
	env := staleRefEnv(t, "alpha")
	writeAuthoredDir(t, env, "alpha")

	diags, err := CheckStaleSkillRefs(env)
	if err != nil {
		t.Fatalf("CheckStaleSkillRefs: %v", err)
	}
	if hasStaleRef(diags, "alpha") {
		t.Fatalf("authored skill alpha should not be flagged: %+v", diags)
	}
}

// TestCheckStaleSkillRefsLockedOK: a lock-only skill is not stale.
func TestCheckStaleSkillRefsLockedOK(t *testing.T) {
	env := staleRefEnv(t, "beta")
	writeLockWith(t, env, "beta")

	diags, err := CheckStaleSkillRefs(env)
	if err != nil {
		t.Fatalf("CheckStaleSkillRefs: %v", err)
	}
	if hasStaleRef(diags, "beta") {
		t.Fatalf("locked skill beta should not be flagged: %+v", diags)
	}
}

// TestCheckStaleSkillRefsMissingYAML: no skills.yaml yields no diagnostics.
func TestCheckStaleSkillRefsMissingYAML(t *testing.T) {
	env := Env{Root: t.TempDir(), RootOverride: true}
	diags, err := CheckStaleSkillRefs(env)
	if err != nil {
		t.Fatalf("CheckStaleSkillRefs: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %+v", diags)
	}
}
