package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// edgeOut is an edge in command output.
type edgeOut struct {
	From string `json:"from"`
	Type string `json:"type"`
	To   string `json:"to"`
}

func toEdgeOut(e graph.Edge) edgeOut { return edgeOut{From: e.From, Type: e.Type, To: e.To} }

// fieldFlag binds one generated flag to a node field (or an object member).
type fieldFlag struct {
	flag   string
	field  string
	member string // non-empty for a KindObject member
	spec   schema.FieldSpec
}

// typeFlags generates a node type's flags from the registry (D-7): one typed
// flag per field (object members become --<field>-<member>), and one
// repeatable flag per edge type the node may start, named after the edge.
func typeFlags(nt schema.NodeType) (*pflag.FlagSet, []fieldFlag, []schema.EdgeType) {
	fs := pflag.NewFlagSet(nt.Name, pflag.ContinueOnError)
	fs.SortFlags = false
	var bound []fieldFlag
	define := func(name string, f schema.FieldSpec) {
		if f.Kind == schema.KindList {
			fs.StringArray(name, nil, flagUsage(f))
		} else {
			fs.String(name, "", flagUsage(f))
		}
	}
	for _, f := range nt.Fields {
		if f.Kind == schema.KindObject {
			for _, m := range f.Fields {
				name := f.Name + "-" + m.Name
				define(name, m)
				bound = append(bound, fieldFlag{flag: name, field: f.Name, member: m.Name, spec: m})
			}
			continue
		}
		define(f.Name, f)
		bound = append(bound, fieldFlag{flag: f.Name, field: f.Name, spec: f})
	}
	edges := schema.Registry.EdgesFrom(nt.Name)
	for _, e := range edges {
		fs.StringArray(e.Name, nil, fmt.Sprintf("%s (edge to %s; repeatable)", e.Help, strings.Join(e.To, "|")))
	}
	fs.Bool("text", false, "human-readable text output instead of JSON")
	fs.BoolP("help", "h", false, "help for this type")
	return fs, bound, edges
}

func flagUsage(f schema.FieldSpec) string {
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
	if f.Required {
		u += " [required]"
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
		fs, _, _ := typeFlags(nt)
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

	fs, bound, edgeTypes := typeFlags(nt)
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

	fields, err := collectFields(cmd, application, fs, bound)
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
	node, errs := l.graph.Add(typ, fields, edges)
	if len(errs) > 0 {
		return fail(cmd, text, errs, "graph.json was not changed; fix the flagged values (see `auto plan add "+l.plan.ID+" "+typ+" --help`)")
	}
	if err := l.save(cmd, text); err != nil {
		return err
	}

	out := map[string]any{"type": node.Type, "rank": node.Rank}
	var added []edgeOut
	for _, e := range edges {
		added = append(added, edgeOut{From: node.ID, Type: e.Type, To: e.To})
	}
	if len(added) > 0 {
		out["edges"] = added
	}
	return emit(cmd, text, mutationResult(l.plan.ID, node.ID, out), func() string {
		var b strings.Builder
		fmt.Fprintf(&b, "added %s %s (rank %s) to %s\n", node.Type, node.ID, node.Rank, l.plan.Folder())
		for _, e := range added {
			fmt.Fprintf(&b, "  %s %s %s\n", e.From, e.Type, e.To)
		}
		return b.String()
	})
}

// collectFields turns the flags that were set into node fields. Text values
// accept @file / @-.
func collectFields(cmd *cobra.Command, application *app.App, fs *pflag.FlagSet, bound []fieldFlag) (map[string]any, error) {
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
		fmt.Fprintf(&b, "  %-12s %s → %s  %s\n", e.Name, strings.Join(e.From, "|"), strings.Join(e.To, "|"), e.Help)
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
	e, errs := l.graph.Link(from, typ, to)
	if len(errs) > 0 {
		return fail(cmd, text, errs, "graph.json was not changed; check both IDs exist and the edge type fits them (`auto plan link --help`)")
	}
	if err := l.save(cmd, text); err != nil {
		return err
	}
	out := toEdgeOut(e)
	return emit(cmd, text, mutationResult(l.plan.ID, from, map[string]any{"edges": []edgeOut{out}}), func() string {
		return fmt.Sprintf("linked %s %s %s in %s\n", out.From, out.Type, out.To, l.plan.Folder())
	})
}
