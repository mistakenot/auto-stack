package discovery

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeSkill creates a SKILL.md file at dir with the given frontmatter name and body.
func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: \"test\"\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeFile creates an arbitrary file in dir.
func writeFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSingleSkillAtRoot(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "root-skill", "Body content.")

	results, err := Discover(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	d := results[0]
	if d.Name != "root-skill" {
		t.Errorf("name = %q, want %q", d.Name, "root-skill")
	}
	if d.Container != "root" {
		t.Errorf("container = %q, want %q", d.Container, "root")
	}
	if d.Subpath != "." {
		t.Errorf("subpath = %q, want %q", d.Subpath, ".")
	}
	if !d.NameValid {
		t.Error("expected NameValid = true")
	}
	if d.Digest == "" {
		t.Error("expected non-empty digest")
	}
}

func TestSkillsDirectory(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "alpha"), "alpha", "Alpha body.")
	writeSkill(t, filepath.Join(root, "skills", "beta"), "beta", "Beta body.")

	results, err := Discover(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	names := map[string]bool{}
	for _, d := range results {
		names[d.Name] = true
		if d.Container != "skills" {
			t.Errorf("expected container=skills, got %q for %q", d.Container, d.Name)
		}
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected alpha and beta, got %v", names)
	}
}

func TestCatalogLayout(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "dev-tools", "linter"), "linter", "Linter body.")
	writeSkill(t, filepath.Join(root, "skills", "ops", "deploy"), "deploy", "Deploy body.")

	results, err := Discover(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	names := map[string]bool{}
	for _, d := range results {
		names[d.Name] = true
		if d.Container != "skills" {
			t.Errorf("expected container=skills, got %q for %q", d.Container, d.Name)
		}
	}
	if !names["linter"] || !names["deploy"] {
		t.Errorf("expected linter and deploy, got %v", names)
	}
}

func TestShadowing(t *testing.T) {
	root := t.TempDir()
	// Parent SKILL.md exists — should be found.
	writeSkill(t, filepath.Join(root, "skills", "a"), "a", "Parent.")
	// Nested SKILL.md under 'a' — should be shadowed.
	writeSkill(t, filepath.Join(root, "skills", "a", "nested"), "a-nested", "Nested.")

	results, err := Discover(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (shadowing), got %d", len(results))
	}
	if results[0].Name != "a" {
		t.Errorf("expected name=a, got %q", results[0].Name)
	}
}

func TestAgentDirs(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, ".claude", "skills", "x"), "x", "Agent skill.")

	results, err := Discover(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	d := results[0]
	if d.Name != "x" {
		t.Errorf("name = %q, want %q", d.Name, "x")
	}
	if d.Container != ".claude/skills" {
		t.Errorf("container = %q, want %q", d.Container, ".claude/skills")
	}
}

func TestExplicitPaths(t *testing.T) {
	root := t.TempDir()
	// Skill in a custom location, not in standard containers.
	writeSkill(t, filepath.Join(root, "custom", "dir", "my-skill"), "my-skill", "Custom.")
	// Skill in standard location — should NOT be found with explicit paths.
	writeSkill(t, filepath.Join(root, "skills", "standard"), "standard", "Standard.")

	results, err := Discover(root, Options{Paths: []string{"custom/dir"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "my-skill" {
		t.Errorf("name = %q, want %q", results[0].Name, "my-skill")
	}
	if results[0].Container != "explicit" {
		t.Errorf("container = %q, want %q", results[0].Container, "explicit")
	}
}

func TestDigestIdenticalDedupe(t *testing.T) {
	root := t.TempDir()
	body := "Identical body content for both."
	writeSkill(t, filepath.Join(root, "skills", "shared"), "shared", body)
	writeSkill(t, filepath.Join(root, ".claude", "skills", "shared"), "shared", body)

	results, err := Discover(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after dedupe, got %d", len(results))
	}
	// skills/ should win over .claude/skills/.
	if results[0].Container != "skills" {
		t.Errorf("expected container=skills (higher priority), got %q", results[0].Container)
	}
}

func TestDivergenceError(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "clash"), "clash", "Version A.")
	writeSkill(t, filepath.Join(root, ".claude", "skills", "clash"), "clash", "Version B, different content.")

	_, err := Discover(root, Options{})
	if err == nil {
		t.Fatal("expected DivergenceError, got nil")
	}

	var divErr *DivergenceError
	if !errors.As(err, &divErr) {
		t.Fatalf("expected *DivergenceError, got %T: %v", err, err)
	}
	if divErr.Name != "clash" {
		t.Errorf("divergence name = %q, want %q", divErr.Name, "clash")
	}
	if len(divErr.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(divErr.Entries))
	}
}

func TestInvalidName(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "bad"), "Bad_Name", "Bad name skill.")

	results, err := Discover(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].NameValid {
		t.Error("expected NameValid = false for Bad_Name")
	}
	if results[0].Name != "Bad_Name" {
		t.Errorf("name = %q, want %q", results[0].Name, "Bad_Name")
	}
}

func TestFullDepthFallback(t *testing.T) {
	root := t.TempDir()
	// Skill buried deep, not in any standard container.
	writeSkill(t, filepath.Join(root, "deep", "nested", "hidden"), "hidden", "Deep skill.")

	results, err := Discover(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from full-depth fallback, got %d", len(results))
	}
	if results[0].Name != "hidden" {
		t.Errorf("name = %q, want %q", results[0].Name, "hidden")
	}
	if results[0].Container != "full-depth" {
		t.Errorf("container = %q, want %q", results[0].Container, "full-depth")
	}
}

func TestFullDepthForced(t *testing.T) {
	root := t.TempDir()
	// Skill in standard container.
	writeSkill(t, filepath.Join(root, "skills", "visible"), "visible", "Standard.")
	// Skill buried deep.
	writeSkill(t, filepath.Join(root, "deep", "extra"), "extra", "Deep.")

	results, err := Discover(root, Options{FullDepth: true})
	if err != nil {
		t.Fatal(err)
	}
	// Full-depth scan should find both skills.
	if len(results) != 2 {
		t.Fatalf("expected 2 results with FullDepth, got %d", len(results))
	}

	names := map[string]bool{}
	for _, d := range results {
		names[d.Name] = true
		if d.Container != "full-depth" {
			t.Errorf("expected container=full-depth, got %q for %q", d.Container, d.Name)
		}
	}
	if !names["visible"] || !names["extra"] {
		t.Errorf("expected visible and extra, got %v", names)
	}
}

func TestNameFromDirectoryFallback(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "my-dir-name")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// SKILL.md with no name field in frontmatter.
	content := "---\ndescription: \"no name field here\"\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := Discover(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "my-dir-name" {
		t.Errorf("name = %q, want %q (directory fallback)", results[0].Name, "my-dir-name")
	}
}

func TestTreeDigestStability(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "stable")
	writeSkill(t, dir, "stable", "Content.")
	writeFile(t, dir, "extra.txt", "Extra file.")

	d1, err := treeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := treeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("digest not stable: %q != %q", d1, d2)
	}
	if len(d1) != 64 {
		t.Errorf("expected 64-char hex digest, got %d chars", len(d1))
	}
}

func TestTreeDigestChangesWithContent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "mutable")
	writeSkill(t, dir, "mutable", "Version 1.")

	d1, err := treeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Modify the file.
	writeSkill(t, dir, "mutable", "Version 2.")

	d2, err := treeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Error("digest should change when content changes")
	}
}

func TestParseFrontmatterName(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "standard frontmatter",
			content: "---\nname: my-skill\ndescription: \"test\"\n---\nBody.",
			want:    "my-skill",
		},
		{
			name:    "quoted name",
			content: "---\nname: \"quoted-skill\"\n---\nBody.",
			want:    "quoted-skill",
		},
		{
			name:    "single-quoted name",
			content: "---\nname: 'single-quoted'\n---\nBody.",
			want:    "single-quoted",
		},
		{
			name:    "no frontmatter",
			content: "Just body content.",
			want:    "",
		},
		{
			name:    "no name field",
			content: "---\ndescription: \"test\"\n---\nBody.",
			want:    "",
		},
		{
			name:    "empty name",
			content: "---\nname: \n---\nBody.",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFrontmatterName(tt.content)
			if got != tt.want {
				t.Errorf("parseFrontmatterName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmptyRoot(t *testing.T) {
	root := t.TempDir()

	results, err := Discover(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty root, got %d", len(results))
	}
}

func TestDivergenceErrorMessage(t *testing.T) {
	e := &DivergenceError{
		Name: "test-skill",
		Entries: []DivergenceEntry{
			{Subpath: "skills/test-skill", Container: "skills", Digest: "aaaa1111bbbb2222cccc3333"},
			{Subpath: ".claude/skills/test-skill", Container: ".claude/skills", Digest: "dddd4444eeee5555ffff6666"},
		},
	}
	msg := e.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	if !contains(msg, "test-skill") {
		t.Error("error message should contain skill name")
	}
	if !contains(msg, "--path") {
		t.Error("error message should mention --path")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
