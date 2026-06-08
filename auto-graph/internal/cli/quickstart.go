package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# auto graph quickstart

Build and query code context graphs. Supports TypeScript (via ast-grep) and Go (via go/parser) import graphs.

## Before you start

For TypeScript projects, ensure ast-grep is installed:

` + "```" + `bash
npm install -g @ast-grep/cli
auto graph doctor
` + "```" + `

For Go projects, no external dependencies are needed.

## Core workflow

### 1. Generate an import graph (TypeScript)

` + "```" + `bash
# Scan a TypeScript project and output the import graph as JSON (default)
auto graph code graph ./my-ts-project

# Output as Graphviz DOT
auto graph code graph ./my-ts-project --format=dot

# Output as Mermaid
auto graph code graph ./my-ts-project --format=mermaid
` + "```" + `

### 2. Generate an import graph (Go)

` + "```" + `bash
# Scan a Go project (auto-detected from go.mod)
auto graph code graph ./my-go-project

# Explicit language override
auto graph code graph ./my-go-project --lang=go

# Output as DOT
auto graph code graph ./my-go-project --format=dot
` + "```" + `

### 3. Specify language explicitly

` + "```" + `bash
# Auto-detection uses go.mod or tsconfig.json presence; override with --lang
auto graph code graph ./my-project --lang=typescript
auto graph code graph ./my-project --lang=go
` + "```" + `

### 4. Pipe to other tools

` + "```" + `bash
# Render DOT output with Graphviz
auto graph code graph ./my-project --format=dot | dot -Tpng -o graph.png

# Use jq to extract specific nodes
auto graph code graph ./my-project | jq '.nodes[] | select(.path | contains("utils"))'
` + "```" + `

## Output format

JSON output contains a graph with nodes for each file and edges for each import relationship:

` + "```" + `json
{
  "root": "./my-project",
  "nodes": [
    {"id": "src/index.ts", "kind": "file", "path": "src/index.ts", "language": "typescript"},
    {"id": "cmd/main.go", "kind": "file", "path": "cmd/main.go", "language": "go"}
  ],
  "edges": [
    {"source": "src/index.ts", "target": "src/utils.ts", "kind": "import"},
    {"source": "cmd/main.go", "target": "internal/server/server.go", "kind": "import"}
  ]
}
` + "```" + `

### 4. Build a context pack

` + "```" + `bash
# Select relevant files around a seed file, output as markdown (default)
auto graph code context ./my-project --token-limit 12000 --file src/App.tsx

# Multiple seed files, JSON output
auto graph code context ./my-project --token-limit 8000 --file src/App.tsx --file src/hooks/useAuth.ts --format=json
` + "```" + `

Run ` + "`auto graph <command> --help`" + ` for full flag details on any command.
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
