package daemoninstall

import (
	"context"
	"fmt"
	"os"
)

func (m *Manager) Restart(ctx context.Context, opts RestartOptions) (RestartResult, error) {
	scope := normalizeScope(opts.Scope)
	home, err := m.unitDirHome(scope)
	if err != nil {
		return RestartResult{}, err
	}
	serviceName, unitPath, err := m.resolveTarget(scope, home, opts.ServiceName, opts.UnitPath)
	if err != nil {
		return RestartResult{}, err
	}
	if err := m.validateTarget(scope, home, serviceName, unitPath); err != nil {
		return RestartResult{}, err
	}
	if _, err := m.stat(unitPath); err != nil {
		if os.IsNotExist(err) {
			return RestartResult{}, remediationError(
				fmt.Sprintf("unit %q is not installed", unitPath),
				installRemediation(scope),
			)
		}
		return RestartResult{}, remediationError(
			fmt.Sprintf("failed to inspect unit %q: %v", unitPath, err),
			installRemediation(scope),
		)
	}

	running, err := m.readSystemctlState(ctx, scope, "is-active", serviceName)
	if err != nil {
		return RestartResult{}, err
	}
	if !running {
		return RestartResult{}, remediationError(
			fmt.Sprintf("service %q is installed but not running", serviceName),
			startRemediation(scope, serviceName),
		)
	}

	if err := m.runSystemctl(ctx, scope, restartRemediation(scope), "try-restart", serviceName); err != nil {
		return RestartResult{}, err
	}
	return RestartResult{
		ServiceName: serviceName,
		UnitPath:    unitPath,
	}, nil
}
