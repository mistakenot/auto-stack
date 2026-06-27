package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# auto ui quickstart

Serve the auto-ui local web dashboard from a single Go binary. The dashboard is a no-build
Preact + htm single-page app served either embedded (shipped binary) or live from disk (dev loop).

> **Prerequisite — at least one autowatch backend.** auto-ui is now a proxy: it no longer reads
> the local filesystem for projects or docs. It needs **at least one reachable autowatch backend**
> registered in ` + "`~/.auto/ui/backends.json`" + `. ` + "`auto ui serve`" + ` **fails fast** with a
> remediation hint if no backend is configured. Register one with ` + "`auto ui backends add <uri>`" + `
> before serving (see step 2).

## Core workflow

### 1. Initialize settings

` + "```" + `bash
auto ui init
` + "```" + `

This creates ` + "`~/.auto/ui/settings.json`" + ` (default port 8080).

### 2. Register an autowatch backend

` + "```" + `bash
# Register a backend by its dial URI (tcp:// or unix://). By default the backend
# is dialed once to confirm it is reachable and to learn its authoritative hostId.
auto ui backends add tcp://127.0.0.1:9400 --name local
# unix-socket backend:
auto ui backends add unix:///run/auto/autowatch.sock --name local

# Register without dialing (e.g. the daemon isn't up yet):
auto ui backends add tcp://127.0.0.1:9400 --name local --no-verify

# Inspect / remove registered backends:
auto ui backends list
auto ui backends remove tcp://127.0.0.1:9400
` + "```" + `

Then verify configuration and per-backend health:

` + "```" + `bash
auto ui doctor
` + "```" + `

` + "`auto ui doctor`" + ` now reports a ` + "`backends_config`" + ` check plus one connectivity check
per backend (dial + ` + "`daemon.status`" + ` + ` + "`project.list`" + ` probe). It exits non-zero if any
backend is unreachable, so it doubles as a pre-serve readiness gate.

### 3. Serve the dashboard

` + "```" + `bash
# Serve on the configured port (default 8080)
auto ui serve

# Override the port
auto ui serve --port 9090
` + "```" + `

Then open http://localhost:8080 in a browser. (` + "`serve`" + ` fails fast if no backend is registered.)

### 4. Develop the frontend (live-from-disk)

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
- ` + "`GET /api/debug/recent`" + ` — returns the last N relayed bus events as JSON, but only
  when the server was started with ` + "`AUTO_UI_DEBUG=1`" + `. Without that env var the route returns
  ` + "`404`" + `. Use it to confirm a ` + "`doc.changed`" + ` event reached auto-ui over the relay.

### Triggering edits: trigger via CLI, observe via browser

auto-ui has no local ingest endpoint — it receives events only over the backend relay. Trigger
a ` + "`doc.changed`" + ` from a CLI and **observe** the result in the browser (or via
` + "`/api/debug/recent`" + ` when debug is enabled); you do not trigger from the browser.

` + "```" + `bash
# Drive the hook producer with a canned PostToolUse payload; it posts to autowatch,
# which derives the doc.changed and relays it to every connected auto-ui.
echo '{"hook_event_name":"PostToolUse","tool_name":"Edit","tool_input":{"file_path":"docs/tasks/x.md"}}' | auto hooks fire --agent claude
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

- ` + "`AUTO_UI_PORT`" + ` — port to serve on (honored by ` + "`serve`" + `); lower precedence than an
  explicit ` + "`--port`" + `.
- ` + "`AUTO_PROJECTS_PATH`" + ` — projects registry path (equivalent to ` + "`--projects`" + `).
- ` + "`AUTO_UI_DEBUG=1`" + ` — enable the ` + "`/api/debug/recent`" + ` event buffer.

A typical harness reads ` + "`--ready-file`" + ` to learn the bound port, registers an autowatch
backend, sets ` + "`AUTO_UI_DEBUG=1`" + `, triggers via ` + "`auto hooks fire`" + `, and asserts the
relayed ` + "`doc.changed`" + ` via ` + "`/api/debug/recent`" + `.

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
