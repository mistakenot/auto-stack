package cli

import (
	"fmt"
	"strings"

	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/daemoninstall"
	"github.com/mistakenot/auto-watch/internal/textout"
	"github.com/spf13/cobra"
)

func newDaemonCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the autowatch systemd service",
	}
	cmd.AddCommand(
		newDaemonInstallCmd(application),
		newDaemonRestartCmd(application),
		newDaemonStatusCmd(application),
	)
	return cmd
}

func newDaemonInstallCmd(application *app.App) *cobra.Command {
	var opts daemoninstall.InstallOptions
	var system bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install or update the systemd unit for autowatch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Scope = scopeFor(system)
			manager := daemoninstall.NewManager(application.Runner)
			result, err := manager.Install(cmd.Context(), &opts)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if opts.PrintUnit {
				fmt.Fprint(cmd.OutOrStdout(), result.Unit)
				return nil
			}
			if opts.DryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "service: %s\n", result.Spec.ServiceName)
				fmt.Fprintf(cmd.OutOrStdout(), "unit path: %s\n", result.Spec.UnitPath)
				fmt.Fprintf(cmd.OutOrStdout(), "runtime user: %s\n", result.Spec.RuntimeUser)
				fmt.Fprintf(cmd.OutOrStdout(), "planned actions:\n")
				for _, action := range result.PlannedActions {
					fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", action)
				}
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprint(cmd.OutOrStdout(), result.Unit)
				return nil
			}

			action := "created"
			if result.ExistingUnit && result.Changed {
				action = "updated"
			}
			if !result.Changed {
				action = "unchanged"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "service: %s\n", result.Spec.ServiceName)
			fmt.Fprintf(cmd.OutOrStdout(), "unit path: %s\n", result.Spec.UnitPath)
			fmt.Fprintf(cmd.OutOrStdout(), "runtime user: %s\n", result.Spec.RuntimeUser)
			fmt.Fprintf(cmd.OutOrStdout(), "unit state: %s\n", action)
			if len(result.CompletedActions) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "systemctl actions: %s\n", strings.Join(result.CompletedActions, ", "))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "next:")
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n", statusHint(opts.Scope, result.Spec.ServiceName))
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n", journalHint(opts.Scope, result.Spec.ServiceName))
			fmt.Fprintln(cmd.OutOrStdout(), "auto watch daemon status")
			fmt.Fprintln(cmd.OutOrStdout(), "auto watch status")
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.ServiceName, "service-name", "", "service name to install, with or without .service suffix")
	cmd.Flags().StringVar(&opts.RuntimeUser, "user", "", "runtime user for the service")
	cmd.Flags().StringVar(&opts.HomeDir, "home", "", "home directory for the runtime user")
	cmd.Flags().StringVar(&opts.WorkingDir, "working-dir", "", "working directory for the service")
	cmd.Flags().StringVar(&opts.BinPath, "bin", "", "absolute path to the auto binary")
	cmd.Flags().StringVar(&opts.PathEnv, "path-env", "", "PATH environment for the service")
	cmd.Flags().BoolVar(&system, "system", false, "install a system-level unit (/etc/systemd/system, requires root); default is a no-sudo user unit")
	cmd.Flags().BoolVar(&opts.Enable, "enable", true, "enable the service at boot (use --enable=false to opt out)")
	cmd.Flags().BoolVar(&opts.Start, "start", true, "start the service after installation (use --start=false to opt out)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "show the rendered unit and planned actions without writing")
	cmd.Flags().BoolVar(&opts.PrintUnit, "print-unit", false, "print the rendered unit and exit")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "print-unit")
	return cmd
}

func newDaemonRestartCmd(application *app.App) *cobra.Command {
	var opts daemoninstall.RestartOptions
	var system bool
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Try-restart the installed autowatch systemd service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Scope = scopeFor(system)
			manager := daemoninstall.NewManager(application.Runner)
			result, err := manager.Restart(cmd.Context(), opts)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "service: %s\n", result.ServiceName)
			fmt.Fprintf(cmd.OutOrStdout(), "unit path: %s\n", result.UnitPath)
			fmt.Fprintf(cmd.OutOrStdout(), "action: %s try-restart %s\n", systemctlCmd(opts.Scope), result.ServiceName)
			fmt.Fprintf(cmd.OutOrStdout(), "next: %s\n", statusHint(opts.Scope, result.ServiceName))
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.ServiceName, "service-name", "", "service name to restart, with or without .service suffix")
	cmd.Flags().BoolVar(&system, "system", false, "target the system-level unit; default is the user unit")
	return cmd
}

func newDaemonStatusCmd(application *app.App) *cobra.Command {
	var opts daemoninstall.StatusOptions
	var jsonOutput bool
	var system bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show installed and runtime state for the autowatch systemd service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Scope = scopeFor(system)
			manager := daemoninstall.NewManager(application.Runner)
			result, err := manager.Status(cmd.Context(), opts)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if jsonOutput {
				if err := textout.WriteJSON(cmd.OutOrStdout(), result); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "daemon installed: %s\n", yesNo(result.Daemon.Installed))
			fmt.Fprintf(cmd.OutOrStdout(), "daemon enabled: %s\n", yesNo(result.Daemon.Enabled))
			fmt.Fprintf(cmd.OutOrStdout(), "daemon running: %s\n", yesNo(result.Daemon.Running))
			fmt.Fprintf(cmd.OutOrStdout(), "service manager: %s\n", result.Daemon.Manager)
			if result.Daemon.User != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "runtime user: %s\n", result.Daemon.User)
			}
			if result.Runtime != nil {
				if value, ok := result.Runtime["daemon_running"].(bool); ok {
					fmt.Fprintf(cmd.OutOrStdout(), "runtime daemon: %s\n", yesNo(value))
				}
				if projects, ok := projectCount(result.Runtime["projects"]); ok {
					fmt.Fprintf(cmd.OutOrStdout(), "runtime projects: %d\n", projects)
				}
				if triggerCounts, ok := result.Runtime["trigger_counts"].(map[string]any); ok {
					if total, ok := asInt(triggerCounts["total"]); ok {
						fmt.Fprintf(cmd.OutOrStdout(), "runtime triggers: %d\n", total)
					}
				}
				if health, ok := result.Runtime["health"].(map[string]any); ok {
					status, _ := health["status"].(string)
					issues, _ := asInt(health["issueCount"])
					if status != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "runtime health: %s (%d issues)\n", status, issues)
					}
				}
			}
			if result.RuntimeWarning != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "runtime warning: %s\n", result.RuntimeWarning)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.ServiceName, "service-name", "", "service name to inspect, with or without .service suffix")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output JSON")
	cmd.Flags().BoolVar(&system, "system", false, "inspect the system-level unit; default is the user unit")
	return cmd
}

// scopeFor maps the --system flag to a daemoninstall.Scope. Omitting --system
// selects the no-sudo user scope (the default).
func scopeFor(system bool) daemoninstall.Scope {
	if system {
		return daemoninstall.ScopeSystem
	}
	return daemoninstall.ScopeUser
}

// systemctlCmd renders the systemctl invocation prefix for a scope: system
// units use "sudo systemctl"; user units use "systemctl --user" (no sudo).
func systemctlCmd(scope daemoninstall.Scope) string {
	if scope == daemoninstall.ScopeSystem {
		return "sudo systemctl"
	}
	return "systemctl --user"
}

// statusHint returns the scope-aware "check status" command to print after an
// install/restart.
func statusHint(scope daemoninstall.Scope, serviceName string) string {
	return fmt.Sprintf("%s status %s", systemctlCmd(scope), serviceName)
}

// journalHint returns the scope-aware journalctl follow command. User units use
// journalctl --user-unit; system units use journalctl -u.
func journalHint(scope daemoninstall.Scope, serviceName string) string {
	if scope == daemoninstall.ScopeSystem {
		return fmt.Sprintf("journalctl -u %s -f", serviceName)
	}
	return fmt.Sprintf("journalctl --user-unit %s -f", serviceName)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func asInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func projectCount(value any) (int, bool) {
	if count, ok := asInt(value); ok {
		return count, true
	}
	switch typed := value.(type) {
	case []any:
		return len(typed), true
	default:
		return 0, false
	}
}
