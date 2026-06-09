package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mistakenot/auto-ui/internal/config"
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
		Short: "Check auto ui configuration",
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

	// Check UI settings exist and are valid.
	uiPath, err := config.UISettingsPath()
	if err != nil {
		checks = append(checks, doctorCheck{
			Check:   "ui_settings",
			Status:  "fail",
			Message: fmt.Sprintf("cannot determine ui settings path: %v", err),
			Hint:    "run auto ui init",
		})
		return checks
	}

	cfg, err := config.LoadUISettings(uiPath)
	if err != nil {
		checks = append(checks, doctorCheck{
			Check:   "ui_settings",
			Status:  "fail",
			Message: fmt.Sprintf("ui settings invalid or missing: %v", err),
			Hint:    "run auto ui init",
		})
		return checks
	}

	checks = append(checks, doctorCheck{
		Check:   "ui_settings",
		Status:  "pass",
		Message: "ui settings found at " + uiPath,
	})

	// Informational check on the configured port range (not a live bind probe).
	if cfg.Port < 1 || cfg.Port > 65535 {
		checks = append(checks, doctorCheck{
			Check:   "port",
			Status:  "fail",
			Message: fmt.Sprintf("configured port %d is out of range (1-65535)", cfg.Port),
			Hint:    "set a valid port in " + uiPath,
		})
	} else {
		checks = append(checks, doctorCheck{
			Check:   "port",
			Status:  "pass",
			Message: fmt.Sprintf("configured port %d is in valid range (1-65535)", cfg.Port),
		})
	}

	return checks
}
