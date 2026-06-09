package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/daemoninstall"
	"github.com/mistakenot/auto-watch/internal/gitx"
	"github.com/mistakenot/auto-watch/internal/model"
)

func Run(ctx context.Context, cwd string) (string, []model.DoctorCheck) {
	checks := []model.DoctorCheck{
		checkTmux(ctx),
		checkClaude(),
		checkGit(ctx),
		checkSettings(),
		checkDaemonUnit(),
	}
	if repoRoot, err := gitx.FindRepoRoot(cwd); err == nil {
		checks = append(checks, checkProjectConfig(repoRoot))
	}
	status := "ok"
	for _, check := range checks {
		if check.Status == "fail" {
			status = "fail"
			break
		}
	}
	return status, checks
}

func HasFailures(checks []model.DoctorCheck) bool {
	for _, check := range checks {
		if check.Status == "fail" {
			return true
		}
	}
	return false
}

func checkTmux(ctx context.Context) model.DoctorCheck {
	cmd := exec.CommandContext(ctx, "tmux", "-V")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return model.DoctorCheck{
			Name:        "tmux",
			Status:      "fail",
			Message:     "tmux is not available",
			Remediation: "install tmux 3.0+ and ensure it is on PATH",
			Details:     strings.TrimSpace(string(out)),
		}
	}
	version := strings.TrimSpace(string(out))
	if !versionAtLeast(version, 3, 0) {
		return model.DoctorCheck{
			Name:        "tmux",
			Status:      "fail",
			Message:     "tmux version is too old: " + version,
			Remediation: "upgrade tmux to version 3.0 or newer",
		}
	}
	return model.DoctorCheck{Name: "tmux", Status: "ok", Message: version}
}

func checkClaude() model.DoctorCheck {
	path, err := exec.LookPath("claude")
	if err != nil {
		return model.DoctorCheck{
			Name:        "claude",
			Status:      "fail",
			Message:     "claude CLI is not available",
			Remediation: "install claude and ensure it is on PATH",
		}
	}
	return model.DoctorCheck{Name: "claude", Status: "ok", Message: path}
}

func checkGit(ctx context.Context) model.DoctorCheck {
	cmd := exec.CommandContext(ctx, "git", "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return model.DoctorCheck{
			Name:        "git",
			Status:      "fail",
			Message:     "git is not available",
			Remediation: "install git 2.20+ and ensure it is on PATH",
			Details:     strings.TrimSpace(string(out)),
		}
	}
	version := strings.TrimSpace(string(out))
	if !versionAtLeast(version, 2, 20) {
		return model.DoctorCheck{
			Name:        "git",
			Status:      "fail",
			Message:     "git version is too old: " + version,
			Remediation: "upgrade git to version 2.20 or newer",
		}
	}
	return model.DoctorCheck{Name: "git", Status: "ok", Message: version}
}

func checkSettings() model.DoctorCheck {
	path, err := config.SettingsPath()
	if err != nil {
		return model.DoctorCheck{Name: "settings", Status: "fail", Message: err.Error(), Remediation: "run auto watch init"}
	}
	if _, err := os.Stat(path); err != nil {
		return model.DoctorCheck{
			Name:        "settings",
			Status:      "fail",
			Message:     "global settings.json is missing",
			Remediation: "run auto watch init",
		}
	}
	cfg, err := config.LoadGlobalConfig(path)
	if err != nil {
		return model.DoctorCheck{
			Name:        "settings",
			Status:      "fail",
			Message:     "failed to load global settings",
			Remediation: "fix ~/.auto/watch/settings.json or rerun auto watch init",
			Details:     err.Error(),
		}
	}
	if errs := config.ValidateGlobalConfig(cfg); len(errs) > 0 {
		return model.DoctorCheck{
			Name:        "settings",
			Status:      "fail",
			Message:     "global settings failed validation",
			Remediation: "fix ~/.auto/watch/settings.json or rerun auto watch init",
			Details:     errs[0].Message,
		}
	}
	return model.DoctorCheck{Name: "settings", Status: "ok", Message: path}
}

// checkDaemonUnit locates the watch daemon systemd unit (user scope first, then
// system scope) and verifies its ExecStart points at an existing, executable
// binary that uses the current `auto watch start` form. No unit installed is a
// passing state (the daemon is optional); a dangling or stale ExecStart fails.
func checkDaemonUnit() model.DoctorCheck {
	return checkDaemonUnitAt(daemoninstall.DefaultUnitPaths(), os.ReadFile, os.Stat)
}

func checkDaemonUnitAt(
	unitPaths []string,
	readFile func(string) ([]byte, error),
	stat func(string) (os.FileInfo, error),
) model.DoctorCheck {
	const name = "daemon_unit"
	const remediation = "auto watch daemon install"

	unitPath := ""
	var content []byte
	for _, candidate := range unitPaths {
		data, err := readFile(candidate)
		if err != nil {
			continue
		}
		unitPath = candidate
		content = data
		break
	}
	if unitPath == "" {
		return model.DoctorCheck{Name: name, Status: "ok", Message: "no daemon unit installed"}
	}

	execStart, binPath := parseExecStart(string(content))
	if execStart == "" || binPath == "" {
		return model.DoctorCheck{
			Name:        name,
			Status:      "fail",
			Message:     "daemon unit " + unitPath + " has no ExecStart binary",
			Remediation: remediation,
		}
	}

	// A stale unit installed before task 017 invokes the old `autowatch` binary
	// directly (e.g. .../autowatch start) instead of the unified `auto` binary
	// (.../auto watch start). Treat that as a failure even if the binary exists.
	if filepath.Base(binPath) == "autowatch" {
		return model.DoctorCheck{
			Name:        name,
			Status:      "fail",
			Message:     "daemon unit " + unitPath + " references the old autowatch binary",
			Remediation: remediation,
			Details:     "ExecStart=" + execStart,
		}
	}

	info, err := stat(binPath)
	if err != nil || info.IsDir() {
		return model.DoctorCheck{
			Name:        name,
			Status:      "fail",
			Message:     "daemon unit " + unitPath + " ExecStart binary " + binPath + " is missing",
			Remediation: remediation,
			Details:     "ExecStart=" + execStart,
		}
	}
	if info.Mode().Perm()&0o111 == 0 {
		return model.DoctorCheck{
			Name:        name,
			Status:      "fail",
			Message:     "daemon unit " + unitPath + " ExecStart binary " + binPath + " is not executable",
			Remediation: remediation,
			Details:     "ExecStart=" + execStart,
		}
	}

	return model.DoctorCheck{Name: name, Status: "ok", Message: unitPath}
}

// parseExecStart returns the raw ExecStart value and the resolved binary path
// (the first token) from a systemd unit file. Empty strings indicate no
// ExecStart line or no binary token.
func parseExecStart(content string) (execStart, binPath string) {
	for rawLine := range strings.SplitSeq(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		execStart = strings.TrimSpace(strings.TrimPrefix(line, "ExecStart="))
		fields := strings.Fields(execStart)
		if len(fields) > 0 {
			binPath = filepath.Clean(fields[0])
		}
		return execStart, binPath
	}
	return "", ""
}

func checkProjectConfig(repoRoot string) model.DoctorCheck {
	path := config.ProjectConfigPath(repoRoot)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return model.DoctorCheck{Name: "project_config", Status: "ok", Message: "project config not present in current repo"}
	}
	cfg, err := config.LoadProjectConfig(repoRoot)
	if err != nil {
		return model.DoctorCheck{
			Name:        "project_config",
			Status:      "fail",
			Message:     "failed to load project config",
			Remediation: "fix .auto/watch/project.json or rerun auto watch init",
			Details:     err.Error(),
		}
	}
	if errs := config.ValidateProjectConfig(cfg); len(errs) > 0 {
		return model.DoctorCheck{
			Name:        "project_config",
			Status:      "fail",
			Message:     "project config failed validation",
			Remediation: "fix .auto/watch/project.json",
			Details:     errs[0].Message,
		}
	}
	return model.DoctorCheck{Name: "project_config", Status: "ok", Message: path}
}

func versionAtLeast(version string, wantMajor, wantMinor int) bool {
	re := regexp.MustCompile(`(\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(version)
	if len(matches) != 3 {
		return false
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	if major != wantMajor {
		return major > wantMajor
	}
	return minor >= wantMinor
}
