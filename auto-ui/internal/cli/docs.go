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
				"",
				"## HTTP routes",
				"",
				"- `GET /api/doc/raw?project=&path=&worktree=`: serve a verbatim `.html` doc under docs/ as `text/html` (.md and traversal rejected).",
				"- `GET /api/debug/recent`: last N relayed bus events as JSON when `AUTO_UI_DEBUG=1`; otherwise `404`.",
				"",
				"## Triggering edits",
				"",
				"auto-ui receives events only via the backend relay; it has no local ingest endpoint. Trigger an edit by piping a hook payload to `auto hooks fire --agent claude` (which posts to autowatch), e.g. `echo '{\"hook_event_name\":\"PostToolUse\",\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"docs/foo.md\"}}' | auto hooks fire --agent claude`, then observe results in the browser or, with `AUTO_UI_DEBUG=1`, via `/api/debug/recent`.",
				"",
				"## Environment variables",
				"",
				"- `AUTO_UI_PORT`: port to serve on (honored by `serve`); lower precedence than an explicit `--port`.",
				"- `AUTO_PROJECTS_PATH`: projects registry path (equivalent to `--projects`).",
				"- `AUTO_UI_DEBUG`: set to `1` to enable the `/api/debug/recent` event buffer.",
			}, "\n")
			_, err := fmt.Fprintln(cmd.OutOrStdout(), docs)
			return err
		},
	}
}
