package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-artifact/internal/app"
	"github.com/spf13/cobra"
)

// agentsLine is the single managed line dropped into agent instruction files so
// coding agents discover the tool. Kept to one line on purpose.
const agentsLine = "Use `auto artifact quickstart` to upload evidence files (screenshots, videos, logs) to S3 and get a permanent public URL you can embed in a PR comment."

const (
	agentsBeginMarker = "<!-- BEGIN auto-artifact (managed) -->"
	agentsEndMarker   = "<!-- END auto-artifact (managed) -->"
)

// agentsTargets are the root-level agent instruction files we manage.
var agentsTargets = []string{"CLAUDE.md", "AGENTS.md"}

func newAgentsCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "agents",
		Short: "Insert a managed one-line pointer to `auto artifact quickstart` into root CLAUDE.md / AGENTS.md",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgents(cmd, application)
		},
	}
}

func runAgents(cmd *cobra.Command, application *app.App) error {
	block := agentsBeginMarker + "\n" + agentsLine + "\n" + agentsEndMarker

	results := make([]map[string]any, 0, len(agentsTargets))
	touched := 0
	for _, name := range agentsTargets {
		path := filepath.Join(application.CWD, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				results = append(results, map[string]any{"file": name, "action": "skipped", "reason": "not found"})
				continue
			}
			return &ExitError{Code: 1, Err: err}
		}

		action, updated := upsertAgentsBlock(string(data), block)
		if action != "unchanged" {
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
		}
		results = append(results, map[string]any{"file": name, "action": action})
		touched++
	}

	if touched == 0 {
		return &ExitError{Code: 1, Err: fmt.Errorf("no CLAUDE.md or AGENTS.md found in %s — create one, then re-run `auto artifact agents`", application.CWD)}
	}

	return writeJSON(cmd.OutOrStdout(), map[string]any{"results": results})
}

// upsertAgentsBlock returns the action taken and the new file content. If the
// managed markers are present it replaces the block in place (idempotent);
// otherwise it appends the block at the end of the file.
func upsertAgentsBlock(content, block string) (action, updated string) {
	begin := strings.Index(content, agentsBeginMarker)
	end := strings.Index(content, agentsEndMarker)
	if begin >= 0 && end > begin {
		end += len(agentsEndMarker)
		if content[begin:end] == block {
			return "unchanged", content
		}
		return "updated", content[:begin] + block + content[end:]
	}

	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return "inserted", block + "\n"
	}
	return "inserted", trimmed + "\n\n" + block + "\n"
}
