package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-plan/internal/workspace"
	"github.com/spf13/cobra"
)

// Renumber-only codes.
const (
	CodeFrozenRef       = "frozen-ref"
	CodePlanNumberTaken = "plan-number-taken"
)

func newRenumberCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "renumber <plan> [--to NNN]",
		Short: "Give a plan a new number: rename its folder, rewrite its ID and every reference to it",
		Long: `Move a plan to another number — usually after a merge in which two branches each created plan
NNN (lint reports duplicate-plan-number). The folder NNN-name becomes MMM-name, the plan ID
NNN-xxxx becomes MMM-xxxx (the random suffix is kept), and every reference to the old ID in
every plan under .auto/plan/plans is rewritten: qualified edge targets, [[NNN-xxxx:id]] prose
references, and the epic, child plan and rail deferred fields. A shorthand prose reference
[[NNN:id]] keeps its meaning: while NNN names only this plan it moves with it; during a
collision, one whose node is in this plan becomes a full [[MMM-xxxx:id]], one whose node is in
the other plan is left alone (NNN names that plan once this one moves), and one that both plans
could satisfy refuses the renumber (ambiguous-ref) until it is rewritten with a full plan ID.
--to defaults to the next free
number (the highest + 1). With --to equal to the folder's own number, only the ID is rewritten
(the fix for plan-id-mismatch).

Plans are mutable until merged, so this is a legitimate write. It is refused, and nothing
changes, when the plan is frozen (frozen), when a shorthand reference is ambiguous
(ambiguous-ref), when a frozen plan references it (frozen-ref: a done
plan, or one of another format version, is never rewritten), when any rewritten graph would
fail validation, or when the target number is taken.

Every new graph.json is encoded and validated first and staged as a temp file beside its
target; only then are the temp files renamed into place, and the folder renamed last. A
failure while staging changes nothing; a failure among the renames is rolled back best effort,
and the error says whether that rollback succeeded or names the files it could not restore.
The renames of several files are not one atomic step, so a crash in that window can leave some
plans rewritten: run auto plan lint all to see what remains.

Prints {plan, id, from, to, path, rewritten:[…]}: plan is the new plan ID, id is "plan" (the
node that changed), from and to the old and new plan IDs, path the new folder, and rewritten
every graph.json that changed.`,
		Example: "  auto plan renumber 004-m3x9\n  auto plan renumber 004-gamma --to 007",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			to, _ := cmd.Flags().GetString("to")
			return runRenumber(cmd, application, args[0], strings.TrimSpace(to))
		},
	}
	cmd.Flags().String("to", "", "the new plan number (NNN; default: the next free number)")
	return cmd
}

// rewrite is one graph.json that renumber changes.
type rewrite struct {
	plan workspace.Plan
	path string // absolute graph.json path before the folder rename
	orig []byte
	data []byte
	tmp  string
}

func runRenumber(cmd *cobra.Command, application *app.App, arg, to string) error {
	text := textMode(cmd)
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return err
	}
	p, err := resolveOne(cmd, ws, text, arg)
	if err != nil {
		return err
	}
	set, err := ws.PlanSet()
	if err != nil {
		return failOne(cmd, text, "read-failed", "$", "", err.Error(), nil, "check that "+workspace.PlansDir+" is readable")
	}
	g, err := graph.Decode(ws.GraphPath(p))
	if err != nil {
		return failOne(cmd, text, "parse-error", "$", "", err.Error(), nil, "fix graph.json by hand, then run `auto plan lint "+p.Ref()+"`")
	}
	if reason := g.Frozen(); reason != "" {
		return frozenFailure(cmd, text, p.Ref(), reason)
	}
	oldID := g.ID
	if !graph.PlanIDPattern.MatchString(oldID) {
		return failOne(cmd, text, graph.CodeBadID, "$.id", "id", p.Dir+" records no valid plan ID", oldID,
			"restore the plan's ID (NNN-xxxx) in graph.json by hand, then run `auto plan lint "+p.Ref()+"`")
	}
	if same := set.WithID(oldID); len(same) > 1 {
		return failOne(cmd, text, "duplicate-plan-id", "$.id", "id",
			fmt.Sprintf("plan ID %s is recorded by %d folders, so references to it cannot be told apart", oldID, len(same)), oldID,
			"a copied folder keeps the original's ID: remove the copy first (see `auto plan lint all`)")
	}

	if to == "" {
		if to, err = ws.NextNumber(); err != nil {
			return failOne(cmd, text, "plan-number", "flags.to", "to", err.Error(), nil, "pass --to NNN")
		}
	}
	if !planNumberRE.MatchString(to) {
		return failOne(cmd, text, "usage", "flags.to", "to", "--to must be a 3-digit plan number (NNN)", to, "e.g. --to 007")
	}
	newID := to + oldID[3:]
	if to == p.Number && newID == oldID {
		return failOne(cmd, text, "usage", "flags.to", "to", "plan "+oldID+" is already number "+to, to,
			"pass another --to, or leave it out for the next free number")
	}
	newDir := workspace.PlansDir + "/" + to + "-" + p.Name
	if to != p.Number {
		if taken := set.ByNumber(to); len(taken) > 0 {
			return failOne(cmd, text, CodePlanNumberTaken, "flags.to", "to", "plan number "+to+" is taken by "+taken[0].Ref()+" ("+taken[0].Dir+")", to,
				"leave --to out for the next free number")
		}
		if _, err := os.Stat(ws.Abs(newDir)); err == nil {
			return failOne(cmd, text, CodePlanNumberTaken, "flags.to", "to", newDir+" already exists", to, "leave --to out for the next free number")
		}
	}
	if len(set.WithID(newID)) > 0 {
		return failOne(cmd, text, CodePlanNumberTaken, "flags.to", "to", "plan ID "+newID+" is already in use", to, "pass another --to")
	}
	// A shorthand [[NNN:id]] with this plan's old number must keep meaning
	// what it meant. While the number is unique it names this plan and moves
	// with it. During a collision it names whichever plan holds the node: a
	// reference to this plan becomes a full ID, one to another plan already
	// resolves correctly once this plan leaves the number, and one that the
	// plans cannot tell apart stops the renumber.
	candidates := set.ByNumber(p.Number)
	short := func(number, id string) (string, bool) {
		if number != p.Number || to == p.Number {
			return "", false
		}
		if len(candidates) == 1 {
			return graph.Qualify(to, id), false
		}
		var holders []string
		for _, c := range candidates {
			if gc, err := set.LoadPlan(c); err == nil && (id == schema.PlanNodeID || hasNode(gc, id)) {
				holders = append(holders, gc.ID)
			}
		}
		switch {
		case len(holders) > 1:
			return "", true
		case len(holders) == 1 && holders[0] == oldID:
			return graph.Qualify(newID, id), false
		}
		return "", false // another plan's node, or dangling either way (lint reports it)
	}

	// Compute every rewrite and validate it before anything is written.
	var rewrites []*rewrite
	var frozen, ambiguous []string
	var errs []graph.ValidationError
	for _, q := range set.Plans() {
		path := ws.GraphPath(q)
		orig, err := os.ReadFile(path)
		if err != nil {
			continue // a folder without graph.json references nothing
		}
		gq, err := graph.Parse(orig)
		if err != nil {
			if bytes.Contains(orig, []byte(oldID)) {
				errs = append(errs, graph.ValidationError{Code: "parse-error", Path: q.Dir + "/" + workspace.GraphFile, Field: "plan", Value: q.Ref(),
					Message: "plan " + q.Ref() + " mentions " + oldID + " but cannot be parsed, so it cannot be rewritten: " + err.Error()})
			}
			continue
		}
		changed, amb := gq.RewritePlanRefs(oldID, newID, short)
		for _, a := range amb {
			ambiguous = append(ambiguous, q.Dir+"/"+workspace.GraphFile+" "+a)
		}
		if q.Dir == p.Dir {
			gq.ID, changed = newID, true
		}
		if !changed {
			continue
		}
		if q.Dir != p.Dir && gq.Frozen() != "" {
			frozen = append(frozen, q.Ref()+" ("+gq.Frozen()+")")
			continue
		}
		for _, ve := range graph.Validate(gq) {
			ve.Message = "plan " + q.Ref() + ": " + ve.Message
			errs = append(errs, ve)
		}
		data, err := graph.Encode(gq)
		if err != nil {
			errs = append(errs, graph.ValidationError{Code: "encode-failed", Path: q.Dir, Message: err.Error()})
			continue
		}
		rewrites = append(rewrites, &rewrite{plan: q, path: path, orig: orig, data: data})
	}
	if len(ambiguous) > 0 {
		return failOne(cmd, text, "ambiguous-ref", "$", "", fmt.Sprintf("%d shorthand reference(s) to plan number %s could name either plan of the collision, "+
			"so renumbering could change what they point at; nothing was renumbered: %s", len(ambiguous), p.Number, strings.Join(ambiguous, "; ")), ambiguous,
			"rewrite each one with the full plan ID it means (e.g. [["+graph.Qualify(oldID, "<id>")+"]]), then retry")
	}
	if len(frozen) > 0 {
		return failOne(cmd, text, CodeFrozenRef, "$", "", "plan "+oldID+" is referenced by frozen plan "+strings.Join(frozen, ", ")+
			"; a frozen plan is never rewritten, so nothing was renumbered", frozen,
			"renumber the other plan of the collision instead (see `auto plan lint all`)")
	}
	if len(errs) > 0 {
		return fail(cmd, text, errs, "nothing was renumbered; run `auto plan lint all`, fix the plans flagged, then retry")
	}
	if err := commitRewrites(rewrites, ws.Abs(p.Dir), ws.Abs(newDir)); err != nil {
		return failOne(cmd, text, "write-failed", "$", "", err.Error(), nil, "check that "+workspace.PlansDir+" is writable, then run `auto plan lint all`")
	}

	paths := make([]string, 0, len(rewrites))
	for _, r := range rewrites {
		dir := r.plan.Dir
		if dir == p.Dir {
			dir = newDir
		}
		paths = append(paths, dir+"/"+workspace.GraphFile)
	}
	out := mutationResult(newID, "plan", map[string]any{"from": oldID, "to": newID, "path": newDir, "rewritten": paths})
	return emit(cmd, text, out, func() string {
		var b strings.Builder
		fmt.Fprintf(&b, "renumbered %s → %s (%s → %s)\n", oldID, newID, p.Dir, newDir)
		for _, path := range paths {
			fmt.Fprintf(&b, "  rewrote %s\n", path)
		}
		return b.String()
	})
}

// commitRewrites stages every new graph.json as a temp file beside its
// target, then renames them into place and finally renames the plan folder
// oldDir → newDir. Staging failures change nothing; a rename failure restores
// the files already replaced, best effort.
func commitRewrites(rewrites []*rewrite, oldDir, newDir string) (err error) {
	defer func() {
		for _, r := range rewrites {
			if r.tmp != "" {
				_ = os.Remove(r.tmp)
			}
		}
	}()
	for _, r := range rewrites {
		if r.tmp, err = stage(r.path, r.data); err != nil {
			return err
		}
	}
	var done []*rewrite
	restore := func(cause error) error {
		var failed []string
		for _, r := range done {
			tmp, serr := stage(r.path, r.orig)
			if serr == nil {
				if rerr := os.Rename(tmp, r.path); rerr != nil {
					_ = os.Remove(tmp)
					serr = rerr
				}
			}
			if serr != nil {
				failed = append(failed, r.path+": "+serr.Error())
			}
		}
		if len(failed) > 0 {
			return fmt.Errorf("%w; restoring the files already replaced FAILED, so these may hold the new plan ID while the folder "+
				"and the other plans keep the old one: %s", cause, strings.Join(failed, "; "))
		}
		if len(done) == 0 {
			return fmt.Errorf("%w (nothing had been replaced)", cause)
		}
		return fmt.Errorf("%w (the %d file(s) already replaced were restored)", cause, len(done))
	}
	for _, r := range rewrites {
		if err := os.Rename(r.tmp, r.path); err != nil {
			return restore(err)
		}
		r.tmp = ""
		done = append(done, r)
	}
	if oldDir != newDir {
		if err := os.Rename(oldDir, newDir); err != nil {
			return restore(err)
		}
	}
	return nil
}

// stage writes data to a temp file beside path and returns its name.
func stage(path string, data []byte) (string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("stage %s: %w", path, err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(name, 0o644)); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("stage %s: %w", path, err)
	}
	return name, nil
}

// hasNode reports whether g holds a node with this ID.
func hasNode(g *graph.Graph, id string) bool {
	_, ok := g.NodeByID(id)
	return ok
}
