package daemoninstall

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var serviceBasePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func (m *Manager) validateInstallSpec(spec *ServiceSpec) error {
	if err := m.ensureSystemdAvailable(); err != nil {
		return err
	}
	if strings.TrimSpace(spec.RuntimeUser) == "" || spec.RuntimeUser == "root" {
		return remediationError(`runtime user cannot be empty or "root"`, "rerun with --user <non-root-user>")
	}
	if strings.TrimSpace(spec.RuntimeGroup) == "" {
		return remediationError("runtime group cannot be empty", "rerun with --user <non-root-user>")
	}
	if err := validateServiceName(spec.ServiceName); err != nil {
		return err
	}
	if err := m.validateUnitPath(spec.UnitPath, spec.ServiceName); err != nil {
		return err
	}
	if err := m.validateDir(spec.HomeDir, "home directory", "rerun with --home <existing-absolute-path>"); err != nil {
		return err
	}
	if err := m.validateDir(spec.WorkingDir, "working directory", "rerun with --working-dir <existing-absolute-path>"); err != nil {
		return err
	}
	if err := m.validateExecutable(spec.BinPath); err != nil {
		return err
	}
	if strings.TrimSpace(spec.PathEnv) == "" {
		return remediationError("PATH environment cannot be empty", "rerun with --path-env <path-list>")
	}
	return nil
}

func (m *Manager) validateTarget(serviceName, unitPath string) error {
	if err := m.ensureSystemdAvailable(); err != nil {
		return err
	}
	if err := validateServiceName(serviceName); err != nil {
		return err
	}
	return m.validateUnitPath(unitPath, serviceName)
}

func (m *Manager) ensureSystemdAvailable() error {
	if m.goos != "linux" {
		return remediationError("systemd daemon management is only supported on Linux", "run this command on a Linux host with systemd")
	}
	if _, err := m.lookPath("systemctl"); err != nil {
		return remediationError("systemctl is not available on PATH", "install systemd and ensure systemctl is available on PATH")
	}
	return nil
}

func validateServiceName(serviceName string) error {
	if !strings.HasSuffix(serviceName, ".service") {
		return remediationError("service name must end with .service", "rerun with --service-name <slug>")
	}
	base := strings.TrimSuffix(serviceName, ".service")
	if !serviceBasePattern.MatchString(base) {
		return remediationError(
			fmt.Sprintf("service name %q is invalid", serviceName),
			"rerun with --service-name <slug>",
		)
	}
	return nil
}

func normalizeServiceName(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = defaultServiceBase
	}
	value = strings.TrimSuffix(value, ".service")
	if !serviceBasePattern.MatchString(value) {
		return "", remediationError(
			fmt.Sprintf("service name %q is invalid", raw),
			"rerun with --service-name <slug>",
		)
	}
	return value + ".service", nil
}

func (m *Manager) validateUnitPath(unitPath, serviceName string) error {
	clean := filepath.Clean(strings.TrimSpace(unitPath))
	if !filepath.IsAbs(clean) {
		return remediationError("unit path must be absolute", "rerun with an absolute systemd unit path")
	}
	if filepath.Dir(clean) != filepath.Clean(m.unitDir) {
		return remediationError(
			"unit path must be directly under "+m.unitDir,
			"rerun with a unit path under "+m.unitDir,
		)
	}
	if filepath.Ext(clean) != ".service" {
		return remediationError("unit path must end with .service", "rerun with a unit path under "+m.unitDir)
	}
	if filepath.Base(clean) != serviceName {
		return remediationError(
			fmt.Sprintf("unit path %q does not match service name %q", clean, serviceName),
			fmt.Sprintf("rerun with --service-name %s or adjust the unit path basename", strings.TrimSuffix(serviceName, ".service")),
		)
	}
	return nil
}

func (m *Manager) validateDir(pathValue, label, remediation string) error {
	pathValue = strings.TrimSpace(pathValue)
	if !filepath.IsAbs(pathValue) {
		return remediationError(label+" must be an absolute path", remediation)
	}
	info, err := m.stat(pathValue)
	if err != nil {
		return remediationError(fmt.Sprintf("%s %q does not exist", label, pathValue), remediation)
	}
	if !info.IsDir() {
		return remediationError(fmt.Sprintf("%s %q is not a directory", label, pathValue), remediation)
	}
	return nil
}

func (m *Manager) validateExecutable(pathValue string) error {
	pathValue = strings.TrimSpace(pathValue)
	if !filepath.IsAbs(pathValue) {
		return remediationError("binary path must be absolute", "rerun with --bin <absolute-path> or run make install first")
	}
	info, err := m.stat(pathValue)
	if err != nil {
		return remediationError(fmt.Sprintf("binary %q does not exist", pathValue), "run make install first")
	}
	if info.IsDir() {
		return remediationError(fmt.Sprintf("binary path %q is a directory", pathValue), "run make install first")
	}
	if info.Mode().Perm()&0o111 == 0 {
		return remediationError(fmt.Sprintf("binary %q is not executable", pathValue), "run chmod +x on the autowatch binary or rerun make install")
	}
	return nil
}

func remediationError(message, remediation string) error {
	if strings.TrimSpace(remediation) == "" {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("%s; remediation: %s", message, remediation)
}
