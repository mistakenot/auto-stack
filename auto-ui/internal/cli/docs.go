package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs",
		Short: "Show command reference",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			docs := strings.Join([]string{
				"# auto ui commands",
				"",
				"- `init`: initialize shared and auto ui settings (~/.auto/ui/settings.json).",
				"- `doctor`: check settings validity and configured port, report as JSON.",
				"- `quickstart`: show a minimal happy-path workflow.",
				"- `docs`: show this command reference.",
				"- `update`: check for and install the latest auto-stack release.",
				"- `serve`: serve the auto-ui dashboard locally.",
				"  - `--port`: port to serve on (overrides settings.json; default 8080; 0 = OS-assigned).",
				"  - `--ready-file`: after binding, write `{\"addr\":\"127.0.0.1:NNNN\"}` JSON to this path (harness).",
				"  - `--projects`: load this projects.json registry instead of ~/.auto/projects.json (harness).",
				"- `emit`: emit a synthetic agent.tool.post event to a running server's /api/rpc (trigger a doc.changed without editing a file).",
				"  - `--project`: project id stamped on the event envelope (required).",
				"  - `--path`: repo-relative doc path under docs/, e.g. docs/tasks/x.md (required).",
				"  - `--worktree`: worktree root; when set, abs path = worktree/path.",
				"  - `--port`: server port (overrides AUTO_UI_PORT; default 8080).",
				"",
				"## HTTP routes",
				"",
				"- `GET /api/doc/raw?project=&path=&worktree=`: serve a verbatim `.html` doc under docs/ as `text/html` (.md and traversal rejected).",
				"- `GET /api/debug/recent`: last N raw + derived bus events as JSON when `AUTO_UI_DEBUG=1`; otherwise `404`.",
				"",
				"## Triggering edits",
				"",
				"The `/api/rpc` ingest rejects any request carrying an `Origin` header (403): trigger via a CLI (no Origin), observe via the browser or `/api/debug/recent`. Use `auto ui emit` for a synthetic event, or `auto hooks fire PostToolUse` with a canned payload as the real-edit alternative.",
				"",
				"## Environment variables",
				"",
				"- `AUTO_UI_PORT`: port to serve on (honored by `serve`, `emit`, and the `auto hooks fire` producer); lower precedence than an explicit `--port`.",
				"- `AUTO_PROJECTS_PATH`: projects registry path (equivalent to `--projects`).",
				"- `AUTO_UI_DEBUG`: set to `1` to enable the `/api/debug/recent` event buffer.",
			}, "\n")
			_, err := fmt.Fprintln(cmd.OutOrStdout(), docs)
			return err
		},
	}
}
