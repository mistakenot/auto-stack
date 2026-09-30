package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/cli"
)

// runCLI drives the command tree in-process from cwd, returning stdout,
// stderr and the exit code (after auto-mail's cli_test.go).
func runCLI(t *testing.T, cwd string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := cli.NewRootCmd(app.New(&outBuf, &errBuf, cwd))
	root.SetArgs(args)
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(""))

	err := root.ExecuteContext(context.Background())
	if err != nil {
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
			errBuf.WriteString(exitErr.Error())
		} else {
			code = 1
			errBuf.WriteString(err.Error())
		}
	}
	return outBuf.String(), errBuf.String(), code
}

// repo creates a temp git repo with an isolated HOME and pinned seed/date.
func repo(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(cli.EnvSeed, "1")
	t.Setenv(cli.EnvDate, "2026-01-01")
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func mustRun(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	stdout, stderr, code := runCLI(t, cwd, args...)
	if code != 0 {
		t.Fatalf("auto plan %v: exit %d\nstderr: %s", args, code, stderr)
	}
	if stderr != "" {
		t.Fatalf("auto plan %v: success must leave stderr empty, got %q", args, stderr)
	}
	return stdout
}

func decode[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	return v
}

func readGraph(t *testing.T, root, folder string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "docs", "plans", folder, "graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// failure is the stderr payload of a failed command.
type failure struct {
	Errors []struct {
		Code, Path, Field, Message string
	} `json:"errors"`
	Hint string `json:"hint"`
}

// expectFailure asserts exit 1, empty stdout, and a structured stderr payload
// carrying code and a hint.
func expectFailure(t *testing.T, cwd, code string, args ...string) {
	t.Helper()
	stdout, stderr, exit := runCLI(t, cwd, args...)
	if exit != 1 {
		t.Fatalf("auto plan %v: exit %d, want 1\nstderr: %s", args, exit, stderr)
	}
	if stdout != "" {
		t.Fatalf("auto plan %v: failure must leave stdout empty, got %q", args, stdout)
	}
	f := decode[failure](t, stderr)
	if f.Hint == "" || len(f.Errors) == 0 {
		t.Fatalf("stderr lacks errors/hint: %s", stderr)
	}
	for _, e := range f.Errors {
		if e.Code == code {
			return
		}
	}
	t.Fatalf("auto plan %v: codes %+v, want %s", args, f.Errors, code)
}

func TestInitIsIdempotent(t *testing.T) {
	root := repo(t)
	first := decode[map[string]any](t, mustRun(t, root, "init"))
	if first["root"] != "docs/plans" || first["created"] != true {
		t.Fatalf("first init = %v", first)
	}
	second := decode[map[string]any](t, mustRun(t, root, "init"))
	if second["created"] != false {
		t.Fatalf("second init = %v", second)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".auto")); err != nil {
		t.Fatalf("init must ensure ~/.auto host config: %v", err)
	}
}

func TestNewNumbersAfterHighestFromSubdirectory(t *testing.T) {
	root := repo(t)
	for _, d := range []string{"docs/plans/001-a", "docs/plans/003-b", "src/deep"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stdout := mustRun(t, filepath.Join(root, "src", "deep"), "new", "stage-briefs", "--kind", "task")
	want := "{\n  \"id\": \"004\",\n  \"name\": \"stage-briefs\",\n  \"kind\": \"task\",\n  \"path\": \"docs/plans/004-stage-briefs\"\n}\n"
	if stdout != want {
		t.Fatalf("new stdout:\n%s\nwant:\n%s", stdout, want)
	}

	var g struct {
		Version int              `json:"version"`
		Nodes   []map[string]any `json:"nodes"`
		Edges   []any            `json:"edges"`
	}
	if err := json.Unmarshal(readGraph(t, root, "004-stage-briefs"), &g); err != nil {
		t.Fatal(err)
	}
	if g.Version != 1 || len(g.Nodes) != 1 || g.Edges == nil || len(g.Edges) != 0 {
		t.Fatalf("graph = %+v", g)
	}
	plan := g.Nodes[0]
	fields, _ := plan["fields"].(map[string]any)
	if plan["id"] != "plan" || plan["type"] != "plan" || fields["lifecycle"] != "requirements" ||
		fields["name"] != "stage-briefs" || fields["kind"] != "task" || fields["created"] != "2026-01-01" {
		t.Fatalf("plan node = %v", plan)
	}
}

func TestNewWithoutPlansFolderStartsAt001(t *testing.T) {
	root := repo(t)
	out := decode[map[string]any](t, mustRun(t, root, "new", "demo", "--kind", "epic"))
	if out["id"] != "001" || out["kind"] != "epic" {
		t.Fatalf("new = %v", out)
	}
}

func TestNewRejectsBadInputAndWritesNothing(t *testing.T) {
	root := repo(t)
	expectFailure(t, root, "invalid-name", "new", "Bad_Name", "--kind", "task")
	expectFailure(t, root, "invalid-kind", "new", "demo", "--kind", "story")
	expectFailure(t, root, "invalid-kind", "new", "demo")
	if _, err := os.Stat(filepath.Join(root, "docs", "plans")); !os.IsNotExist(err) {
		t.Fatalf("rejected new must write nothing (stat err %v)", err)
	}
}

func TestNewOutsideRepoFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if _, err := exec.Command("git", "-C", dir, "rev-parse").CombinedOutput(); err == nil {
		t.Skip("temp dir is inside a git repository")
	}
	expectFailure(t, dir, "not-a-repo", "new", "demo", "--kind", "task")
}

// walk builds the skeleton scenario: two goals, an AC proving the first, a
// decision constraining it. It returns the generated IDs.
func walk(t *testing.T, root string) (g1, g2, ac, d string) {
	t.Helper()
	mustRun(t, root, "new", "demo", "--kind", "task")
	type added struct {
		ID, Plan, Type, Rank string
		Edges                []struct{ From, Type, To string }
	}
	a := decode[added](t, mustRun(t, root, "add", "001", "goal", "--title", "First goal"))
	if a.Plan != "001" || a.Type != "goal" || a.Rank != "a0" || !strings.HasPrefix(a.ID, "g-") || a.Edges != nil {
		t.Fatalf("add goal = %+v", a)
	}
	b := decode[added](t, mustRun(t, root, "add", "001", "goal", "--title", "Second goal"))
	if b.Rank != "a1" {
		t.Fatalf("second goal rank = %s", b.Rank)
	}
	c := decode[added](t, mustRun(t, root, "add", "001-demo", "ac", "--proves", a.ID, "--title", "it works",
		"--verify-cmd", "go test ./...", "--verify-tests", "TestA", "--verify-tests", "TestB"))
	if len(c.Edges) != 1 || c.Edges[0].From != c.ID || c.Edges[0].Type != "proves" || c.Edges[0].To != a.ID {
		t.Fatalf("add ac = %+v", c)
	}
	dd := decode[added](t, mustRun(t, root, "add", "docs/plans/001-demo", "decision", "--title", "use JSON"))
	l := decode[added](t, mustRun(t, root, "link", "001", dd.ID, "constrains", a.ID))
	if l.ID != dd.ID || l.Plan != "001" || len(l.Edges) != 1 || l.Edges[0].To != a.ID {
		t.Fatalf("link = %+v", l)
	}
	return a.ID, b.ID, c.ID, dd.ID
}

func TestAddLinkWriteTypedGraph(t *testing.T) {
	root := repo(t)
	g1, _, ac, d := walk(t, root)
	data := string(readGraph(t, root, "001-demo"))
	for _, want := range []string{
		`"from": "` + ac + `",` + "\n      \"type\": \"proves\",\n      \"to\": \"" + g1 + `"`,
		`"from": "` + d + `",` + "\n      \"type\": \"constrains\"",
		`"tests": [` + "\n            \"TestA\",\n            \"TestB\"",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("graph.json missing %q:\n%s", want, data)
		}
	}
}

func TestAddReadsTextFromFile(t *testing.T) {
	root := repo(t)
	mustRun(t, root, "new", "demo", "--kind", "task")
	gwt := filepath.Join(root, "ac.md")
	if err := os.WriteFile(gwt, []byte("Given a\nWhen b\nThen c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, root, "add", "001", "ac", "--title", "t", "--gwt", "@ac.md")
	if !strings.Contains(string(readGraph(t, root, "001-demo")), `"gwt": "Given a\nWhen b\nThen c"`) {
		t.Fatalf("gwt not read from file:\n%s", readGraph(t, root, "001-demo"))
	}
}

func TestRejectedWritesLeaveGraphByteIdentical(t *testing.T) {
	root := repo(t)
	g1, g2, ac, d := walk(t, root)
	before := readGraph(t, root, "001-demo")

	expectFailure(t, root, "dangling-ref", "link", "001", d, "constrains", "g-zzzz")
	expectFailure(t, root, "wrong-endpoint", "link", "001", g1, "constrains", g2)
	expectFailure(t, root, "wrong-endpoint", "link", "001", ac, "proves", ac)
	expectFailure(t, root, "duplicate-edge", "link", "001", d, "constrains", g1)
	expectFailure(t, root, "unregistered-type", "link", "001", d, "blocks", g1)
	expectFailure(t, root, "missing-field", "add", "001", "goal")
	expectFailure(t, root, "dangling-ref", "add", "001", "ac", "--title", "t", "--proves", "g-zzzz")
	expectFailure(t, root, "invalid-field", "add", "001", "ac", "--title", "t", "--verify-kind", "robot")
	expectFailure(t, root, "unregistered-type", "add", "001", "widget", "--title", "t")
	expectFailure(t, root, "unregistered-type", "add", "001", "plan")
	expectFailure(t, root, "usage", "add", "001", "goal", "--bogus", "x")
	expectFailure(t, root, "plan-not-found", "add", "009", "goal", "--title", "t")

	if after := readGraph(t, root, "001-demo"); !bytes.Equal(before, after) {
		t.Fatalf("rejected writes changed graph.json:\n%s\n---\n%s", before, after)
	}
}

func TestWritesRefuseAlreadyInvalidGraph(t *testing.T) {
	root := repo(t)
	mustRun(t, root, "new", "demo", "--kind", "task")
	path := filepath.Join(root, "docs", "plans", "001-demo", "graph.json")
	broken := strings.Replace(string(readGraph(t, root, "001-demo")), `"edges": []`,
		`"edges": [{"from": "d-zzzz", "type": "constrains", "to": "g-zzzz"}]`, 1)
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, root, "add", "001", "goal", "--title", "t")
	if code != 1 || !strings.Contains(stderr, "auto plan lint 001") {
		t.Fatalf("exit %d, stderr %s", code, stderr)
	}
	if got, _ := os.ReadFile(path); string(got) != broken {
		t.Fatal("refused write changed the file")
	}
}

type report struct {
	Plan   string `json:"plan"`
	OK     bool   `json:"ok"`
	Issues []struct {
		Code, Severity, Path, Field, Message, Hint string
	} `json:"issues"`
}

func TestLintCleanAndBroken(t *testing.T) {
	root := repo(t)
	walk(t, root)
	r := decode[report](t, mustRun(t, root, "lint", "001"))
	if r.Plan != "001" || !r.OK || r.Issues == nil || len(r.Issues) != 0 {
		t.Fatalf("clean lint = %+v", r)
	}

	mustRun(t, root, "new", "broken", "--kind", "task")
	path := filepath.Join(root, "docs", "plans", "002-broken", "graph.json")
	data := strings.Replace(string(readGraph(t, root, "002-broken")), `"edges": []`,
		`"edges": [{"from": "d-zzzz", "type": "constrains", "to": "plan"}]`, 1)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, root, "lint", "002")
	if code != 1 || stderr != "" {
		t.Fatalf("broken lint: exit %d stderr %q", code, stderr)
	}
	r = decode[report](t, stdout)
	if r.OK || len(r.Issues) == 0 || r.Issues[0].Code != "dangling-ref" || r.Issues[0].Hint == "" || r.Issues[0].Severity != "error" {
		t.Fatalf("broken lint = %+v", r)
	}

	stdout, _, code = runCLI(t, root, "lint", "all")
	all := decode[struct {
		OK    bool     `json:"ok"`
		Plans []report `json:"plans"`
	}](t, stdout)
	if code != 1 || all.OK || len(all.Plans) != 2 || !all.Plans[0].OK || all.Plans[1].OK {
		t.Fatalf("lint all: exit %d %+v", code, all)
	}

	text, _, _ := runCLI(t, root, "lint", "all", "--text")
	if !strings.HasPrefix(text, "001  ok  (0 issues)\n002  FAIL") || !strings.Contains(text, "hint: ") {
		t.Fatalf("lint --text:\n%s", text)
	}
}

func TestLintMalformedJSON(t *testing.T) {
	root := repo(t)
	mustRun(t, root, "new", "demo", "--kind", "task")
	path := filepath.Join(root, "docs", "plans", "001-demo", "graph.json")
	if err := os.WriteFile(path, []byte("{\n  \"version\": 1,\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runCLI(t, root, "lint", "001")
	r := decode[report](t, stdout)
	if code != 1 || len(r.Issues) != 1 || r.Issues[0].Code != "parse-error" {
		t.Fatalf("lint = %d %+v", code, r)
	}
}

func TestShowJSONAndText(t *testing.T) {
	root := repo(t)
	g1, g2, ac, d := walk(t, root)
	v := decode[struct {
		Plan  string `json:"plan"`
		Goals []struct {
			ID  string `json:"id"`
			ACs []struct {
				ID, Verify string
			} `json:"acs"`
			Decisions []struct{ ID string } `json:"decisions"`
		} `json:"goals"`
	}](t, mustRun(t, root, "show", "001"))
	if v.Plan != "001" || len(v.Goals) != 2 || v.Goals[0].ID != g1 || v.Goals[1].ID != g2 ||
		len(v.Goals[0].ACs) != 1 || v.Goals[0].ACs[0].ID != ac || v.Goals[0].ACs[0].Verify != "go test ./..." ||
		len(v.Goals[0].Decisions) != 1 || v.Goals[0].Decisions[0].ID != d {
		t.Fatalf("show = %+v", v)
	}

	text := mustRun(t, root, "show", "001", "--text")
	for _, want := range []string{"001-demo  task · requirements", "◎ " + g1, "    ✓ " + ac, "go test ./...", "    ◆ " + d} {
		if !strings.Contains(text, want) {
			t.Errorf("show --text missing %q:\n%s", want, text)
		}
	}
	expectFailure(t, root, "plan-not-found", "show", "all")
}

func TestAddHelpIsGeneratedFromRegistry(t *testing.T) {
	root := repo(t)
	out := mustRun(t, root, "add", "001", "ac", "--help")
	for _, want := range []string{"--title", "--gwt", "--verify-cmd", "--verify-tests", "--proves", "[required]"} {
		if !strings.Contains(out, want) {
			t.Errorf("ac --help missing %s:\n%s", want, out)
		}
	}
	out = mustRun(t, root, "add", "--help")
	for _, want := range []string{"goal —", "ac —", "decision —"} {
		if !strings.Contains(out, want) {
			t.Errorf("add --help missing %q", want)
		}
	}
}
