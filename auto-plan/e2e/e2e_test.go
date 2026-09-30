// Package e2e drives the real `auto` binary through scripted scenarios in a
// temp workspace, exactly as a user would, and asserts every plan's graph.json
// byte for byte at each checkpoint.
//
// A scenario is testdata/scenarios/<name>/commands.txt: one `auto …`
// invocation per line, with two kinds of annotation:
//
//	# checkpoint <n>                      compare docs/plans/*/graph.json with snapshots/checkpoint-<n>/
//	# expect exit=<n> stdout=<file>       applies to the next invocation (either part optional)
//
// Invocations without an expect annotation must exit 0. Run with -update to
// regenerate the snapshots and expected stdout files, then review the diff.
package e2e

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var update = flag.Bool("update", false, "regenerate scenario snapshots and expected stdout")

// seed and date pin ID generation and the plan's created date.
const (
	seed = "1"
	date = "2026-01-01"
)

func TestLifecycle(t *testing.T) { runScenario(t, "lifecycle") }

// TestDogfood rebuilds epic 005, its child task 062 and task 052 as plans 001–003
// from CLI calls alone, lints the family clean and golden-compares the displays.
func TestDogfood(t *testing.T) { runScenario(t, "dogfood") }

// step is one scripted invocation.
type step struct {
	line       int
	args       []string // after the leading "auto"
	exit       int
	stdoutFile string
	checkpoint string // non-empty: a checkpoint marker, not an invocation
}

func runScenario(t *testing.T, name string) {
	if testing.Short() {
		t.Skip("e2e builds the auto binary; skipped in -short mode")
	}
	dir := filepath.Join(moduleDir(), "e2e", "testdata", "scenarios", name)
	steps, err := parseCommands(filepath.Join(dir, "commands.txt"))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildAuto(t)
	ws, env := workspace(t)

	checkpoints := 0
	for _, s := range steps {
		if s.checkpoint != "" {
			checkpoints++
			compareCheckpoint(t, ws, filepath.Join(dir, "snapshots", "checkpoint-"+s.checkpoint))
			continue
		}
		cmd := exec.Command(bin, s.args...)
		cmd.Dir = ws
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		code := 0
		if err := cmd.Run(); err != nil {
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("line %d: auto %v: %v", s.line, s.args, err)
			}
			code = ee.ExitCode()
		}
		if code != s.exit {
			t.Fatalf("line %d: auto %s: exit %d, want %d\nstdout: %s\nstderr: %s",
				s.line, strings.Join(s.args, " "), code, s.exit, stdout.String(), stderr.String())
		}
		if s.stdoutFile != "" {
			compareFile(t, filepath.Join(dir, "snapshots", "stdout", s.stdoutFile), stdout.Bytes(),
				fmt.Sprintf("line %d stdout", s.line))
		}
	}
	if checkpoints == 0 {
		t.Fatal("scenario has no checkpoints")
	}
}

// parseCommands reads a commands.txt script.
func parseCommands(path string) ([]step, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var steps []step
	var pending *step
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		n := i + 1
		switch {
		case line == "":
		case strings.HasPrefix(line, "# checkpoint "):
			steps = append(steps, step{line: n, checkpoint: strings.TrimSpace(strings.TrimPrefix(line, "# checkpoint "))})
		case strings.HasPrefix(line, "# expect "):
			pending = &step{}
			for kv := range strings.FieldsSeq(strings.TrimPrefix(line, "# expect ")) {
				k, v, _ := strings.Cut(kv, "=")
				switch k {
				case "exit":
					code, err := strconv.Atoi(v)
					if err != nil {
						return nil, fmt.Errorf("%s:%d: bad exit %q", path, n, v)
					}
					pending.exit = code
				case "stdout":
					pending.stdoutFile = v
				default:
					return nil, fmt.Errorf("%s:%d: unknown expect key %q", path, n, k)
				}
			}
		case strings.HasPrefix(line, "#"):
		default:
			words, err := splitWords(line)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, n, err)
			}
			if len(words) == 0 || words[0] != "auto" {
				return nil, fmt.Errorf("%s:%d: invocations must start with auto", path, n)
			}
			s := step{}
			if pending != nil {
				s = *pending
				pending = nil
			}
			s.line, s.args = n, words[1:]
			steps = append(steps, s)
		}
	}
	if pending != nil {
		return nil, fmt.Errorf("%s: trailing # expect with no invocation", path)
	}
	return steps, nil
}

// splitWords splits a line into shell-like words: whitespace separates,
// single quotes are literal, double quotes allow \" and \\ escapes.
func splitWords(line string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'':
			end := strings.IndexByte(line[i+1:], '\'')
			if end < 0 {
				return nil, errors.New("unterminated single quote")
			}
			cur.WriteString(line[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case c == '"':
			i++
			for ; i < len(line) && line[i] != '"'; i++ {
				if line[i] == '\\' && i+1 < len(line) && (line[i+1] == '"' || line[i+1] == '\\') {
					i++
				}
				cur.WriteByte(line[i])
			}
			if i >= len(line) {
				return nil, errors.New("unterminated double quote")
			}
			inWord = true
		case c == ' ' || c == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

// compareCheckpoint asserts the workspace's plan graphs equal the snapshot
// directory: the same plan folders, each graph.json byte-identical.
func compareCheckpoint(t *testing.T, ws, snapDir string) {
	t.Helper()
	got := planGraphs(t, filepath.Join(ws, "docs", "plans"))
	if *update {
		if err := os.RemoveAll(snapDir); err != nil {
			t.Fatal(err)
		}
		for folder, data := range got {
			writeFile(t, filepath.Join(snapDir, folder, "graph.json"), data)
		}
		return
	}
	want := planGraphs(t, snapDir)
	for folder, data := range want {
		g, ok := got[folder]
		if !ok {
			t.Fatalf("%s: plan %s missing from workspace", filepath.Base(snapDir), folder)
		}
		if !bytes.Equal(g, data) {
			t.Fatalf("%s: %s/graph.json differs from snapshot (run with -update and review):\n--- got\n%s\n--- want\n%s",
				filepath.Base(snapDir), folder, g, data)
		}
	}
	for folder := range got {
		if _, ok := want[folder]; !ok {
			t.Fatalf("%s: unexpected plan %s in workspace", filepath.Base(snapDir), folder)
		}
	}
}

// planGraphs maps each plan folder under dir to its graph.json bytes.
func planGraphs(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return out
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "graph.json"))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = data
	}
	return out
}

func compareFile(t *testing.T, path string, got []byte, what string) {
	t.Helper()
	if *update {
		writeFile(t, path, got)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: read %s (run with -update to create): %v", what, path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from %s (run with -update and review):\n--- got\n%s\n--- want\n%s", what, filepath.Base(path), got, want)
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// workspace creates a temp git repo with one empty commit, and the pinned
// environment every invocation runs under: an isolated HOME, a fixed ID seed
// and date, and nothing else inherited except PATH.
func workspace(t *testing.T) (string, []string) {
	t.Helper()
	home := t.TempDir()
	ws := t.TempDir()
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"LANG=C.UTF-8",
		"TZ=UTC",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@example.invalid",
		"GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@example.invalid",
		"AUTO_PLAN_SEED=" + seed,
		"AUTO_PLAN_DATE=" + date,
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = ws
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return ws, env
}

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
	buildDir  string
)

// buildAuto builds the unified auto binary from ../auto-cli once per run.
func buildAuto(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "auto-plan-e2e-*")
		if buildErr != nil {
			return
		}
		builtBin = filepath.Join(buildDir, "auto")
		if runtime.GOOS == "windows" {
			builtBin += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", builtBin, "./cmd/auto")
		cmd.Dir = filepath.Join(moduleDir(), "..", "auto-cli")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("build auto: %w\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtBin
}

func TestMain(m *testing.M) {
	flag.Parse()
	code := m.Run()
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// moduleDir is the auto-plan module root.
func moduleDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(file))
}

func TestSplitWords(t *testing.T) {
	got, err := splitWords(`auto plan add 001 goal --title "a \"b\" c" --x 'd e' f`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"auto", "plan", "add", "001", "goal", "--title", `a "b" c`, "--x", "d e", "f"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
	if _, err := splitWords(`"open`); err == nil {
		t.Fatal("want unterminated-quote error")
	}
}
