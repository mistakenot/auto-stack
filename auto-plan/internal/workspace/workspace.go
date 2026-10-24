// Package workspace locates plans on disk: the repo root, the .auto/plan/plans/
// folder, the next plan number, and resolution of a plan argument
// (`NNN`, `NNN-xxxx`, `NNN-name`, a path, or `all`), and the PlanSet that
// resolves qualified cross-plan references (`005-k7q2:r-8hw3`).
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-shared/git"
)

// PlansDir is the plans root, relative to the repo root (D-1).
const PlansDir = ".auto/plan/plans"

// GraphFile is the one source-of-truth file in each plan folder (D-2).
const GraphFile = "graph.json"

// All is the plan argument that targets every plan.
const All = "all"

var (
	folderRE = regexp.MustCompile(`^([0-9]{3})-([a-z0-9]+(?:-[a-z0-9]+)*)$`)
	nameRE   = regexp.MustCompile(schema.NamePattern)
	numberRE = regexp.MustCompile(schema.PlanNumberPattern)
)

// ErrNotFound is returned when a plan argument matches no plan.
var ErrNotFound = errors.New("plan not found")

// ErrAmbiguous is returned (wrapped in an *AmbiguousError) when a plan
// number names more than one plan folder — two branches each created plan
// NNN. `auto plan renumber` resolves it.
var ErrAmbiguous = errors.New("ambiguous plan")

// AmbiguousError names the plans an ambiguous argument matched.
type AmbiguousError struct {
	Arg        string
	Candidates []Plan
}

func (e *AmbiguousError) Error() string {
	names := make([]string, len(e.Candidates))
	for i, p := range e.Candidates {
		names[i] = p.Ref() + " (" + p.Dir + ")"
	}
	return fmt.Sprintf("plan %s is ambiguous: it names %s; use the plan ID", e.Arg, strings.Join(names, ", "))
}

func (e *AmbiguousError) Unwrap() error { return ErrAmbiguous }

// Workspace is a repo's plan root.
type Workspace struct {
	// Root is the absolute repo root.
	Root string
	// CWD is the directory relative paths are resolved from.
	CWD string
}

// Plan is one plan folder.
type Plan struct {
	// Number is the folder's 3-digit number (`004`).
	Number string `json:"number"`
	// ID is the plan ID recorded in graph.json (`004-k7q2`), or "" when the
	// file cannot be read or holds no valid ID.
	ID string `json:"id"`
	// Name is the kebab-case name from the folder (`stage-briefs`).
	Name string `json:"name"`
	// Dir is the folder relative to the repo root (`.auto/plan/plans/004-stage-briefs`).
	Dir string `json:"path"`
}

// Folder returns the plan's folder name (`004-stage-briefs`).
func (p Plan) Folder() string { return p.Number + "-" + p.Name }

// Ref is how the plan is named in output, hints and qualified references:
// its plan ID, or its folder name when graph.json holds no valid ID.
func (p Plan) Ref() string {
	if graph.PlanIDPattern.MatchString(p.ID) {
		return p.ID
	}
	return p.Folder()
}

// Open locates the repo containing cwd.
func Open(cwd string) (*Workspace, error) {
	root, err := git.RepoRoot(cwd)
	if err != nil {
		return nil, fmt.Errorf("not inside a git repository (%s): auto plan keeps plans under <repo>/%s", cwd, PlansDir)
	}
	return &Workspace{Root: root, CWD: cwd}, nil
}

// PlansPath returns the absolute .auto/plan/plans directory.
func (w *Workspace) PlansPath() string { return filepath.Join(w.Root, filepath.FromSlash(PlansDir)) }

// Abs returns the absolute path of a repo-relative path.
func (w *Workspace) Abs(rel string) string { return filepath.Join(w.Root, filepath.FromSlash(rel)) }

// GraphPath returns the absolute path of a plan's graph.json.
func (w *Workspace) GraphPath(p Plan) string { return filepath.Join(w.Abs(p.Dir), GraphFile) }

// FolderPath returns the absolute path of a plan's folder — where its
// graph.json and its annex Markdown files live.
func (w *Workspace) FolderPath(p Plan) string { return w.Abs(p.Dir) }

// FolderFS opens a plan's folder as a read-only file system rooted at the
// folder, so an annex's flat `<kebab>.md` path resolves directly against it
// (fs.Stat, fs.ReadFile). Lint reads annex files through this, never off the
// raw filesystem, so a test can hand it an fstest.MapFS instead.
func (w *Workspace) FolderFS(p Plan) fs.FS { return os.DirFS(w.FolderPath(p)) }

// MarkdownFiles lists the `*.md` file names directly in fsys — a plan folder
// opened with FolderFS — sorted. It never descends into subdirectories.
// graph.json is not Markdown so it never appears, and AGENTS.md/CLAUDE.md live
// in the parent plans/ dir, not a plan folder: a plan folder's only `.md`
// files are its annexes.
func MarkdownFiles(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, e.Name())
		}
	}
	slices.Sort(out)
	return out, nil
}

// Plans lists every plan folder, sorted by folder name, with the plan ID
// each graph.json records. A missing .auto/plan/plans/ is an empty list. Entries
// that are not NNN-name folders are ignored.
func (w *Workspace) Plans() ([]Plan, error) {
	entries, err := os.ReadDir(w.PlansPath())
	if errors.Is(err, os.ErrNotExist) {
		return []Plan{}, nil
	}
	if err != nil {
		return nil, err
	}
	plans := []Plan{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := folderRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		p := Plan{Number: m[1], Name: m[2], Dir: PlansDir + "/" + e.Name()}
		p.ID = readPlanID(w.GraphPath(p))
		plans = append(plans, p)
	}
	slices.SortFunc(plans, func(a, b Plan) int { return strings.Compare(a.Folder(), b.Folder()) })
	return plans, nil
}

// readPlanID reads the top-level "id" of a graph.json, or "" when the file
// cannot be read, is not JSON, or holds no valid plan ID.
func readPlanID(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var top struct {
		ID any `json:"id"`
	}
	if json.Unmarshal(data, &top) != nil {
		return ""
	}
	if id, ok := top.ID.(string); ok && graph.PlanIDPattern.MatchString(id) {
		return id
	}
	return ""
}

// NextNumber returns the next plan number: the highest existing number + 1,
// or 001 when there are none. Gaps are never filled.
func (w *Workspace) NextNumber() (string, error) {
	plans, err := w.Plans()
	if err != nil {
		return "", err
	}
	return nextNumber(plans)
}

func nextNumber(plans []Plan) (string, error) {
	highest := 0
	for _, p := range plans {
		n, _ := strconv.Atoi(p.Number)
		highest = max(highest, n)
	}
	if highest >= 999 {
		return "", errors.New("plan numbers are exhausted (999)")
	}
	return fmt.Sprintf("%03d", highest+1), nil
}

// ValidateName checks a new plan name against the kebab-case convention.
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("plan name %q must be kebab-case (%s)", name, schema.NamePattern)
	}
	return nil
}

// Resolve turns a plan argument into plans: `all` (every plan), `NNN` (the
// plan with that number; an *AmbiguousError when two folders share it),
// `NNN-xxxx` (the plan ID graph.json records), `NNN-name` (the folder), or a
// path (relative to CWD) to a plan folder or its graph.json. A plan ID wins
// over a folder name of the same shape.
func (w *Workspace) Resolve(arg string) ([]Plan, error) {
	plans, err := w.Plans()
	if err != nil {
		return nil, err
	}
	if arg == All {
		return plans, nil
	}
	match := func(keep func(Plan) bool) []Plan {
		var out []Plan
		for _, p := range plans {
			if keep(p) {
				out = append(out, p)
			}
		}
		return out
	}
	one := func(found []Plan) ([]Plan, error) {
		if len(found) > 1 {
			return nil, &AmbiguousError{Arg: arg, Candidates: found}
		}
		return found, nil
	}
	if numberRE.MatchString(arg) {
		found := match(func(p Plan) bool { return p.Number == arg })
		if len(found) == 0 {
			return nil, fmt.Errorf("%w: no %s/%s-* folder", ErrNotFound, PlansDir, arg)
		}
		return one(found)
	}
	if graph.PlanIDPattern.MatchString(arg) {
		if found := match(func(p Plan) bool { return p.ID == arg }); len(found) > 0 {
			return one(found)
		}
	}
	if folderRE.MatchString(arg) {
		if found := match(func(p Plan) bool { return p.Folder() == arg }); len(found) > 0 {
			return found, nil
		}
		return nil, fmt.Errorf("%w: no plan with ID %s and no %s/%s folder", ErrNotFound, arg, PlansDir, arg)
	}

	path := arg
	if !filepath.IsAbs(path) {
		path = filepath.Join(w.CWD, path)
	}
	path = filepath.Clean(path)
	if filepath.Base(path) == GraphFile {
		path = filepath.Dir(path)
	}
	for _, p := range plans {
		if w.Abs(p.Dir) == path {
			return []Plan{p}, nil
		}
	}
	return nil, fmt.Errorf("%w: %q is not NNN, NNN-xxxx, NNN-name, all, or a plan folder under %s", ErrNotFound, arg, PlansDir)
}

// ResolveOne resolves an argument that must name exactly one plan.
func (w *Workspace) ResolveOne(arg string) (Plan, error) {
	if arg == All {
		return Plan{}, errors.New(`"all" is not accepted here; name one plan (NNN, NNN-xxxx, NNN-name or a path)`)
	}
	plans, err := w.Resolve(arg)
	if err != nil {
		return Plan{}, err
	}
	return plans[0], nil
}

// PlanSet is every plan of a workspace, loaded on demand: a plan's graph is
// decoded the first time something asks for it and cached, so resolving a
// plan's qualified references (and theirs, transitively) loads exactly the
// plans they reach. Plans are indexed by the plan ID their graph.json
// records (D-9): a qualified reference `005-k7q2:r-8hw3` resolves against
// the plan whose ID is 005-k7q2, wherever its folder is. A bare number
// (`005`, `005:r-8hw3`) resolves when exactly one folder has it.
type PlanSet struct {
	ws    *Workspace
	all   []Plan
	byRef map[string]Plan
	refs  []string
	cache map[string]loadResult
}

type loadResult struct {
	g   *graph.Graph
	err error
}

// PlanSet indexes the workspace's plan folders by plan ID (a folder whose
// graph.json holds no valid ID is indexed by its folder name). When two
// folders record one ID, the first (sorted) wins.
func (w *Workspace) PlanSet() (*PlanSet, error) {
	plans, err := w.Plans()
	if err != nil {
		return nil, err
	}
	s := &PlanSet{ws: w, all: plans, byRef: map[string]Plan{}, cache: map[string]loadResult{}}
	for _, p := range plans {
		if _, dup := s.byRef[p.Ref()]; dup {
			continue
		}
		s.byRef[p.Ref()] = p
		s.refs = append(s.refs, p.Ref())
	}
	slices.Sort(s.refs)
	return s, nil
}

// Plans lists every plan folder, sorted by folder name.
func (s *PlanSet) Plans() []Plan { return slices.Clone(s.all) }

// FolderFS opens plan p's folder as a file system (see Workspace.FolderFS).
func (s *PlanSet) FolderFS(p Plan) fs.FS { return s.ws.FolderFS(p) }

// IDs lists every plan's Ref (its plan ID), sorted.
func (s *PlanSet) IDs() []string { return slices.Clone(s.refs) }

// ByNumber returns the plan folders with number n, sorted.
func (s *PlanSet) ByNumber(n string) []Plan {
	var out []Plan
	for _, p := range s.all {
		if p.Number == n {
			out = append(out, p)
		}
	}
	return out
}

// WithID returns the plan folders whose graph.json records plan ID id.
func (s *PlanSet) WithID(id string) []Plan {
	var out []Plan
	for _, p := range s.all {
		if p.ID == id {
			out = append(out, p)
		}
	}
	return out
}

// Find resolves a plan reference: a plan ID (or the folder name of a plan
// without one), or a bare number that exactly one folder has. It fails with
// ErrNotFound, or an *AmbiguousError when the number names several plans.
func (s *PlanSet) Find(ref string) (Plan, error) {
	if p, ok := s.byRef[ref]; ok {
		return p, nil
	}
	if graph.PlanNumberPattern.MatchString(ref) {
		switch found := s.ByNumber(ref); len(found) {
		case 0:
		case 1:
			return found[0], nil
		default:
			return Plan{}, &AmbiguousError{Arg: ref, Candidates: found}
		}
		return Plan{}, fmt.Errorf("%w: no %s/%s-* folder", ErrNotFound, PlansDir, ref)
	}
	return Plan{}, fmt.Errorf("%w: no plan %s under %s", ErrNotFound, ref, PlansDir)
}

// Plan returns the plan a reference names (see Find).
func (s *PlanSet) Plan(ref string) (Plan, bool) {
	p, err := s.Find(ref)
	return p, err == nil
}

// Has reports whether ref names exactly one plan.
func (s *PlanSet) Has(ref string) bool {
	_, err := s.Find(ref)
	return err == nil
}

// Path returns the absolute graph.json path of the plan ref names ("" when
// it names none).
func (s *PlanSet) Path(ref string) string {
	p, err := s.Find(ref)
	if err != nil {
		return ""
	}
	return s.ws.GraphPath(p)
}

// Load decodes the graph of the plan ref names (once; later calls return the
// cached result). An unknown or ambiguous ref is the Find error; malformed
// JSON is the decode error.
func (s *PlanSet) Load(ref string) (*graph.Graph, error) {
	p, err := s.Find(ref)
	if err != nil {
		return nil, err
	}
	return s.LoadPlan(p)
}

// LoadPlan decodes plan p's graph (cached by folder).
func (s *PlanSet) LoadPlan(p Plan) (*graph.Graph, error) {
	if r, ok := s.cache[p.Dir]; ok {
		return r.g, r.err
	}
	g, err := graph.Decode(s.ws.GraphPath(p))
	s.cache[p.Dir] = loadResult{g: g, err: err}
	return g, err
}

// Graph is Load for callers that only need to know whether the plan loaded.
func (s *PlanSet) Graph(ref string) (*graph.Graph, bool) {
	g, err := s.Load(ref)
	return g, err == nil
}

// ExpandPlan turns a plan argument of a write into the plan ID to store: a
// bare number becomes the ID of the one plan that has it (ErrNotFound when
// none does, an *AmbiguousError when several do). Anything else is returned
// unchanged, for validation to judge.
func (s *PlanSet) ExpandPlan(v string) (string, error) {
	if !graph.PlanNumberPattern.MatchString(v) {
		return v, nil
	}
	p, err := s.Find(v)
	if err != nil {
		return v, err
	}
	if p.ID == "" {
		return v, fmt.Errorf("%w: %s records no plan ID (run `auto plan lint %s`)", ErrNotFound, p.Dir, p.Ref())
	}
	return p.ID, nil
}

// ExpandRef expands a shorthand qualified reference `NNN:ID` to
// `NNN-xxxx:ID` (see ExpandPlan). Other references are returned unchanged.
func (s *PlanSet) ExpandRef(ref string) (string, error) {
	if !graph.ShortRefPattern.MatchString(ref) {
		return ref, nil
	}
	plan, id, _ := graph.ParseRef(ref)
	full, err := s.ExpandPlan(plan)
	if err != nil {
		return ref, err
	}
	return graph.Qualify(full, id), nil
}

// ErrNodeNotFound is returned when a qualified reference names a plan that
// exists but holds no node with that ID.
var ErrNodeNotFound = errors.New("node not found")

// Lookup resolves a qualified reference — `NNN-xxxx:ID`, or the shorthand
// `NNN:ID` when one plan has the number — to its node, whatever its status.
// It fails with ErrNotFound (no such plan), an *AmbiguousError (the number
// names several plans), ErrNodeNotFound (the plan has no such node) or the
// plan's decode error.
func (s *PlanSet) Lookup(ref string) (graph.Node, error) {
	planID, id, qualified := graph.ParseRef(ref)
	if !qualified || (!graph.QualifiedRefPattern.MatchString(ref) && !graph.ShortRefPattern.MatchString(ref)) {
		return graph.Node{}, fmt.Errorf("%q is not a reference NNN-xxxx:ID", ref)
	}
	g, err := s.Load(planID)
	if err != nil {
		return graph.Node{}, err
	}
	n, ok := g.NodeByID(id)
	if !ok {
		return graph.Node{}, fmt.Errorf("%w: plan %s has no node %s", ErrNodeNotFound, planID, id)
	}
	return n, nil
}

// CheckEdge resolves the qualified target of a cross-plan edge and checks it
// against the edge type's allowed target types. It returns nothing for a
// plan-local target, an unregistered or non-cross-plan edge type, or a
// malformed reference: graph.Validate reports those. Otherwise a target whose
// plan or node does not exist is a dangling-ref, and one of a type the edge
// may not end at is a wrong-endpoint.
func (s *PlanSet) CheckEdge(e graph.Edge) []graph.ValidationError {
	planID, id, qualified := graph.ParseRef(e.To)
	et, ok := schema.Registry.Edge(e.Type)
	if !qualified || !ok || !et.CrossPlan || !graph.QualifiedRefPattern.MatchString(e.To) {
		return nil
	}
	path := graph.EdgePath(e) + ".to"
	n, err := s.Lookup(e.To)
	switch {
	case errors.Is(err, ErrNotFound):
		return []graph.ValidationError{{Code: graph.CodeDanglingRef, Path: path, Field: "to", Value: e.To,
			Message: fmt.Sprintf("edge targets %s, but there is no plan %s under %s", strconv.Quote(e.To), planID, PlansDir)}}
	case errors.Is(err, ErrNodeNotFound):
		return []graph.ValidationError{{Code: graph.CodeDanglingRef, Path: path, Field: "to", Value: e.To,
			Message: fmt.Sprintf("edge targets %s, but plan %s has no node %s", strconv.Quote(e.To), planID, id)}}
	case err != nil:
		return []graph.ValidationError{{Code: graph.CodeDanglingRef, Path: path, Field: "to", Value: e.To,
			Message: fmt.Sprintf("edge targets %s, but plan %s cannot be read: %v", strconv.Quote(e.To), planID, err)}}
	case !et.AllowsTo(n.Type):
		return []graph.ValidationError{{Code: graph.CodeWrongEndpoint, Path: path, Field: "to", Value: e.To,
			Message: fmt.Sprintf("%s edges end at %s, not %s (%s is a %s)", et.Name, et.ToLabel(), n.Type, e.To, n.Type)}}
	}
	return nil
}

// CheckEpic checks that epic (a plan ID, or a number one plan has) names an existing, readable epic
// plan: what `new --epic` and `update … plan --epic` require.
func (s *PlanSet) CheckEpic(epic string) error {
	g, err := s.Load(epic)
	if err != nil {
		return err
	}
	plan, _ := g.Plan()
	if kind := plan.StringField("kind"); kind != "epic" {
		return fmt.Errorf("plan %s is kind %q, not an epic", epic, kind)
	}
	return nil
}

// Family returns the child plan IDs of epic plan epic, sorted: the plans
// its active child nodes name (when they exist) and every plan whose plan
// node declares `epic: <epic>`. Plans that fail to load are skipped.
func (s *PlanSet) Family(epic string) []string {
	var out []string
	if g, ok := s.Graph(epic); ok {
		for _, n := range g.Nodes {
			if n.Type == "child" && n.Active() && s.Has(n.StringField("plan")) {
				out = append(out, n.StringField("plan"))
			}
		}
	}
	for _, id := range s.refs {
		if id == epic {
			continue
		}
		if g, ok := s.Graph(id); ok {
			if p, _ := g.Plan(); p.StringField("epic") == epic {
				out = append(out, id)
			}
		}
	}
	slices.Sort(out)
	out = slices.Compact(out)
	return slices.DeleteFunc(out, func(id string) bool { return id == epic || strings.TrimSpace(id) == "" })
}
