package daemoninstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mistakenot/auto-watch/internal/shell"
)

type runnerStep struct {
	name   string
	args   []string
	stdout string
	stderr string
	err    error
}

type fakeRunner struct {
	t     *testing.T
	steps []runnerStep
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	f.t.Helper()
	if len(f.steps) == 0 {
		f.t.Fatalf("unexpected command: %s %s", name, strings.Join(args, " "))
	}
	step := f.steps[0]
	f.steps = f.steps[1:]
	if step.name != name || !reflect.DeepEqual(step.args, args) {
		f.t.Fatalf("unexpected command: got %s %v want %s %v", name, args, step.name, step.args)
	}
	return step.stdout, step.stderr, step.err
}

func (f *fakeRunner) AssertDone() {
	f.t.Helper()
	if len(f.steps) > 0 {
		step := f.steps[0]
		f.t.Fatalf("expected command was not run: %s %v", step.name, step.args)
	}
}

type testRig struct {
	manager     *Manager
	env         map[string]string
	currentUser *user.User
	users       map[string]*user.User
	unitDir     string
	aliceHome   string
	aliceBin    string
}

func newTestRig(t *testing.T, runner shell.Runner) *testRig {
	t.Helper()
	root := t.TempDir()
	unitDir := filepath.Join(root, "systemd")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	builderHome := filepath.Join(root, "builder")
	aliceHome := filepath.Join(root, "alice")
	for _, dir := range []string{builderHome, aliceHome} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	aliceBin := filepath.Join(aliceHome, ".local", "bin", "auto")
	if err := os.MkdirAll(filepath.Dir(aliceBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aliceBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	currentUser := &user.User{Username: "builder", HomeDir: builderHome, Gid: "1001"}
	users := map[string]*user.User{
		"builder": currentUser,
		"alice":   {Username: "alice", HomeDir: aliceHome, Gid: "1000"},
		"root":    {Username: "root", HomeDir: "/root", Gid: "0"},
	}
	groups := map[string]*user.Group{
		"1000": {Name: "alice"},
		"1001": {Name: "builder"},
		"0":    {Name: "root"},
	}

	manager := NewManager(runner)
	manager.goos = "linux"
	manager.unitDir = unitDir
	manager.getenv = func(key string) string { return "" }
	env := map[string]string{}
	manager.getenv = func(key string) string { return env[key] }
	manager.geteuid = func() int { return 1001 }
	manager.currentUser = func() (*user.User, error) { return currentUser, nil }
	manager.lookupUser = func(name string) (*user.User, error) {
		account, ok := users[name]
		if !ok {
			return nil, fmt.Errorf("unknown user %q", name)
		}
		copy := *account
		return &copy, nil
	}
	manager.lookupGroupID = func(gid string) (*user.Group, error) {
		group, ok := groups[gid]
		if !ok {
			return nil, fmt.Errorf("unknown group %q", gid)
		}
		copy := *group
		return &copy, nil
	}
	manager.lookPath = func(name string) (string, error) {
		switch name {
		case "systemctl":
			return "/usr/bin/systemctl", nil
		case "sudo":
			return "/usr/bin/sudo", nil
		default:
			return "/usr/bin/" + name, nil
		}
	}

	return &testRig{
		manager:     manager,
		env:         env,
		currentUser: currentUser,
		users:       users,
		unitDir:     unitDir,
		aliceHome:   aliceHome,
		aliceBin:    aliceBin,
	}
}

func TestResolveSpecUsesSUDOUserDefaults(t *testing.T) {
	rig := newTestRig(t, &fakeRunner{t: t})
	rig.env["SUDO_USER"] = "alice"

	spec, err := rig.manager.resolveSpec(&InstallOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("resolveSpec() error = %v", err)
	}
	if spec.ServiceName != "autowatch.service" {
		t.Fatalf("ServiceName = %q", spec.ServiceName)
	}
	if spec.RuntimeUser != "alice" {
		t.Fatalf("RuntimeUser = %q", spec.RuntimeUser)
	}
	if spec.RuntimeGroup != "alice" {
		t.Fatalf("RuntimeGroup = %q", spec.RuntimeGroup)
	}
	if spec.HomeDir != rig.aliceHome {
		t.Fatalf("HomeDir = %q", spec.HomeDir)
	}
	if spec.WorkingDir != rig.aliceHome {
		t.Fatalf("WorkingDir = %q", spec.WorkingDir)
	}
	if spec.BinPath != rig.aliceBin {
		t.Fatalf("BinPath = %q", spec.BinPath)
	}
	if spec.PathEnv != filepath.Join(rig.aliceHome, ".local", "bin")+":/usr/local/bin:/usr/bin:/bin" {
		t.Fatalf("PathEnv = %q", spec.PathEnv)
	}
	if spec.UnitPath != filepath.Join(rig.unitDir, "autowatch.service") {
		t.Fatalf("UnitPath = %q", spec.UnitPath)
	}
}

func TestResolveSpecRejectsRootRuntimeUser(t *testing.T) {
	rig := newTestRig(t, &fakeRunner{t: t})

	_, err := rig.manager.resolveSpec(&InstallOptions{Scope: ScopeSystem, RuntimeUser: "root"})
	if err == nil || !strings.Contains(err.Error(), `runtime user cannot be "root"`) {
		t.Fatalf("expected root rejection, got %v", err)
	}
}

func TestRenderUnitAndParseInstalledUnitRoundTrip(t *testing.T) {
	rig := newTestRig(t, &fakeRunner{t: t})
	rig.env["SUDO_USER"] = "alice"

	spec, err := rig.manager.resolveSpec(&InstallOptions{Scope: ScopeSystem, ServiceName: "nightly-watch"})
	if err != nil {
		t.Fatalf("resolveSpec() error = %v", err)
	}
	unit, err := renderUnit(&spec, ScopeSystem)
	if err != nil {
		t.Fatalf("renderUnit() error = %v", err)
	}
	parsed, err := parseInstalledUnit(ScopeSystem, spec.ServiceName, spec.UnitPath, unit)
	if err != nil {
		t.Fatalf("parseInstalledUnit() error = %v", err)
	}
	if parsed.RuntimeUser != spec.RuntimeUser {
		t.Fatalf("RuntimeUser = %q want %q", parsed.RuntimeUser, spec.RuntimeUser)
	}
	if parsed.RuntimeGroup != spec.RuntimeGroup {
		t.Fatalf("RuntimeGroup = %q want %q", parsed.RuntimeGroup, spec.RuntimeGroup)
	}
	if parsed.HomeDir != spec.HomeDir {
		t.Fatalf("HomeDir = %q want %q", parsed.HomeDir, spec.HomeDir)
	}
	if parsed.BinPath != spec.BinPath {
		t.Fatalf("BinPath = %q want %q", parsed.BinPath, spec.BinPath)
	}
}

func TestInstallWritesUnitAndRunsSystemctlActions(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		steps: []runnerStep{
			{name: "systemctl", args: []string{"daemon-reload"}},
			{name: "systemctl", args: []string{"enable", "autowatch.service"}},
			{name: "systemctl", args: []string{"start", "autowatch.service"}},
		},
	}
	rig := newTestRig(t, runner)
	rig.env["SUDO_USER"] = "alice"

	result, err := rig.manager.Install(context.Background(), &InstallOptions{Scope: ScopeSystem, Enable: true, Start: true})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !result.Changed {
		t.Fatalf("expected Changed=true")
	}
	unitBytes, err := os.ReadFile(result.Spec.UnitPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", result.Spec.UnitPath, err)
	}
	if string(unitBytes) != result.Unit {
		t.Fatalf("written unit mismatch")
	}
	runner.AssertDone()
}

func TestInstallSkipsRewriteWhenUnitIsUnchanged(t *testing.T) {
	rig := newTestRig(t, &fakeRunner{t: t})
	rig.env["SUDO_USER"] = "alice"

	spec, err := rig.manager.resolveSpec(&InstallOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("resolveSpec() error = %v", err)
	}
	unit, err := renderUnit(&spec, ScopeSystem)
	if err != nil {
		t.Fatalf("renderUnit() error = %v", err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte(unit), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", spec.UnitPath, err)
	}
	writeCalled := false
	rig.manager.writeFileAtomic = func(string, []byte, os.FileMode) error {
		writeCalled = true
		return nil
	}

	result, err := rig.manager.Install(context.Background(), &InstallOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Changed {
		t.Fatalf("expected Changed=false")
	}
	if writeCalled {
		t.Fatalf("writeFileAtomic was called for unchanged content")
	}
}

func TestRestartUsesTryRestartForRunningService(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		steps: []runnerStep{
			{name: "systemctl", args: []string{"is-active", "autowatch.service"}, stdout: "active"},
			{name: "systemctl", args: []string{"try-restart", "autowatch.service"}},
		},
	}
	rig := newTestRig(t, runner)
	rig.env["SUDO_USER"] = "alice"

	spec, err := rig.manager.resolveSpec(&InstallOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("resolveSpec() error = %v", err)
	}
	unit, err := renderUnit(&spec, ScopeSystem)
	if err != nil {
		t.Fatalf("renderUnit() error = %v", err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte(unit), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", spec.UnitPath, err)
	}

	result, err := rig.manager.Restart(context.Background(), RestartOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	if result.ServiceName != "autowatch.service" {
		t.Fatalf("ServiceName = %q", result.ServiceName)
	}
	runner.AssertDone()
}

func TestStatusReturnsInstalledAndRuntimeState(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		steps: []runnerStep{
			{name: "systemctl", args: []string{"is-enabled", "autowatch.service"}, stdout: "enabled"},
			{name: "systemctl", args: []string{"is-active", "autowatch.service"}, stdout: "active"},
		},
	}
	rig := newTestRig(t, runner)
	rig.env["SUDO_USER"] = "alice"
	rig.currentUser.Username = "root"
	rig.manager.currentUser = func() (*user.User, error) { return &user.User{Username: "root", HomeDir: "/root", Gid: "0"}, nil }
	rig.manager.geteuid = func() int { return 0 }

	spec, err := rig.manager.resolveSpec(&InstallOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("resolveSpec() error = %v", err)
	}
	unit, err := renderUnit(&spec, ScopeSystem)
	if err != nil {
		t.Fatalf("renderUnit() error = %v", err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte(unit), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", spec.UnitPath, err)
	}

	runner.steps = append(runner.steps, runnerStep{
		name:   "sudo",
		args:   []string{"-u", "alice", "env", "HOME=" + rig.aliceHome, rig.aliceBin, "watch", "status", "--json"},
		stdout: `{"daemon_running":true,"trigger_counts":{"total":2},"health":{"status":"ok","issueCount":0}}`,
	})

	status, err := rig.manager.Status(context.Background(), StatusOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Daemon.Installed || !status.Daemon.Enabled || !status.Daemon.Running {
		t.Fatalf("unexpected daemon status: %+v", status.Daemon)
	}
	if status.Daemon.User != "alice" {
		t.Fatalf("Daemon.User = %q", status.Daemon.User)
	}
	if runtimeRunning, ok := status.Runtime["daemon_running"].(bool); !ok || !runtimeRunning {
		t.Fatalf("runtime daemon_running = %#v", status.Runtime["daemon_running"])
	}
	triggerCounts, ok := status.Runtime["trigger_counts"].(map[string]any)
	if !ok {
		t.Fatalf("trigger_counts missing from runtime payload")
	}
	if total, ok := triggerCounts["total"].(float64); !ok || int(total) != 2 {
		t.Fatalf("trigger total = %#v", triggerCounts["total"])
	}
	if status.RuntimeWarning != "" {
		t.Fatalf("RuntimeWarning = %q", status.RuntimeWarning)
	}
	runner.AssertDone()
}

func TestStatusKeepsInstallStateWhenRuntimeCallFails(t *testing.T) {
	runner := &fakeRunner{
		t: t,
	}
	rig := newTestRig(t, runner)
	rig.env["SUDO_USER"] = "alice"
	rig.manager.currentUser = func() (*user.User, error) { return rig.users["alice"], nil }
	rig.manager.geteuid = func() int { return 1000 }

	spec, err := rig.manager.resolveSpec(&InstallOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("resolveSpec() error = %v", err)
	}
	unit, err := renderUnit(&spec, ScopeSystem)
	if err != nil {
		t.Fatalf("renderUnit() error = %v", err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte(unit), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", spec.UnitPath, err)
	}

	runner.steps = []runnerStep{
		{name: "systemctl", args: []string{"is-enabled", "autowatch.service"}, stdout: "enabled"},
		{name: "systemctl", args: []string{"is-active", "autowatch.service"}, stdout: "active"},
		{
			name:   "env",
			args:   []string{"HOME=" + rig.aliceHome, rig.aliceBin, "watch", "status", "--json"},
			stderr: "boom",
			err:    errors.New("exit status 1"),
		},
	}

	status, err := rig.manager.Status(context.Background(), StatusOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Daemon.Installed || !status.Daemon.Running {
		t.Fatalf("unexpected daemon status: %+v", status.Daemon)
	}
	if status.Runtime != nil {
		t.Fatalf("expected Runtime=nil when invocation fails, got %+v", status.Runtime)
	}
	if !strings.Contains(status.RuntimeWarning, "runtime status invocation failed") {
		t.Fatalf("RuntimeWarning = %q", status.RuntimeWarning)
	}
	runner.AssertDone()
}

// TestDefaultBinPathIsMergedAutoBinary asserts the generated unit points at the
// merged `auto` binary (AC-5: default BinPath ends in /bin/auto).
func TestDefaultBinPathIsMergedAutoBinary(t *testing.T) {
	rig := newTestRig(t, &fakeRunner{t: t})
	rig.env["SUDO_USER"] = "alice"

	spec, err := rig.manager.resolveSpec(&InstallOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("resolveSpec() error = %v", err)
	}
	want := filepath.Join(rig.aliceHome, ".local", "bin", "auto")
	if spec.BinPath != want {
		t.Fatalf("BinPath = %q want %q", spec.BinPath, want)
	}
}

// TestGeneratedUnitExecStartUsesWatchStart asserts the rendered systemd unit
// invokes `<BinPath> watch start` (AC-5: generated unit ExecStart).
func TestGeneratedUnitExecStartUsesWatchStart(t *testing.T) {
	rig := newTestRig(t, &fakeRunner{t: t})
	rig.env["SUDO_USER"] = "alice"

	spec, err := rig.manager.resolveSpec(&InstallOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("resolveSpec() error = %v", err)
	}
	unit, err := renderUnit(&spec, ScopeSystem)
	if err != nil {
		t.Fatalf("renderUnit() error = %v", err)
	}
	wantExec := "ExecStart=" + spec.BinPath + " watch start"
	if !strings.Contains(unit, wantExec) {
		t.Fatalf("rendered unit missing %q\n%s", wantExec, unit)
	}
	if !strings.HasSuffix(spec.BinPath, filepath.Join("bin", "auto")) {
		t.Fatalf("BinPath = %q, expected to end in bin/auto", spec.BinPath)
	}
}

// TestParsedExecStartReconstructsWatchStart asserts the status ExecStart field
// reconstructed from a parsed unit is `<bin> watch start` (AC-5: reconstructed ExecStart).
func TestParsedExecStartReconstructsWatchStart(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		steps: []runnerStep{
			{name: "systemctl", args: []string{"is-enabled", "autowatch.service"}, stdout: "disabled"},
			{name: "systemctl", args: []string{"is-active", "autowatch.service"}, stdout: "inactive"},
		},
	}
	rig := newTestRig(t, runner)
	rig.env["SUDO_USER"] = "alice"

	spec, err := rig.manager.resolveSpec(&InstallOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("resolveSpec() error = %v", err)
	}
	unit, err := renderUnit(&spec, ScopeSystem)
	if err != nil {
		t.Fatalf("renderUnit() error = %v", err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte(unit), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", spec.UnitPath, err)
	}

	status, err := rig.manager.Status(context.Background(), StatusOptions{Scope: ScopeSystem})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	want := spec.BinPath + " watch start"
	if status.Daemon.ExecStart != want {
		t.Fatalf("ExecStart = %q want %q", status.Daemon.ExecStart, want)
	}
	runner.AssertDone()
}

// TestRuntimeStatusShellOutUsesWatchInfix asserts both the env and sudo runtime
// status invocations include the `watch` infix (AC-5: live-status path).
func TestRuntimeStatusShellOutUsesWatchInfix(t *testing.T) {
	cases := []struct {
		name     string
		asRoot   bool
		command  string
		wantArgs []string
	}{
		{
			name:    "env_variant",
			asRoot:  false,
			command: "env",
		},
		{
			name:    "sudo_variant",
			asRoot:  true,
			command: "sudo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{t: t}
			rig := newTestRig(t, runner)
			rig.env["SUDO_USER"] = "alice"
			if tc.asRoot {
				rig.manager.currentUser = func() (*user.User, error) { return &user.User{Username: "root", HomeDir: "/root", Gid: "0"}, nil }
				rig.manager.geteuid = func() int { return 0 }
			} else {
				rig.manager.currentUser = func() (*user.User, error) { return rig.users["alice"], nil }
				rig.manager.geteuid = func() int { return 1000 }
			}

			spec, err := rig.manager.resolveSpec(&InstallOptions{Scope: ScopeSystem})
			if err != nil {
				t.Fatalf("resolveSpec() error = %v", err)
			}
			unit, err := renderUnit(&spec, ScopeSystem)
			if err != nil {
				t.Fatalf("renderUnit() error = %v", err)
			}
			if err := os.WriteFile(spec.UnitPath, []byte(unit), 0o644); err != nil {
				t.Fatalf("WriteFile(%q) error = %v", spec.UnitPath, err)
			}

			var wantArgs []string
			if tc.asRoot {
				wantArgs = []string{"-u", "alice", "env", "HOME=" + rig.aliceHome, rig.aliceBin, "watch", "status", "--json"}
			} else {
				wantArgs = []string{"HOME=" + rig.aliceHome, rig.aliceBin, "watch", "status", "--json"}
			}

			runner.steps = []runnerStep{
				{name: "systemctl", args: []string{"is-enabled", "autowatch.service"}, stdout: "enabled"},
				{name: "systemctl", args: []string{"is-active", "autowatch.service"}, stdout: "active"},
				{
					name:   tc.command,
					args:   wantArgs,
					stdout: `{"daemon_running":true}`,
				},
			}

			if _, err := rig.manager.Status(context.Background(), StatusOptions{Scope: ScopeSystem}); err != nil {
				t.Fatalf("Status() error = %v", err)
			}
			runner.AssertDone()
		})
	}
}

// userScopeSetup makes the current (builder) user installable in user scope:
// it creates the binary at <builderHome>/.local/bin/auto and sets a user-bus
// environment so the user-bus preflight passes. It returns the builder home,
// the user unit dir, and the resolved bin path.
func userScopeSetup(t *testing.T, rig *testRig) (home, userUnitDir, binPath string) {
	t.Helper()
	home = rig.currentUser.HomeDir
	binPath = filepath.Join(home, ".local", "bin", "auto")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	userUnitDir = filepath.Join(home, ".config", "systemd", "user")
	rig.env["XDG_RUNTIME_DIR"] = "/run/user/1001"
	return home, userUnitDir, binPath
}

// TestUserScopeInstallWritesUserUnitAndEnablesLinger covers the default
// (no-flag) user install: bus preflight, unit under ~/.config/systemd/user,
// systemctl --user daemon-reload/enable/start, WantedBy=default.target,
// ExecStart=<home>/.local/bin/auto watch start, and a best-effort enable-linger.
func TestUserScopeInstallWritesUserUnitAndEnablesLinger(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		steps: []runnerStep{
			{name: "systemctl", args: []string{"--user", "is-system-running"}, stdout: "running"},
			{name: "systemctl", args: []string{"--user", "daemon-reload"}},
			{name: "systemctl", args: []string{"--user", "enable", "autowatch.service"}},
			{name: "systemctl", args: []string{"--user", "start", "autowatch.service"}},
			{name: "loginctl", args: []string{"enable-linger", "builder"}},
		},
	}
	rig := newTestRig(t, runner)
	_, userUnitDir, binPath := userScopeSetup(t, rig)

	result, err := rig.manager.Install(context.Background(), &InstallOptions{Enable: true, Start: true})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	wantUnitPath := filepath.Join(userUnitDir, "autowatch.service")
	if result.Spec.UnitPath != wantUnitPath {
		t.Fatalf("UnitPath = %q want %q", result.Spec.UnitPath, wantUnitPath)
	}
	if result.Spec.RuntimeUser != "builder" {
		t.Fatalf("RuntimeUser = %q want builder", result.Spec.RuntimeUser)
	}
	if result.Spec.BinPath != binPath {
		t.Fatalf("BinPath = %q want %q", result.Spec.BinPath, binPath)
	}
	if !strings.Contains(result.Unit, "WantedBy=default.target") {
		t.Fatalf("unit missing WantedBy=default.target:\n%s", result.Unit)
	}
	// A systemctl --user unit must NOT carry User=/Group= — the user manager
	// rejects them with status=216/GROUP ("Failed to determine supplementary
	// groups"), so the daemon would never start. (Regression guard.)
	if strings.Contains(result.Unit, "\nUser=") || strings.Contains(result.Unit, "\nGroup=") {
		t.Fatalf("user-scope unit must omit User=/Group=:\n%s", result.Unit)
	}
	wantExec := "ExecStart=" + binPath + " watch start"
	if !strings.Contains(result.Unit, wantExec) {
		t.Fatalf("unit missing %q:\n%s", wantExec, result.Unit)
	}
	// The unit dir must have been created and the file written there.
	if _, err := os.Stat(wantUnitPath); err != nil {
		t.Fatalf("expected unit file at %q: %v", wantUnitPath, err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", result.Warnings)
	}
	runner.AssertDone()
}

// TestUserScopeInstallLingerFailureIsNonFatal asserts a failed enable-linger
// surfaces a warning but does not fail the install.
func TestUserScopeInstallLingerFailureIsNonFatal(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		steps: []runnerStep{
			{name: "systemctl", args: []string{"--user", "is-system-running"}, stdout: "running"},
			{name: "systemctl", args: []string{"--user", "daemon-reload"}},
			{name: "systemctl", args: []string{"--user", "enable", "autowatch.service"}},
			{name: "systemctl", args: []string{"--user", "start", "autowatch.service"}},
			{name: "loginctl", args: []string{"enable-linger", "builder"}, stderr: "Could not enable linger: Access denied", err: errors.New("exit status 1")},
		},
	}
	rig := newTestRig(t, runner)
	userScopeSetup(t, rig)

	result, err := rig.manager.Install(context.Background(), &InstallOptions{Enable: true, Start: true})
	if err != nil {
		t.Fatalf("Install() error = %v (linger failure must be non-fatal)", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected a linger warning, got none")
	}
	if !strings.Contains(result.Warnings[0], "enable-linger") {
		t.Fatalf("warning = %q, expected enable-linger remediation", result.Warnings[0])
	}
	runner.AssertDone()
}

// TestUserScopeInstallMissingBusRemediation covers Step 1.5a: with no
// XDG_RUNTIME_DIR, the install returns a clear remediation and never touches
// systemctl/the filesystem.
func TestUserScopeInstallMissingBusRemediation(t *testing.T) {
	runner := &fakeRunner{t: t} // no steps: any systemctl call would fail the test
	rig := newTestRig(t, runner)
	userScopeSetup(t, rig)
	delete(rig.env, "XDG_RUNTIME_DIR")

	_, err := rig.manager.Install(context.Background(), &InstallOptions{Enable: true, Start: true})
	if err == nil {
		t.Fatalf("expected missing-bus error, got nil")
	}
	if !strings.Contains(err.Error(), "user D-Bus session") {
		t.Fatalf("error = %v, expected user D-Bus remediation", err)
	}
	if !strings.Contains(err.Error(), "--system") {
		t.Fatalf("error = %v, expected --system fallback hint", err)
	}
	runner.AssertDone()
}

// TestUserScopeInstallProbeFailureRemediation covers the probe variant of
// 1.5a: XDG_RUNTIME_DIR is set but the user manager is unreachable.
func TestUserScopeInstallProbeFailureRemediation(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		steps: []runnerStep{
			{name: "systemctl", args: []string{"--user", "is-system-running"}, stderr: "Failed to connect to bus: No such file or directory", err: errors.New("exit status 1")},
		},
	}
	rig := newTestRig(t, runner)
	userScopeSetup(t, rig)

	_, err := rig.manager.Install(context.Background(), &InstallOptions{Enable: true, Start: true})
	if err == nil || !strings.Contains(err.Error(), "user D-Bus session") {
		t.Fatalf("expected user D-Bus remediation, got %v", err)
	}
	runner.AssertDone()
}

// TestUserScopeStatusUsesSystemctlUser asserts status drives systemctl --user
// and locates the unit under ~/.config/systemd/user.
func TestUserScopeStatusUsesSystemctlUser(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		steps: []runnerStep{
			{name: "systemctl", args: []string{"--user", "is-enabled", "autowatch.service"}, stdout: "enabled"},
			{name: "systemctl", args: []string{"--user", "is-active", "autowatch.service"}, stdout: "inactive"},
		},
	}
	rig := newTestRig(t, runner)
	_, userUnitDir, _ := userScopeSetup(t, rig)

	// Write the unit where user scope expects it.
	spec, err := rig.manager.resolveSpec(&InstallOptions{})
	if err != nil {
		t.Fatalf("resolveSpec() error = %v", err)
	}
	unit, err := renderUnit(&spec, ScopeUser)
	if err != nil {
		t.Fatalf("renderUnit() error = %v", err)
	}
	if err := os.MkdirAll(userUnitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte(unit), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", spec.UnitPath, err)
	}

	status, err := rig.manager.Status(context.Background(), StatusOptions{})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Daemon.Installed || !status.Daemon.Enabled {
		t.Fatalf("unexpected daemon status: %+v", status.Daemon)
	}
	if filepath.Dir(status.Daemon.UnitPath) != userUnitDir {
		t.Fatalf("UnitPath = %q, expected dir %q", status.Daemon.UnitPath, userUnitDir)
	}
	runner.AssertDone()
}

// TestUserScopeRestartUsesSystemctlUser asserts restart drives systemctl --user.
func TestUserScopeRestartUsesSystemctlUser(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		steps: []runnerStep{
			{name: "systemctl", args: []string{"--user", "is-active", "autowatch.service"}, stdout: "active"},
			{name: "systemctl", args: []string{"--user", "try-restart", "autowatch.service"}},
		},
	}
	rig := newTestRig(t, runner)
	_, userUnitDir, _ := userScopeSetup(t, rig)

	spec, err := rig.manager.resolveSpec(&InstallOptions{})
	if err != nil {
		t.Fatalf("resolveSpec() error = %v", err)
	}
	unit, err := renderUnit(&spec, ScopeUser)
	if err != nil {
		t.Fatalf("renderUnit() error = %v", err)
	}
	if err := os.MkdirAll(userUnitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte(unit), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", spec.UnitPath, err)
	}

	result, err := rig.manager.Restart(context.Background(), RestartOptions{})
	if err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	if filepath.Dir(result.UnitPath) != userUnitDir {
		t.Fatalf("UnitPath = %q, expected dir %q", result.UnitPath, userUnitDir)
	}
	runner.AssertDone()
}

// TestSystemScopeInstallParity is the explicit --system parity test: unit under
// /etc/systemd/system (via the rig's overridable unitDir), bare systemctl (no
// --user), and WantedBy=multi-user.target.
func TestSystemScopeInstallParity(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		steps: []runnerStep{
			{name: "systemctl", args: []string{"daemon-reload"}},
			{name: "systemctl", args: []string{"enable", "autowatch.service"}},
			{name: "systemctl", args: []string{"start", "autowatch.service"}},
		},
	}
	rig := newTestRig(t, runner)
	rig.env["SUDO_USER"] = "alice"

	result, err := rig.manager.Install(context.Background(), &InstallOptions{Scope: ScopeSystem, Enable: true, Start: true})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if filepath.Dir(result.Spec.UnitPath) != rig.unitDir {
		t.Fatalf("UnitPath = %q, expected dir %q", result.Spec.UnitPath, rig.unitDir)
	}
	if !strings.Contains(result.Unit, "WantedBy=multi-user.target") {
		t.Fatalf("unit missing WantedBy=multi-user.target:\n%s", result.Unit)
	}
	// No --user, no loginctl: the fakeRunner asserts the exact bare sequence.
	runner.AssertDone()
}

// TestUserScopeRegenerateOverStaleUnit covers AC-5: an existing unit with a
// stale ExecStart (`…/autowatch start`) is regenerated to `…/auto watch start`
// while retaining the service identity (autowatch.service, description).
func TestUserScopeRegenerateOverStaleUnit(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		steps: []runnerStep{
			{name: "systemctl", args: []string{"--user", "is-system-running"}, stdout: "running"},
			{name: "systemctl", args: []string{"--user", "daemon-reload"}},
			{name: "systemctl", args: []string{"--user", "enable", "autowatch.service"}},
			{name: "systemctl", args: []string{"--user", "start", "autowatch.service"}},
			{name: "loginctl", args: []string{"enable-linger", "builder"}},
		},
	}
	rig := newTestRig(t, runner)
	_, userUnitDir, binPath := userScopeSetup(t, rig)

	// Seed a stale unit pointing at the old standalone binary name.
	staleExec := filepath.Join(filepath.Dir(binPath), "autowatch") + " start"
	staleUnit := "[Unit]\nDescription=autowatch daemon\n\n[Service]\nExecStart=" + staleExec + "\n\n[Install]\nWantedBy=default.target\n"
	if err := os.MkdirAll(userUnitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(userUnitDir, "autowatch.service")
	if err := os.WriteFile(unitPath, []byte(staleUnit), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := rig.manager.Install(context.Background(), &InstallOptions{Enable: true, Start: true})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !result.ExistingUnit {
		t.Fatalf("expected ExistingUnit=true")
	}
	if !result.Changed {
		t.Fatalf("expected Changed=true for a stale unit")
	}
	if result.Spec.ServiceName != "autowatch.service" {
		t.Fatalf("ServiceName = %q, identity must be retained", result.Spec.ServiceName)
	}
	if result.Spec.Description != "autowatch daemon" {
		t.Fatalf("Description = %q, identity must be retained", result.Spec.Description)
	}
	rewritten, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rewritten), "autowatch start") {
		t.Fatalf("rewritten unit still references the old binary:\n%s", rewritten)
	}
	if !strings.Contains(string(rewritten), binPath+" watch start") {
		t.Fatalf("rewritten unit missing %q:\n%s", binPath+" watch start", rewritten)
	}
	runner.AssertDone()
}
