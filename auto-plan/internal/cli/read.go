package cli

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

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
		Use:   "list [<plan|all>] [--type <type>] [--kind task|epic]",
		Short: "List the plans (no argument), or a plan's nodes (or every plan's): IDs, metadata and titles only",
		Long: `Three forms, all on the cheap rung (IDs and metadata; describe and get go deeper):

  auto plan list               the plans themselves: one row per folder under .auto/plan/plans
  auto plan list <plan>        that plan's nodes
  auto plan list all           every plan's nodes

With no argument, each row is {id, number, name, path, kind, lifecycle, version, frozen,
frozen_reason, epic}: id is the plan ID, frozen says whether writes are refused and
frozen_reason why ("done": its lifecycle is done; "version": another format version wrote it),
and epic is the epic's plan ID (omitted when none). Rows are sorted by folder. --kind keeps one
plan kind (trimmed, case-insensitive). Prints {plans:[…]}.

With a plan, nodes are listed with their ID, type, status, rank and title, in registry type order
and then reading order. Retired nodes are included (status says so). --type keeps one node type
(trimmed, case-insensitive, checked against the registry). One plan prints {plan, name, kind,
lifecycle, nodes:[…]}; "all" prints {plans:[…]}.

Every readable plan is always listed. A plan whose graph.json is malformed or invalid, and two
folders sharing a plan number (duplicate-plan-number), are reported on stderr afterwards and the
exit code is 1.`,
		Example: "  auto plan list --text\n  auto plan list --kind epic\n  auto plan list 004 --type ac\n  auto plan list all --text",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, _ := cmd.Flags().GetString("type")
			kind, _ := cmd.Flags().GetString("kind")
			if len(args) == 0 {
				if cmd.Flags().Changed("type") {
					return failOne(cmd, textMode(cmd), "usage", "flags.type", "type", "--type filters a plan's nodes; name a plan (or all)", typ,
						"auto plan list <plan|all> --type "+typ+", or auto plan list --kind task|epic for the plans")
				}
				return runListPlans(cmd, application, kind)
			}
			if cmd.Flags().Changed("kind") {
				return failOne(cmd, textMode(cmd), "usage", "flags.kind", "kind", "--kind filters the plan listing; drop the plan argument", kind,
					"auto plan list --kind "+kind)
			}
			return runList(cmd, application, args[0], typ)
		},
	}
	cmd.Flags().String("type", "", "with a plan: only nodes of this type ("+strings.Join(nodeTypeNames(), "|")+")")
	cmd.Flags().String("kind", "", "with no plan: only plans of this kind ("+strings.Join(planKinds, "|")+")")
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

// planRow is one plan on the plan listing (`auto plan list`).
type planRow struct {
	ID        string `json:"id"`
	Number    string `json:"number"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Lifecycle string `json:"lifecycle"`
	Version   string `json:"version"`
	Frozen    bool   `json:"frozen"`
	// FrozenReason is "done" or "version" when Frozen.
	FrozenReason string `json:"frozen_reason,omitempty"`
	Epic         string `json:"epic,omitempty"`
}

type planRows struct {
	Plans []planRow `json:"plans"`
}

// Frozen reasons on the plan listing.
const (
	frozenDone    = "done"
	frozenVersion = "version"
)

func runListPlans(cmd *cobra.Command, application *app.App, kind string) error {
	text := textMode(cmd)
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "" && !slices.Contains(planKinds, kind) {
		return failOne(cmd, text, "invalid-kind", "flags.kind", "kind", "--kind must be one of "+strings.Join(planKinds, "|"), kind,
			"pass --kind task or --kind epic, or leave it out for every plan")
	}
	plans, loadErrs, err := loadMany(cmd, application, text, workspace.All)
	if err != nil {
		return err
	}
	// Collisions are counted over every discovered folder, so a malformed plan
	// that shares a number still makes the number ambiguous.
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return err
	}
	folders, err := ws.Plans()
	if err != nil {
		return failOne(cmd, text, "read-failed", workspace.PlansDir, "", err.Error(), nil, "check that "+workspace.PlansDir+" is readable")
	}
	numbers := map[string][]string{}
	for _, f := range folders {
		numbers[f.Number] = append(numbers[f.Number], f.Ref())
	}
	result := planRows{Plans: []planRow{}}
	for _, lp := range plans {
		g := lp.graph
		row := planRow{ID: g.ID, Number: lp.plan.Number, Name: lp.plan.Name, Path: lp.plan.Dir, Version: g.Version}
		if n, ok := g.Plan(); ok {
			row.Kind, row.Lifecycle, row.Epic = n.StringField("kind"), n.StringField("lifecycle"), n.StringField("epic")
		}
		switch {
		case g.OtherVersion():
			row.Frozen, row.FrozenReason = true, frozenVersion
		case row.Lifecycle == string(schema.LifecycleDone):
			row.Frozen, row.FrozenReason = true, frozenDone
		}
		if kind == "" || row.Kind == kind {
			result.Plans = append(result.Plans, row)
		}
	}
	for _, n := range slices.Sorted(maps.Keys(numbers)) {
		if ids := numbers[n]; len(ids) > 1 {
			loadErrs = append(loadErrs, graph.ValidationError{Code: "duplicate-plan-number", Path: workspace.PlansDir, Field: "plan", Value: ids,
				Message: "plan number " + n + " is used by " + strings.Join(ids, " and ") + "; renumber one with `auto plan renumber " + ids[len(ids)-1] + "`"})
		}
	}
	if err := emit(cmd, text, result, func() string { return planRowsText(result.Plans) }); err != nil {
		return err
	}
	return reportLoadErrors(cmd, text, loadErrs)
}

// planRowsText prints one aligned line per plan.
func planRowsText(rows []planRow) string {
	if len(rows) == 0 {
		return "no plans under " + workspace.PlansDir + "\n"
	}
	idW, nameW, kindW := 0, 0, 0
	kinds := make([]string, len(rows))
	for i := range rows {
		r := &rows[i]
		kinds[i] = r.Kind + " · " + r.Lifecycle
		idW, nameW, kindW = max(idW, len(r.ID)), max(nameW, len(r.Name)), max(kindW, utf8.RuneCountInString(kinds[i]))
	}
	var b strings.Builder
	for i := range rows {
		r := &rows[i]
		line := fmt.Sprintf("%-*s  %-*s  %s", idW, r.ID, nameW, r.Name, kinds[i]+strings.Repeat(" ", kindW-utf8.RuneCountInString(kinds[i])))
		if r.Frozen {
			line += "  [frozen: " + r.FrozenReason + "]"
		}
		if r.Epic != "" {
			line += "  epic " + r.Epic
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	return b.String()
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
		fmt.Fprintf(&b, "%s  %s  %s · %s\n", p.Plan, p.Name, p.Kind, p.Lifecycle)
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
			v, ok := render.Describe(p.Ref(), g, args[1], set)
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
marked qualified and resolved through .auto/plan/plans/NNN-*/graph.json; edges other plans hold into
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
			v, ok := render.Card(p.Ref(), g, args[1], set)
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
	plans, err := resolveMany(cmd, ws, text, arg)
	if err != nil {
		return nil, nil, err
	}
	var out []loadedPlan
	var errs []graph.ValidationError
	for _, p := range plans {
		g, err := graph.Decode(ws.GraphPath(p))
		if err != nil {
			errs = append(errs, graph.ValidationError{Code: "parse-error", Path: p.Dir + "/" + workspace.GraphFile, Field: "plan", Message: err.Error(), Value: p.Ref()})
			continue
		}
		for _, ve := range graph.Validate(g) {
			ve.Message = "plan " + p.Ref() + ": " + ve.Message
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
	for i := range schema.Registry.Nodes {
		out[i] = schema.Registry.Nodes[i].Name
	}
	return out
}
