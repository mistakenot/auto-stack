package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/cli"
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
)

// The plan IDs `new` generates under AUTO_PLAN_SEED=1, by plan number.
const (
	p1 = "001-acs4"
	p2 = "002-j8nx"
	p3 = "003-gevp"
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
	want := "{\n  \"id\": \"004-vejj\",\n  \"number\": \"004\",\n  \"name\": \"stage-briefs\",\n  \"kind\": \"task\",\n  \"path\": \"docs/plans/004-stage-briefs\",\n  \"scaffolded\": [\n    \"docs/plans/AGENTS.md\",\n    \"docs/plans/CLAUDE.md\"\n  ]\n}\n"
	if stdout != want {
		t.Fatalf("new stdout:\n%s\nwant:\n%s", stdout, want)
	}

	var g struct {
		Version string           `json:"version"`
		ID      string           `json:"id"`
		Nodes   []map[string]any `json:"nodes"`
		Edges   []any            `json:"edges"`
	}
	if err := json.Unmarshal(readGraph(t, root, "004-stage-briefs"), &g); err != nil {
		t.Fatal(err)
	}
	if g.Version != "1.0.0" || g.ID != "004-vejj" || len(g.Nodes) != 1 || g.Edges == nil || len(g.Edges) != 0 {
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
	if out["id"] != p1 || out["number"] != "001" || out["kind"] != "epic" {
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
	if a.Plan != p1 || a.Type != "goal" || a.Rank != "a0" || !strings.HasPrefix(a.ID, "g-") || a.Edges != nil {
		t.Fatalf("add goal = %+v", a)
	}
	b := decode[added](t, mustRun(t, root, "add", "001", "goal", "--title", "Second goal"))
	if b.Rank != "a1" {
		t.Fatalf("second goal rank = %s", b.Rank)
	}
	c := decode[added](t, mustRun(t, root, "add", "001-demo", "ac", "--proves", a.ID, "--title", "it works",
		"--gwt", "Given a plan, when it runs, then it works", "--verify-cmd", "go test ./...", "--verify-tests", "TestA", "--verify-tests", "TestB"))
	if len(c.Edges) != 1 || c.Edges[0].From != c.ID || c.Edges[0].Type != "proves" || c.Edges[0].To != a.ID {
		t.Fatalf("add ac = %+v", c)
	}
	dd := decode[added](t, mustRun(t, root, "add", "docs/plans/001-demo", "decision", "--title", "use JSON",
		"--chosen", "JSON", "--why", "diffable", "--by", "charlie"))
	l := decode[added](t, mustRun(t, root, "link", "001", dd.ID, "constrains", a.ID))
	if l.ID != dd.ID || l.Plan != p1 || len(l.Edges) != 1 || l.Edges[0].To != a.ID {
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
		`"edges": [{"id": "e-zzzz", "from": "d-zzzz", "type": "constrains", "to": "g-zzzz"}]`, 1)
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
	// At requirements the only issue is a warning, and warnings alone exit 0.
	r := decode[report](t, mustRun(t, root, "lint", "001"))
	if r.Plan != p1 || !r.OK || len(r.Issues) != 1 || r.Issues[0].Code != "decision-no-alternative" || r.Issues[0].Severity != "warning" {
		t.Fatalf("clean lint = %+v", r)
	}

	mustRun(t, root, "new", "broken", "--kind", "task")
	path := filepath.Join(root, "docs", "plans", "002-broken", "graph.json")
	data := strings.Replace(string(readGraph(t, root, "002-broken")), `"edges": []`,
		`"edges": [{"id": "e-zzzz", "from": "d-zzzz", "type": "constrains", "to": "plan"}]`, 1)
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
	if !strings.HasPrefix(text, p1+"  ok  (0 errors, 1 warning)\n"+p2+"  FAIL  (2 errors, 0 warnings)\n") ||
		!strings.Contains(text, "\nremediation:\n  "+p1+" decision-no-alternative: auto plan add "+p1+" alternative") ||
		strings.Index(text, "remediation:") < strings.Index(text, p2+" error dangling-ref") {
		t.Fatalf("lint --text:\n%s", text)
	}
}

// TestLintLifecycleGating: advancing the lifecycle turns on the rules for
// that step (AC-9), and an open question gates at every step.
func TestLintLifecycleGating(t *testing.T) {
	root := repo(t)
	_, g2, _, _ := walk(t, root)
	mustRun(t, root, "update", "001", "plan", "--lifecycle", "solution")
	stdout, _, code := runCLI(t, root, "lint", "001")
	r := decode[report](t, stdout)
	var errs []string
	for _, is := range r.Issues {
		if is.Severity == "error" {
			errs = append(errs, is.Code+" "+is.Path)
		}
	}
	if code != 1 || r.OK || len(errs) != 1 || errs[0] != "goal-no-ac $.nodes["+g2+"]" {
		t.Fatalf("lint at solution: exit %d %+v", code, r)
	}

	mustRun(t, root, "update", "001", "plan", "--lifecycle", "requirements")
	q := decode[struct{ ID string }](t, mustRun(t, root, "add", "001", "question", "--title", "Which store?", "--status", "open"))
	stdout, _, code = runCLI(t, root, "lint", "001")
	r = decode[report](t, stdout)
	if code != 1 || r.OK || r.Issues[0].Code != "open-question" || !strings.Contains(r.Issues[0].Message, q.ID) {
		t.Fatalf("open question at requirements: exit %d %+v", code, r)
	}
	mustRun(t, root, "update", "001", q.ID, "--status", "answered", "--answer", "JSON")
	if _, _, code = runCLI(t, root, "lint", "001"); code != 0 {
		t.Fatalf("answered question: exit %d", code)
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
	if v.Plan != p1 || len(v.Goals) != 2 || v.Goals[0].ID != g1 || v.Goals[1].ID != g2 ||
		len(v.Goals[0].ACs) != 1 || v.Goals[0].ACs[0].ID != ac || v.Goals[0].ACs[0].Verify != "go test ./..." ||
		len(v.Goals[0].Decisions) != 1 || v.Goals[0].Decisions[0].ID != d {
		t.Fatalf("show = %+v", v)
	}

	text := mustRun(t, root, "show", "001", "--text")
	for _, want := range []string{p1 + "  demo  task · requirements", "◎ G1     " + g1, "    ✓ AC1.1  " + ac, "go test ./...", "    ◆ D1     " + d} {
		if !strings.Contains(text, want) {
			t.Errorf("show --text missing %q:\n%s", want, text)
		}
	}
	expectFailure(t, root, "plan-not-found", "show", "all")
}

func TestAddHelpIsGeneratedFromRegistry(t *testing.T) {
	root := repo(t)
	out := mustRun(t, root, "add", "001", "ac", "--help")
	for _, want := range []string{"--title", "--gwt", "--verify-cmd", "--verify-tests", "--proves", "--discharges", "[required]"} {
		if !strings.Contains(out, want) {
			t.Errorf("ac --help missing %s:\n%s", want, out)
		}
	}
	out = mustRun(t, root, "add", "--help")
	for _, want := range []string{"goal —", "ac —", "decision —", "stage —", "child —"} {
		if !strings.Contains(out, want) {
			t.Errorf("add --help missing %q", want)
		}
	}
}

// TestEveryTypeGetsGeneratedFlags checks add and update expose one flag per
// registered field and add one flag per outgoing edge type, for every type.
func TestEveryTypeGetsGeneratedFlags(t *testing.T) {
	root := repo(t)
	for _, nt := range schema.Registry.Nodes {
		var fieldFlags []string
		for _, f := range nt.Fields {
			if f.Kind == schema.KindObject {
				for _, m := range f.Fields {
					fieldFlags = append(fieldFlags, "--"+f.Name+"-"+m.Name)
				}
				continue
			}
			fieldFlags = append(fieldFlags, "--"+f.Name)
		}
		if !nt.Singleton() {
			out := mustRun(t, root, "add", "001", nt.Name, "--help")
			want := slices.Clone(fieldFlags)
			for _, e := range schema.Registry.EdgesFrom(nt.Name) {
				want = append(want, "--"+e.Name)
			}
			for _, w := range want {
				if !strings.Contains(out, w+" ") {
					t.Errorf("add %s --help missing %s:\n%s", nt.Name, w, out)
				}
			}
		}
		up := mustRun(t, root, "update", "--help")
		if !strings.Contains(up, nt.Name+" — ") {
			t.Errorf("update --help missing type %s", nt.Name)
		}
	}
	for _, w := range []string{"--lifecycle", "--epic", "--reversibility", "--steps"} {
		if !strings.Contains(mustRun(t, root, "update", "--help"), w) {
			t.Errorf("update --help missing %s", w)
		}
	}
}

// TestThrowawayTypeGetsFlagsWithoutCLIChanges is the extensibility contract
// (D-6, D-7): registering a type is the only change needed for add, update,
// link, their --help, and validation to handle it.
func TestThrowawayTypeGetsFlagsWithoutCLIChanges(t *testing.T) {
	saved := schema.Registry
	t.Cleanup(func() { schema.Registry = saved })
	schema.Registry.Nodes = append(slices.Clone(saved.Nodes), schema.NodeType{
		Name: "widget", Prefix: "w", Term: "Widget", MinLifecycle: schema.LifecycleRequirements,
		Help: "A test-only type.",
		Fields: []schema.FieldSpec{
			{Name: "title", Kind: schema.KindString, Required: true, Help: "Widget name"},
			{Name: "colour", Kind: schema.KindEnum, Enum: []string{"red", "blue"}, Help: "Widget colour"},
		},
	})
	schema.Registry.Edges = append(slices.Clone(saved.Edges), schema.EdgeType{
		Name: "decorates", From: []string{"widget"}, To: []string{"goal"}, Help: "The goal this widget decorates",
	})

	root := repo(t)
	g1, _, ac, _ := walk(t, root)

	help := mustRun(t, root, "add", "001", "widget", "--help")
	for _, want := range []string{"widget — A test-only type.", "--title", "--colour", "(red|blue)", "--decorates", "edge to goal"} {
		if !strings.Contains(help, want) {
			t.Errorf("widget --help missing %q:\n%s", want, help)
		}
	}
	if !strings.Contains(mustRun(t, root, "add", "--help"), "widget — ") {
		t.Error("add --help does not list widget")
	}

	w := decode[struct{ ID, Type string }](t, mustRun(t, root, "add", "001", "widget", "--title", "w", "--colour", "red", "--decorates", g1))
	if w.Type != "widget" || !strings.HasPrefix(w.ID, "w-") {
		t.Fatalf("add widget = %+v", w)
	}
	u := decode[struct {
		Fields map[string]any `json:"fields"`
	}](t, mustRun(t, root, "update", "001", w.ID, "--colour", "blue"))
	if u.Fields["colour"] != "blue" {
		t.Fatalf("update widget = %+v", u)
	}
	expectFailure(t, root, "invalid-field", "update", "001", w.ID, "--colour", "green")
	expectFailure(t, root, "wrong-endpoint", "link", "001", w.ID, "decorates", ac)
	if !strings.Contains(mustRun(t, root, "link", "--help"), "decorates") {
		t.Error("link --help does not list the decorates edge")
	}
}

type mutation struct {
	Plan   string         `json:"plan"`
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Rank   string         `json:"rank"`
	Status string         `json:"status"`
	Fields map[string]any `json:"fields"`
	Edges  []struct {
		From, Type, To string
	} `json:"edges"`
	Removed []struct {
		From, Type, To string
	} `json:"removed"`
}

func TestUpdateUnlinkRetireMove(t *testing.T) {
	root := repo(t)
	g1, g2, ac, d := walk(t, root)

	u := decode[mutation](t, mustRun(t, root, "update", "001", ac, "--title", "it really works", "--verify-kind", "manual", "--verify-cmd", ""))
	if u.Plan != p1 || u.ID != ac || u.Type != "ac" || u.Fields["title"] != "it really works" {
		t.Fatalf("update = %+v", u)
	}
	if v, _ := u.Fields["verify"].(map[string]any); v["kind"] != "manual" || v["cmd"] != nil || v["tests"] == nil {
		t.Fatalf("update verify merge = %+v", u.Fields["verify"])
	}
	if text := mustRun(t, root, "update", "001", ac, "--title", "it really works", "--text"); !strings.Contains(text, "nothing changed") {
		t.Fatalf("no-op update --text = %q", text)
	}

	p := decode[mutation](t, mustRun(t, root, "update", "001", "plan", "--lifecycle", "solution"))
	if p.ID != "plan" || p.Fields["lifecycle"] != "solution" {
		t.Fatalf("update plan = %+v", p)
	}
	if !strings.Contains(mustRun(t, root, "show", "001", "--text"), "task · solution") {
		t.Fatal("lifecycle not advanced")
	}

	un := decode[mutation](t, mustRun(t, root, "unlink", "001", d, "constrains", g1))
	if un.ID != d || len(un.Removed) != 1 || un.Removed[0].To != g1 {
		t.Fatalf("unlink = %+v", un)
	}

	m := decode[mutation](t, mustRun(t, root, "move", "001", g2, "--before", g1))
	if m.ID != g2 || m.Type != "goal" || m.Rank == "" || m.Rank >= "a0" {
		t.Fatalf("move = %+v", m)
	}
	show := mustRun(t, root, "show", "001", "--text")
	if strings.Index(show, g2) > strings.Index(show, g1) {
		t.Fatalf("show does not reflect the move:\n%s", show)
	}

	r := decode[mutation](t, mustRun(t, root, "retire", "001", ac))
	if r.ID != ac || r.Status != "retired" || r.Type != "ac" {
		t.Fatalf("retire = %+v", r)
	}
	data := string(readGraph(t, root, "001-demo"))
	if !strings.Contains(data, `"id": "`+ac+`",`+"\n      \"type\": \"ac\",\n      \"status\": \"retired\"") {
		t.Fatalf("retired node not kept:\n%s", data)
	}
}

func TestRejectedMutationsLeaveGraphByteIdentical(t *testing.T) {
	root := repo(t)
	g1, g2, ac, d := walk(t, root)
	before := readGraph(t, root, "001-demo")

	expectFailure(t, root, "node-not-found", "update", "001", "g-zzzz", "--title", "x")
	expectFailure(t, root, "missing-field", "update", "001", g1, "--title", "")
	expectFailure(t, root, "invalid-field", "update", "001", "plan", "--lifecycle", "shipped")
	expectFailure(t, root, "usage", "update", "001", "plan", "--name", "other")
	expectFailure(t, root, "usage", "update", "001", g1)
	expectFailure(t, root, "usage", "update", "001", g1, "--proves", g2)
	expectFailure(t, root, "edge-not-found", "unlink", "001", d, "constrains", g2)
	expectFailure(t, root, "node-not-found", "retire", "001", "g-zzzz")
	expectFailure(t, root, "not-retirable", "retire", "001", "plan")
	expectFailure(t, root, "usage", "move", "001", g1)
	expectFailure(t, root, "usage", "move", "001", g1, "--before", g2, "--after", g2)
	expectFailure(t, root, "not-sibling", "move", "001", g1, "--before", ac)
	expectFailure(t, root, "not-movable", "move", "001", "plan", "--before", g1)
	expectFailure(t, root, "wrong-endpoint", "link", "001", ac, "rejects", g1)

	if after := readGraph(t, root, "001-demo"); !bytes.Equal(before, after) {
		t.Fatalf("rejected mutations changed graph.json:\n%s\n---\n%s", before, after)
	}
}

func TestUpdateHelpForOneNode(t *testing.T) {
	root := repo(t)
	_, _, ac, _ := walk(t, root)
	out := mustRun(t, root, "update", "001", ac, "--help")
	for _, want := range []string{"auto plan update <plan> " + ac, "--gwt", "--verify-cmd", `("" removes it)`} {
		if !strings.Contains(out, want) {
			t.Errorf("update %s --help missing %q:\n%s", ac, want, out)
		}
	}
	if strings.Contains(out, "--proves") || strings.Contains(out, "[required]") {
		t.Errorf("update --help must not offer edge flags or [required]:\n%s", out)
	}
	if out := mustRun(t, root, "update", "001", "plan", "--help"); strings.Contains(out, "--name") || strings.Contains(out, "--created") {
		t.Errorf("fixed plan fields must not be updatable:\n%s", out)
	}
}

type fmtOut struct {
	Plan      string `json:"plan"`
	Path      string `json:"path"`
	Canonical bool   `json:"canonical"`
	Changed   bool   `json:"changed"`
	Errors    []struct{ Code string }
}

func TestFmtCanonicalisesHandEdits(t *testing.T) {
	root := repo(t)
	walk(t, root)
	canonical := readGraph(t, root, "001-demo")
	path := filepath.Join(root, "docs", "plans", "001-demo", "graph.json")

	// Hand-scramble: reverse node and edge order, compact, add an unknown key.
	var raw map[string]any
	if err := json.Unmarshal(canonical, &raw); err != nil {
		t.Fatal(err)
	}
	nodes := raw["nodes"].([]any)
	slices.Reverse(nodes)
	edges := raw["edges"].([]any)
	slices.Reverse(edges)
	raw["zz-note"] = "kept <as is>"
	scrambled, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, scrambled, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runCLI(t, root, "fmt", "001", "--check")
	if r := decode[fmtOut](t, stdout); code != 1 || r.Canonical || r.Changed || r.Path != "docs/plans/001-demo/graph.json" {
		t.Fatalf("fmt --check on scrambled: exit %d %+v", code, r)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, scrambled) {
		t.Fatal("fmt --check wrote the file")
	}

	r := decode[fmtOut](t, mustRun(t, root, "fmt", "001"))
	if r.Canonical || !r.Changed {
		t.Fatalf("fmt = %+v", r)
	}
	got := readGraph(t, root, "001-demo")
	if want := strings.Replace(string(canonical), "\n  ]\n}\n", "\n  ],\n  \"zz-note\": \"kept <as is>\"\n}\n", 1); string(got) != want {
		t.Fatalf("fmt output:\n%s\nwant:\n%s", got, want)
	}

	r = decode[fmtOut](t, mustRun(t, root, "fmt", "001", "--check"))
	if !r.Canonical || r.Changed {
		t.Fatalf("fmt --check after fmt = %+v", r)
	}
	r = decode[fmtOut](t, mustRun(t, root, "fmt", "001"))
	if !r.Canonical || r.Changed {
		t.Fatalf("second fmt = %+v", r)
	}
}

func TestFmtAllAndLossyFiles(t *testing.T) {
	root := repo(t)
	walk(t, root)
	mustRun(t, root, "new", "broken", "--kind", "task")
	path := filepath.Join(root, "docs", "plans", "002-broken", "graph.json")
	lossy := strings.Replace(string(readGraph(t, root, "002-broken")), `"edges": []`, `"edges": [42]`, 1)
	if err := os.WriteFile(path, []byte(lossy), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runCLI(t, root, "fmt", "all")
	all := decode[struct {
		OK    bool     `json:"ok"`
		Plans []fmtOut `json:"plans"`
	}](t, stdout)
	if code != 1 || all.OK || len(all.Plans) != 2 || !all.Plans[0].Canonical || len(all.Plans[0].Errors) != 0 ||
		len(all.Plans[1].Errors) == 0 || all.Plans[1].Errors[0].Code != "invalid-field" {
		t.Fatalf("fmt all: exit %d %+v", code, all)
	}
	if got, _ := os.ReadFile(path); string(got) != lossy {
		t.Fatal("fmt rewrote a file it could not represent")
	}
	text, _, _ := runCLI(t, root, "fmt", "all", "--text")
	if !strings.HasPrefix(text, p1+"  canonical") || !strings.Contains(text, p2+"  ERROR") || !strings.Contains(text, "hint: ") {
		t.Fatalf("fmt all --text:\n%s", text)
	}

	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = runCLI(t, root, "fmt", "002")
	if r := decode[fmtOut](t, stdout); code != 1 || len(r.Errors) != 1 || r.Errors[0].Code != "parse-error" {
		t.Fatalf("fmt malformed: exit %d %+v", code, r)
	}
}

// lens builds a plan for the read lenses on top of walk: an alternative with
// a long reason, a rail discharged locally and across plans, an open
// question, two stages (the second depends on the first), three files and a
// tree. It returns the generated IDs by role.
// handLink appends an edge to a plan's graph.json the way a hand edit would,
// bypassing the CLI's write checks.
func handLink(t *testing.T, root, folder string, e graph.Edge) {
	t.Helper()
	path := filepath.Join(root, "docs", "plans", folder, "graph.json")
	g, err := graph.Decode(path)
	if err != nil {
		t.Fatal(err)
	}
	g.Edges = append(g.Edges, e)
	if err := graph.Save(path, g); err != nil {
		t.Fatal(err)
	}
}

func lens(t *testing.T, root string) map[string]string {
	t.Helper()
	g1, g2, ac, d := walk(t, root)
	ids := map[string]string{"g1": g1, "g2": g2, "ac": ac, "d": d}
	add := func(role string, args ...string) {
		t.Helper()
		ids[role] = decode[struct{ ID string }](t, mustRun(t, root, append([]string{"add", "001"}, args...)...)).ID
	}
	add("a", "alternative", "--title", "YAML", "--why", strings.Repeat("merge conflicts everywhere ", 12))
	mustRun(t, root, "link", "001", d, "rejects", ids["a"])
	add("ac2", "ac", "--proves", g2, "--title", "second criterion", "--gwt", "Given x, when y, then z", "--verify-cmd", "go test ./b")
	add("r", "rail", "--title", "No network calls")
	mustRun(t, root, "link", "001", ac, "discharges", ids["r"])
	// A qualified target in a plan that does not exist: link refuses it, so
	// it is a hand edit, and the read lenses print it as a raw reference.
	handLink(t, root, "001-demo", graph.Edge{ID: "e-zzzz", From: ac, Type: "discharges", To: "005-m3x9:r-8hw3"})
	add("q", "question", "--title", "Ship it behind a flag?", "--status", "open", "--recommended", "no")
	add("s1", "stage", "--title", "First stage", "--steps", "SECRET-STEP-ONE", "--commit", "feat: one", "--status", "done")
	add("s2", "stage", "--title", "Second stage", "--steps", "step two", "--commit", "feat: two", "--dependsOn", ids["s1"])
	add("f1", "file", "--path", "pkg/a.go", "--change", "add", "--why", "new")
	add("f2", "file", "--path", "pkg/b.go", "--change", "edit", "--why", "changed")
	add("f3", "file", "--path", "README.md", "--change", "delete", "--why", "gone")
	mustRun(t, root, "link", "001", ids["s1"], "touches", ids["f1"])
	mustRun(t, root, "link", "001", ids["s2"], "touches", ids["f2"])
	mustRun(t, root, "link", "001", ids["s2"], "touches", ids["f3"])
	mustRun(t, root, "link", "001", ids["s1"], "covers", ac)
	mustRun(t, root, "link", "001", ids["s2"], "covers", ids["ac2"])
	add("t", "tree", "--title", "call path", "--kind", "call", "--body", "run\n  check "+ac+"  the AC", "--about", ac)
	return ids
}

type listOut struct {
	Plan      string `json:"plan"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Lifecycle string `json:"lifecycle"`
	Nodes     []struct {
		ID, Type, Status, Rank, Title string
	} `json:"nodes"`
}

func TestListFiltersAndAll(t *testing.T) {
	root := repo(t)
	ids := lens(t, root)
	mustRun(t, root, "retire", "001", ids["ac2"])

	one := decode[listOut](t, mustRun(t, root, "list", "001"))
	if one.Plan != p1 || one.Name != "demo" || one.Kind != "task" || one.Lifecycle != "requirements" || len(one.Nodes) != 15 {
		t.Fatalf("list 001 = %+v", one)
	}
	if n := one.Nodes[0]; n.ID != "plan" || n.Title != "demo" {
		t.Fatalf("registry type order puts the plan node first: %+v", n)
	}
	acs := decode[listOut](t, mustRun(t, root, "list", "001", "--type", "  AC "))
	statuses := map[string]string{}
	for _, n := range acs.Nodes {
		statuses[n.ID] = n.Status
	}
	if len(acs.Nodes) != 2 || statuses[ids["ac"]] != "active" || statuses[ids["ac2"]] != "retired" {
		t.Fatalf("--type is normalised and retired nodes are listed: %+v", acs.Nodes)
	}
	if strings.Contains(mustRun(t, root, "list", "001"), `"gwt"`) {
		t.Fatal("list carries IDs, metadata and titles only")
	}
	expectFailure(t, root, "invalid-type", "list", "001", "--type", "story")

	text := mustRun(t, root, "list", "001", "--type", "ac", "--text")
	if !strings.HasPrefix(text, p1+"  demo  task · requirements\n") || !strings.Contains(text, "second criterion  (retired)") {
		t.Fatalf("list --text:\n%s", text)
	}

	// all: every plan; a malformed plan is reported on stderr, the rest still listed.
	mustRun(t, root, "new", "other", "--kind", "epic")
	mustRun(t, root, "new", "broken", "--kind", "task")
	if err := os.WriteFile(filepath.Join(root, "docs", "plans", "003-broken", "graph.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, root, "list", "all")
	all := decode[struct{ Plans []listOut }](t, stdout)
	if code != 1 || len(all.Plans) != 2 || all.Plans[1].Plan != p2 || all.Plans[1].Kind != "epic" {
		t.Fatalf("list all: exit %d %+v", code, all)
	}
	if f := decode[failure](t, stderr); len(f.Errors) != 1 || f.Errors[0].Code != "parse-error" || f.Hint == "" {
		t.Fatalf("list all stderr = %s", stderr)
	}
}

// TestListingsValidateGraphs: a plan that is valid JSON but structurally
// invalid is still listed and searched, and its validation errors go to stderr
// with exit 1.
func TestListingsValidateGraphs(t *testing.T) {
	root := repo(t)
	mustRun(t, root, "new", "demo", "--kind", "task")
	mustRun(t, root, "add", "001", "goal", "--title", "Findable goal")
	path := filepath.Join(root, "docs", "plans", "001-demo", "graph.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(raw), `"edges": []`, `"edges": [{"from": "plan", "type": "proves", "to": "g-zzzz"}]`, 1)
	if broken == string(raw) {
		t.Fatalf("fixture edit did not apply:\n%s", raw)
	}
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"list", "001"}, {"list", "all"}, {"search", "all", "findable"}} {
		stdout, stderr, code := runCLI(t, root, args...)
		if code != 1 || !strings.Contains(stdout, "Findable goal") {
			t.Fatalf("%v: exit %d, results must still be printed:\n%s", args, code, stdout)
		}
		f := decode[failure](t, stderr)
		if len(f.Errors) == 0 || !strings.Contains(f.Hint, "auto plan lint") {
			t.Fatalf("%v stderr = %s", args, stderr)
		}
	}
}

func TestDescribeTruncatesWithRecoveryCommand(t *testing.T) {
	root := repo(t)
	ids := lens(t, root)
	v := decode[struct {
		ID, Type, Term, Status, Rank, Title, Get string
		Fields                                   map[string]any
		Truncated                                []string
		Edges                                    struct{ Out, In map[string]int }
	}](t, mustRun(t, root, "describe", "001", ids["a"]))
	why, _ := v.Fields["why"].(string)
	if v.Type != "alternative" || v.Term != "Alternative" || v.Title != "YAML" || v.Rank != "a0" ||
		len([]rune(why)) != 201 || !strings.HasSuffix(why, "…") || !slices.Equal(v.Truncated, []string{"why"}) ||
		v.Get != "auto plan get "+p1+" "+ids["a"] || v.Edges.In["rejects"] != 1 {
		t.Fatalf("describe = %+v", v)
	}
	text := mustRun(t, root, "describe", "001", ids["a"], "--text")
	if !strings.Contains(text, "truncated: why; full node: auto plan get "+p1+" "+ids["a"]) {
		t.Fatalf("describe --text must print the recovery command:\n%s", text)
	}
	expectFailure(t, root, "node-not-found", "describe", "001", "a-zzzz")
}

func TestGetIsFullFidelityWithNeighbours(t *testing.T) {
	root := repo(t)
	ids := lens(t, root)
	type row struct {
		Edge, ID, Type, Title, Status string
		Qualified                     bool
	}
	v := decode[struct {
		ID     string
		Fields map[string]any
		Out    []row
		In     []row
	}](t, mustRun(t, root, "get", "001", ids["a"]))
	if why, _ := v.Fields["why"].(string); why != strings.Repeat("merge conflicts everywhere ", 12) {
		t.Fatalf("get must return the full field, got %q", why)
	}
	if len(v.In) != 1 || v.In[0] != (row{Edge: "rejects", ID: ids["d"], Type: "decision", Title: "use JSON", Status: "active"}) {
		t.Fatalf("in = %+v", v.In)
	}
	ac := decode[struct{ Out, In []row }](t, mustRun(t, root, "get", "001", ids["ac"]))
	want := []row{
		{Edge: "proves", ID: ids["g1"], Type: "goal", Title: "First goal", Status: "active"},
		{Edge: "discharges", ID: "005-m3x9:r-8hw3", Qualified: true},
		{Edge: "discharges", ID: ids["r"], Type: "rail", Title: "No network calls", Status: "active"},
	}
	if !slices.Equal(ac.Out, want) || len(ac.In) != 2 {
		t.Fatalf("get ac out = %+v in = %+v", ac.Out, ac.In)
	}
	text := mustRun(t, root, "get", "001", ids["ac"], "--text")
	for _, s := range []string{"gwt ", "verify.cmd ", "go test ./...", "proves     → ◎ " + ids["g1"], "⇢ 005-m3x9:r-8hw3", "covers ← ▶ " + ids["s1"], "verify.tests  - TestA\n                - TestB\n"} {
		if !strings.Contains(text, s) {
			t.Errorf("get --text missing %q:\n%s", s, text)
		}
	}
	expectFailure(t, root, "node-not-found", "get", "001", "ac-zzzz")
}

func TestSearchIsCaseInsensitiveAcrossPlans(t *testing.T) {
	root := repo(t)
	ids := lens(t, root)
	mustRun(t, root, "new", "other", "--kind", "task")
	mustRun(t, root, "add", "002", "goal", "--title", "Another MERGE story")
	type match struct {
		Plan, ID, Type, Title string
		Fields                []string
	}
	r := decode[struct {
		Query   string
		Matches []match
	}](t, mustRun(t, root, "search", "all", "  Merge "))
	if r.Query != "merge" || len(r.Matches) != 2 || r.Matches[0].ID != ids["a"] || !slices.Equal(r.Matches[0].Fields, []string{"why"}) ||
		r.Matches[1].Plan != p2 || r.Matches[1].Type != "goal" {
		t.Fatalf("search all = %+v", r)
	}
	one := decode[struct{ Matches []match }](t, mustRun(t, root, "search", "001", "merge"))
	if len(one.Matches) != 1 {
		t.Fatalf("search 001 = %+v", one)
	}
	nested := decode[struct{ Matches []match }](t, mustRun(t, root, "search", "001", "GO TEST ./B"))
	if len(nested.Matches) != 1 || !slices.Equal(nested.Matches[0].Fields, []string{"verify.cmd"}) {
		t.Fatalf("object members are searched: %+v", nested)
	}
	if none := decode[struct{ Matches []match }](t, mustRun(t, root, "search", "001", "nothing-like-this")); none.Matches == nil || len(none.Matches) != 0 {
		t.Fatalf("no match is an empty list: %+v", none)
	}
	if text := mustRun(t, root, "search", "all", "merge", "--text"); !strings.HasPrefix(text, "2 matches for \"merge\"") {
		t.Fatalf("search --text:\n%s", text)
	}
	expectFailure(t, root, "usage", "search", "001", "   ")
}

func TestShowLadderWithBlocksAndLabels(t *testing.T) {
	root := repo(t)
	ids := lens(t, root)
	v := decode[struct {
		Goals []struct {
			Label     string
			Decisions []struct {
				Label        string
				Alternatives []struct{ Label, ID, Why string }
			}
		}
		Rails     []struct{ Label, ID string }
		Questions []struct{ Label, ID string }
	}](t, mustRun(t, root, "show", "001"))
	alts := v.Goals[0].Decisions[0].Alternatives
	if v.Goals[0].Label != "G1" || v.Goals[1].Label != "G2" || len(alts) != 1 || alts[0].ID != ids["a"] || alts[0].Label != "A1.1" ||
		!strings.HasSuffix(alts[0].Why, "…") || len(v.Rails) != 1 || v.Rails[0].ID != ids["r"] || len(v.Questions) != 1 {
		t.Fatalf("show = %+v", v)
	}
	text := mustRun(t, root, "show", "001", "--text")
	for _, s := range []string{"◎ G1 ", "✓ AC1.1  " + ids["ac"], "◆ D1 ", "    -   A1.1   " + ids["a"], "\nrails\n", "\nopen questions\n    ? Q1 "} {
		if !strings.Contains(text, s) {
			t.Errorf("show --text missing %q:\n%s", s, text)
		}
	}
}

func TestTraceCommand(t *testing.T) {
	root := repo(t)
	ids := lens(t, root)
	type step struct {
		Edge, Dir, ID, Type string
		Qualified           bool
		Children            []step
	}
	type traceOut struct {
		Direction string
		Root      step
		Up, Down  []step
	}
	up := decode[traceOut](t, mustRun(t, root, "trace", "001", ids["ac"], "--up"))
	if up.Direction != "up" || up.Root.ID != ids["ac"] || len(up.Down) != 0 || len(up.Up) != 3 || len(up.Up[0].Children) != 1 ||
		up.Up[0].Edge != "proves" || up.Up[0].ID != ids["g1"] || up.Up[0].Dir != "out" {
		t.Fatalf("trace --up = %+v", up)
	}
	down := decode[traceOut](t, mustRun(t, root, "trace", "001", ids["ac"], "--down"))
	if len(down.Down) != 2 || down.Down[0].Edge != "covers" || down.Down[0].ID != ids["s1"] ||
		len(down.Down[0].Children) != 1 || down.Down[0].Children[0].ID != ids["f1"] || down.Down[1].Edge != "about" {
		t.Fatalf("trace --down = %+v", down)
	}
	both := decode[traceOut](t, mustRun(t, root, "trace", "001", ids["f2"]))
	if both.Direction != "both" || len(both.Up) != 1 || both.Up[0].ID != ids["s2"] || len(both.Down) != 0 {
		t.Fatalf("trace (both) = %+v", both)
	}
	text := mustRun(t, root, "trace", "001", ids["g1"], "--down", "--text")
	for _, s := range []string{"◎ " + ids["g1"] + "  First goal\n", "\ndown\n", "  proves  ← ✓ " + ids["ac"], "    covers  ← ▶ " + ids["s1"], "      touches → □ " + ids["f1"]} {
		if !strings.Contains(text, s) {
			t.Errorf("trace --text missing %q:\n%s", s, text)
		}
	}
	expectFailure(t, root, "usage", "trace", "001", ids["ac"], "--up", "--down")
	expectFailure(t, root, "node-not-found", "trace", "001", "ac-zzzz")
}

func TestTreeCommand(t *testing.T) {
	root := repo(t)
	ids := lens(t, root)
	v := decode[struct {
		ID, Kind string
		About    []string
		Tree     struct {
			Lines []struct {
				Depth         int
				Name, Comment string
			}
		}
		Errors []any
	}](t, mustRun(t, root, "tree", "001", ids["t"]))
	if v.Kind != "call" || !slices.Equal(v.About, []string{ids["ac"]}) || len(v.Tree.Lines) != 2 || v.Errors == nil || len(v.Errors) != 0 {
		t.Fatalf("tree = %+v", v)
	}
	text := mustRun(t, root, "tree", "001", ids["t"], "--text")
	if !strings.Contains(text, "  check ✓ "+ids["ac"]+"  the AC\n") {
		t.Fatalf("tree --text annotates node IDs with their glyph:\n%s", text)
	}

	files := decode[struct {
		Files []struct{ ID, Path, Change string }
	}](t, mustRun(t, root, "tree", "001", "--files"))
	if len(files.Files) != 3 || files.Files[0].Path != "README.md" {
		t.Fatalf("tree --files = %+v", files)
	}
	want := "- README.md  " + ids["f3"] + "  gone\n~ pkg/\n+ ├── a.go   " + ids["f1"] + "  new\n~ └── b.go   " + ids["f2"] + "  changed\n"
	if got := mustRun(t, root, "tree", "001", "--files", "--text"); got != want {
		t.Fatalf("tree --files --text:\n%s\nwant:\n%s", got, want)
	}
	staged := mustRun(t, root, "tree", "001", "--files", "--stage", ids["s2"], "--text")
	if strings.Contains(staged, "a.go") || !strings.Contains(staged, "b.go") || !strings.Contains(staged, "README.md") {
		t.Fatalf("--stage shows only that stage's files:\n%s", staged)
	}

	expectFailure(t, root, "usage", "tree", "001")
	expectFailure(t, root, "usage", "tree", "001", ids["t"], "--files")
	expectFailure(t, root, "usage", "tree", "001", ids["t"], "--stage", ids["s1"])
	expectFailure(t, root, "wrong-type", "tree", "001", ids["ac"])
	expectFailure(t, root, "wrong-type", "tree", "001", "--files", "--stage", ids["ac"])
	expectFailure(t, root, "node-not-found", "tree", "001", "t-zzzz")

	// A broken body still renders best effort, with the errors on stderr.
	path := filepath.Join(root, "docs", "plans", "001-demo", "graph.json")
	broken := strings.Replace(string(readGraph(t, root, "001-demo")), `"run\n  check`, `"run\n   check`, 1)
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, root, "tree", "001", ids["t"])
	if code != 1 || !strings.Contains(stdout, `"errors": [`) {
		t.Fatalf("broken tree: exit %d stdout %s", code, stdout)
	}
	if f := decode[failure](t, stderr); len(f.Errors) != 1 || f.Errors[0].Code != "tree-syntax" {
		t.Fatalf("broken tree stderr = %s", stderr)
	}
}

func TestBriefCommand(t *testing.T) {
	root := repo(t)
	ids := lens(t, root)
	v := decode[struct {
		Stage struct {
			ID, Status, Commit string
			Steps              []string
		}
		DependsOn []struct{ ID, Title, Status string }
		Files     []struct{ ID string }
		ACs       []struct {
			ID     string
			Verify struct{ Cmd string }
		} `json:"acs"`
		Decisions []struct{ ID string }
		Rails     []struct {
			ID        string
			Qualified bool
		}
		Questions []struct{ ID string }
	}](t, mustRun(t, root, "brief", "001", ids["s2"]))
	if v.Stage.ID != ids["s2"] || v.Stage.Status != "todo" || v.Stage.Commit != "feat: two" || len(v.DependsOn) != 1 ||
		v.DependsOn[0].ID != ids["s1"] || v.DependsOn[0].Status != "done" || len(v.Files) != 2 || len(v.ACs) != 1 ||
		v.ACs[0].ID != ids["ac2"] || v.ACs[0].Verify.Cmd != "go test ./b" || len(v.Decisions) != 0 || len(v.Rails) != 0 || len(v.Questions) != 1 {
		t.Fatalf("brief s2 = %+v", v)
	}
	first := mustRun(t, root, "brief", "001", ids["s1"], "--text")
	for _, s := range []string{"# Stage " + ids["s1"] + ": First stage\n", "1. SECRET-STEP-ONE", "`feat: one`", "+ └── a.go", "### " + ids["ac"] + ": it works",
		"- Verify: `go test ./...`", "### " + ids["d"] + ": use JSON", "- 005-m3x9:r-8hw3: a rail in plan 005-m3x9", "- " + ids["r"] + ": No network calls", "- " + ids["q"] + ": Ship it behind a flag?"} {
		if !strings.Contains(first, s) {
			t.Errorf("brief --text missing %q:\n%s", s, first)
		}
	}
	second := mustRun(t, root, "brief", "001", ids["s2"], "--text")
	if strings.Contains(second, "SECRET-STEP-ONE") || strings.Contains(second, "feat: one") || !strings.Contains(second, "- "+ids["s1"]+": First stage (done)") {
		t.Fatalf("a brief names other stages by ID, title and status only:\n%s", second)
	}
	expectFailure(t, root, "wrong-type", "brief", "001", ids["ac"])
	expectFailure(t, root, "node-not-found", "brief", "001", "s-zzzz")
}

// TestEpicFamilyAcrossPlans: new --epic, cross-plan writes resolved against
// the target plan (rejected byte-identically when they do not), lint all over
// the family, and the views following qualified references (AC-5, AC-11,
// AC-13, AC-14).
func TestEpicFamilyAcrossPlans(t *testing.T) {
	root := repo(t)
	id := func(args ...string) string {
		t.Helper()
		return decode[struct{ ID string }](t, mustRun(t, root, args...)).ID
	}
	id("new", "mail-mvp", "--kind", "epic")
	eg := id("add", "001", "goal", "--title", "Agents exchange durable mail")
	rail := id("add", "001", "rail", "--title", "No network calls")
	j := id("add", "001", "journey", "--title", "Send and ack")
	leg := id("add", "001", "leg", "--actor", "agent", "--action", "sends mail", "--in", j)

	created := decode[map[string]string](t, mustRun(t, root, "new", "walking-skeleton", "--kind", "task", "--epic", "001"))
	if created["id"] != p2 || created["number"] != "002" || created["epic"] != p1 {
		t.Fatalf("new --epic = %+v", created)
	}
	if !strings.Contains(string(readGraph(t, root, "002-walking-skeleton")), `"epic": "`+p1+`"`) {
		t.Fatal("new --epic records plan.fields.epic")
	}
	expectFailure(t, root, "epic-not-found", "new", "x", "--kind", "task", "--epic", "009")
	expectFailure(t, root, "epic-not-found", "new", "x", "--kind", "task", "--epic", "002")
	expectFailure(t, root, "usage", "new", "x", "--kind", "epic", "--epic", "001")
	expectFailure(t, root, "invalid-field", "new", "x", "--kind", "task", "--epic", "1")
	if _, err := os.Stat(filepath.Join(root, "docs", "plans", "003-x")); !os.IsNotExist(err) {
		t.Fatal("a rejected new must create nothing")
	}
	child := id("add", "001", "child", "--plan", "002", "--title", "walking skeleton")

	// Cross-plan writes resolve their target, or change nothing.
	before := readGraph(t, root, "002-walking-skeleton")
	expectFailure(t, root, "dangling-ref", "link", "002", "plan", "honors", "009:r-8hw3")
	expectFailure(t, root, "dangling-ref", "link", "002", "plan", "honors", "001:r-zzzz")
	expectFailure(t, root, "wrong-endpoint", "link", "002", "plan", "honors", "001:"+eg)
	if !bytes.Equal(before, readGraph(t, root, "002-walking-skeleton")) {
		t.Fatal("rejected cross-plan links must leave graph.json byte-identical")
	}
	g := id("add", "002", "goal", "--title", "round trip")
	before = readGraph(t, root, "002-walking-skeleton")
	expectFailure(t, root, "dangling-ref", "add", "002", "ac", "--proves", g, "--title", "t", "--gwt", "x", "--discharges", "001:r-zzzz")
	expectFailure(t, root, "epic-not-found", "update", "002", "plan", "--epic", "009")
	if !bytes.Equal(before, readGraph(t, root, "002-walking-skeleton")) {
		t.Fatal("rejected cross-plan writes must leave graph.json byte-identical")
	}
	mustRun(t, root, "link", "002", "plan", "honors", "001:"+rail)
	mustRun(t, root, "link", "002", "plan", "delivers", "001:"+leg)
	ac := id("add", "002", "ac", "--proves", g, "--title", "read back", "--gwt", "x", "--verify-cmd", "go test ./...", "--discharges", "001:"+rail)

	type report struct {
		Plan   string
		OK     bool
		Issues []struct{ Code, Severity string }
	}
	errorsOf := func(r report) []string {
		var out []string
		for _, is := range r.Issues {
			if is.Severity == "error" {
				out = append(out, is.Code)
			}
		}
		return out
	}
	mustRun(t, root, "update", "001", "plan", "--lifecycle", "plan")
	mustRun(t, root, "update", "002", "plan", "--lifecycle", "solution")
	all := decode[struct {
		OK    bool
		Plans []report
	}](t, mustRun(t, root, "lint", "all"))
	if !all.OK || len(all.Plans) != 2 {
		t.Fatalf("lint all over a clean family = %+v", all)
	}

	// Views follow the qualified references.
	show := decode[struct {
		Journeys []struct {
			Legs []struct{ DeliveredBy []string }
		}
		Children []struct{ Plan, Lifecycle string }
	}](t, mustRun(t, root, "show", "001"))
	if !slices.Equal(show.Journeys[0].Legs[0].DeliveredBy, []string{p2}) || show.Children[0].Lifecycle != "solution" {
		t.Fatalf("epic show = %+v", show)
	}
	if text := mustRun(t, root, "show", "001", "--text"); !strings.Contains(text, "\nchild plans\n") || !strings.Contains(text, "delivered by "+p2) {
		t.Fatalf("epic show --text:\n%s", text)
	}
	trace := mustRun(t, root, "trace", "002", ac, "--up", "--text")
	for _, s := range []string{"child-of   → ◎ " + p1 + ":" + eg + "  Agents exchange durable mail  (via " + p1 + ":" + child + ")", "discharges → ‖ " + p1 + ":" + rail + "  No network calls"} {
		if !strings.Contains(trace, s) {
			t.Errorf("trace --up missing %q:\n%s", s, trace)
		}
	}
	type row struct {
		Edge, ID, Type, Title string
		Qualified             bool
	}
	card := decode[struct{ In []row }](t, mustRun(t, root, "get", "001", rail))
	if len(card.In) != 2 || card.In[1] != (row{Edge: "honors", ID: p2 + ":plan", Type: "plan", Title: "walking-skeleton", Qualified: true}) {
		t.Fatalf("get rail in = %+v", card.In)
	}

	// Breaking the family is caught on the plan that must change.
	mustRun(t, root, "unlink", "002", "plan", "delivers", "001:"+leg)
	stdout, _, code := runCLI(t, root, "lint", "001")
	if r := decode[report](t, stdout); code != 1 || !slices.Equal(errorsOf(r), []string{"leg-undelivered"}) {
		t.Fatalf("lint 001 = exit %d %+v", code, r)
	}
	mustRun(t, root, "retire", "001", child)
	stdout, _, code = runCLI(t, root, "lint", "002")
	if r := decode[report](t, stdout); code != 1 || !slices.Equal(errorsOf(r), []string{"child-epic-mismatch"}) {
		t.Fatalf("lint 002 = exit %d %+v", code, r)
	}
}

// TestDocsCoversEveryRegisteredType asserts the reference is generated from
// the registry: every node type (with its fields) and every edge type (with
// its endpoints and cross-plan flag) appears, as does every command.
func TestDocsCoversEveryRegisteredType(t *testing.T) {
	dir := repo(t)
	out := mustRun(t, dir, "docs")
	for _, n := range schema.Registry.Nodes {
		if !strings.Contains(out, "### "+n.Name+" — "+n.Term+"\n") {
			t.Errorf("docs lacks node type %s", n.Name)
		}
		for _, f := range n.Fields {
			if !strings.Contains(out, "| `"+f.Name+"` | "+string(f.Kind)+" |") {
				t.Errorf("docs lacks field %s.%s", n.Name, f.Name)
			}
			for _, m := range f.Fields {
				if !strings.Contains(out, "| `"+f.Name+"."+m.Name+"` |") {
					t.Errorf("docs lacks field %s.%s.%s", n.Name, f.Name, m.Name)
				}
			}
		}
	}
	for _, e := range schema.Registry.Edges {
		cross := "no"
		if e.CrossPlan {
			cross = "yes"
		}
		row := "| `" + e.Name + "` | "
		i := strings.Index(out, row)
		if i < 0 {
			t.Errorf("docs lacks edge type %s", e.Name)
			continue
		}
		line := out[i : i+strings.IndexByte(out[i:], '\n')]
		if !strings.Contains(line, strings.ReplaceAll(e.ToLabel(), "|", `\|`)) || !strings.Contains(line, "| "+cross+" |") {
			t.Errorf("docs row for %s lacks its endpoints or cross-plan flag: %s", e.Name, line)
		}
	}
	for _, verb := range []string{"init", "new", "add", "update", "link", "unlink", "retire", "move", "renumber", "lint", "fmt",
		"list", "describe", "get", "search", "show", "trace", "tree", "brief", "quickstart", "docs"} {
		if !strings.Contains(out, "- `auto plan "+verb) {
			t.Errorf("docs lacks command %s", verb)
		}
	}
}

func TestQuickstartNamesTheHappyPath(t *testing.T) {
	out := mustRun(t, t.TempDir(), "quickstart")
	if strings.TrimSpace(out) == "" {
		t.Fatal("quickstart is empty")
	}
	for _, verb := range []string{"list", "init", "new", "add", "link", "unlink", "lint", "show", "brief", "renumber"} {
		if !strings.Contains(out, "auto plan "+verb+" ") {
			t.Errorf("quickstart does not show auto plan %s", verb)
		}
	}
}

// planID reads the top-level plan ID of a plan folder's graph.json.
func planID(t *testing.T, root, folder string) string {
	t.Helper()
	return decode[struct{ ID string }](t, string(readGraph(t, root, folder))).ID
}

// collide simulates a merge in which two branches each created plan NNN:
// it moves folder from to number to, and rewrites its plan ID to match, as
// the other branch's `new` would have written it.
func collide(t *testing.T, root, from, to string) string {
	t.Helper()
	plans := filepath.Join(root, "docs", "plans")
	dest := to + from[3:]
	if err := os.Rename(filepath.Join(plans, from), filepath.Join(plans, dest)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(plans, dest, "graph.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"id": "`+from[:3]+"-"), []byte(`"id": "`+to+"-"), 1)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dest
}

// TestPlanIDsAndArguments: new generates NNN-xxxx; every plan argument form
// resolves; a bare number shared by two folders is ambiguous.
func TestPlanIDsAndArguments(t *testing.T) {
	root := repo(t)
	mustRun(t, root, "init")
	created := decode[map[string]string](t, mustRun(t, root, "new", "demo", "--kind", "task"))
	if created["id"] != p1 || created["number"] != "001" || planID(t, root, "001-demo") != p1 {
		t.Fatalf("new = %v", created)
	}
	text := mustRun(t, root, "new", "other", "--kind", "task", "--text")
	if text != "created plan "+p2+" in docs/plans/002-other (task, requirements)\n" {
		t.Fatalf("new --text = %q", text)
	}
	for _, arg := range []string{"001", p1, "001-demo", "docs/plans/001-demo", "docs/plans/001-demo/graph.json"} {
		if got := decode[report](t, mustRun(t, root, "lint", arg)); got.Plan != p1 {
			t.Errorf("lint %s names %s", arg, got.Plan)
		}
	}

	mustRun(t, root, "new", "gamma", "--kind", "task")
	gamma := collide(t, root, "003-gamma", "002")
	_, stderr, code := runCLI(t, root, "show", "002")
	f := decode[failure](t, stderr)
	if code != 1 || f.Errors[0].Code != "ambiguous-plan" || !strings.Contains(f.Errors[0].Message, "002-gamma") ||
		!strings.Contains(f.Errors[0].Message, "002-other") || !strings.Contains(f.Hint, "auto plan renumber") {
		t.Fatalf("show 002: exit %d %s", code, stderr)
	}
	// The plan ID and the folder still name each plan.
	stdout, _, _ := runCLI(t, root, "lint", gamma)
	if got := decode[report](t, stdout); got.Plan != planID(t, root, gamma) || got.OK {
		t.Errorf("lint by folder = %+v", got)
	}
}

// TestShorthandExpansion: NNN and NNN:ID are accepted on input and stored as
// the plan ID; a number two plans share is refused with ambiguous-plan.
func TestShorthandExpansion(t *testing.T) {
	root := repo(t)
	id := func(args ...string) string {
		t.Helper()
		return decode[struct{ ID string }](t, mustRun(t, root, args...)).ID
	}
	id("new", "epic", "--kind", "epic")
	rail := id("add", "001", "rail", "--title", "No network calls")
	mustRun(t, root, "new", "child", "--kind", "task", "--epic", "001")
	id("add", "001", "child", "--plan", "002", "--title", "the child")
	mustRun(t, root, "update", "001", rail, "--deferred", "002")
	l := decode[struct {
		Edges []struct{ ID, To string }
	}](t, mustRun(t, root, "link", "002", "plan", "honors", "001:"+rail))
	if len(l.Edges) != 1 || l.Edges[0].To != p1+":"+rail || !graph.EdgeIDPattern.MatchString(l.Edges[0].ID) {
		t.Fatalf("link = %+v", l)
	}
	epic, child := string(readGraph(t, root, "001-epic")), string(readGraph(t, root, "002-child"))
	if !strings.Contains(epic, `"plan": "`+p2+`"`) || strings.Count(epic, `"`+p2+`"`) != 2 { // child.plan, rail.deferred
		t.Errorf("epic does not name %s twice:\n%s", p2, epic)
	}
	for _, want := range []string{`"epic": "` + p1 + `"`, `"to": "` + p1 + ":" + rail + `"`} {
		if !strings.Contains(child, want) {
			t.Errorf("child lacks %q:\n%s", want, child)
		}
	}
	if strings.Contains(child, `"001:`) || strings.Contains(epic, `"002"`) {
		t.Error("a bare number was stored")
	}
	// unlink accepts the shorthand too.
	mustRun(t, root, "unlink", "002", "plan", "honors", "001:"+rail)

	mustRun(t, root, "new", "other", "--kind", "task")
	collide(t, root, "003-other", "002")
	before := readGraph(t, root, "001-epic")
	expectFailure(t, root, "ambiguous-plan", "add", "001", "child", "--plan", "002")
	expectFailure(t, root, "ambiguous-plan", "update", "001", rail, "--deferred", "002")
	expectFailure(t, root, "ambiguous-plan", "link", p2, "plan", "honors", "002:plan")
	expectFailure(t, root, "plan-not-found", "add", "001", "child", "--plan", "009")
	expectFailure(t, root, "dangling-ref", "link", p2, "plan", "honors", "009:r-zzzz")
	if !bytes.Equal(before, readGraph(t, root, "001-epic")) {
		t.Fatal("refused writes changed the epic")
	}
}

// TestRenumber: lint all reports the collision; renumber moves one plan to
// the next free number and rewrites every reference to it across the epic
// (child node, deferred list, prose) and the plan itself; lint all is clean
// again. A frozen referrer refuses the whole operation.
func TestRenumber(t *testing.T) {
	root := repo(t)
	id := func(args ...string) string {
		t.Helper()
		return decode[struct{ ID string }](t, mustRun(t, root, args...)).ID
	}
	id("new", "epic", "--kind", "epic")
	rail := id("add", "001", "rail", "--title", "No network calls")
	mustRun(t, root, "new", "base", "--kind", "task", "--epic", "001")
	mustRun(t, root, "new", "late", "--kind", "task", "--epic", "001")
	id("add", "001", "child", "--plan", "002", "--title", "base")
	id("add", "001", "child", "--plan", "003", "--title", "late")
	mustRun(t, root, "link", "002", "plan", "honors", "001:"+rail)
	mustRun(t, root, "update", "001", rail, "--deferred", "003", "--description", "late is [["+p3+":plan]]")

	late := collide(t, root, "003-late", "002")
	lateID := planID(t, root, late)
	// The epic still names the moved plan by its old ID: rewrite it to match,
	// as the other branch's epic would have.
	epicPath := filepath.Join(root, "docs", "plans", "001-epic", "graph.json")
	data := bytes.ReplaceAll(readGraph(t, root, "001-epic"), []byte(p3), []byte(lateID))
	if err := os.WriteFile(epicPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runCLI(t, root, "lint", "all")
	all := decode[struct {
		OK    bool
		Plans []report
	}](t, stdout)
	dups := 0
	for _, r := range all.Plans {
		for _, is := range r.Issues {
			if is.Code == "duplicate-plan-number" {
				dups++
				if !strings.Contains(is.Message, p2) || !strings.Contains(is.Message, lateID) || is.Hint != "auto plan renumber "+r.Plan+" (moves it to the next free number and rewrites every reference to it)" {
					t.Errorf("duplicate-plan-number = %+v", is)
				}
			}
		}
	}
	if code != 1 || all.OK || dups != 2 {
		t.Fatalf("lint all: exit %d %+v", code, all)
	}

	// A frozen referrer refuses the whole operation and changes nothing.
	mustRun(t, root, "update", "001", "plan", "--lifecycle", "done")
	snapshot := map[string][]byte{"001-epic": readGraph(t, root, "001-epic"), late: readGraph(t, root, late)}
	expectFailure(t, root, "frozen-ref", "renumber", lateID)
	for folder, want := range snapshot {
		if !bytes.Equal(want, readGraph(t, root, folder)) {
			t.Fatalf("refused renumber changed %s", folder)
		}
	}
	// Unfreeze by hand (as a test fixture only) and renumber for real.
	if err := os.WriteFile(epicPath, bytes.Replace(snapshot["001-epic"], []byte(`"lifecycle": "done"`), []byte(`"lifecycle": "requirements"`), 1), 0o644); err != nil {
		t.Fatal(err)
	}

	out := decode[struct {
		Plan, ID, From, To, Path string
		Rewritten                []string
	}](t, mustRun(t, root, "renumber", lateID))
	newID := "003" + lateID[3:]
	if out.Plan != newID || out.ID != "plan" || out.From != lateID || out.To != newID || out.Path != "docs/plans/003-late" ||
		!slices.Equal(out.Rewritten, []string{"docs/plans/001-epic/graph.json", "docs/plans/003-late/graph.json"}) {
		t.Fatalf("renumber = %+v", out)
	}
	if planID(t, root, "003-late") != newID {
		t.Fatal("plan ID not rewritten")
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "plans", late)); !os.IsNotExist(err) {
		t.Fatal("old folder still exists")
	}
	epic := string(readGraph(t, root, "001-epic"))
	if strings.Contains(epic, lateID) || strings.Count(epic, newID) != 3 { // child.plan, deferred, prose
		t.Fatalf("epic refs not rewritten:\n%s", epic)
	}
	if r := decode[struct{ OK bool }](t, mustRun(t, root, "lint", "all")); !r.OK {
		t.Fatalf("lint all after renumber = %+v", r)
	}

	expectFailure(t, root, "plan-number-taken", "renumber", newID, "--to", "002")
	expectFailure(t, root, "usage", "renumber", newID, "--to", "003")
	expectFailure(t, root, "usage", "renumber", newID, "--to", "3")
	text := mustRun(t, root, "renumber", newID, "--to", "007", "--text")
	if !strings.HasPrefix(text, "renumbered "+newID+" → 007"+newID[3:]+" (docs/plans/003-late → docs/plans/007-late)\n") {
		t.Fatalf("renumber --text:\n%s", text)
	}
}

// TestRenumberFixesIDMismatch: --to the folder's own number rewrites only
// the ID (the fix plan-id-mismatch suggests).
func TestRenumberFixesIDMismatch(t *testing.T) {
	root := repo(t)
	mustRun(t, root, "new", "demo", "--kind", "task")
	path := filepath.Join(root, "docs", "plans", "001-demo", "graph.json")
	if err := os.WriteFile(path, bytes.Replace(readGraph(t, root, "001-demo"), []byte(`"`+p1+`"`), []byte(`"009`+p1[3:]+`"`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	r := decode[report](t, func() string { s, _, _ := runCLI(t, root, "lint", "001"); return s }())
	if r.OK || r.Issues[0].Code != "plan-id-mismatch" || !strings.Contains(r.Issues[0].Hint, "auto plan renumber 001-demo --to 001") {
		t.Fatalf("lint = %+v", r)
	}
	mustRun(t, root, "renumber", "001-demo", "--to", "001")
	if planID(t, root, "001-demo") != p1 {
		t.Fatal("ID not fixed")
	}
}

// TestEdgeIDs: add and link return edge IDs, get shows them, and unlink
// takes one.
func TestEdgeIDs(t *testing.T) {
	root := repo(t)
	g1, _, ac, d := walk(t, root)
	type edges struct {
		Edges []struct{ ID, From, Type, To string }
	}
	add := decode[edges](t, mustRun(t, root, "add", "001", "ac", "--proves", g1, "--title", "t", "--gwt", "x"))
	if len(add.Edges) != 1 || !graph.EdgeIDPattern.MatchString(add.Edges[0].ID) {
		t.Fatalf("add edges = %+v", add)
	}
	card := decode[struct {
		Out []struct {
			EdgeID string `json:"edge_id"`
			Edge   string
		}
	}](t, mustRun(t, root, "get", "001", ac))
	if len(card.Out) != 1 || !graph.EdgeIDPattern.MatchString(card.Out[0].EdgeID) {
		t.Fatalf("get out = %+v", card.Out)
	}
	if text := mustRun(t, root, "get", "001", ac, "--text"); !strings.Contains(text, card.Out[0].EdgeID+"  proves") {
		t.Fatalf("get --text lacks the edge ID:\n%s", text)
	}
	constrains := decode[struct {
		In []struct {
			EdgeID string `json:"edge_id"`
			Edge   string
		}
	}](t, mustRun(t, root, "get", "001", g1))
	eid := ""
	for _, r := range constrains.In {
		if r.Edge == "constrains" {
			eid = r.EdgeID
		}
	}
	removed := decode[struct {
		ID      string
		Removed []struct{ ID, From, Type, To string }
	}](t, mustRun(t, root, "unlink", "001", eid))
	if removed.ID != d || len(removed.Removed) != 1 || removed.Removed[0].ID != eid || removed.Removed[0].To != g1 {
		t.Fatalf("unlink by ID = %+v", removed)
	}
	expectFailure(t, root, "edge-not-found", "unlink", "001", eid)
	if _, stderr, code := runCLI(t, root, "unlink", "001", "a", "b"); code != 1 || !strings.Contains(stderr, "<edge-id>") {
		t.Fatalf("three arguments: exit %d %s", code, stderr)
	}
}

// TestVersionsAndFreezing: reads accept any version (another major best
// effort, with a warning); writes are refused for another major, a newer
// version, and a done plan. Setting done is allowed; fmt --check still works.
func TestVersionsAndFreezing(t *testing.T) {
	root := repo(t)
	walk(t, root)
	path := filepath.Join(root, "docs", "plans", "001-demo", "graph.json")
	orig := readGraph(t, root, "001-demo")
	setVersion := func(v string) {
		t.Helper()
		if err := os.WriteFile(path, bytes.Replace(orig, []byte(`"version": "1.0.0"`), []byte(`"version": "`+v+`"`), 1), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"2.0.0", "1.1.0"} {
		setVersion(v)
		r := decode[report](t, mustRun(t, root, "lint", "001"))
		if r.Issues[0].Code != "other-version" || r.Issues[0].Severity != "warning" || !r.OK {
			t.Errorf("lint %s = %+v", v, r)
		}
		mustRun(t, root, "show", "001")
		mustRun(t, root, "fmt", "001", "--check")
		expectFailure(t, root, "frozen", "add", "001", "goal", "--title", "t")
		expectFailure(t, root, "frozen", "update", "001", "plan", "--lifecycle", "solution")
		rows := decode[struct {
			Plans []struct {
				Frozen       bool
				FrozenReason string `json:"frozen_reason"`
				Version      string
			}
		}](t, mustRun(t, root, "list"))
		if !rows.Plans[0].Frozen || rows.Plans[0].FrozenReason != "version" || rows.Plans[0].Version != v {
			t.Errorf("list %s = %+v", v, rows)
		}
	}
	setVersion("1.0.0")
	mustRun(t, root, "update", "001", "plan", "--lifecycle", "done")
	for _, args := range [][]string{
		{"add", "001", "goal", "--title", "t"}, {"update", "001", "plan", "--lifecycle", "plan"},
		{"link", "001", "a", "b", "c"}, {"unlink", "001", "e-0000"}, {"retire", "001", "g-0000"},
		{"move", "001", "g-0000", "--before", "g-0001"}, {"renumber", "001"},
	} {
		expectFailure(t, root, "frozen", args...)
	}
	mustRun(t, root, "fmt", "001", "--check")
	mustRun(t, root, "fmt", "001") // canonical: nothing to rewrite
	done := readGraph(t, root, "001-demo")
	if err := os.WriteFile(path, append(bytes.TrimRight(done, "\n"), "\n\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runCLI(t, root, "fmt", "001")
	if r := decode[fmtOut](t, stdout); code != 1 || len(r.Errors) != 1 || r.Errors[0].Code != "frozen" {
		t.Fatalf("fmt of a non-canonical frozen plan: exit %d %+v", code, r)
	}
}

type planRow struct {
	ID, Number, Name, Path, Kind, Lifecycle, Version string
	Frozen                                           bool
	FrozenReason                                     string `json:"frozen_reason"`
	Epic                                             string
}

// TestListPlans: list with no argument lists the plans themselves.
func TestListPlans(t *testing.T) {
	root := repo(t)
	if out := decode[struct{ Plans []planRow }](t, mustRun(t, root, "list")); out.Plans == nil || len(out.Plans) != 0 {
		t.Fatalf("no plans = %+v", out)
	}
	mustRun(t, root, "new", "epic", "--kind", "epic")
	mustRun(t, root, "new", "child", "--kind", "task", "--epic", "001")
	mustRun(t, root, "new", "finished", "--kind", "task")
	mustRun(t, root, "update", "003", "plan", "--lifecycle", "done")

	rows := decode[struct{ Plans []planRow }](t, mustRun(t, root, "list")).Plans
	want := []planRow{
		{ID: p1, Number: "001", Name: "epic", Path: "docs/plans/001-epic", Kind: "epic", Lifecycle: "requirements", Version: "1.0.0"},
		{ID: p2, Number: "002", Name: "child", Path: "docs/plans/002-child", Kind: "task", Lifecycle: "requirements", Version: "1.0.0", Epic: p1},
		{ID: p3, Number: "003", Name: "finished", Path: "docs/plans/003-finished", Kind: "task", Lifecycle: "done", Version: "1.0.0", Frozen: true, FrozenReason: "done"},
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("list =\n%+v\nwant\n%+v", rows, want)
	}
	if rows := decode[struct{ Plans []planRow }](t, mustRun(t, root, "list", "--kind", "  EPIC ")).Plans; len(rows) != 1 || rows[0].ID != p1 {
		t.Fatalf("--kind epic = %+v", rows)
	}
	expectFailure(t, root, "invalid-kind", "list", "--kind", "story")
	expectFailure(t, root, "usage", "list", "--type", "ac")
	expectFailure(t, root, "usage", "list", "001", "--kind", "task")

	text := mustRun(t, root, "list", "--text")
	wantText := p1 + "  epic      epic · requirements\n" +
		p2 + "  child     task · requirements  epic " + p1 + "\n" +
		p3 + "  finished  task · done          [frozen: done]\n"
	if text != wantText {
		t.Fatalf("list --text:\n%s\nwant:\n%s", text, wantText)
	}

	// A malformed plan and a number collision: every readable plan is still
	// listed, the problems go to stderr, exit 1.
	mustRun(t, root, "new", "broken", "--kind", "task")
	if err := os.WriteFile(filepath.Join(root, "docs", "plans", "004-broken", "graph.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, root, "new", "dup", "--kind", "task")
	collide(t, root, "005-dup", "002")
	stdout, stderr, code := runCLI(t, root, "list")
	rows = decode[struct{ Plans []planRow }](t, stdout).Plans
	f := decode[failure](t, stderr)
	var errCodes []string
	for _, e := range f.Errors {
		errCodes = append(errCodes, e.Code)
	}
	if code != 1 || len(rows) != 4 || rows[1].Number != "002" || rows[2].Number != "002" ||
		!slices.Contains(errCodes, "parse-error") || !slices.Contains(errCodes, "duplicate-plan-number") {
		t.Fatalf("list with problems: exit %d rows %+v stderr %s", code, rows, stderr)
	}
}
