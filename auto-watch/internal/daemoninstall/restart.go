package daemoninstall

import (
	"context"
	"fmt"
	"os"
)

func (m *Manager) Restart(ctx context.Context, opts RestartOptions) (RestartResult, error) {
	serviceName, unitPath, err := m.resolveTarget(opts.ServiceName, opts.UnitPath)
	if err != nil {
		return RestartResult{}, err
	}
	if err := m.validateTarget(serviceName, unitPath); err != nil {
		return RestartResult{}, err
	}
	if _, err := m.stat(unitPath); err != nil {
		if os.IsNotExist(err) {
			return RestartResult{}, remediationError(
				fmt.Sprintf("unit %q is not installed", unitPath),
				"run sudo autowatch daemon install first",
			)
		}
		return RestartResult{}, remediationError(
			fmt.Sprintf("failed to inspect unit %q: %v", unitPath, err),
			"run sudo autowatch daemon install first",
		)
	}

	running, err := m.readSystemctlState(ctx, "is-active", serviceName)
	if err != nil {
		return RestartResult{}, err
	}
	if !running {
		return RestartResult{}, remediationError(
			fmt.Sprintf("service %q is installed but not running", serviceName),
			"run sudo systemctl start "+serviceName,
		)
	}

	if err := m.runSystemctl(ctx, "rerun with sudo autowatch daemon restart", "try-restart", serviceName); err != nil {
		return RestartResult{}, err
	}
	return RestartResult{
		ServiceName: serviceName,
		UnitPath:    unitPath,
	}, nil
}
