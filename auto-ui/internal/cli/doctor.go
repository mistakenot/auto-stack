package cli

import (
	"context"
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
			checks := runDoctorChecks(cmd.Context())

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

func runDoctorChecks(ctx context.Context) []doctorCheck {
	if ctx == nil {
		ctx = context.Background()
	}
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

	checks = append(checks, runBackendDoctorChecks(ctx)...)

	return checks
}

// runBackendDoctorChecks validates ~/.auto/ui/backends.json and probes each
// configured backend. It emits a backends_config check (the GR-F6 prerequisite:
// serve requires at least one reachable backend) plus one connectivity check per
// backend. Each connectivity probe is a bounded one-shot dial (reusing
// verifyBackend) so doctor never hangs against an unresponsive backend.
func runBackendDoctorChecks(ctx context.Context) []doctorCheck {
	var checks []doctorCheck

	path, err := config.BackendsPath()
	if err != nil {
		return append(checks, doctorCheck{
			Check:   "backends_config",
			Status:  "fail",
			Message: fmt.Sprintf("cannot determine backends config path: %v", err),
			Hint:    "run auto ui backends add <uri>",
		})
	}

	cfg, err := config.LoadBackends(path)
	if err != nil {
		return append(checks, doctorCheck{
			Check:   "backends_config",
			Status:  "fail",
			Message: fmt.Sprintf("backends config invalid: %v", err),
			Hint:    "fix " + path + " or run auto ui backends add <uri>",
		})
	}

	if len(cfg.Backends) == 0 {
		// GR-F6: serve fails fast without at least one backend.
		return append(checks, doctorCheck{
			Check:   "backends_config",
			Status:  "fail",
			Message: "no autowatch backends configured; auto ui serve requires at least one reachable backend",
			Hint:    "run auto ui backends add <uri>",
		})
	}

	checks = append(checks, doctorCheck{
		Check:   "backends_config",
		Status:  "pass",
		Message: fmt.Sprintf("%d backend(s) configured in %s", len(cfg.Backends), path),
	})

	// One-shot connectivity probe per backend: dial + daemon.status (learn the
	// hostId) + project.list. verifyBackend bounds each probe with verifyTimeout
	// and tears the connection down before returning, so no goroutine leaks.
	for _, b := range cfg.Backends {
		check := "backend:" + b.URI
		hostID, projectCount, verr := verifyBackend(ctx, b.URI)
		if verr != nil {
			checks = append(checks, doctorCheck{
				Check:   check,
				Status:  "fail",
				Message: fmt.Sprintf("backend %s unreachable: %v", b.URI, verr),
				Hint:    "ensure autowatch is running and reachable at " + b.URI,
			})
			continue
		}
		checks = append(checks, doctorCheck{
			Check:   check,
			Status:  "pass",
			Message: fmt.Sprintf("backend %s reachable: hostId=%s, %d project(s)", b.URI, hostID, projectCount),
		})
	}

	return checks
}
