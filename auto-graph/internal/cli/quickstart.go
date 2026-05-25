package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# autograph quickstart

Build and query code context graphs. Currently supports TypeScript import graphs via ast-grep.

## Before you start

Ensure ast-grep is installed:

` + "```" + `bash
npm install -g @ast-grep/cli
autograph doctor
` + "```" + `

## Core workflow

### 1. Generate an import graph

` + "```" + `bash
# Scan a TypeScript project and output the import graph as JSON (default)
autograph code graph ./my-project

# Output as Graphviz DOT
autograph code graph ./my-project --format=dot

# Output as Mermaid
autograph code graph ./my-project --format=mermaid
` + "```" + `

### 2. Specify language explicitly

` + "```" + `bash
# Auto-detection uses tsconfig.json presence; override with --lang
autograph code graph ./my-project --lang=typescript
` + "```" + `

### 3. Pipe to other tools

` + "```" + `bash
# Render DOT output with Graphviz
autograph code graph ./my-project --format=dot | dot -Tpng -o graph.png

# Use jq to extract specific nodes
autograph code graph ./my-project | jq '.nodes[] | select(.path | contains("utils"))'
` + "```" + `

## Output format

JSON output contains a graph with nodes for each file and edges for each import relationship:

` + "```" + `json
{
  "root": "./my-project",
  "nodes": [
    {"id": "src/index.ts", "kind": "file", "path": "src/index.ts", "language": "typescript"}
  ],
  "edges": [
    {"source": "src/index.ts", "target": "src/utils.ts", "kind": "import"}
  ]
}
` + "```" + `

Run ` + "`autograph <command> --help`" + ` for full flag details on any command.
`

func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Print an LLM-friendly guide to using autograph",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), quickstartMarkdown)
			return nil
		},
	}
}
