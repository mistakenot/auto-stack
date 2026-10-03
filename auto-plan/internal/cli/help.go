package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/spf13/cobra"
)

// quickstartText is the LLM-friendly happy path: list → init → new → add →
// link → lint → show → brief, then renumber after a merge. The IDs in it are examples; every command prints the
// real ones.
const quickstartText = "# auto plan quickstart\n\n" +
	"`auto plan` keeps each plan as one deterministic `docs/plans/NNN-name/graph.json`: typed nodes\n" +
	"(goal, ac, decision, stage, file, …) joined by typed edges (proves, constrains, covers, …),\n" +
	"validated on every write. Never edit graph.json by hand; every change below is a CLI call.\n" +
	"Output is JSON on stdout; add `--text` for humans. Errors go to stderr with a `hint`.\n\n" +
	"## 0. What plans exist?\n\n" +
	"```bash\n" +
	"auto plan list --text                     # one line per plan: ID, name, kind · lifecycle, frozen?, epic\n" +
	"```\n\n" +
	"## 1. Start a plan\n\n" +
	"```bash\n" +
	"auto plan init                            # creates docs/plans/ + AGENTS.md, CLAUDE.md → AGENTS.md (idempotent)\n" +
	"auto plan new demo --kind task            # → {\"id\":\"001-k7q2\",\"number\":\"001\",\"name\":\"demo\",…}\n" +
	"```\n\n" +
	"Every plan has a plan ID, `NNN-xxxx`: its folder number and 4 random characters. `<plan>` in\n" +
	"every later command is `001`, the ID `001-k7q2`, the folder `001-demo`, or a path.\n\n" +
	"## 2. Add nodes (IDs are generated: read `id` from each result)\n\n" +
	"```bash\n" +
	"auto plan add 001 goal --title \"Agents get one stage's context in one call\"      # → g-k7q2\n" +
	"auto plan add 001 ac --proves g-k7q2 --title \"brief is self-contained\" \\\n" +
	"  --gwt \"Given a stage, when brief runs, then nothing else is needed\" \\\n" +
	"  --verify-cmd \"go test ./e2e -run Brief\"                                        # → ac-3fxm\n" +
	"auto plan add 001 alternative --title \"one big Markdown file\" --why \"drifts\"      # → a-9t2w\n" +
	"auto plan add 001 decision --title \"graph.json is the source\" --chosen \"typed JSON\" \\\n" +
	"  --why \"diffable and validated\" --by agent --rejects a-9t2w                        # → d-4hn8\n" +
	"auto plan add 001 file --path cmd/brief.go --change add --why \"the brief command\"   # → f-2m6c\n" +
	"auto plan add 001 stage --title \"Brief command\" --steps \"write it\" \\\n" +
	"  --commit \"feat: brief\" --touches f-2m6c --covers ac-3fxm                          # → s-8p1d\n" +
	"```\n\n" +
	"Flags come from the type registry: `auto plan add 001 <type> --help` lists a type's fields\n" +
	"and the edges it can start. Long text takes `@file` (or `@-` for stdin).\n\n" +
	"## 3. Link, change, reorder\n\n" +
	"```bash\n" +
	"auto plan link 001 d-4hn8 constrains g-k7q2    # endpoint types are checked; → the edge's ID e-xxxx\n" +
	"auto plan unlink 001 e-7k2q                    # by edge ID (or: unlink 001 d-4hn8 constrains g-k7q2)\n" +
	"auto plan update 001 g-k7q2 --description \"…\"  # or: update 001 plan --lifecycle solution\n" +
	"auto plan move 001 g-k7q2 --before <goal-id>   # reading order; IDs never change\n" +
	"auto plan retire 001 a-9t2w                    # kept in place, edges kept\n" +
	"```\n\n" +
	"## 4. Lint\n\n" +
	"```bash\n" +
	"auto plan lint 001       # or: auto plan lint all — exit 1 on any error, warnings don't fail\n" +
	"```\n\n" +
	"Rules switch on with the plan's lifecycle (requirements → solution → plan → executing → done):\n" +
	"at `solution` every goal needs an AC and every AC a verify command; at `plan` every file must\n" +
	"be touched by a stage. Each issue carries a one-command `hint`. Setting `--lifecycle done` is a\n" +
	"plan's last write: a done plan is frozen.\n\n" +
	"## 5. Read it back\n\n" +
	"```bash\n" +
	"auto plan show 001 --text             # the goal ladder\n" +
	"auto plan trace 001 ac-3fxm --text    # lineage up (goal, decisions, rails) and down (stages, files)\n" +
	"auto plan tree 001 --files --text     # the planned file tree\n" +
	"auto plan brief 001 s-8p1d --text     # a Stage Brief: everything one stage needs, nothing else\n" +
	"auto plan list 001 --type ac          # IDs + titles; describe/get/search for more\n" +
	"```\n\n" +
	"## Epics\n\n" +
	"```bash\n" +
	"auto plan new mail-mvp --kind epic                     # 001: goals, rails, journeys, legs\n" +
	"auto plan new walking-skeleton --kind task --epic 001  # 002\n" +
	"auto plan add 001 child --plan 002 --title \"…\"\n" +
	"auto plan link 002 plan honors 001:r-8hw3              # NNN:ID is stored as 001-k7q2:r-8hw3\n" +
	"auto plan add 002 ac … --discharges 001:r-8hw3\n" +
	"```\n\n" +
	"## After a merge\n\n" +
	"Two branches that each created plan 004 merge cleanly; `auto plan lint all` then reports\n" +
	"`duplicate-plan-number`. Move one of them:\n\n" +
	"```bash\n" +
	"auto plan renumber 004-m3x9                 # → next free number; rewrites every reference to it\n" +
	"```\n\n" +
	"Run `auto plan docs` for every command, node type, field and edge type.\n"

func newQuickstartCmd(_ *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Print the happy path as Markdown: list → init → new → add → link → lint → show → brief",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := io.WriteString(cmd.OutOrStdout(), quickstartText)
			return err
		},
	}
}

func newDocsCmd(_ *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "docs",
		Short: "Print the full reference as Markdown: every command, node type, field and edge type",
		Long: `Print the full auto plan reference as Markdown. The node and edge sections are generated
from the type registry (internal/schema/registry.go), so they always match what add, link,
update and lint accept.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := io.WriteString(cmd.OutOrStdout(), docsText(cmd.Root()))
			return err
		},
	}
}

// docsText renders the reference: commands from the cobra tree, node and edge
// types from the registry.
func docsText(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString("# auto plan reference\n\n")
	b.WriteString("A Plan is one deterministic `docs/plans/NNN-name/graph.json`: typed nodes and typed edges,\n")
	b.WriteString("validated on every write. Every command reads `auto plan <verb> <plan> [args…]`, where\n")
	b.WriteString("`<plan>` is `NNN`, the plan ID `NNN-xxxx`, the folder `NNN-name`, a path, or `all` where it\n")
	b.WriteString("makes sense (a bare `NNN` that two folders share fails with `ambiguous-plan`). Output is JSON on\n")
	b.WriteString("stdout by default and text with `--text`; failures print\n")
	b.WriteString("`{\"errors\":[{code,path,field,message,value}],\"hint\":…}` on stderr and exit 1.\n\n")

	b.WriteString("## Commands\n\n")
	cmds := slices.Clone(root.Commands())
	slices.SortFunc(cmds, func(a, c *cobra.Command) int { return strings.Compare(a.Name(), c.Name()) })
	for _, c := range cmds {
		if !c.IsAvailableCommand() {
			continue
		}
		fmt.Fprintf(&b, "- `auto plan %s` — %s\n", c.Use, c.Short)
	}

	b.WriteString("\n## The plans folder\n\n")
	b.WriteString("Plans live in `docs/plans/NNN-name/graph.json`. `init` and `new` also create, when missing,\n")
	b.WriteString("`docs/plans/AGENTS.md` (the folders are plans managed by auto plan: change them with the CLI)\n")
	b.WriteString("and `docs/plans/CLAUDE.md`, a relative symlink to it (a copy where symlinks fail). Existing files\n")
	b.WriteString("are never overwritten; a CLAUDE.md that is not that symlink gets a note on stderr. The result's\n")
	b.WriteString("`scaffolded` lists what a run created. A repo that runs `auto doc` should ignore `docs/plans/*`\n")
	b.WriteString("in `.auto/doc/settings.json`, or auto doc treats the AGENTS.md as a doc and indexes into it.\n")
	b.WriteString("\n## IDs and references\n\n")
	b.WriteString("- A plan ID is the folder number + 4 Crockford base32 characters (`004-k7q2`), generated by `new`\n")
	b.WriteString("  and stored as the top-level `id` of graph.json. Qualified references name it, so two plans that\n")
	b.WriteString("  collide on a number (two branches each created 004) stay distinct; `auto plan renumber <plan>`\n")
	b.WriteString("  moves one to a free number and rewrites every reference to it in every writable plan.\n")
	b.WriteString("- A node ID is a type prefix + 4 Crockford base32 characters (`ac-3fxm`); the plan node is `plan`.\n")
	b.WriteString("- An edge ID is `e-` + 4 characters (`e-7k2q`), unique with the node IDs of its plan. Edges are\n")
	b.WriteString("  `{id, from, type, to}`; a (from, type, to) triple still appears once. `unlink` takes either.\n")
	b.WriteString("- A qualified reference `NNN-xxxx:ID` names a node in another plan (`001-k7q2:r-8hw3`); only edge\n")
	b.WriteString("  types marked cross-plan below accept one. On input the CLI also takes the shorthand `NNN:ID`\n")
	b.WriteString("  (and `--epic NNN`, `--plan NNN`, `--deferred NNN`) and stores the plan ID when exactly one plan\n")
	b.WriteString("  has that number.\n")
	b.WriteString("- Prose fields may cite nodes as `[[ID]]`, `[[NNN-xxxx:ID]]` or `[[NNN:ID]]`; lint checks they\n")
	b.WriteString("  resolve (`ambiguous-ref` when the number names two plans).\n")

	b.WriteString("\n## Versions and freezing\n\n")
	fmt.Fprintf(&b, "- graph.json records the semver format `version` it was written with; this tool writes `%s`.\n", schema.Version)
	b.WriteString("  Plans are never migrated: a new version changes only plans created after it.\n")
	b.WriteString("- Reads (lint, list, show, get, …) accept any `1.x.y`. A plan of another major is read best effort\n")
	b.WriteString("  and not checked against this registry; lint warns `other-version` for it and for a newer 1.x.\n")
	b.WriteString("- Writes (add, update, link, unlink, retire, move, renumber, fmt without --check) are refused with\n")
	b.WriteString("  `frozen` when the plan is of another major or newer than this tool, or its lifecycle is `done`.\n")
	b.WriteString("  Setting `--lifecycle done` is allowed: it is the last write. `renumber` refuses with `frozen-ref`\n")
	b.WriteString("  when a frozen plan references the plan it would move.\n")

	b.WriteString("\n## Lifecycle\n\n")
	fmt.Fprintf(&b, "Steps, in order: %s.\n\n", strings.Join(schema.LifecycleNames(), " → "))
	b.WriteString("The sequence is owned by the registry (`schema.Registry.Lifecycle`) and so by the format version.\n")
	b.WriteString("Node types and lint rules switch on at a step; `auto plan update <plan> plan --lifecycle <step>`\n")
	b.WriteString("takes exactly these.\n")

	b.WriteString("\n## Node types\n\n")
	b.WriteString("`auto plan add <plan> <type>` takes one flag per field, plus one repeatable flag per edge the\n")
	b.WriteString("type can start. `auto plan update <plan> <id>` takes the same field flags.\n")
	for _, n := range schema.Registry.Nodes {
		id := "`" + n.Prefix + "-xxxx`"
		if n.Singleton() {
			id = "`" + n.Name + "`"
		}
		fmt.Fprintf(&b, "\n### %s — %s\n\n%s ID %s; expected from lifecycle `%s`.\n\n",
			n.Name, n.Term, n.Help, id, n.MinLifecycle)
		b.WriteString("| field | kind | required | values | help |\n|---|---|---|---|---|\n")
		for i := range n.Fields {
			writeFieldRow(&b, "", &n.Fields[i])
		}
		if edges := schema.Registry.EdgesFrom(n.Name); len(edges) > 0 {
			names := make([]string, len(edges))
			for i, e := range edges {
				names[i] = "`" + e.Name + "`"
			}
			fmt.Fprintf(&b, "\nStarts edges: %s.\n", strings.Join(names, ", "))
		}
	}

	b.WriteString("\n## Edge types\n\n")
	b.WriteString("`auto plan link <plan> <from> <edge> <to>`; `unlink` with the same arguments or `<plan> <edge-id>`.\n\n")
	b.WriteString("| edge | from | to | cross-plan | help |\n|---|---|---|---|---|\n")
	for _, e := range schema.Registry.Edges {
		cross := "no"
		if e.CrossPlan {
			cross = "yes"
		}
		from := strings.Join(e.From, "|")
		if e.SameType {
			from += " (same type both ends)"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", e.Name, cellEscape(from), cellEscape(e.ToLabel()), cross, e.Help)
	}

	b.WriteString("\n## Environment\n\n")
	b.WriteString("- `" + EnvSeed + "` (uint64) seeds ID generation; `" + EnvDate + "` (YYYY-MM-DD) pins the date `new`\n")
	b.WriteString("  records. Both are for reproducible tests; leave them unset in normal use.\n")
	return b.String()
}

// writeFieldRow writes one field (and, for an object, its members as
// `field.member` rows) of a node-type table.
func writeFieldRow(b *strings.Builder, parent string, f *schema.FieldSpec) {
	name := f.Name
	if parent != "" {
		name = parent + "." + f.Name
	}
	req := "no"
	if f.Required {
		req = "yes"
	}
	var values []string
	if len(f.Enum) > 0 {
		values = append(values, strings.Join(f.Enum, "\\|"))
	}
	if f.Pattern != "" {
		values = append(values, "`"+strings.ReplaceAll(f.Pattern, "|", "\\|")+"`")
	}
	help := f.Help
	if f.Fixed {
		help += " (fixed at creation)"
	}
	fmt.Fprintf(b, "| `%s` | %s | %s | %s | %s |\n", name, f.Kind, req, strings.Join(values, " "), help)
	for i := range f.Fields {
		writeFieldRow(b, name, &f.Fields[i])
	}
}

// cellEscape escapes the pipes of an `a|b` type list inside a Markdown table cell.
func cellEscape(s string) string { return strings.ReplaceAll(s, "|", "\\|") }
