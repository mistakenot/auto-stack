package cli

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/render"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-plan/internal/workspace"
	"github.com/spf13/cobra"
)

// listItem is one node on the cheap rungs (list, search): IDs, metadata and
// title only.
type listItem struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Rank   string `json:"rank"`
	Title  string `json:"title"`
}

// planList is one plan's list result.
type planList struct {
	Plan      string     `json:"plan"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Lifecycle string     `json:"lifecycle"`
	Nodes     []listItem `json:"nodes"`
}

type listAllResult struct {
	Plans []planList `json:"plans"`
}

func newListCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <plan|all> [--type <type>]",
		Short: "List a plan's nodes (or every plan's): IDs, metadata and titles only",
		Long: `List nodes with their ID, type, status, rank and title, in registry type order and then
reading order. Retired nodes are included (status says so). With no filter every node is
listed; --type keeps one node type (trimmed, case-insensitive, checked against the registry).

One plan prints {plan, name, kind, lifecycle, nodes:[…]}; "all" prints {plans:[…]}. A plan whose
graph.json is malformed is reported on stderr, the other plans are still listed, and the exit
code is 1. Use describe for a summary and get for the full node.`,
		Example: "  auto plan list 004 --type ac\n  auto plan list all --text",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, _ := cmd.Flags().GetString("type")
			return runList(cmd, application, args[0], typ)
		},
	}
	cmd.Flags().String("type", "", "only nodes of this type ("+strings.Join(nodeTypeNames(), "|")+")")
	return cmd
}

func runList(cmd *cobra.Command, application *app.App, arg, typ string) error {
	text := textMode(cmd)
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ != "" {
		if _, ok := schema.Registry.Node(typ); !ok {
			return failOne(cmd, text, "invalid-type", "flags.type", "type", "node type "+typ+" is not registered", typ,
				"use one of: "+strings.Join(nodeTypeNames(), ", "))
		}
	}
	plans, loadErrs, err := loadMany(cmd, application, text, arg)
	if err != nil {
		return err
	}
	result := listAllResult{Plans: []planList{}}
	for _, lp := range plans {
		pl := planHeader(lp)
		for _, n := range readingOrder(lp.graph.Nodes) {
			if typ == "" || n.Type == typ {
				pl.Nodes = append(pl.Nodes, item(n))
			}
		}
		result.Plans = append(result.Plans, pl)
	}
	var v any = result
	if arg != workspace.All && len(result.Plans) == 1 {
		v = result.Plans[0]
	}
	if err := emit(cmd, text, v, func() string { return listText(result.Plans) }); err != nil {
		return err
	}
	return reportLoadErrors(cmd, text, loadErrs)
}

func listText(plans []planList) string {
	var b strings.Builder
	if len(plans) == 0 {
		b.WriteString("no plans under " + workspace.PlansDir + "\n")
	}
	for i, p := range plans {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s-%s  %s · %s\n", p.Plan, p.Name, p.Kind, p.Lifecycle)
		if len(p.Nodes) == 0 {
			b.WriteString("  (no nodes)\n")
			continue
		}
		b.WriteString(itemsText(p.Nodes, ""))
	}
	return b.String()
}

// itemsText prints items with aligned ID, type and rank columns; retired
// nodes are marked. prefix leads every line (search uses the plan number).
func itemsText(items []listItem, prefix string) string {
	idW, typeW, rankW := 0, 0, 0
	for _, it := range items {
		idW, typeW, rankW = max(idW, len(it.ID)), max(typeW, len(it.Type)), max(rankW, len(it.Rank))
	}
	var b strings.Builder
	for _, it := range items {
		title := it.Title
		if it.Status == graph.StatusRetired {
			title += "  (retired)"
		}
		fmt.Fprintf(&b, "  %s%s %-*s  %-*s  %-*s  %s\n", prefix, render.Glyph(it.Type), idW, it.ID, typeW, it.Type, rankW, it.Rank, title)
	}
	return b.String()
}

func newDescribeCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <plan> <id>",
		Short: "Summarise one node: fields cut to ~200 characters, edge counts, and the get command",
		Long: fmt.Sprintf(`Print the summary rung of one node: ID, type, status, rank, title, every field with text cut to
%d characters (the cut fields are listed in "truncated"), and its edge counts by type in each
direction. "get" is the exact command that prints the full node.`, render.DescribeWidth),
		Example: "  auto plan describe 004 d-9t2w",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := textMode(cmd)
			p, g, set, err := loadInSet(cmd, application, text, args[0])
			if err != nil {
				return err
			}
			v, ok := render.Describe(p.ID, g, args[1], set)
			if !ok {
				return nodeNotFound(cmd, text, p, args[1])
			}
			return emitView(cmd, text, v)
		},
	}
}

func newGetCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "get <plan> <id>",
		Short: "Print one node in full, with every incoming and outgoing edge",
		Long: `Print one node at full fidelity: every field, and each outgoing ("out") and incoming ("in")
edge with the neighbour's ID, type, title and status. A neighbour in another plan (NNN:ID) is
marked qualified and resolved through docs/plans/NNN-*/graph.json; edges other plans hold into
the node are listed under "in" with their qualified source. A reference that does not resolve
is printed raw.`,
		Example: "  auto plan get 004 d-9t2w --text",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := textMode(cmd)
			p, g, set, err := loadInSet(cmd, application, text, args[0])
			if err != nil {
				return err
			}
			v, ok := render.Card(p.ID, g, args[1], set)
			if !ok {
				return nodeNotFound(cmd, text, p, args[1])
			}
			return emitView(cmd, text, v)
		},
	}
}

// searchMatch is one node whose text contains the query.
type searchMatch struct {
	Plan string `json:"plan"`
	listItem
	// Fields names where the query matched ("id" for the ID itself;
	// object members as field.member).
	Fields []string `json:"fields"`
}

type searchResult struct {
	Query   string        `json:"query"`
	Matches []searchMatch `json:"matches"`
}

func newSearchCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "search <plan|all> <query>",
		Short: "Find nodes whose ID or text contains a query (case-insensitive)",
		Long: `Find nodes in one plan (or every plan) whose ID or any text field contains the query. The query
is trimmed and matched case-insensitively as a plain substring. Each match carries the cheap
list metadata plus the fields that matched; retired nodes match too. No match is not an error.

Prints {query, matches:[{plan, id, type, status, rank, title, fields}]}. A plan whose graph.json
is malformed is reported on stderr and the exit code is 1.`,
		Example: "  auto plan search all \"stage brief\"\n  auto plan search 004 verify --text",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, application, args[0], args[1])
		},
	}
}

func runSearch(cmd *cobra.Command, application *app.App, arg, query string) error {
	text := textMode(cmd)
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return failOne(cmd, text, "usage", "args.query", "query", "search needs a non-blank query", nil,
			"auto plan search <plan|all> <text>")
	}
	plans, loadErrs, err := loadMany(cmd, application, text, arg)
	if err != nil {
		return err
	}
	result := searchResult{Query: query, Matches: []searchMatch{}}
	for _, lp := range plans {
		for _, n := range readingOrder(lp.graph.Nodes) {
			if fields := matchFields(n, query); len(fields) > 0 {
				result.Matches = append(result.Matches, searchMatch{Plan: lp.plan.ID, listItem: item(n), Fields: fields})
			}
		}
	}
	if err := emit(cmd, text, result, func() string { return searchText(result) }); err != nil {
		return err
	}
	return reportLoadErrors(cmd, text, loadErrs)
}

// matchFields returns where query occurs in n: "id", then fields in sorted
// order.
func matchFields(n graph.Node, query string) []string {
	var out []string
	if strings.Contains(strings.ToLower(n.ID), query) {
		out = append(out, "id")
	}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case string:
			if strings.Contains(strings.ToLower(x), query) && !slices.Contains(out, path) {
				out = append(out, path)
			}
		case []any:
			for _, el := range x {
				walk(path, el)
			}
		case map[string]any:
			for _, k := range slices.Sorted(maps.Keys(x)) {
				walk(path+"."+k, x[k])
			}
		}
	}
	for _, k := range slices.Sorted(maps.Keys(n.Fields)) {
		walk(k, n.Fields[k])
	}
	return out
}

func searchText(r searchResult) string {
	if len(r.Matches) == 0 {
		return fmt.Sprintf("no matches for %q\n", r.Query)
	}
	var b strings.Builder
	noun := "matches"
	if len(r.Matches) == 1 {
		noun = "match"
	}
	fmt.Fprintf(&b, "%d %s for %q\n\n", len(r.Matches), noun, r.Query)
	items := make([]listItem, len(r.Matches))
	for i, m := range r.Matches {
		items[i] = m.listItem
	}
	lines := strings.SplitAfter(itemsText(items, ""), "\n")
	for i, m := range r.Matches {
		b.WriteString("  " + m.Plan + strings.TrimSuffix(lines[i], "\n") + "  [" + strings.Join(m.Fields, ", ") + "]\n")
	}
	return b.String()
}

// loadedPlan is a plan decoded for reading.
type loadedPlan struct {
	plan  workspace.Plan
	graph *graph.Graph
}

// loadMany resolves a plan argument (`all` included), decodes every plan and
// validates it. Plans that decode are always returned, so listings keep every
// available result; malformed and structurally invalid plans add errors.
// A plan that fails to decode is collected as an error and skipped, so the
// others are still returned.
func loadMany(cmd *cobra.Command, application *app.App, text bool, arg string) ([]loadedPlan, []graph.ValidationError, error) {
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return nil, nil, err
	}
	plans, err := ws.Resolve(arg)
	if err != nil {
		return nil, nil, failOne(cmd, text, "plan-not-found", "args.plan", "plan", err.Error(), arg,
			"name a plan as NNN, NNN-name, a path, or all")
	}
	var out []loadedPlan
	var errs []graph.ValidationError
	for _, p := range plans {
		g, err := graph.Decode(ws.GraphPath(p))
		if err != nil {
			errs = append(errs, graph.ValidationError{Code: "parse-error", Path: p.Dir + "/" + workspace.GraphFile, Field: "plan", Message: err.Error(), Value: p.ID})
			continue
		}
		for _, ve := range graph.Validate(g) {
			ve.Message = "plan " + p.ID + ": " + ve.Message
			errs = append(errs, ve)
		}
		out = append(out, loadedPlan{plan: p, graph: g})
	}
	return out, errs, nil
}

// reportLoadErrors prints plans that failed to load or validate (after the
// results) and returns exit 1, or nil when there were none.
func reportLoadErrors(cmd *cobra.Command, text bool, errs []graph.ValidationError) error {
	if len(errs) == 0 {
		return nil
	}
	return fail(cmd, text, errs, "every readable plan was listed; run `auto plan lint all` for each problem and its fix")
}

func planHeader(lp loadedPlan) planList {
	pl := planList{Plan: lp.plan.ID, Name: lp.plan.Name, Nodes: []listItem{}}
	if n, ok := lp.graph.Plan(); ok {
		pl.Kind, pl.Lifecycle = n.StringField("kind"), n.StringField("lifecycle")
	}
	return pl
}

func item(n graph.Node) listItem {
	return listItem{ID: n.ID, Type: n.Type, Status: n.Status, Rank: n.Rank, Title: render.NodeTitle(n)}
}

// readingOrder sorts nodes by registry type order, then rank, then ID.
func readingOrder(nodes []graph.Node) []graph.Node {
	out := slices.Clone(nodes)
	typeIndex := func(t string) int {
		i := slices.IndexFunc(schema.Registry.Nodes, func(nt schema.NodeType) bool { return nt.Name == t })
		if i < 0 {
			return len(schema.Registry.Nodes)
		}
		return i
	}
	slices.SortStableFunc(out, func(a, b graph.Node) int {
		return cmp.Or(cmp.Compare(typeIndex(a.Type), typeIndex(b.Type)), cmp.Compare(a.Type, b.Type),
			cmp.Compare(a.Rank, b.Rank), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// nodeTypeNames lists every registered node type.
func nodeTypeNames() []string {
	out := make([]string, len(schema.Registry.Nodes))
	for i, nt := range schema.Registry.Nodes {
		out[i] = nt.Name
	}
	return out
}
