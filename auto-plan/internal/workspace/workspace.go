// Package workspace locates plans on disk: the repo root, the docs/plans/
// folder, the next plan number, and resolution of a plan argument
// (`NNN`, `NNN-name`, a path, or `all`), and the PlanSet that resolves
// qualified cross-plan references (`005:r-8hw3`).
package workspace

import (
	"errors"
	"fmt"
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
const PlansDir = "docs/plans"

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

// Workspace is a repo's plan root.
type Workspace struct {
	// Root is the absolute repo root.
	Root string
	// CWD is the directory relative paths are resolved from.
	CWD string
}

// Plan is one plan folder.
type Plan struct {
	// ID is the plan's 3-digit number (`004`).
	ID string `json:"id"`
	// Name is the kebab-case name from the folder (`stage-briefs`).
	Name string `json:"name"`
	// Dir is the folder relative to the repo root (`docs/plans/004-stage-briefs`).
	Dir string `json:"path"`
}

// Folder returns the plan's folder name (`004-stage-briefs`).
func (p Plan) Folder() string { return p.ID + "-" + p.Name }

// Open locates the repo containing cwd.
func Open(cwd string) (*Workspace, error) {
	root, err := git.RepoRoot(cwd)
	if err != nil {
		return nil, fmt.Errorf("not inside a git repository (%s): auto plan keeps plans under <repo>/%s", cwd, PlansDir)
	}
	return &Workspace{Root: root, CWD: cwd}, nil
}

// PlansPath returns the absolute docs/plans directory.
func (w *Workspace) PlansPath() string { return filepath.Join(w.Root, filepath.FromSlash(PlansDir)) }

// Abs returns the absolute path of a repo-relative path.
func (w *Workspace) Abs(rel string) string { return filepath.Join(w.Root, filepath.FromSlash(rel)) }

// GraphPath returns the absolute path of a plan's graph.json.
func (w *Workspace) GraphPath(p Plan) string { return filepath.Join(w.Abs(p.Dir), GraphFile) }

// Plans lists every plan folder, sorted by number. A missing docs/plans/ is
// an empty list. Entries that are not NNN-name folders are ignored.
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
		plans = append(plans, Plan{ID: m[1], Name: m[2], Dir: PlansDir + "/" + e.Name()})
	}
	slices.SortFunc(plans, func(a, b Plan) int { return strings.Compare(a.Folder(), b.Folder()) })
	return plans, nil
}

// NextID returns the next plan number: the highest existing number + 1, or
// 001 when there are none. Gaps are never filled.
func (w *Workspace) NextID() (string, error) {
	plans, err := w.Plans()
	if err != nil {
		return "", err
	}
	highest := 0
	for _, p := range plans {
		n, _ := strconv.Atoi(p.ID)
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

// Resolve turns a plan argument into plans: `all` (every plan), `NNN`,
// `NNN-name`, or a path (relative to CWD) to a plan folder or its graph.json.
func (w *Workspace) Resolve(arg string) ([]Plan, error) {
	plans, err := w.Plans()
	if err != nil {
		return nil, err
	}
	if arg == All {
		return plans, nil
	}
	if numberRE.MatchString(arg) {
		for _, p := range plans {
			if p.ID == arg {
				return []Plan{p}, nil
			}
		}
		return nil, fmt.Errorf("%w: no %s/%s-* folder", ErrNotFound, PlansDir, arg)
	}
	if folderRE.MatchString(arg) {
		for _, p := range plans {
			if p.Folder() == arg {
				return []Plan{p}, nil
			}
		}
		return nil, fmt.Errorf("%w: no %s/%s folder", ErrNotFound, PlansDir, arg)
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
	return nil, fmt.Errorf("%w: %q is not NNN, NNN-name, all, or a plan folder under %s", ErrNotFound, arg, PlansDir)
}

// ResolveOne resolves an argument that must name exactly one plan.
func (w *Workspace) ResolveOne(arg string) (Plan, error) {
	if arg == All {
		return Plan{}, errors.New(`"all" is not accepted here; name one plan (NNN, NNN-name or a path)`)
	}
	plans, err := w.Resolve(arg)
	if err != nil {
		return Plan{}, err
	}
	return plans[0], nil
}

// ErrNodeNotFound is returned when a qualified reference names a plan that
// exists but holds no node with that ID.
var ErrNodeNotFound = errors.New("node not found")

// PlanSet is every plan of a workspace, loaded on demand: a plan's graph is
// decoded the first time something asks for it and cached, so resolving a
// plan's qualified references (and theirs, transitively) loads exactly the
// plans they reach. Qualified references (`005:r-8hw3`) resolve against
// docs/plans/005-*/graph.json (D-9).
type PlanSet struct {
	ws    *Workspace
	plans map[string]Plan
	ids   []string
	cache map[string]loadResult
}

type loadResult struct {
	g   *graph.Graph
	err error
}

// PlanSet indexes the workspace's plan folders. No graph is read yet.
func (w *Workspace) PlanSet() (*PlanSet, error) {
	plans, err := w.Plans()
	if err != nil {
		return nil, err
	}
	s := &PlanSet{ws: w, plans: map[string]Plan{}, cache: map[string]loadResult{}}
	for _, p := range plans {
		if _, dup := s.plans[p.ID]; dup {
			continue // two folders with one number: the first (sorted) wins
		}
		s.plans[p.ID] = p
		s.ids = append(s.ids, p.ID)
	}
	return s, nil
}

// IDs lists every plan number, sorted.
func (s *PlanSet) IDs() []string { return slices.Clone(s.ids) }

// Plan returns the plan folder with number id.
func (s *PlanSet) Plan(id string) (Plan, bool) {
	p, ok := s.plans[id]
	return p, ok
}

// Has reports whether a plan folder with number id exists.
func (s *PlanSet) Has(id string) bool {
	_, ok := s.plans[id]
	return ok
}

// Path returns the absolute graph.json path of plan id ("" when absent).
func (s *PlanSet) Path(id string) string {
	p, ok := s.plans[id]
	if !ok {
		return ""
	}
	return s.ws.GraphPath(p)
}

// Load decodes plan id's graph (once; later calls return the cached result).
// A missing folder is ErrNotFound; malformed JSON is the decode error.
func (s *PlanSet) Load(id string) (*graph.Graph, error) {
	if r, ok := s.cache[id]; ok {
		return r.g, r.err
	}
	p, ok := s.plans[id]
	if !ok {
		return nil, fmt.Errorf("%w: no %s/%s-* folder", ErrNotFound, PlansDir, id)
	}
	g, err := graph.Decode(s.ws.GraphPath(p))
	s.cache[id] = loadResult{g: g, err: err}
	return g, err
}

// Graph is Load for callers that only need to know whether the plan loaded.
func (s *PlanSet) Graph(id string) (*graph.Graph, bool) {
	g, err := s.Load(id)
	return g, err == nil
}

// Lookup resolves a qualified reference `NNN:ID` to its node, whatever its
// status. It fails with ErrNotFound (no such plan), ErrNodeNotFound (the plan
// has no such node) or the plan's decode error.
func (s *PlanSet) Lookup(ref string) (graph.Node, error) {
	planID, id, qualified := graph.ParseRef(ref)
	if !qualified || !graph.QualifiedRefPattern.MatchString(ref) {
		return graph.Node{}, fmt.Errorf("%q is not a reference NNN:ID", ref)
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

// CheckEpic checks that plan number epic names an existing, readable epic
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

// Family returns the child plan numbers of epic plan epic, sorted: the plans
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
	for _, id := range s.ids {
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
