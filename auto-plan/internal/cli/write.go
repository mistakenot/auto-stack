package cli

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-plan/internal/workspace"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// edgeOut is an edge in command output.
type edgeOut struct {
	ID   string `json:"id"`
	From string `json:"from"`
	Type string `json:"type"`
	To   string `json:"to"`
}

func toEdgeOut(e graph.Edge) edgeOut { return edgeOut{ID: e.ID, From: e.From, Type: e.Type, To: e.To} }

// fieldFlag binds one generated flag to a node field (or an object member).
type fieldFlag struct {
	flag   string
	field  string
	member string // non-empty for a KindObject member
	spec   schema.FieldSpec
}

// flagMode selects which generated flags a command gets.
type flagMode int

const (
	// forAdd: every field, marked [required] where the registry says so, plus
	// one repeatable flag per edge type the node may start.
	forAdd flagMode = iota
	// forUpdate: every field except Fixed ones; an empty value removes a field.
	forUpdate
)

// typeFlags generates a node type's flags from the registry (D-7): one typed
// flag per field (object members become --<field>-<member>) and, for add, one
// repeatable flag per edge type the node may start, named after the edge.
func typeFlags(nt schema.NodeType, mode flagMode) (*pflag.FlagSet, []fieldFlag, []schema.EdgeType) {
	fs := pflag.NewFlagSet(nt.Name, pflag.ContinueOnError)
	fs.SortFlags = false
	var bound []fieldFlag
	define := func(name string, f schema.FieldSpec) {
		usage := flagUsage(f, mode)
		if f.Kind == schema.KindList {
			fs.StringArray(name, nil, usage)
		} else {
			fs.String(name, "", usage)
		}
	}
	for i := range nt.Fields {
		f := &nt.Fields[i]
		if mode == forUpdate && f.Fixed {
			continue
		}
		if f.Kind == schema.KindObject {
			for j := range f.Fields {
				m := &f.Fields[j]
				name := f.Name + "-" + m.Name
				define(name, *m)
				bound = append(bound, fieldFlag{flag: name, field: f.Name, member: m.Name, spec: *m})
			}
			continue
		}
		define(f.Name, *f)
		bound = append(bound, fieldFlag{flag: f.Name, field: f.Name, spec: *f})
	}
	var edges []schema.EdgeType
	if mode == forAdd {
		edges = schema.Registry.EdgesFrom(nt.Name)
		for _, e := range edges {
			fs.StringArray(e.Name, nil, fmt.Sprintf("%s (edge to %s; repeatable)", e.Help, e.ToLabel()))
		}
	}
	fs.Bool("text", false, "human-readable text output instead of JSON")
	fs.BoolP("help", "h", false, "help for this type")
	return fs, bound, edges
}

func flagUsage(f schema.FieldSpec, mode flagMode) string {
	u := f.Help
	switch f.Kind {
	case schema.KindEnum:
		u += " (" + strings.Join(f.Enum, "|") + ")"
	case schema.KindText:
		u += " (@file or @- reads it)"
	case schema.KindList:
		u += " (repeatable)"
	case schema.KindString, schema.KindObject:
	}
	switch {
	case mode == forAdd && f.Required:
		u += " [required]"
	case mode == forUpdate && !f.Required:
		u += " (\"\" removes it)"
	}
	return u
}

// addLong documents every addable type and its generated flags.
func addLong() string {
	var b strings.Builder
	b.WriteString("Add a node to a plan. The ID (type prefix + 4 characters) and the rank are generated,\n")
	b.WriteString("and the whole graph is validated before anything is written.\n\n")
	b.WriteString("Flags are generated from the type registry; `auto plan add <plan> <type> --help` shows one type.\n")
	for _, name := range planTypes() {
		nt, _ := schema.Registry.Node(name)
		fs, _, _ := typeFlags(nt, forAdd)
		fmt.Fprintf(&b, "\n%s — %s\n%s", nt.Name, nt.Help, fs.FlagUsages())
	}
	return b.String()
}

func newAddCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "add <plan> <type> [flags]",
		Short: "Add a node (" + strings.Join(planTypes(), ", ") + ") with registry-generated flags",
		Long:  addLong(),
		Example: `  auto plan add 004 goal --title "An agent gets one stage's context in one call"
  auto plan add 004 ac --proves g-k7q2 --title "brief is self-contained" --gwt @ac.md --verify-cmd "go test ./e2e -run Brief"`,
		// Flags depend on <type>, so they are parsed after it is known.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, application, args)
		},
	}
}

func runAdd(cmd *cobra.Command, application *app.App, args []string) error {
	text := slices.Contains(args, "--text")
	if len(args) < 2 || strings.HasPrefix(args[0], "-") || strings.HasPrefix(args[1], "-") {
		if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
			return cmd.Help()
		}
		return failOne(cmd, text, "usage", "args", "", "add needs <plan> <type> before any flags", strings.Join(args, " "),
			"auto plan add <plan> <type> [flags]; types: "+strings.Join(planTypes(), ", "))
	}
	planArg, typ := args[0], args[1]
	nt, ok := schema.Registry.Node(typ)
	if !ok || nt.Singleton() {
		return failOne(cmd, text, graph.CodeUnregisteredType, "args.type", "type", fmt.Sprintf("%q is not a type that can be added", typ), typ,
			"use one of: "+strings.Join(planTypes(), ", "))
	}

	fs, bound, edgeTypes := typeFlags(nt, forAdd)
	fs.SetOutput(cmd.ErrOrStderr())
	if err := fs.Parse(args[2:]); err != nil {
		return failOne(cmd, text, "usage", "flags", "", err.Error(), nil, "see `auto plan add "+planArg+" "+typ+" --help`")
	}
	if help, _ := fs.GetBool("help"); help {
		fmt.Fprintf(cmd.OutOrStdout(), "Usage:\n  auto plan add <plan> %s [flags]\n\n%s — %s\n\nFlags:\n%s", typ, nt.Name, nt.Help, fs.FlagUsages())
		return nil
	}
	if fs.NArg() > 0 {
		return failOne(cmd, text, "usage", "args", "", "unexpected arguments: "+strings.Join(fs.Args(), " "), fs.Args(),
			"quote values that contain spaces; see `auto plan add "+planArg+" "+typ+" --help`")
	}

	fields, err := collectFields(cmd, application, fs, bound, false)
	if err != nil {
		return failOne(cmd, text, "usage", "flags", "", err.Error(), nil, "check the @file path")
	}
	var edges []graph.Edge
	for _, et := range edgeTypes {
		targets, _ := fs.GetStringArray(et.Name)
		for _, to := range targets {
			edges = append(edges, graph.Edge{Type: et.Name, To: to})
		}
	}

	l, err := loadForWrite(cmd, application, text, planArg)
	if err != nil {
		return err
	}
	if err := l.expandPlanRefs(cmd, text, nt, fields); err != nil {
		return err
	}
	if err := l.expandEdgeTargets(cmd, text, edges); err != nil {
		return err
	}
	node, own, errs := l.graph.Add(typ, fields, edges)
	if len(errs) > 0 {
		return fail(cmd, text, errs, "graph.json was not changed; fix the flagged values (see `auto plan add "+l.ref()+" "+typ+" --help`)")
	}
	if err := l.checkCrossPlan(cmd, text, own, fields); err != nil {
		return err
	}
	if err := l.save(cmd, text); err != nil {
		return err
	}

	out := map[string]any{"type": node.Type, "rank": node.Rank}
	var added []edgeOut
	for _, e := range own {
		added = append(added, toEdgeOut(e))
	}
	if len(added) > 0 {
		out["edges"] = added
	}
	return emit(cmd, text, mutationResult(l.ref(), node.ID, out), func() string {
		var b strings.Builder
		fmt.Fprintf(&b, "added %s %s (rank %s) to %s\n", node.Type, node.ID, node.Rank, l.plan.Folder())
		for _, e := range added {
			fmt.Fprintf(&b, "  %s  %s %s %s\n", e.ID, e.From, e.Type, e.To)
		}
		return b.String()
	})
}

// expandPlanRefs expands each bare plan number in the registry's PlanRef
// fields (`--epic 004`, `--plan 004`, `--deferred 004`) to the plan ID of the
// one plan that has it, before anything is validated or saved. A number that
// names no plan or several fails the write.
func (l *loaded) expandPlanRefs(cmd *cobra.Command, text bool, nt schema.NodeType, fields map[string]any) error {
	for i := range nt.Fields {
		f := &nt.Fields[i]
		val, ok := fields[f.Name]
		if !f.PlanRef || !ok {
			continue
		}
		expand := func(v any) (any, error) {
			s, ok := v.(string)
			if !ok || !planNumberRE.MatchString(strings.TrimSpace(s)) {
				return v, nil
			}
			set, err := l.planSet(cmd, text)
			if err != nil {
				return nil, err
			}
			full, err := set.ExpandPlan(strings.TrimSpace(s))
			if err != nil {
				code := "plan-not-found"
				if f.Name == "epic" {
					code = "epic-not-found"
				}
				if errors.Is(err, workspace.ErrAmbiguous) {
					return nil, planArgFailure(cmd, text, "flags."+f.Name, f.Name, s, err, "")
				}
				return nil, failOne(cmd, text, code, "flags."+f.Name, f.Name, "--"+f.Name+" "+s+": "+err.Error(), s,
					"graph.json was not changed; name an existing plan by number or ID (list them with `auto plan list`)")
			}
			return full, nil
		}
		switch v := val.(type) {
		case []any:
			out := make([]any, len(v))
			for j, item := range v {
				x, err := expand(item)
				if err != nil {
					return err
				}
				out[j] = x
			}
			fields[f.Name] = out
		default:
			x, err := expand(v)
			if err != nil {
				return err
			}
			fields[f.Name] = x
		}
	}
	return nil
}

// expandEdgeTargets expands shorthand qualified targets (`004:r-8hw3`) to
// `004-k7q2:r-8hw3` before anything is validated or saved.
func (l *loaded) expandEdgeTargets(cmd *cobra.Command, text bool, edges []graph.Edge) error {
	for i := range edges {
		to, err := l.expandRef(cmd, text, edges[i], "--"+edges[i].Type)
		if err != nil {
			return err
		}
		edges[i].To = to
	}
	return nil
}

// expandRef expands one edge's shorthand target; flag names where it came from.
func (l *loaded) expandRef(cmd *cobra.Command, text bool, e graph.Edge, flag string) (string, error) {
	if !graph.ShortRefPattern.MatchString(e.To) {
		return e.To, nil
	}
	set, err := l.planSet(cmd, text)
	if err != nil {
		return "", err
	}
	full, err := set.ExpandRef(e.To)
	if err == nil {
		return full, nil
	}
	planID, _, _ := graph.ParseRef(e.To)
	if errors.Is(err, workspace.ErrAmbiguous) {
		return "", planArgFailure(cmd, text, graph.EdgePath(e)+".to", "to", e.To, err, "")
	}
	return "", failOne(cmd, text, graph.CodeDanglingRef, graph.EdgePath(e)+".to", "to",
		fmt.Sprintf("%s %s: there is no plan %s under %s", flag, e.To, planID, workspace.PlansDir), e.To,
		"graph.json was not changed; a cross-plan target is NNN:ID or NNN-xxxx:ID in an existing plan (list plans with `auto plan list`)")
}

// collectFields turns the flags that were set into node fields. Text values
// accept @file / @-. With clear set (update), an empty value becomes nil,
// which removes the field (or object member).
func collectFields(cmd *cobra.Command, application *app.App, fs *pflag.FlagSet, bound []fieldFlag, clear bool) (map[string]any, error) {
	fields := map[string]any{}
	for i := range bound {
		b := &bound[i]
		if !fs.Changed(b.flag) {
			continue
		}
		var val any
		if b.spec.Kind == schema.KindList {
			items, _ := fs.GetStringArray(b.flag)
			list := make([]any, len(items))
			for i, it := range items {
				list[i] = it
			}
			val = list
			if clear && len(items) == 1 && items[0] == "" {
				val = nil
			}
		} else {
			s, _ := fs.GetString(b.flag)
			if b.spec.Kind == schema.KindText {
				expanded, err := readValue(cmd, application, s)
				if err != nil {
					return nil, fmt.Errorf("--%s: %w", b.flag, err)
				}
				s = expanded
			}
			val = s
			if clear && s == "" {
				val = nil
			}
		}
		if b.member == "" {
			fields[b.field] = val
			continue
		}
		obj, _ := fields[b.field].(map[string]any)
		if obj == nil {
			obj = map[string]any{}
			fields[b.field] = obj
		}
		obj[b.member] = val
	}
	return fields, nil
}

// linkLong documents every edge type and its allowed endpoints.
func linkLong() string {
	var b strings.Builder
	b.WriteString("Add one typed edge. Endpoint types are checked against the registry, and the whole graph\n")
	b.WriteString("is validated before anything is written.\n\nEdge types (from → to):\n")
	for _, e := range schema.Registry.Edges {
		note := ""
		if e.CrossPlan {
			note = " (target may be another plan's node: NNN-xxxx:ID, or NNN:ID)"
		}
		fmt.Fprintf(&b, "  %-12s %s → %s  %s%s\n", e.Name, strings.Join(e.From, "|"), e.ToLabel(), e.Help, note)
	}
	return b.String()
}

func newLinkCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "link <plan> <from> <edge> <to>",
		Short:   "Add a typed edge between two nodes",
		Long:    linkLong(),
		Example: "  auto plan link 004 d-9t2w constrains g-k7q2",
		Args:    cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLink(cmd, application, args[0], args[1], args[2], args[3])
		},
	}
}

func runLink(cmd *cobra.Command, application *app.App, planArg, from, typ, to string) error {
	text := textMode(cmd)
	l, err := loadForWrite(cmd, application, text, planArg)
	if err != nil {
		return err
	}
	to, err = l.expandRef(cmd, text, graph.Edge{From: from, Type: typ, To: to}, "link")
	if err != nil {
		return err
	}
	e, errs := l.graph.Link(from, typ, to)
	if len(errs) > 0 {
		return fail(cmd, text, errs, "graph.json was not changed; check both IDs exist and the edge type fits them (`auto plan link --help`)")
	}
	if err := l.checkCrossPlan(cmd, text, []graph.Edge{e}, nil); err != nil {
		return err
	}
	if err := l.save(cmd, text); err != nil {
		return err
	}
	out := toEdgeOut(e)
	return emit(cmd, text, mutationResult(l.ref(), from, map[string]any{"edges": []edgeOut{out}}), func() string {
		return fmt.Sprintf("linked %s %s %s %s in %s\n", out.ID, out.From, out.Type, out.To, l.plan.Folder())
	})
}

// updateLong documents every type's updatable flags.
func updateLong() string {
	var b strings.Builder
	b.WriteString("Change fields of one node. Flags are generated from the node's type in the registry;\n")
	b.WriteString("only the flags given change, and an empty value (\"\") removes an optional field.\n")
	b.WriteString("Edges change with link/unlink, status with retire, and reading order with move.\n")
	b.WriteString("`auto plan update <plan> <id> --help` shows one node's flags.\n")
	for _, nt := range schema.Registry.Nodes {
		fs, _, _ := typeFlags(nt, forUpdate)
		fmt.Fprintf(&b, "\n%s — %s\n%s", nt.Name, nt.Help, fs.FlagUsages())
	}
	return b.String()
}

func newUpdateCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "update <plan> <id> [flags]",
		Short: "Change a node's fields with registry-generated flags (plan --lifecycle advances a plan)",
		Long:  updateLong(),
		Example: `  auto plan update 004 plan --lifecycle solution
  auto plan update 004 ac-3fxm --gwt @ac.md --verify-cmd "go test ./e2e -run Brief"
  auto plan update 004 g-k7q2 --description ""`,
		// Flags depend on the node's type, so they are parsed after it is known.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd, application, args)
		},
	}
}

func runUpdate(cmd *cobra.Command, application *app.App, args []string) error {
	text := slices.Contains(args, "--text")
	if len(args) < 2 || strings.HasPrefix(args[0], "-") || strings.HasPrefix(args[1], "-") {
		if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
			return cmd.Help()
		}
		return failOne(cmd, text, "usage", "args", "", "update needs <plan> <id> before any flags", strings.Join(args, " "),
			"auto plan update <plan> <id> [flags]; see `auto plan update --help`")
	}
	planArg, id := args[0], args[1]
	l, err := loadForWrite(cmd, application, text, planArg)
	if err != nil {
		return err
	}
	node, ok := l.graph.NodeByID(id)
	if !ok {
		return failOne(cmd, text, graph.CodeNodeNotFound, "args.id", "id", fmt.Sprintf("no node %q in plan %s", id, l.ref()), id,
			"list the plan's nodes with `auto plan show "+l.ref()+"`")
	}
	nt, _ := schema.Registry.Node(node.Type) // registered: loadForWrite validated the graph

	fs, bound, _ := typeFlags(nt, forUpdate)
	fs.SetOutput(cmd.ErrOrStderr())
	hint := "see `auto plan update " + l.ref() + " " + id + " --help`"
	if err := fs.Parse(args[2:]); err != nil {
		return failOne(cmd, text, "usage", "flags", "", err.Error(), nil, hint)
	}
	if help, _ := fs.GetBool("help"); help {
		fmt.Fprintf(cmd.OutOrStdout(), "Usage:\n  auto plan update <plan> %s [flags]\n\n%s — %s\n\nFlags:\n%s", id, nt.Name, nt.Help, fs.FlagUsages())
		return nil
	}
	if fs.NArg() > 0 {
		return failOne(cmd, text, "usage", "args", "", "unexpected arguments: "+strings.Join(fs.Args(), " "), fs.Args(),
			"quote values that contain spaces; "+hint)
	}
	fields, err := collectFields(cmd, application, fs, bound, true)
	if err != nil {
		return failOne(cmd, text, "usage", "flags", "", err.Error(), nil, "check the @file path")
	}
	if len(fields) == 0 {
		return failOne(cmd, text, "usage", "flags", "", "update needs at least one field flag", nil, hint)
	}
	if err := l.expandPlanRefs(cmd, text, nt, fields); err != nil {
		return err
	}

	changed, errs := l.graph.Update(id, fields)
	if len(errs) > 0 {
		return fail(cmd, text, errs, "graph.json was not changed; fix the flagged values ("+hint+")")
	}
	if nt.Name == schema.PlanNodeID {
		if err := l.checkCrossPlan(cmd, text, nil, fields); err != nil {
			return err
		}
	}
	if err := l.save(cmd, text); err != nil {
		return err
	}
	return emit(cmd, text, mutationResult(l.ref(), id, map[string]any{"type": nt.Name, "fields": changed}), func() string {
		names := slices.Sorted(maps.Keys(changed))
		if len(names) == 0 {
			return fmt.Sprintf("%s %s in %s: nothing changed\n", nt.Name, id, l.plan.Folder())
		}
		return fmt.Sprintf("updated %s %s in %s: %s\n", nt.Name, id, l.plan.Folder(), strings.Join(names, ", "))
	})
}

func newUnlinkCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <plan> <edge-id> | unlink <plan> <from> <edge> <to>",
		Short: "Remove one typed edge, named by its ID or by its endpoints",
		Long: `Remove one edge, named by its edge ID (e-xxxx; get and card print it) or by <from> <edge> <to>.
Both nodes are kept, and the whole graph is validated before anything is written.`,
		Example: "  auto plan unlink 004 e-7k2q\n  auto plan unlink 004 d-9t2w constrains g-k7q2",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 && len(args) != 4 {
				return fmt.Errorf("unlink takes <plan> <edge-id> or <plan> <from> <edge> <to>, got %d argument(s)", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUnlink(cmd, application, args[0], args[1:])
		},
	}
}

func runUnlink(cmd *cobra.Command, application *app.App, planArg string, edge []string) error {
	text := textMode(cmd)
	l, err := loadForWrite(cmd, application, text, planArg)
	if err != nil {
		return err
	}
	var e graph.Edge
	var errs []graph.ValidationError
	if len(edge) == 1 {
		e, errs = l.graph.UnlinkID(edge[0])
	} else {
		to, err := l.expandRef(cmd, text, graph.Edge{From: edge[0], Type: edge[1], To: edge[2]}, "unlink")
		if err != nil {
			return err
		}
		e, errs = l.graph.Unlink(edge[0], edge[1], to)
	}
	if len(errs) > 0 {
		return fail(cmd, text, errs, "graph.json was not changed; name an existing edge by its ID or as <from> <edge> <to> (see `auto plan get "+l.ref()+" <node-id>`)")
	}
	if err := l.save(cmd, text); err != nil {
		return err
	}
	out := toEdgeOut(e)
	return emit(cmd, text, mutationResult(l.ref(), e.From, map[string]any{"removed": []edgeOut{out}}), func() string {
		return fmt.Sprintf("unlinked %s %s %s %s in %s\n", out.ID, out.From, out.Type, out.To, l.plan.Folder())
	})
}

func newRetireCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "retire <plan> <id>",
		Short: "Retire a node: status becomes retired, the ID and its edges are kept",
		Long: `Retire a node. Its status becomes retired; its ID, fields, rank and edges are kept, and the ID
is never handed out again. Retiring twice is a no-op. The plan node cannot be retired.`,
		Example: "  auto plan retire 004 ac-7w1e",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRetire(cmd, application, args[0], args[1])
		},
	}
}

func runRetire(cmd *cobra.Command, application *app.App, planArg, id string) error {
	text := textMode(cmd)
	l, err := loadForWrite(cmd, application, text, planArg)
	if err != nil {
		return err
	}
	n, errs := l.graph.Retire(id)
	if len(errs) > 0 {
		return fail(cmd, text, errs, "graph.json was not changed; name an existing node other than plan (see `auto plan show "+l.ref()+"`)")
	}
	if err := l.save(cmd, text); err != nil {
		return err
	}
	return emit(cmd, text, mutationResult(l.ref(), n.ID, map[string]any{"type": n.Type, "status": n.Status}), func() string {
		return fmt.Sprintf("retired %s %s in %s\n", n.Type, n.ID, l.plan.Folder())
	})
}

func newMoveCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "move <plan> <id> --before <id> | --after <id>",
		Short: "Reorder a node among its siblings (rewrites one rank, no ID changes)",
		Long: `Give a node one new rank so it reads directly before or after a sibling. No other node
changes. Siblings share a type and, for ACs, the goal they prove (legs: the journey they are in).`,
		Example: "  auto plan move 004 g-m4t8 --before g-k7q2",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			before, _ := cmd.Flags().GetString("before")
			after, _ := cmd.Flags().GetString("after")
			return runMove(cmd, application, args[0], args[1], before, after)
		},
	}
	cmd.Flags().String("before", "", "sibling ID to place the node directly before")
	cmd.Flags().String("after", "", "sibling ID to place the node directly after")
	return cmd
}

func runMove(cmd *cobra.Command, application *app.App, planArg, id, before, after string) error {
	text := textMode(cmd)
	if (before == "") == (after == "") {
		return failOne(cmd, text, "usage", "flags", "", "move needs exactly one of --before or --after", nil,
			"auto plan move <plan> <id> --before <sibling> (or --after <sibling>)")
	}
	anchor := before + after
	l, err := loadForWrite(cmd, application, text, planArg)
	if err != nil {
		return err
	}
	n, errs := l.graph.Move(id, anchor, after != "")
	if len(errs) > 0 {
		return fail(cmd, text, errs, "graph.json was not changed; move a node relative to a sibling of the same type (see `auto plan show "+l.ref()+"`)")
	}
	if err := l.save(cmd, text); err != nil {
		return err
	}
	return emit(cmd, text, mutationResult(l.ref(), n.ID, map[string]any{"type": n.Type, "rank": n.Rank}), func() string {
		return fmt.Sprintf("moved %s %s to rank %s in %s\n", n.Type, n.ID, n.Rank, l.plan.Folder())
	})
}

// checkCrossPlan resolves what a write points at in other plans before it is
// saved: the qualified target of each cross-plan edge must be an existing node
// of a type the edge allows (D-9), and a plan's `epic` must name an existing
// epic plan. On failure nothing is written.
func (l *loaded) checkCrossPlan(cmd *cobra.Command, text bool, edges []graph.Edge, fields map[string]any) error {
	epic, _ := fields["epic"].(string)
	qualified := slices.ContainsFunc(edges, func(e graph.Edge) bool {
		_, _, q := graph.ParseRef(e.To)
		return q
	})
	if !qualified && epic == "" {
		return nil
	}
	set, err := l.planSet(cmd, text)
	if err != nil {
		return err
	}
	var errs []graph.ValidationError
	for _, e := range edges {
		errs = append(errs, set.CheckEdge(e)...)
	}
	if len(errs) > 0 {
		return fail(cmd, text, errs, "graph.json was not changed; a cross-plan target is NNN:ID (or NNN-xxxx:ID) in an existing plan "+
			"(find it with `auto plan list <plan>` or `auto plan search all <text>`) and of a type the edge allows (`auto plan link --help`)")
	}
	if epic != "" {
		if err := set.CheckEpic(epic); err != nil {
			return failOne(cmd, text, "epic-not-found", graph.NodePath(schema.PlanNodeID)+".fields.epic", "epic", err.Error(), epic,
				"graph.json was not changed; name an existing epic plan (create one with `auto plan new <name> --kind epic`)")
		}
	}
	return nil
}
