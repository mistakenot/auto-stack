package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# auto ui quickstart

Serve the auto-ui local web dashboard from a single Go binary. The dashboard is a no-build
Preact + htm single-page app served either embedded (shipped binary) or live from disk (dev loop).

## Core workflow

### 1. Initialize settings

` + "```" + `bash
auto ui init
auto ui doctor
` + "```" + `

This creates ` + "`~/.auto/ui/settings.json`" + ` (default port 8080) and verifies the configuration.

### 2. Serve the dashboard

` + "```" + `bash
# Serve on the configured port (default 8080)
auto ui serve

# Override the port
auto ui serve --port 9090
` + "```" + `

Then open http://localhost:8080 in a browser.

### 3. Develop the frontend (live-from-disk)

` + "```" + `bash
# Build the auto binary with the dev tag (from repo root), then run it from
# the auto-ui/ module root so assets resolve live from web/static:
go build -tags dev -o bin/auto ./auto-cli/cmd/auto
cd auto-ui && ../bin/auto ui serve
` + "```" + `

Edit files under ` + "`web/static/`" + ` and refresh the browser — no Go rebuild required.

## Planning docs dashboard backend

The server browses each registered project's whole ` + "`docs/`" + ` tree (` + "`.md`" + ` and ` + "`.html`" + `),
renders markdown inline, serves self-contained HTML verbatim, and live-refreshes when an agent
edits a doc. The following routes, flags, and helpers support that workflow.

### HTTP routes

- ` + "`GET /api/doc/raw?project=<id>&path=docs/...&worktree=<root>`" + ` — serves a verbatim
  ` + "`.html`" + ` doc as ` + "`text/html`" + ` (for inline iframe rendering). Only ` + "`.html`" + ` paths under
  ` + "`docs/`" + ` are allowed; ` + "`.md`" + ` and path traversal are rejected. (Markdown is served via the
  ` + "`doc.get`" + ` JSON-RPC method, which still accepts only ` + "`.md`" + `.)
- ` + "`GET /api/debug/recent`" + ` — returns the last N raw + derived bus events as JSON, but only
  when the server was started with ` + "`AUTO_UI_DEBUG=1`" + `. Without that env var the route returns
  ` + "`404`" + `. Use it to confirm an edit derived a ` + "`doc.changed`" + ` event.

### Triggering edits: trigger via CLI, observe via browser

The ingest endpoint ` + "`POST /api/rpc`" + ` **rejects any request carrying an ` + "`Origin`" + ` header**
(returns ` + "`403`" + `). Triggers therefore come from a CLI (which sends no ` + "`Origin`" + `), never from a
browser ` + "`fetch`" + `. You **observe** results in the browser (or via ` + "`/api/debug/recent`" + ` when
debug is enabled) — you do not trigger from it.

Two ways to trigger a ` + "`doc.changed`" + ` derivation:

` + "```" + `bash
# 1. Synthetic event — no file edit required:
auto ui emit --project my-proj --path docs/tasks/x.md --worktree /path/to/worktree
#   --project    project id stamped on the event envelope (required)
#   --path       repo-relative doc path under docs/ (required)
#   --worktree   worktree root; when set, abs path = worktree/path
#   --port       server port (overrides AUTO_UI_PORT; default 8080)
# Sends an agent.tool.post envelope to /api/rpc with NO Origin header. JSON result
# goes to stdout, diagnostics to stderr; non-zero exit on POST failure.

# 2. Real-edit recipe — drive the actual hook producer with a canned PostToolUse payload:
echo '{"tool":"Edit","path":"docs/tasks/x.md"}' | auto hooks fire PostToolUse
# This is the real-edit alternative to emit; it exercises the same ingest path a
# live agent edit would.
` + "```" + `

### Agent harness flags + env vars

For driving auto-ui from an automated test harness:

` + "```" + `bash
auto ui serve --port 0 --ready-file /tmp/ready.json --projects /tmp/fixture-projects.json
#   --port 0        bind an OS-assigned ephemeral port
#   --ready-file    after binding, write {"addr":"127.0.0.1:NNNN"} JSON to this path
#   --projects      load this projects.json registry instead of ~/.auto/projects.json
` + "```" + `

Env vars:

- ` + "`AUTO_UI_PORT`" + ` — port to serve on (honored by ` + "`serve`" + `, ` + "`emit`" + `, and the
  ` + "`auto hooks fire`" + ` producer); lower precedence than an explicit ` + "`--port`" + `.
- ` + "`AUTO_PROJECTS_PATH`" + ` — projects registry path (equivalent to ` + "`--projects`" + `).
- ` + "`AUTO_UI_DEBUG=1`" + ` — enable the ` + "`/api/debug/recent`" + ` event buffer.

A typical harness reads ` + "`--ready-file`" + ` to learn the bound port, pins ` + "`AUTO_UI_PORT`" + ` so the
producer hits the same server, sets ` + "`AUTO_UI_DEBUG=1`" + `, triggers via ` + "`emit`" + ` (or ` + "`hooks fire`" + `),
and asserts the derived ` + "`doc.changed`" + ` via ` + "`/api/debug/recent`" + `.

Run ` + "`auto ui <command> --help`" + ` for full flag details on any command.
`

func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Print an LLM-friendly guide to using autoui",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), quickstartMarkdown)
			return nil
		},
	}
}
