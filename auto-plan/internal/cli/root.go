// Package cli is the `auto plan` command tree. Every command reads
// `auto plan <verb> <plan> [args…]` (D-13); data goes to stdout as JSON by
// default (or text with --text), and diagnostics go to stderr.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-plan/internal/workspace"
	"github.com/mistakenot/auto-shared/version"
	"github.com/spf13/cobra"
)

// Environment variables that pin otherwise time- or randomness-dependent
// output, for reproducible tests and e2e snapshots. Each is read only when set.
const (
	// EnvSeed seeds node-ID generation (a uint64).
	EnvSeed = "AUTO_PLAN_SEED"
	// EnvDate pins the date `new` records (YYYY-MM-DD).
	EnvDate = "AUTO_PLAN_DATE"
)

// ExitError carries a specific process exit code out of a command's RunE.
// Commands print their own diagnostics before returning one, so a nil Err
// renders as an empty message and the dispatcher prints nothing more.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return ""
}

// ExitCode lets the merged `auto` dispatcher honor the declared exit code.
func (e *ExitError) ExitCode() int { return e.Code }

func (e *ExitError) Unwrap() error { return e.Err }

// Execute runs the standalone auto-plan binary.
func Execute(ctx context.Context, stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	rootCmd := NewRootCmd(app.New(stdout, stderr, cwd))
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			if msg := exitErr.Error(); msg != "" {
				fmt.Fprintln(stderr, msg)
			}
			return exitErr.Code
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// NewRootCmd builds the auto-plan command tree (mounted as `auto plan`).
func NewRootCmd(application *app.App) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "plan",
		Short: "Structured plan graphs: goals, acceptance criteria and decisions in one graph.json",
		Long: `auto plan keeps each plan as one deterministic graph.json under .auto/plan/plans/NNN-name/:
typed nodes (plan, goal, ac, decision, …) and typed edges (proves, constrains, …),
validated on every write against one type registry.

Every command reads: auto plan <verb> <plan> [args…], where <plan> is NNN, the plan ID
NNN-xxxx, the folder NNN-name, a path, or "all" where it makes sense. Output is JSON by
default; --text is for humans.`,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	rootCmd.Version = version.Version
	rootCmd.SetOut(application.Stdout)
	rootCmd.SetErr(application.Stderr)
	rootCmd.PersistentFlags().Bool("text", false, "human-readable text output instead of JSON")

	rootCmd.AddCommand(
		newInitCmd(application),
		newNewCmd(application),
		newAddCmd(application),
		newUpdateCmd(application),
		newLinkCmd(application),
		newUnlinkCmd(application),
		newRetireCmd(application),
		newMoveCmd(application),
		newRenumberCmd(application),
		newLintCmd(application),
		newFmtCmd(application),
		newListCmd(application),
		newDescribeCmd(application),
		newGetCmd(application),
		newSearchCmd(application),
		newShowCmd(application),
		newTraceCmd(application),
		newTreeCmd(application),
		newBriefCmd(application),
		newQuickstartCmd(application),
		newDocsCmd(application),
	)
	return rootCmd
}

func textMode(cmd *cobra.Command) bool {
	text, _ := cmd.Flags().GetBool("text")
	return text
}

// writeJSON writes v as 2-space-indented JSON without HTML escaping.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// mutationResult returns a mutation's stdout payload: the changed fields plus
// a predictable top-level plan and id (after auto-reflect's output.go).
func mutationResult(plan, id string, fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields)+2)
	maps.Copy(out, fields)
	out["plan"] = plan
	out["id"] = id
	return out
}

// failure is the stderr payload of a failed command.
type failure struct {
	Errors []graph.ValidationError `json:"errors"`
	Hint   string                  `json:"hint"`
}

// fail reports structured errors and a remediation hint on stderr and returns
// the exit error. stdout is left untouched.
func fail(cmd *cobra.Command, text bool, errs []graph.ValidationError, hint string) error {
	w := cmd.ErrOrStderr()
	if text {
		for _, e := range errs {
			fmt.Fprintf(w, "error %s %s: %s\n", e.Code, e.Path, e.Message)
		}
		if hint != "" {
			fmt.Fprintf(w, "hint: %s\n", hint)
		}
	} else if err := writeJSON(w, failure{Errors: errs, Hint: hint}); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return &ExitError{Code: 1}
}

// failOne reports a single error.
func failOne(cmd *cobra.Command, text bool, code, path, field, msg string, value any, hint string) error {
	return fail(cmd, text, []graph.ValidationError{{Code: code, Path: path, Field: field, Message: msg, Value: value}}, hint)
}

// emit writes a result: JSON by default, or the text form in --text mode.
func emit(cmd *cobra.Command, text bool, v any, textForm func() string) error {
	if text {
		_, err := io.WriteString(cmd.OutOrStdout(), textForm())
		return err
	}
	return writeJSON(cmd.OutOrStdout(), v)
}

// openWorkspace locates the repo, reporting failure on stderr.
func openWorkspace(cmd *cobra.Command, application *app.App, text bool) (*workspace.Workspace, error) {
	ws, err := workspace.Open(application.CWD)
	if err != nil {
		return nil, failOne(cmd, text, "not-a-repo", "$", "", err.Error(), application.CWD,
			"run auto plan inside a git repository (git init), then `auto plan init`")
	}
	return ws, nil
}

// CodeAmbiguousPlan reports a bare plan number that names several plans.
const CodeAmbiguousPlan = "ambiguous-plan"

// resolveOne resolves a single-plan argument, reporting failure on stderr.
func resolveOne(cmd *cobra.Command, ws *workspace.Workspace, text bool, arg string) (workspace.Plan, error) {
	p, err := ws.ResolveOne(arg)
	if err != nil {
		return workspace.Plan{}, planArgFailure(cmd, text, "args.plan", "plan", arg, err,
			"name a plan as NNN, NNN-xxxx, NNN-name or a path; list plans with `auto plan list` or create one with `auto plan new <name> --kind task`")
	}
	return p, nil
}

// resolveMany resolves a plan argument that may be `all`.
func resolveMany(cmd *cobra.Command, ws *workspace.Workspace, text bool, arg string) ([]workspace.Plan, error) {
	plans, err := ws.Resolve(arg)
	if err != nil {
		return nil, planArgFailure(cmd, text, "args.plan", "plan", arg, err,
			"name a plan as NNN, NNN-xxxx, NNN-name, a path, or all")
	}
	return plans, nil
}

// planArgFailure reports a plan reference that did not resolve: an
// ambiguous number lists its candidates (ambiguous-plan); anything else is
// plan-not-found with hint.
func planArgFailure(cmd *cobra.Command, text bool, path, field, arg string, err error, hint string) error {
	var amb *workspace.AmbiguousError
	if errors.As(err, &amb) {
		ids := make([]string, len(amb.Candidates))
		for i, c := range amb.Candidates {
			ids[i] = c.Ref()
		}
		return failOne(cmd, text, CodeAmbiguousPlan, path, field, err.Error(), ids,
			"name the plan by its ID ("+strings.Join(ids, " or ")+"), and give each plan its own number with `auto plan renumber "+ids[len(ids)-1]+"`")
	}
	return failOne(cmd, text, "plan-not-found", path, field, err.Error(), arg, hint)
}

// loaded is a plan opened for a write.
type loaded struct {
	ws    *workspace.Workspace
	plan  workspace.Plan
	path  string
	graph *graph.Graph
	set   *workspace.PlanSet
}

// ref is how results and hints name the plan: its plan ID.
func (l *loaded) ref() string { return l.plan.Ref() }

// planSet returns the workspace's PlanSet, built on first use.
func (l *loaded) planSet(cmd *cobra.Command, text bool) (*workspace.PlanSet, error) {
	if l.set != nil {
		return l.set, nil
	}
	set, err := l.ws.PlanSet()
	if err != nil {
		return nil, failOne(cmd, text, "read-failed", "$", "", err.Error(), nil, "check that "+workspace.PlansDir+" is readable")
	}
	l.set = set
	return set, nil
}

// frozenFailure refuses a write to a frozen plan (see graph.Frozen).
func frozenFailure(cmd *cobra.Command, text bool, ref, reason string) error {
	return failOne(cmd, text, graph.CodeFrozen, "$", "", "plan "+ref+" is frozen: "+reason+"; writes are refused", ref,
		"a plan is immutable once done or when another format version wrote it; read it with list/show/get, and start new work in a new plan (`auto plan new <name> --kind task`)")
}

// loadForWrite opens a plan for mutation. It refuses a frozen plan (done, or
// of another format version) and a graph that is already invalid (writes
// never save a graph with errors), and seeds ID generation from
// AUTO_PLAN_SEED when set.
func loadForWrite(cmd *cobra.Command, application *app.App, text bool, arg string) (*loaded, error) {
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return nil, err
	}
	p, err := resolveOne(cmd, ws, text, arg)
	if err != nil {
		return nil, err
	}
	path := ws.GraphPath(p)
	g, err := graph.Decode(path)
	if err != nil {
		return nil, failOne(cmd, text, "parse-error", "$", "", err.Error(), nil,
			"fix graph.json by hand, then run `auto plan lint "+p.Ref()+"`")
	}
	if reason := g.Frozen(); reason != "" {
		return nil, frozenFailure(cmd, text, p.Ref(), reason)
	}
	if errs := graph.Validate(g); len(errs) > 0 {
		return nil, fail(cmd, text, errs,
			"graph.json is already invalid, so writes are refused; run `auto plan lint "+p.Ref()+"` and fix the file first")
	}
	r, err := seededRand(cmd, text, uint64(len(g.Nodes)))
	if err != nil {
		return nil, err
	}
	if r != nil {
		g.SetRand(r)
	}
	return &loaded{ws: ws, plan: p, path: path, graph: g}, nil
}

// seededRand returns the AUTO_PLAN_SEED random source mixed with stream, or
// nil when the variable is unset. Mixing in the plan's node count gives each
// invocation a fresh but reproducible sequence.
func seededRand(cmd *cobra.Command, text bool, stream uint64) (*rand.Rand, error) {
	seed, ok := os.LookupEnv(EnvSeed)
	if !ok {
		return nil, nil
	}
	n, err := strconv.ParseUint(seed, 10, 64)
	if err != nil {
		return nil, failOne(cmd, text, "invalid-env", "env."+EnvSeed, EnvSeed, EnvSeed+" must be an unsigned integer", seed,
			"unset "+EnvSeed+" or set it to a number")
	}
	return rand.New(rand.NewPCG(n, stream)), nil //nolint:gosec // G404: IDs are opaque labels, not secrets
}

// save writes the graph and reports an I/O failure on stderr.
func (l *loaded) save(cmd *cobra.Command, text bool) error {
	if err := graph.Save(l.path, l.graph); err != nil {
		return failOne(cmd, text, "write-failed", "$", "", err.Error(), l.path, "check that "+l.path+" is writable")
	}
	return nil
}

// today returns the date `new` records, honoring AUTO_PLAN_DATE.
func today() (string, error) {
	if d, ok := os.LookupEnv(EnvDate); ok {
		if _, err := time.Parse(time.DateOnly, d); err != nil {
			return "", fmt.Errorf("%s must be YYYY-MM-DD, got %q", EnvDate, d)
		}
		return d, nil
	}
	return time.Now().Format(time.DateOnly), nil
}

// readValue expands a text flag value: `@path` reads a file (relative to the
// working directory) and `@-` reads stdin. Other values are returned as is.
func readValue(cmd *cobra.Command, application *app.App, v string) (string, error) {
	if !strings.HasPrefix(v, "@") {
		return v, nil
	}
	src := strings.TrimPrefix(v, "@")
	var data []byte
	var err error
	if src == "-" {
		data, err = io.ReadAll(cmd.InOrStdin())
	} else {
		if !filepath.IsAbs(src) {
			src = filepath.Join(application.CWD, src)
		}
		data, err = os.ReadFile(src)
	}
	if err != nil {
		return "", err
	}
	return string(bytes.TrimRight(data, "\n")), nil
}

// planTypes lists the registered types `add` accepts.
func planTypes() []string {
	var out []string
	for _, n := range schema.Registry.Nodes {
		if !n.Singleton() {
			out = append(out, n.Name)
		}
	}
	return out
}
