package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func manifest(name string) string {
	return `{"$schema":"` + SchemaURL + `","name":"` + name + `","description":"d"}`
}

func TestValidateName(t *testing.T) {
	ok := []string{"a", "planning-workflow", "a.b", "a1-b2.c3", strings.Repeat("a", 64)}
	bad := []string{"", "-a", "a-", ".a", "a.", "a--b", "a..b", "A", "a_b", "a/b", strings.Repeat("a", 65)}
	for _, n := range ok {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", n, err)
		}
	}
	for _, n := range bad {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", n)
		}
	}
}

func TestParseManifest(t *testing.T) {
	m, unknown, err := ParseManifest([]byte(manifest("demo")))
	if err != nil {
		t.Fatalf("valid manifest: %v", err)
	}
	if m.Name != "demo" || m.Description != "d" || len(unknown) != 0 {
		t.Fatalf("unexpected manifest %+v unknown=%v", m, unknown)
	}

	// Unknown top-level fields are reported, not fatal.
	_, unknown, err = ParseManifest([]byte(`{"$schema":"` + SchemaURL + `","name":"demo","zzz":1,"aaa":2}`))
	if err != nil {
		t.Fatalf("unknown fields must not be fatal: %v", err)
	}
	if strings.Join(unknown, ",") != "aaa,zzz" {
		t.Fatalf("unknown = %v, want [aaa zzz]", unknown)
	}

	var me *ManifestError
	cases := map[string]string{
		"not json":     `{`,
		"not object":   `[]`,
		"wrong schema": `{"$schema":"https://example.com/x.json","name":"demo"}`,
		"no schema":    `{"name":"demo"}`,
		"bad name":     `{"$schema":"` + SchemaURL + `","name":"Bad_Name"}`,
		"no name":      `{"$schema":"` + SchemaURL + `"}`,
		"mistyped":     `{"$schema":"` + SchemaURL + `","name":"demo","keywords":"x"}`,
	}
	for label, data := range cases {
		_, _, err := ParseManifest([]byte(data))
		if err == nil {
			t.Errorf("%s: expected error", label)
			continue
		}
		if !errors.As(err, &me) {
			t.Errorf("%s: expected *ManifestError, got %T", label, err)
		}
	}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func skillMD(name string) string { return "---\nname: " + name + "\ndescription: x\n---\nbody\n" }

func TestDiscoverSkills(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"plugin.json":                      manifest("demo"),
		"skills/beta/SKILL.md":             skillMD("beta"),
		"skills/alpha/SKILL.md":            skillMD("alpha"),
		"skills/no-md/README.md":           "x",
		"skills/nested/deeper/SKILL.md":    skillMD("deeper"), // not an immediate child: ignored
		"skills/Bad_Name/SKILL.md":         skillMD("Bad_Name"),
		"skills/dup/SKILL.md":              skillMD("alpha"), // duplicate declared name
		"skills/nameless/SKILL.md":         "no frontmatter",
		"skills/loose-file.md":             "not a dir",
		"mcp.json":                         "{}",
		"com.example.client/whatever.json": "{}",
	})
	skills, skipped, err := DiscoverSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range skills {
		names = append(names, s.Name+"@"+s.Subpath)
	}
	if got := strings.Join(names, " "); got != "alpha@skills/alpha beta@skills/beta nameless@skills/nameless" {
		t.Fatalf("skills = %s", got)
	}
	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.Dir] = s.Reason
	}
	for _, dir := range []string{"no-md", "nested", "Bad_Name", "dup"} {
		if _, ok := reasons[dir]; !ok {
			t.Errorf("expected %s to be skipped; skipped=%v", dir, skipped)
		}
	}
	if len(skipped) != 4 {
		t.Errorf("skipped = %v, want exactly 4", skipped)
	}

	// Missing skills/ is not an error; a skills file (wrong kind) disables the component.
	if s, sk, err := DiscoverSkills(t.TempDir()); err != nil || len(s) != 0 || len(sk) != 0 {
		t.Fatalf("missing skills/: %v %v %v", s, sk, err)
	}
	wrong := t.TempDir()
	writeTree(t, wrong, map[string]string{"skills": "file"})
	if s, sk, err := DiscoverSkills(wrong); err != nil || len(s) != 0 || len(sk) != 1 {
		t.Fatalf("skills as file: %v %v %v", s, sk, err)
	}
}

func TestLocate(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"plugins/one/plugin.json":         manifest("one"),
		"plugins/one/skills/a/SKILL.md":   skillMD("a"),
		"plugins/two/plugin.json":         manifest("two"),
		"plugins/broken/plugin.json":      `{"name":"broken"}`,
		"plugins/twin-a/plugin.json":      manifest("twin"),
		"plugins/twin-b/plugin.json":      manifest("twin"),
		"plugins/no-manifest/skills/x.md": "x",
	})
	tree := DirTree{Root: root}

	loc, err := Locate(tree, "plugins/one")
	if err != nil || loc.Root != "plugins/one" || loc.Manifest.Name != "one" {
		t.Fatalf("by path: %+v %v", loc, err)
	}
	if loc, err := Locate(tree, "./plugins/two/"); err != nil || loc.Root != "plugins/two" {
		t.Fatalf("by ./path/: %+v %v", loc, err)
	}
	if loc, err := Locate(tree, "two"); err != nil || loc.Root != "plugins/two" {
		t.Fatalf("by name: %+v %v", loc, err)
	}

	var me *ManifestError
	check := func(arg, code string) {
		t.Helper()
		_, err := Locate(tree, arg)
		if !errors.As(err, &me) || me.Code != code {
			t.Errorf("Locate(%q) = %v, want code %s", arg, err, code)
		}
	}
	check("plugins/no-manifest", CodeManifestMissing)
	check("plugins/missing", CodeManifestMissing)
	check("../escape", CodeNotFound)
	check("nope", CodeNotFound)
	check("twin", CodeAmbiguous)
	check("Bad_Name", CodeInvalidName)
	if _, err := Locate(tree, "plugins/broken"); !errors.As(err, &me) || me.Code != CodeManifestInvalid {
		t.Errorf("broken manifest: %v", err)
	}
	// The not-found hint lists the manifests the tree holds.
	_, err = Locate(tree, "nope")
	if !strings.Contains(err.Error(), "plugins/one") || !strings.Contains(err.Error(), "plugins/two") {
		t.Errorf("not-found hint should list available plugins: %v", err)
	}

	// Root-level plugin: "." resolves to root "".
	single := t.TempDir()
	writeTree(t, single, map[string]string{"plugin.json": manifest("solo")})
	if loc, err := Locate(DirTree{Root: single}, "."); err != nil || loc.Root != "" || loc.Manifest.Name != "solo" {
		t.Fatalf("root plugin: %+v %v", loc, err)
	}
}

func TestDiscoverSkillsTree(t *testing.T) {
	files := map[string]string{
		"plugins/p/plugin.json":              manifest("p"),
		"plugins/p/skills/b/SKILL.md":        skillMD("b"),
		"plugins/p/skills/a/SKILL.md":        skillMD("a"),
		"plugins/p/skills/a/references/x.md": "side file",
		"plugins/p/skills/deep/x/SKILL.md":   skillMD("deep"),
		"plugins/p/skills/BAD/SKILL.md":      skillMD("BAD"),
		"plugins/other/skills/z/SKILL.md":    skillMD("z"),
	}
	tree := FuncTree{
		FilesFn: func() ([]string, error) {
			var out []string
			for f := range files {
				out = append(out, f)
			}
			return out, nil
		},
		ReadFileFn: func(rel string) ([]byte, error) {
			c, ok := files[rel]
			if !ok {
				return nil, errors.New("missing")
			}
			return []byte(c), nil
		},
	}
	m, skills, skipped, err := DiscoverSkillsTree(tree, "plugins/p")
	if err != nil || m.Name != "p" {
		t.Fatalf("manifest: %+v %v", m, err)
	}
	if len(skills) != 2 || skills[0].Name != "a" || skills[1].Name != "b" || skills[1].Subpath != "skills/b" {
		t.Fatalf("skills = %+v", skills)
	}
	if len(skipped) != 1 || skipped[0].Dir != "BAD" {
		t.Fatalf("skipped = %+v", skipped)
	}
	if _, _, _, err := DiscoverSkillsTree(tree, "plugins/other"); err == nil {
		t.Fatal("missing manifest must error")
	}
}
