package daemoninstall

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func (m *Manager) Install(ctx context.Context, opts *InstallOptions) (InstallResult, error) {
	scope := normalizeScope(opts.Scope)
	spec, err := m.resolveSpec(opts)
	if err != nil {
		return InstallResult{}, err
	}
	if err := m.validateInstallSpec(scope, &spec); err != nil {
		return InstallResult{}, err
	}
	rendered, err := renderUnit(&spec, scope)
	if err != nil {
		return InstallResult{}, fmt.Errorf("render unit: %w", err)
	}

	result := InstallResult{
		Spec: spec,
		Unit: rendered,
	}
	if opts.PrintUnit {
		return result, nil
	}

	existing, readErr := m.readFile(spec.UnitPath)
	switch {
	case readErr == nil:
		result.ExistingUnit = true
		result.Changed = string(existing) != rendered
	case os.IsNotExist(readErr):
		result.Changed = true
	default:
		return InstallResult{}, remediationError(
			fmt.Sprintf("failed to read existing unit %q: %v", spec.UnitPath, readErr),
			installRemediation(scope),
		)
	}

	result.PlannedActions = plannedInstallActions(&result, opts)
	if opts.DryRun {
		return result, nil
	}

	// User scope drives systemctl --user, which requires a running user D-Bus
	// session (XDG_RUNTIME_DIR). Preflight before mutating anything so a missing
	// bus surfaces a clear remediation rather than a raw "Failed to connect to bus".
	if scope == ScopeUser {
		if err := m.checkUserBus(ctx); err != nil {
			return InstallResult{}, err
		}
		if err := m.ensureUnitDir(spec.UnitPath); err != nil {
			return InstallResult{}, err
		}
	}

	if result.Changed {
		if err := m.writeFileAtomic(spec.UnitPath, []byte(rendered), 0o644); err != nil {
			return InstallResult{}, remediationError(
				fmt.Sprintf("failed to write unit %q: %v", spec.UnitPath, err),
				installRemediation(scope),
			)
		}
		if err := m.runSystemctl(ctx, scope, installRemediation(scope), "daemon-reload"); err != nil {
			return InstallResult{}, err
		}
		result.CompletedActions = append(result.CompletedActions, completedAction(scope, "daemon-reload"))
	}
	if opts.Enable {
		if err := m.runSystemctl(ctx, scope, installRemediation(scope), "enable", spec.ServiceName); err != nil {
			return InstallResult{}, err
		}
		result.CompletedActions = append(result.CompletedActions, completedAction(scope, "enable "+spec.ServiceName))
	}
	if opts.Start {
		if err := m.runSystemctl(ctx, scope, installRemediation(scope), "start", spec.ServiceName); err != nil {
			return InstallResult{}, err
		}
		result.CompletedActions = append(result.CompletedActions, completedAction(scope, "start "+spec.ServiceName))
	}

	// After a user-scope enable+start, best-effort enable lingering so the unit
	// survives logout and starts at boot. Never fail the install on error — just
	// surface a clear warning.
	if scope == ScopeUser && opts.Enable && opts.Start {
		if warning := m.tryEnableLinger(ctx, spec.RuntimeUser); warning != "" {
			result.Warnings = append(result.Warnings, warning)
		} else {
			result.CompletedActions = append(result.CompletedActions, "loginctl enable-linger "+spec.RuntimeUser)
		}
	}

	return result, nil
}

// completedAction renders a human-readable record of a systemctl action,
// including the --user flag for user scope.
func completedAction(scope Scope, action string) string {
	return "systemctl " + strings.Join(systemctlArgs(scope, action), " ")
}

// ensureUnitDir creates the directory holding the unit file (user scope writes
// into ~/.config/systemd/user, which may not exist yet).
func (m *Manager) ensureUnitDir(unitPath string) error {
	dir := filepath.Dir(unitPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return remediationError(
			fmt.Sprintf("failed to create unit directory %q: %v", dir, err),
			"ensure your home directory is writable, or pass --system",
		)
	}
	return nil
}

// checkUserBus verifies a user D-Bus session is reachable before issuing
// systemctl --user calls. It checks XDG_RUNTIME_DIR is set and probes the user
// manager; a missing bus yields a clear remediation instead of a raw
// "Failed to connect to bus" deep in a later call.
func (m *Manager) checkUserBus(ctx context.Context) error {
	if strings.TrimSpace(m.getenv("XDG_RUNTIME_DIR")) == "" {
		return userBusError()
	}
	_, stderr, err := m.Runner.Run(ctx, "systemctl", "--user", "is-system-running")
	if err != nil && strings.Contains(strings.ToLower(stderr), "failed to connect to bus") {
		return userBusError()
	}
	return nil
}

func userBusError() error {
	return remediationError(
		"no user D-Bus session for systemctl --user",
		"start a login session (or `loginctl enable-linger <user>`), or rerun with --system",
	)
}

// tryEnableLinger best-effort enables lingering for the runtime user. It returns
// a warning string on failure (never an error) so the install still succeeds.
func (m *Manager) tryEnableLinger(ctx context.Context, runtimeUser string) string {
	_, stderr, err := m.Runner.Run(ctx, "loginctl", "enable-linger", runtimeUser)
	if err != nil {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Sprintf(
			"could not enable lingering for %q (%s); the daemon will stop at logout until you run `sudo loginctl enable-linger %s`",
			runtimeUser, detail, runtimeUser,
		)
	}
	return ""
}

func plannedInstallActions(result *InstallResult, opts *InstallOptions) []string {
	actions := []string{}
	if result.Changed {
		actions = append(actions, "write unit to "+result.Spec.UnitPath, "systemctl daemon-reload")
	} else {
		actions = append(actions, "unit already matches rendered content")
	}
	if opts.Enable {
		actions = append(actions, "systemctl enable "+result.Spec.ServiceName)
	}
	if opts.Start {
		actions = append(actions, "systemctl start "+result.Spec.ServiceName)
	}
	return actions
}

func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (m *Manager) runSystemctl(ctx context.Context, scope Scope, remediation string, args ...string) error {
	if len(args) == 0 {
		return nil
	}
	full := systemctlArgs(scope, args...)
	stdout, stderr, err := m.Runner.Run(ctx, "systemctl", full...)
	if err != nil {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = strings.TrimSpace(stdout)
		}
		if detail == "" {
			detail = err.Error()
		}
		return remediationError(
			fmt.Sprintf("systemctl %s failed: %s", strings.Join(full, " "), detail),
			remediation,
		)
	}
	return nil
}
