// Package e2e drives the real `auto` binary through scripted scenarios in a
// temp workspace, exactly as a user would, and asserts every plan's graph.json
// byte for byte at each checkpoint.
//
// A scenario is testdata/scenarios/<name>/commands.txt: one `auto …`
// invocation per line, with three kinds of annotation:
//
//	# checkpoint <n>                      compare .auto/plan/plans/*/{graph.json,*.md} with snapshots/checkpoint-<n>/
//	# expect exit=<n> stdout=<file>       applies to the next invocation (either part optional)
//	# preview <folder> <file>             derive the AC-14 preview-data contract from a plan, compare with snapshots/<file>
//
// A checkpoint diffs every plan folder's graph.json AND every annex `*.md`
// sidecar beside it, byte for byte; a scenario with no annex files is
// unaffected. Invocations without an expect annotation must exit 0. Run with
// -update to regenerate the snapshots and expected stdout files, then review
// the diff.
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	planws "github.com/mistakenot/auto-plan/internal/workspace"
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
	preview    string // non-empty: "<folder> <file>", a preview-data marker, not an invocation
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
		if s.preview != "" {
			comparePreview(t, ws, dir, s.preview)
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
		case strings.HasPrefix(line, "# preview "):
			steps = append(steps, step{line: n, preview: strings.TrimSpace(strings.TrimPrefix(line, "# preview "))})
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

// compareCheckpoint asserts the workspace's plan folders equal the snapshot
// directory: the same plan folders, and inside each the graph.json and every
// annex `*.md` sidecar byte-identical. It also asserts the scaffold init/new
// create: .auto/plan/plans/AGENTS.md and a CLAUDE.md symlinked to it.
func compareCheckpoint(t *testing.T, ws, snapDir string) {
	t.Helper()
	plansDir := filepath.Join(ws, filepath.FromSlash(planws.PlansDir))
	if _, err := os.Stat(filepath.Join(plansDir, "AGENTS.md")); err != nil {
		t.Fatalf("%s: %s/AGENTS.md: %v", filepath.Base(snapDir), planws.PlansDir, err)
	}
	if target, err := os.Readlink(filepath.Join(plansDir, "CLAUDE.md")); err != nil || target != "AGENTS.md" {
		t.Fatalf("%s: %s/CLAUDE.md should link to AGENTS.md: %q, %v", filepath.Base(snapDir), planws.PlansDir, target, err)
	}
	got := planFiles(t, plansDir)
	if *update {
		if err := os.RemoveAll(snapDir); err != nil {
			t.Fatal(err)
		}
		for rel, data := range got {
			writeFile(t, filepath.Join(snapDir, filepath.FromSlash(rel)), data)
		}
		return
	}
	want := planFiles(t, snapDir)
	for rel, data := range want {
		g, ok := got[rel]
		if !ok {
			t.Fatalf("%s: %s missing from workspace", filepath.Base(snapDir), rel)
		}
		if !bytes.Equal(g, data) {
			t.Fatalf("%s: %s differs from snapshot (run with -update and review):\n--- got\n%s\n--- want\n%s",
				filepath.Base(snapDir), rel, g, data)
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			t.Fatalf("%s: unexpected file %s in workspace", filepath.Base(snapDir), rel)
		}
	}
}

// planFiles maps each plan folder's graph.json and annex `*.md` sidecars to
// their bytes, keyed by the slash path "<folder>/<file>". It reads only the
// top level of each folder (annex paths are flat <kebab>.md), so a snapshot
// directory and a live workspace are walked identically. AGENTS.md and the
// CLAUDE.md symlink live in the plans root (not inside an NNN-name folder) and
// are checked separately, so they never appear here.
func planFiles(t *testing.T, dir string) map[string][]byte {
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
		folder := e.Name()
		files, err := os.ReadDir(filepath.Join(dir, folder))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			name := f.Name()
			if f.IsDir() || (name != planws.GraphFile && !strings.HasSuffix(name, ".md")) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, folder, name))
			if err != nil {
				t.Fatal(err)
			}
			out[folder+"/"+name] = data
		}
	}
	return out
}

// comparePreview derives the AC-14 preview-data contract from one plan's final
// graph.json and its annex files and byte-compares it with snapshots/<file>.
// spec is "<folder> <file>". It is the preview-rebuild oracle: the in-scope
// subset AC-14 pins — per-layer AC counts, the annex inventory, and the raw
// usage/structures Markdown bodies — assembled from committed facts alone, and
// deliberately none of the richer fields the preview spike faked
// (contract/example definitions, test-layer summaries/harness/expects).
func comparePreview(t *testing.T, ws, dir, spec string) {
	t.Helper()
	folder, file, ok := strings.Cut(spec, " ")
	if !ok || folder == "" || strings.TrimSpace(file) == "" {
		t.Fatalf("bad # preview directive %q (want '<folder> <file>')", spec)
	}
	planDir := filepath.Join(ws, filepath.FromSlash(planws.PlansDir), folder)
	got := derivePreview(t, planDir)
	compareFile(t, filepath.Join(dir, "snapshots", strings.TrimSpace(file)), got, "preview "+folder)
}

// previewData is the AC-14 preview-data contract (its in-scope subset only).
type previewData struct {
	LayerACCounts  map[string]int `json:"layer_ac_counts"`
	Annexes        []previewAnnex `json:"annexes"`
	UsageBody      string         `json:"usage_body"`
	StructuresBody string         `json:"structures_body"`
}

// previewAnnex is one annex inventory record: its kind, title, path, and the
// ids its `about` edges point at (sorted).
type previewAnnex struct {
	Kind  string   `json:"kind"`
	Title string   `json:"title"`
	Path  string   `json:"path"`
	About []string `json:"about"`
}

// previewGraph is the minimal view of a graph.json the derivation reads. It is
// decoded independently of internal/graph so the oracle stays a plain reader of
// committed bytes, not a second use of the code under test.
type previewGraph struct {
	Nodes []struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Status string `json:"status"`
		Fields struct {
			Kind  string `json:"kind"`
			Title string `json:"title"`
			Path  string `json:"path"`
			Layer string `json:"layer"`
		} `json:"fields"`
	} `json:"nodes"`
	Edges []struct {
		From string `json:"from"`
		Type string `json:"type"`
		To   string `json:"to"`
	} `json:"edges"`
}

// derivePreview builds the preview-data contract from planDir, deterministically
// (sorted keys, annexes sorted by kind, sorted `about` targets).
func derivePreview(t *testing.T, planDir string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(planDir, planws.GraphFile))
	if err != nil {
		t.Fatal(err)
	}
	var g previewGraph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("decode %s: %v", planDir, err)
	}
	pd := previewData{LayerACCounts: map[string]int{}, Annexes: []previewAnnex{}}
	for i := range g.Nodes {
		n := g.Nodes[i]
		if n.Type == "ac" && previewActive(n.Status) && n.Fields.Layer != "" {
			pd.LayerACCounts[n.Fields.Layer]++
		}
	}
	for i := range g.Nodes {
		n := g.Nodes[i]
		if n.Type != "annex" || !previewActive(n.Status) {
			continue
		}
		about := []string{}
		for j := range g.Edges {
			e := g.Edges[j]
			if e.From == n.ID && e.Type == "about" {
				about = append(about, e.To)
			}
		}
		sort.Strings(about)
		pd.Annexes = append(pd.Annexes, previewAnnex{
			Kind: n.Fields.Kind, Title: n.Fields.Title, Path: n.Fields.Path, About: about,
		})
		if n.Fields.Path == "" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(planDir, filepath.FromSlash(n.Fields.Path)))
		if err != nil {
			t.Fatal(err)
		}
		switch n.Fields.Kind {
		case "usage":
			pd.UsageBody = string(body)
		case "structures":
			pd.StructuresBody = string(body)
		}
	}
	sort.Slice(pd.Annexes, func(i, j int) bool { return pd.Annexes[i].Kind < pd.Annexes[j].Kind })
	out, err := json.MarshalIndent(pd, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(out, '\n')
}

// previewActive treats a missing status as active, matching graph semantics.
func previewActive(status string) bool { return status == "" || status == "active" }

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
