package daemoninstall

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (m *Manager) resolveSpec(opts *InstallOptions) (ServiceSpec, error) {
	scope := normalizeScope(opts.Scope)

	// Resolve the runtime user and home BEFORE building the unit path: in user
	// scope the unit dir is <home>/.config/systemd/user, so it depends on the
	// resolved home directory. (System scope ignores home for the unit dir.)
	runtimeUser, err := m.resolveRuntimeUser(scope, strings.TrimSpace(opts.RuntimeUser))
	if err != nil {
		return ServiceSpec{}, err
	}

	account, err := m.lookupUser(runtimeUser)
	if err != nil {
		return ServiceSpec{}, remediationError(
			fmt.Sprintf("runtime user %q does not exist", runtimeUser),
			"rerun with --user <non-root-user>",
		)
	}

	groupName := account.Username
	if account.Gid != "" {
		group, groupErr := m.lookupGroupID(account.Gid)
		if groupErr == nil && strings.TrimSpace(group.Name) != "" {
			groupName = group.Name
		} else if strings.TrimSpace(account.Gid) != "" {
			groupName = account.Gid
		}
	}

	homeDir := strings.TrimSpace(opts.HomeDir)
	if homeDir == "" {
		homeDir = account.HomeDir
	}

	serviceName, unitPath, err := m.resolveTarget(scope, homeDir, opts.ServiceName, opts.UnitPath)
	if err != nil {
		return ServiceSpec{}, err
	}

	workingDir := strings.TrimSpace(opts.WorkingDir)
	if workingDir == "" {
		workingDir = homeDir
	}
	binPath := strings.TrimSpace(opts.BinPath)
	if binPath == "" {
		binPath = filepath.Join(homeDir, ".local", "bin", "auto")
	}
	pathEnv := strings.TrimSpace(opts.PathEnv)
	if pathEnv == "" {
		pathEnv = defaultPathEnv(homeDir)
	}
	description := strings.TrimSpace(opts.Description)
	if description == "" {
		description = defaultDescription
	}

	return ServiceSpec{
		ServiceName:  serviceName,
		Description:  description,
		RuntimeUser:  runtimeUser,
		RuntimeGroup: groupName,
		HomeDir:      homeDir,
		WorkingDir:   workingDir,
		BinPath:      binPath,
		PathEnv:      pathEnv,
		UnitPath:     unitPath,
	}, nil
}

func (m *Manager) resolveTarget(scope Scope, home, rawServiceName, rawUnitPath string) (string, string, error) {
	serviceName, err := normalizeServiceName(rawServiceName)
	if err != nil {
		return "", "", err
	}
	unitPath := strings.TrimSpace(rawUnitPath)
	if unitPath == "" {
		unitPath = filepath.Join(m.unitDirFor(scope, home), serviceName)
	}
	return serviceName, unitPath, nil
}

func (m *Manager) resolveRuntimeUser(scope Scope, explicit string) (string, error) {
	candidate := strings.TrimSpace(explicit)
	// User scope runs as the invoking user — no SUDO_USER indirection.
	if candidate == "" && normalizeScope(scope) == ScopeSystem {
		candidate = strings.TrimSpace(m.getenv("SUDO_USER"))
	}
	if candidate == "" {
		current, err := m.currentUser()
		if err != nil {
			return "", remediationError(
				"failed to determine the current user",
				"rerun with --user <non-root-user>",
			)
		}
		candidate = current.Username
	}
	if candidate == "root" {
		return "", remediationError(
			`runtime user cannot be "root"`,
			"rerun with --user <non-root-user>",
		)
	}
	return candidate, nil
}

func defaultPathEnv(homeDir string) string {
	return filepath.Join(homeDir, ".local", "bin") + ":/usr/local/bin:/usr/bin:/bin"
}
