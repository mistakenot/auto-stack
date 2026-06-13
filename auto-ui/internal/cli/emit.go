package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-ui/internal/app"
	"github.com/spf13/cobra"
)

// emitPostTimeout bounds the synthetic POST so the helper never hangs against an
// unresponsive server.
const emitPostTimeout = 5 * time.Second

// newEmitCmd builds the `auto ui emit` command: a synthetic event helper that
// constructs a valid agent.tool.post envelope and POSTs it to a running auto-ui
// server's /api/rpc ingest endpoint, exercising the same path a real hook
// producer takes. It deliberately sets NO Origin header so the ingest accepts it
// (the server rejects cross-origin requests). It is a test trigger, not a new
// producer; observe the resulting doc.changed in the browser (or, with
// AUTO_UI_DEBUG=1, via GET /api/debug/recent).
//
// emit does not load the project registry: when --worktree is given the absolute
// path is filepath.Join(worktree, path), otherwise it is left empty. Derivation
// keys off the repo-relative path, so exactly one doc.changed derives regardless.
func newEmitCmd(application *app.App) *cobra.Command {
	var (
		project  string
		path     string
		worktree string
		port     int
	)
	cmd := &cobra.Command{
		Use:   "emit",
		Short: "Emit a synthetic agent.tool.post event to a running auto-ui server",
		Long: "Build a valid agent.tool.post envelope for a docs/ path and POST it as a " +
			"JSON-RPC notification to a running auto-ui server's /api/rpc endpoint (no Origin " +
			"header, so the ingest accepts it). Useful for triggering a doc.changed derivation " +
			"without editing a file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				return &ExitError{Code: 2, Err: errors.New("--project is required")}
			}
			if path == "" {
				return &ExitError{Code: 2, Err: errors.New("--path is required")}
			}

			// Resolve the target port: explicit --port flag > AUTO_UI_PORT env >
			// built-in default. --port has a default of 8080, so we key off whether
			// the flag was changed rather than a zero check.
			if !cmd.Flags().Changed("port") {
				if v := os.Getenv("AUTO_UI_PORT"); v != "" {
					if n, err := strconv.Atoi(v); err == nil && n > 0 {
						port = n
					}
				}
			}

			// abs resolution without a registry load: join the worktree root when
			// given, else leave empty. DeriveDocChanged carries abs into AbsPath
			// without validating it, so derivation is unaffected by an empty abs.
			absPath := ""
			if worktree != "" {
				absPath = filepath.Join(worktree, path)
			}

			tp := bus.ToolPost{
				Tool:  "Edit",
				Event: "PostToolUse",
				Paths: []bus.PathRef{{Rel: path, Abs: absPath}},
			}
			ev, err := bus.NewEvent("agent.tool.post", "auto/ui/emit", tp)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("build event: %w", err)}
			}
			ev.Project = project
			ev.Worktree = worktree

			body, err := json.Marshal(ev.AsNotification())
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("marshal notification: %w", err)}
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), emitPostTimeout)
			defer cancel()
			url := fmt.Sprintf("http://127.0.0.1:%d/api/rpc", port)
			// No Origin header: the ingest rejects cross-origin requests (403), so
			// this server-trusted CLI trigger must omit it.
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("build request: %w", err)}
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("POST %s: %w", url, err)}
			}
			defer resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return &ExitError{Code: 1, Err: fmt.Errorf("POST %s returned HTTP %d", url, resp.StatusCode)}
			}

			out := map[string]any{
				"id":       ev.ID,
				"type":     ev.Type,
				"project":  ev.Project,
				"path":     path,
				"port":     port,
				"status":   resp.StatusCode,
				"emitted":  true,
				"worktree": worktree,
			}
			data, err := json.Marshal(out)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("marshal result: %w", err)}
			}
			fmt.Fprintln(application.Stdout, string(data))
			fmt.Fprintf(application.Stderr, "emitted agent.tool.post for %s (project=%s) to %s -> HTTP %d\n", path, project, url, resp.StatusCode)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project id stamped on the event envelope (required)")
	cmd.Flags().StringVar(&path, "path", "", "repo-relative doc path, e.g. docs/tasks/x.md (required)")
	cmd.Flags().StringVar(&worktree, "worktree", "", "worktree root; when set, abs path = worktree/path")
	cmd.Flags().IntVar(&port, "port", 8080, "auto-ui server port (overrides AUTO_UI_PORT)")
	return cmd
}
