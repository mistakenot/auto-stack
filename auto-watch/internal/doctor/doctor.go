package doctor

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/gitx"
	"github.com/mistakenot/auto-watch/internal/model"
)

func Run(ctx context.Context, cwd string) (string, []model.DoctorCheck) {
	checks := []model.DoctorCheck{
		checkTmux(ctx),
		checkClaude(),
		checkGit(ctx),
		checkSettings(),
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
		return model.DoctorCheck{Name: "settings", Status: "fail", Message: err.Error(), Remediation: "run autowatch init"}
	}
	if _, err := os.Stat(path); err != nil {
		return model.DoctorCheck{
			Name:        "settings",
			Status:      "fail",
			Message:     "global settings.json is missing",
			Remediation: "run autowatch init",
		}
	}
	cfg, err := config.LoadGlobalConfig(path)
	if err != nil {
		return model.DoctorCheck{
			Name:        "settings",
			Status:      "fail",
			Message:     "failed to load global settings",
			Remediation: "fix ~/.auto/watch/settings.json or rerun autowatch init",
			Details:     err.Error(),
		}
	}
	if errs := config.ValidateGlobalConfig(cfg); len(errs) > 0 {
		return model.DoctorCheck{
			Name:        "settings",
			Status:      "fail",
			Message:     "global settings failed validation",
			Remediation: "fix ~/.auto/watch/settings.json or rerun autowatch init",
			Details:     errs[0].Message,
		}
	}
	return model.DoctorCheck{Name: "settings", Status: "ok", Message: path}
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
			Remediation: "fix .auto/watch/project.json or rerun autowatch init",
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
