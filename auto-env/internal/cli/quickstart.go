package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# auto env quickstart

Template-based config file generation with deterministic per-worktree port allocation.
Manages isolated development environments so multiple git worktrees (e.g. concurrent coding agents)
can run the same project on different ports without conflicts.

## Setup

### 1. Initialize the project

Run this from any git repository:

` + "```" + `bash
auto env init
` + "```" + `

This creates the ` + "`" + `.auto/env/` + "`" + ` directory with:
- ` + "`" + `config.json` + "`" + ` — project configuration (checked into git)
- ` + "`" + `files/` + "`" + ` — template file tree (checked into git)

### 2. Configure commands

Edit ` + "`" + `.auto/env/config.json` + "`" + `:

` + "```" + `json
{
  "up_command": "pm2 start ecosystem.config.js",
  "down_command": "pm2 delete all"
}
` + "```" + `

Both fields are required. Commands run via ` + "`" + `sh -c` + "`" + ` from the repo root.

Optional overrides:

` + "```" + `json
{
  "up_command": "pm2 start ecosystem.config.js",
  "down_command": "pm2 delete all",
  "port_base": 4000,
  "port_stride": 50,
  "delimiters": ["[[", "]]"]
}
` + "```" + `

| Field | Default | Description |
|---|---|---|
| ` + "`" + `port_base` + "`" + ` | 3000 | Starting port for slot 0 |
| ` + "`" + `port_stride` + "`" + ` | 100 | Port gap between worktree slots |
| ` + "`" + `delimiters` + "`" + ` | ` + "`" + `["{{", "}}"]` + "`" + ` | Template delimiters (use ` + "`" + `["[[", "]]"]` + "`" + ` for JS/Vue/Handlebars projects) |

### 3. Add template files

Place template files in ` + "`" + `.auto/env/files/` + "`" + `. The directory structure mirrors the repo root:

` + "```" + `
.auto/env/files/ecosystem.config.js   →  ./ecosystem.config.js
.auto/env/files/Caddyfile              →  ./Caddyfile
.auto/env/files/web/vite.config.ts     →  ./web/vite.config.ts
` + "```" + `

Templates use Go template syntax with the configured delimiters:

` + "```" + `js
// .auto/env/files/ecosystem.config.js (using default {{ }} delimiters)
module.exports = {
  apps: [
    {
      name: "web-{{.BranchSlug}}",
      script: "npm",
      args: "run dev -- --port {{.Port.web}}",
    },
    {
      name: "api-{{.BranchSlug}}",
      script: "npm",
      args: "run api -- --port {{.Port.api}}",
    },
  ],
};
` + "```" + `

` + "```" + `js
// Same template using [[ ]] delimiters (for projects with JS template literals)
module.exports = {
  apps: [
    {
      name: "web-[[.BranchSlug]]",
      script: "npm",
      args: ` + "`" + `run dev -- --port [[.Port.web]]` + "`" + `,
    },
  ],
};
` + "```" + `

## Template variables

| Variable | Type | Description |
|---|---|---|
| ` + "`" + `.Port.xxx` + "`" + ` | int | Auto-allocated port for the named service |
| ` + "`" + `.Name` + "`" + ` | string | Worktree directory name |
| ` + "`" + `.Branch` + "`" + ` | string | Current git branch |
| ` + "`" + `.BranchSlug` + "`" + ` | string | Branch name sanitized for identifiers (lowercase, hyphens only, max 63 chars) |
| ` + "`" + `.Slot` + "`" + ` | int | Worktree slot number (0 = main) |
| ` + "`" + `.RepoRoot` + "`" + ` | string | Absolute path to repo root |
| ` + "`" + `.WorktreePath` + "`" + ` | string | Absolute path to current worktree |

## Port allocation

Ports are deterministic — no state file needed.

- Main worktree gets **slot 0**, linked worktrees get a slot from a hash of their name (1–99)
- Port names are discovered from templates and sorted alphabetically
- Each name gets: ` + "`" + `base + (slot × stride) + index` + "`" + `

**Example** (slot 0, base 3000, stride 100):

| Port Name | Index | Port |
|---|---|---|
| ` + "`" + `api` + "`" + ` | 0 | 3000 |
| ` + "`" + `caddy` + "`" + ` | 1 | 3001 |
| ` + "`" + `db` + "`" + ` | 2 | 3002 |
| ` + "`" + `web` + "`" + ` | 3 | 3003 |

A linked worktree at slot 5 would get 3500, 3501, 3502, 3503.

## Core workflow

### Start the environment

` + "```" + `bash
auto env up
` + "```" + `

This renders templates → writes files → runs ` + "`" + `up_command` + "`" + `. Outputs JSON with assigned ports:

` + "```" + `json
{"name": "my-project", "slot": 0, "ports": {"api": 3000, "web": 3003}}
` + "```" + `

Re-running ` + "`" + `up` + "`" + ` automatically stops and cleans up the previous environment first.

### Preview without side effects

` + "```" + `bash
auto env up --dry-run
` + "```" + `

Renders templates and prints the output without writing files or running commands.
Useful for verifying port assignments and template output before committing.

### Overwrite existing files

` + "```" + `bash
auto env up --force
` + "```" + `

By default, ` + "`" + `up` + "`" + ` errors if a destination file already exists (and wasn't placed by a previous ` + "`" + `up` + "`" + `).
Use ` + "`" + `--force` + "`" + ` to overwrite.

### Check status

` + "```" + `bash
auto env status
` + "```" + `

Returns JSON showing whether the environment is provisioned, port assignments, and generated files:

` + "```" + `json
{"provisioned": true, "name": "my-project", "slot": 0, "ports": {"web": 3003}, "files": ["ecosystem.config.js"]}
` + "```" + `

When not provisioned: ` + "`" + `{"provisioned": false}` + "`" + `

### Stop the environment

` + "```" + `bash
auto env down
` + "```" + `

Runs ` + "`" + `down_command` + "`" + `, then deletes all generated files and the manifest.
If ` + "`" + `down_command` + "`" + ` fails, generated files are preserved so you can fix the issue and retry.

## Example: full setup for a Node.js project

` + "```" + `bash
# 1. Initialize
auto env init

# 2. Configure
cat > .auto/env/config.json << 'EOF'
{
  "up_command": "pm2 start ecosystem.config.js",
  "down_command": "pm2 delete all",
  "delimiters": ["[[", "]]"]
}
EOF

# 3. Create a template
mkdir -p .auto/env/files
cat > .auto/env/files/ecosystem.config.js << 'EOF'
module.exports = {
  apps: [
    {
      name: ` + "`" + `web-[[.BranchSlug]]` + "`" + `,
      script: "npm",
      args: "run dev -- --port [[.Port.web]]",
      cwd: "[[.WorktreePath]]/web",
    },
    {
      name: ` + "`" + `api-[[.BranchSlug]]` + "`" + `,
      script: "npm",
      args: "run api -- --port [[.Port.api]]",
      cwd: "[[.WorktreePath]]/api",
    },
  ],
};
EOF

# 4. Add generated files to .gitignore
echo "ecosystem.config.js" >> .gitignore

# 5. Start
auto env up

# 6. Check
auto env status

# 7. Stop when done
auto env down
` + "```" + `

## Multi-worktree workflow

The main use case — multiple agents working on the same project simultaneously:

` + "```" + `bash
# Main worktree (slot 0, ports 3000+)
cd /home/user/my-project
auto env up

# Create a linked worktree for a feature branch
git worktree add ../my-project-feature feature-branch
cd ../my-project-feature
auto env up  # slot N (hash-based), ports offset by N × stride
` + "```" + `

Each worktree gets its own port range, so services don't collide.

## File layout

` + "```" + `
project/
  .auto/env/
    config.json              # checked into git
    files/                   # template tree, checked into git
      ecosystem.config.js
      Caddyfile
    .generated               # manifest, gitignored
    .gitignore               # ignores .generated

  ecosystem.config.js        # generated by auto env up, gitignored
  Caddyfile                  # generated by auto env up, gitignored
` + "```" + `

## Error handling

| Condition | Behavior |
|---|---|
| No ` + "`" + `config.json` + "`" + ` | Error: run ` + "`" + `auto env init` + "`" + ` |
| Missing ` + "`" + `up_command` + "`" + `/` + "`" + `down_command` + "`" + ` | Error listing missing fields |
| No template files | Error: no template files found |
| Destination file exists | Error listing conflicts (use ` + "`" + `--force` + "`" + `) |
| Template syntax error | Error with file path and details |
| ` + "`" + `up_command` + "`" + ` fails | Error; generated files left for debugging |
| ` + "`" + `down_command` + "`" + ` fails | Error; files preserved, fix and retry |
| Not a git repository | Error: auto env requires a git repository |

## Related tools

- ` + "`" + `auto watch` + "`" + ` — can trigger ` + "`" + `auto env up` + "`" + ` automatically when templates change.

Run ` + "`auto env <command> --help`" + ` for full flag details on any command.
`

func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Print an LLM-friendly guide to using autoenv",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), quickstartMarkdown)
			return nil
		},
	}
}
