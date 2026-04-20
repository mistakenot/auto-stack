package template

import (
	"os"
	"path/filepath"
	"testing"
)

func setupFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestScanPortNamesDefault(t *testing.T) {
	dir := setupFiles(t, map[string]string{
		"app.js":    `const port = {{.Port.web}}; const db = {{.Port.db}};`,
		"other.txt": `no ports here`,
	})

	paths, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	names, err := ScanPortNames(dir, paths, [2]string{"{{", "}}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "db" || names[1] != "web" {
		t.Errorf("got %v, want [db, web]", names)
	}
}

func TestScanPortNamesCustom(t *testing.T) {
	dir := setupFiles(t, map[string]string{
		"app.js": `const port = [[.Port.web]]; const fake = {{.Port.fake}};`,
	})

	paths, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	names, err := ScanPortNames(dir, paths, [2]string{"[[", "]]"})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "web" {
		t.Errorf("got %v, want [web]", names)
	}
}

func TestRenderBasic(t *testing.T) {
	dir := setupFiles(t, map[string]string{
		"config.js": `module.exports = { port: {{.Port.web}}, name: "{{.Name}}" };`,
	})

	paths, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	data := Data{
		Port: map[string]int{"web": 3003},
		Name: "my-project",
	}

	results, err := Render(dir, paths, &data, [2]string{"{{", "}}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	want := `module.exports = { port: 3003, name: "my-project" };`
	if string(results[0].Content) != want {
		t.Errorf("got %q, want %q", string(results[0].Content), want)
	}
}

func TestRenderCustomDelimiters(t *testing.T) {
	dir := setupFiles(t, map[string]string{
		"app.js": `const jsTemplate = "Hello {{name}}"; const port = [[.Port.web]];`,
	})

	paths, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	data := Data{
		Port: map[string]int{"web": 3003},
	}

	results, err := Render(dir, paths, &data, [2]string{"[[", "]]"})
	if err != nil {
		t.Fatal(err)
	}

	want := `const jsTemplate = "Hello {{name}}"; const port = 3003;`
	if string(results[0].Content) != want {
		t.Errorf("got %q, want %q", string(results[0].Content), want)
	}
}

func TestRenderGoldenFile(t *testing.T) {
	tmpl := `module.exports = {
  apps: [{
    name: "{{.Name}}-web",
    script: "npm",
    args: "run dev -- --port {{.Port.web}}",
    env: {
      NODE_ENV: "development",
      API_PORT: "{{.Port.api}}",
      DB_PORT: "{{.Port.db}}"
    }
  }]
};
`
	dir := setupFiles(t, map[string]string{
		"ecosystem.config.js": tmpl,
	})

	paths, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	data := Data{
		Port: map[string]int{"api": 3000, "db": 3001, "web": 3003},
		Name: "myapp",
	}

	results, err := Render(dir, paths, &data, [2]string{"{{", "}}"})
	if err != nil {
		t.Fatal(err)
	}

	want := `module.exports = {
  apps: [{
    name: "myapp-web",
    script: "npm",
    args: "run dev -- --port 3003",
    env: {
      NODE_ENV: "development",
      API_PORT: "3000",
      DB_PORT: "3001"
    }
  }]
};
`
	if string(results[0].Content) != want {
		t.Errorf("golden mismatch:\ngot:\n%s\nwant:\n%s", string(results[0].Content), want)
	}
}

func TestDiscoverEmpty(t *testing.T) {
	dir := t.TempDir()
	_, err := Discover(dir)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}
