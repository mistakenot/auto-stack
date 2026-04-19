package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/frontmatter"
)

var e2eBinaryPath string
var moduleRoot string

func TestMain(m *testing.M) {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	moduleRoot = filepath.Clean(filepath.Join(cwd, ".."))

	binDir, err := os.MkdirTemp("", "autodoc-e2e-bin-*")
	if err != nil {
		panic(err)
	}
	e2eBinaryPath = filepath.Join(binDir, "autodoc")
	build := exec.Command("go", "build", "-o", e2eBinaryPath, "./cmd/autodoc")
	build.Dir = moduleRoot
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("build failed: " + err.Error())
	}

	code := m.Run()
	os.RemoveAll(binDir)
	os.Exit(code)
}

func copyFixtureTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copyFixtureTree: %v", err)
	}
}

func initGitRepo(t *testing.T, cwd string) {
	t.Helper()
	runRaw(t, cwd, "git", "init")
	runRaw(t, cwd, "git", "config", "user.email", "autodoc-e2e@example.com")
	runRaw(t, cwd, "git", "config", "user.name", "Autodoc E2E")
	runRaw(t, cwd, "git", "add", ".")
	runRaw(t, cwd, "git", "commit", "--allow-empty", "-m", "initial")
}

func runCLI(t *testing.T, cwd string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	cmd := exec.Command(e2eBinaryPath, args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exit = 0
	if err != nil {
		var exitErr *exec.ExitError
		if ok := asExitError(err, &exitErr); ok {
			exit = exitErr.ExitCode()
		} else {
			t.Fatalf("runCLI %v failed: %v", args, err)
		}
	}
	return outBuf.String(), errBuf.String(), exit
}

func asExitError(err error, target **exec.ExitError) bool {
	if err == nil {
		return false
	}
	if !errors.As(err, target) {
		return false
	}
	return true
}

func runRaw(t *testing.T, cwd string, command string, args ...string) {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", command, args, err, string(out))
	}
}

func extractCurrentHashes(t *testing.T, fixOutput string) (docHash, scopeHash string) {
	t.Helper()
	docRe := regexp.MustCompile(`current doc hash:\s+([0-9a-f]{8})`)
	scopeRe := regexp.MustCompile(`current scope hash:\s+([0-9a-f]{8})`)

	docMatch := docRe.FindStringSubmatch(fixOutput)
	scopeMatch := scopeRe.FindStringSubmatch(fixOutput)
	if len(docMatch) != 2 || len(scopeMatch) != 2 {
		t.Fatalf("could not extract current hashes from output:\n%s", fixOutput)
	}
	return docMatch[1], scopeMatch[1]
}

func rewriteAutodocTag(t *testing.T, filePath, docID, docHash, scopeHash string) {
	t.Helper()
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	re := regexp.MustCompile(`\[autodoc\([0-9a-f]{8}@[0-9a-f]{8},\s*[0-9a-f]{8}\)\]`)
	loc := re.FindStringIndex(string(data))
	if loc == nil {
		t.Fatalf("autodoc tag not found in %s", filePath)
	}
	newTag := fmt.Sprintf("[autodoc(%s@%s, %s)]", docID, docHash, scopeHash)
	updated := string(data[:loc[0]]) + newTag + string(data[loc[1]:])

	if err := os.WriteFile(filePath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func rewriteMarkdownTag(t *testing.T, filePath, docID, docHash, scopeHash string) {
	t.Helper()
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	re := regexp.MustCompile(`<!--\s*\[autodoc\([0-9a-f]{8}@[0-9a-f]{8},\s*[0-9a-f]{8}\)\]\s*-->`)
	loc := re.FindStringIndex(string(data))
	if loc == nil {
		t.Fatalf("markdown autodoc tag not found in %s", filePath)
	}
	newTag := fmt.Sprintf("<!-- [autodoc(%s@%s, %s)] -->", docID, docHash, scopeHash)
	updated := string(data[:loc[0]]) + newTag + string(data[loc[1]:])

	if err := os.WriteFile(filePath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func rewriteText(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	updated := strings.Replace(string(data), old, new, 1)
	if updated == string(data) {
		t.Fatalf("text %q not found in %s", old, path)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func readDoc(t *testing.T, path string) frontmatter.Doc {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	return frontmatter.Parse(string(data))
}
