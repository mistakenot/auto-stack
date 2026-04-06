package daemoninstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (m *Manager) Status(ctx context.Context, opts StatusOptions) (CombinedStatus, error) {
	serviceName, unitPath, err := m.resolveTarget(opts.ServiceName, opts.UnitPath)
	if err != nil {
		return CombinedStatus{}, err
	}
	if err := m.validateTarget(serviceName, unitPath); err != nil {
		return CombinedStatus{}, err
	}

	result := CombinedStatus{
		Daemon: ServiceStatus{
			Installed:   false,
			Enabled:     false,
			Running:     false,
			ServiceName: serviceName,
			UnitPath:    unitPath,
			Manager:     "systemd",
		},
	}

	unitBytes, err := m.readFile(unitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return CombinedStatus{}, remediationError(
			fmt.Sprintf("failed to read unit %q: %v", unitPath, err),
			"rerun with sudo autowatch daemon status",
		)
	}
	parsed, err := parseInstalledUnit(serviceName, unitPath, string(unitBytes))
	if err != nil {
		return CombinedStatus{}, remediationError(
			fmt.Sprintf("failed to parse unit %q: %v", unitPath, err),
			"rerun sudo autowatch daemon install to rewrite the unit",
		)
	}

	result.Daemon = ServiceStatus{
		Installed:   true,
		ServiceName: serviceName,
		UnitPath:    unitPath,
		Manager:     "systemd",
		User:        parsed.RuntimeUser,
		Group:       parsed.RuntimeGroup,
		HomeDir:     parsed.HomeDir,
		WorkingDir:  parsed.WorkingDir,
		ExecStart:   parsed.BinPath + " start",
	}

	enabled, err := m.readSystemctlState(ctx, "is-enabled", serviceName)
	if err != nil {
		return CombinedStatus{}, err
	}
	running, err := m.readSystemctlState(ctx, "is-active", serviceName)
	if err != nil {
		return CombinedStatus{}, err
	}
	result.Daemon.Enabled = enabled
	result.Daemon.Running = running

	if !running {
		return result, nil
	}

	runtime, warning := m.readRuntimeStatus(ctx, &parsed)
	result.Runtime = runtime
	result.RuntimeWarning = warning
	return result, nil
}

func parseInstalledUnit(serviceName, unitPath, content string) (ServiceSpec, error) {
	spec := ServiceSpec{
		ServiceName: serviceName,
		UnitPath:    unitPath,
	}
	for rawLine := range strings.SplitSeq(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "User="):
			spec.RuntimeUser = strings.TrimSpace(strings.TrimPrefix(line, "User="))
		case strings.HasPrefix(line, "Group="):
			spec.RuntimeGroup = strings.TrimSpace(strings.TrimPrefix(line, "Group="))
		case strings.HasPrefix(line, "WorkingDirectory="):
			spec.WorkingDir = strings.TrimSpace(strings.TrimPrefix(line, "WorkingDirectory="))
		case strings.HasPrefix(line, "Environment="):
			value := strings.TrimSpace(strings.TrimPrefix(line, "Environment="))
			switch {
			case strings.HasPrefix(value, "HOME="):
				spec.HomeDir = strings.TrimSpace(strings.TrimPrefix(value, "HOME="))
			case strings.HasPrefix(value, "PATH="):
				spec.PathEnv = strings.TrimSpace(strings.TrimPrefix(value, "PATH="))
			}
		case strings.HasPrefix(line, "ExecStart="):
			execValue := strings.TrimSpace(strings.TrimPrefix(line, "ExecStart="))
			fields := strings.Fields(execValue)
			if len(fields) > 0 {
				spec.BinPath = filepath.Clean(fields[0])
			}
		}
	}
	if strings.TrimSpace(spec.RuntimeUser) == "" {
		return ServiceSpec{}, errors.New("missing User field")
	}
	if strings.TrimSpace(spec.RuntimeGroup) == "" {
		return ServiceSpec{}, errors.New("missing Group field")
	}
	if strings.TrimSpace(spec.HomeDir) == "" {
		return ServiceSpec{}, errors.New("missing HOME environment")
	}
	if strings.TrimSpace(spec.BinPath) == "" {
		return ServiceSpec{}, errors.New("missing ExecStart binary")
	}
	return spec, nil
}

func (m *Manager) readSystemctlState(ctx context.Context, subcommand, serviceName string) (bool, error) {
	stdout, stderr, err := m.Runner.Run(ctx, "systemctl", subcommand, serviceName)
	state := strings.TrimSpace(stdout)
	if state == "" {
		state = strings.TrimSpace(stderr)
	}
	switch subcommand {
	case "is-enabled":
		switch state {
		case "enabled":
			return true, nil
		case "disabled", "indirect", "static", "generated", "alias", "linked", "masked":
			return false, nil
		}
	case "is-active":
		switch state {
		case "active":
			return true, nil
		case "inactive", "failed", "activating", "deactivating", "unknown":
			return false, nil
		}
	}
	if err != nil {
		return false, remediationError(
			fmt.Sprintf("systemctl %s %s failed: %s", subcommand, serviceName, fallbackDetail(stdout, stderr, err)),
			"rerun with sudo autowatch daemon status",
		)
	}
	return false, remediationError(
		fmt.Sprintf("systemctl %s %s returned unexpected state %q", subcommand, serviceName, state),
		"inspect the service with systemctl status",
	)
}

func (m *Manager) readRuntimeStatus(ctx context.Context, spec *ServiceSpec) (map[string]any, string) {
	command := "env"
	args := []string{"HOME=" + spec.HomeDir, spec.BinPath, "status", "--json"}
	current, _ := m.currentUser()
	currentName := ""
	if current != nil {
		currentName = current.Username
	}
	if m.geteuid() == 0 || currentName != spec.RuntimeUser {
		command = "sudo"
		args = []string{"-u", spec.RuntimeUser, "env", "HOME=" + spec.HomeDir, spec.BinPath, "status", "--json"}
	}

	stdout, stderr, err := m.Runner.Run(ctx, command, args...)
	if err != nil {
		return nil, remediationError(
			"runtime status invocation failed: "+fallbackDetail(stdout, stderr, err),
			fmt.Sprintf("run sudo -u %s env HOME=%s %s status --json", spec.RuntimeUser, spec.HomeDir, spec.BinPath),
		).Error()
	}
	var payload map[string]any
	if unmarshalErr := json.Unmarshal([]byte(stdout), &payload); unmarshalErr != nil {
		return nil, remediationError(
			fmt.Sprintf("runtime status returned invalid JSON: %v", unmarshalErr),
			fmt.Sprintf("run sudo -u %s env HOME=%s %s status --json", spec.RuntimeUser, spec.HomeDir, spec.BinPath),
		).Error()
	}
	return payload, ""
}

func fallbackDetail(stdout, stderr string, err error) string {
	if strings.TrimSpace(stderr) != "" {
		return strings.TrimSpace(stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		return strings.TrimSpace(stdout)
	}
	return err.Error()
}
