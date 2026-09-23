// Command golist dumps a Go module's import graph as JSON for the eval task
// generator. Run it from inside a module directory with the Go toolchain on
// PATH. It shells out to `go list` for package-level facts (including the
// transitive non-test Deps) and parses every selected file for its own imports,
// because `go list` only reports imports per package, not per file.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
)

type listed struct {
	ImportPath   string
	Dir          string
	Name         string
	Module       *struct{ Path, Dir string }
	GoFiles      []string
	CgoFiles     []string
	TestGoFiles  []string
	XTestGoFiles []string
	Imports      []string
	Deps         []string
	TestImports  []string
	XTestImports []string
	Error        *struct{ Err string }
}

type file struct {
	Path    string   `json:"path"`
	Kind    string   `json:"kind"` // go | test | xtest
	Imports []string `json:"imports"`
}

type pkg struct {
	ImportPath   string   `json:"import_path"`
	Dir          string   `json:"dir"`
	Name         string   `json:"name"`
	Imports      []string `json:"imports"`
	Deps         []string `json:"deps"`
	TestImports  []string `json:"test_imports"`
	XTestImports []string `json:"xtest_imports"`
	Files        []file   `json:"files"`
	Error        string   `json:"error,omitempty"`
}

type graph struct {
	ModulePath string `json:"module_path"`
	Packages   []pkg  `json:"packages"`
}

func main() {
	root, err := os.Getwd()
	must(err)

	cmd := exec.Command("go", "list", "-e", "-json", "./...")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "go list failed: %v\n%s", err, stderr.String())
		os.Exit(1)
	}

	g := graph{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var l listed
		if err := dec.Decode(&l); err == io.EOF {
			break
		} else {
			must(err)
		}
		if l.Module != nil && g.ModulePath == "" {
			g.ModulePath = l.Module.Path
		}
		rel, err := filepath.Rel(root, l.Dir)
		must(err)
		p := pkg{
			ImportPath: l.ImportPath, Dir: filepath.ToSlash(rel), Name: l.Name,
			Imports: nonNil(l.Imports), Deps: nonNil(l.Deps),
			TestImports: nonNil(l.TestImports), XTestImports: nonNil(l.XTestImports),
		}
		if l.Error != nil {
			p.Error = l.Error.Err
		}
		add := func(names []string, kind string) {
			for _, n := range names {
				p.Files = append(p.Files, parseFile(root, filepath.Join(l.Dir, n), kind))
			}
		}
		add(l.GoFiles, "go")
		add(l.CgoFiles, "go")
		add(l.TestGoFiles, "test")
		add(l.XTestGoFiles, "xtest")
		g.Packages = append(g.Packages, p)
	}
	sort.Slice(g.Packages, func(i, j int) bool { return g.Packages[i].ImportPath < g.Packages[j].ImportPath })

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	must(enc.Encode(g))
}

func parseFile(root, path, kind string) file {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	must(err)
	rel, err := filepath.Rel(root, path)
	must(err)
	imports := []string{}
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		must(err)
		imports = append(imports, p)
	}
	return file{Path: filepath.ToSlash(rel), Kind: kind, Imports: imports}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
