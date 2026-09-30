// Package workspace locates plans on disk: the repo root, the docs/plans/
// folder, the next plan number, and resolution of a plan argument
// (`NNN`, `NNN-name`, a path, or `all`).
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
