package template

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
)

type Data struct {
	Port         map[string]int
	Name         string
	Branch       string
	BranchSlug   string
	Slot         int
	RepoRoot     string
	WorktreePath string
}

func Discover(filesDir string) ([]string, error) {
	var paths []string
	err := filepath.Walk(filesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(filesDir, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover templates: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no template files found in %s", filesDir)
	}
	return paths, nil
}

func ScanPortNames(filesDir string, paths []string, delimiters [2]string) ([]string, error) {
	open := regexp.QuoteMeta(delimiters[0])
	close := regexp.QuoteMeta(delimiters[1])
	pattern := open + `\s*\.Port\.(\w+)\s*` + close
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile port pattern: %w", err)
	}

	seen := map[string]bool{}
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join(filesDir, p))
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", p, err)
		}
		matches := re.FindAllSubmatch(data, -1)
		for _, m := range matches {
			seen[string(m[1])] = true
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

type RenderResult struct {
	RelPath string
	Content []byte
}

func Render(filesDir string, paths []string, data *Data, delimiters [2]string) ([]RenderResult, error) {
	var results []RenderResult
	for _, p := range paths {
		raw, err := os.ReadFile(filepath.Join(filesDir, p))
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", p, err)
		}

		tmpl := template.New(p).Delims(delimiters[0], delimiters[1])
		tmpl, err = tmpl.Parse(string(raw))
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", p, err)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("render template %s: %w", p, err)
		}

		results = append(results, RenderResult{
			RelPath: p,
			Content: buf.Bytes(),
		})
	}
	return results, nil
}

func FormatDryRun(results []RenderResult) string {
	var sb strings.Builder
	for i, r := range results {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "=== %s ===\n", r.RelPath)
		sb.Write(r.Content)
		if len(r.Content) > 0 && r.Content[len(r.Content)-1] != '\n' {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
