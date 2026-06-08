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
	spec, err := m.resolveSpec(opts)
	if err != nil {
		return InstallResult{}, err
	}
	if err := m.validateInstallSpec(&spec); err != nil {
		return InstallResult{}, err
	}
	rendered, err := renderUnit(&spec)
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
			"rerun with sudo auto watch daemon install",
		)
	}

	result.PlannedActions = plannedInstallActions(&result, opts)
	if opts.DryRun {
		return result, nil
	}

	if result.Changed {
		if err := m.writeFileAtomic(spec.UnitPath, []byte(rendered), 0o644); err != nil {
			return InstallResult{}, remediationError(
				fmt.Sprintf("failed to write unit %q: %v", spec.UnitPath, err),
				"rerun with sudo auto watch daemon install",
			)
		}
		if err := m.runSystemctl(ctx, "rerun with sudo auto watch daemon install", "daemon-reload"); err != nil {
			return InstallResult{}, err
		}
		result.CompletedActions = append(result.CompletedActions, "systemctl daemon-reload")
	}
	if opts.Enable {
		if err := m.runSystemctl(ctx, "rerun with sudo auto watch daemon install --enable", "enable", spec.ServiceName); err != nil {
			return InstallResult{}, err
		}
		result.CompletedActions = append(result.CompletedActions, "systemctl enable "+spec.ServiceName)
	}
	if opts.Start {
		if err := m.runSystemctl(ctx, "rerun with sudo auto watch daemon install --start", "start", spec.ServiceName); err != nil {
			return InstallResult{}, err
		}
		result.CompletedActions = append(result.CompletedActions, "systemctl start "+spec.ServiceName)
	}
	return result, nil
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

func (m *Manager) runSystemctl(ctx context.Context, remediation string, args ...string) error {
	if len(args) == 0 {
		return nil
	}
	stdout, stderr, err := m.Runner.Run(ctx, "systemctl", args...)
	if err != nil {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = strings.TrimSpace(stdout)
		}
		if detail == "" {
			detail = err.Error()
		}
		return remediationError(
			fmt.Sprintf("systemctl %s failed: %s", strings.Join(args, " "), detail),
			remediation,
		)
	}
	return nil
}
