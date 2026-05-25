package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/mistakenot/auto-graph/internal/config"
	"github.com/spf13/cobra"
)

type doctorCheck struct {
	Check   string `json:"check"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check autograph configuration and dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := runDoctorChecks()

			data, err := json.MarshalIndent(checks, "", "  ")
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))

			for _, c := range checks {
				if c.Status == "fail" {
					return &ExitError{Code: 1}
				}
			}
			return nil
		},
	}
}

func runDoctorChecks() []doctorCheck {
	var checks []doctorCheck

	// Check ast-grep is installed (required for TypeScript scanning only).
	if _, err := exec.LookPath("ast-grep"); err != nil {
		checks = append(checks, doctorCheck{
			Check:   "ast-grep",
			Status:  "fail",
			Message: "ast-grep is not installed or not in PATH (required for TypeScript scanning only)",
			Hint:    "install ast-grep: npm install -g @ast-grep/cli",
		})
	} else {
		checks = append(checks, doctorCheck{
			Check:   "ast-grep",
			Status:  "pass",
			Message: "ast-grep is installed (required for TypeScript scanning)",
		})
	}

	// Check shared settings exist
	sharedPath, err := config.SharedSettingsPath()
	if err != nil {
		checks = append(checks, doctorCheck{
			Check:   "shared_settings",
			Status:  "fail",
			Message: fmt.Sprintf("cannot determine shared settings path: %v", err),
			Hint:    "run autograph init",
		})
	} else if _, err := config.LoadSharedSettings(sharedPath); err != nil {
		checks = append(checks, doctorCheck{
			Check:   "shared_settings",
			Status:  "fail",
			Message: fmt.Sprintf("shared settings invalid or missing: %v", err),
			Hint:    "run autograph init",
		})
	} else {
		checks = append(checks, doctorCheck{
			Check:   "shared_settings",
			Status:  "pass",
			Message: fmt.Sprintf("shared settings found at %s", sharedPath),
		})
	}

	// Check graph settings exist
	graphPath, err := config.GraphSettingsPath()
	if err != nil {
		checks = append(checks, doctorCheck{
			Check:   "graph_settings",
			Status:  "fail",
			Message: fmt.Sprintf("cannot determine graph settings path: %v", err),
			Hint:    "run autograph init",
		})
	} else if _, err := config.LoadGraphSettings(graphPath); err != nil {
		checks = append(checks, doctorCheck{
			Check:   "graph_settings",
			Status:  "fail",
			Message: fmt.Sprintf("graph settings invalid or missing: %v", err),
			Hint:    "run autograph init",
		})
	} else {
		checks = append(checks, doctorCheck{
			Check:   "graph_settings",
			Status:  "pass",
			Message: fmt.Sprintf("graph settings found at %s", graphPath),
		})
	}

	return checks
}
